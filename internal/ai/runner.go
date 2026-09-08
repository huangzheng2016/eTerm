package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type EventType int

const (
	EventTextDelta EventType = iota
	EventThinkingDelta
	EventToolCall
	EventToolResult
	EventDone
	EventError
	EventSteer
)

type Event struct {
	Type     EventType
	Text     string
	ToolName string
	ToolArgs string
	Err      error
}

type Agent struct {
	agent         *adk.ChatModelAgent
	chatModel     model.ChatModel
	mu            sync.Mutex
	histMu        sync.Mutex
	history       []*schema.Message
	historyBudget int64
	contextWindow int
	tasks         *TaskManager
	queue         *steerQueue
}

type Config struct {
	Provider       *Provider
	Model          string
	MaxContextSize int
	MaxIterations  int
	Executor       Executor
	Daemons        bool
	Cron           *CronScheduler
}

func NewAgent(ctx context.Context, cfg Config) (*Agent, error) {
	chatModel, err := NewChatModel(ctx, cfg.Provider, cfg.Model)
	if err != nil {
		return nil, err
	}
	tools, err := BuildTools(cfg.Executor, cfg.Cron, cfg.Daemons)
	if err != nil {
		return nil, err
	}
	sleepTool, err := buildSleepTool()
	if err != nil {
		return nil, err
	}
	baseTools := append(tools, sleepTool)
	localTools, err := BuildLocalTools()
	if err != nil {
		return nil, err
	}
	baseTools = append(baseTools, localTools...)
	instruction := agentInstruction(cfg.Daemons)
	queue := &steerQueue{}
	tm := NewTaskManager(func(ctx context.Context) (*adk.ChatModelAgent, error) {
		return buildADKAgent(ctx, chatModel, baseTools, instruction, cfg.MaxIterations, cfg.MaxContextSize, nil)
	})
	taskTools, err := tm.Tools()
	if err != nil {
		return nil, err
	}
	adkAgent, err := buildADKAgent(ctx, chatModel, append(baseTools, taskTools...), instruction, cfg.MaxIterations, cfg.MaxContextSize, queue)
	if err != nil {
		return nil, err
	}
	contextWindow := cfg.MaxContextSize
	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}
	return &Agent{
		agent:         adkAgent,
		chatModel:     chatModel,
		historyBudget: int64(float64(contextWindow) * historyBudgetRatio),
		contextWindow: contextWindow,
		tasks:         tm,
		queue:         queue,
	}, nil
}

func (a *Agent) Run(ctx context.Context, input string) <-chan Event {
	ch := make(chan Event, 64)
	go func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		defer close(ch)
		a.run(ctx, input, ch)
	}()
	return ch
}

func (a *Agent) Enqueue(text string) {
	if a.queue != nil {
		a.queue.enqueue(text)
	}
}

func (a *Agent) ClearQueue() {
	if a.queue != nil {
		a.queue.clear()
	}
}

func (a *Agent) DequeueLast() (string, bool) {
	if a.queue == nil {
		return "", false
	}
	a.queue.mu.Lock()
	defer a.queue.mu.Unlock()
	n := len(a.queue.msgs)
	if n == 0 {
		return "", false
	}
	text := a.queue.msgs[n-1]
	a.queue.msgs = a.queue.msgs[:n-1]
	return text, true
}

func (a *Agent) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.histMu.Lock()
	defer a.histMu.Unlock()
	a.history = nil
	a.ClearQueue()
}

func (a *Agent) ExportHistory(capBytes int) ([]byte, error) {
	a.histMu.Lock()
	defer a.histMu.Unlock()
	if len(a.history) == 0 {
		return nil, nil
	}
	msgs := a.history
	data, err := json.Marshal(msgs)
	if err != nil {
		return nil, err
	}
	for capBytes > 0 && len(data) > capBytes {
		i := 1
		for i < len(msgs) && msgs[i].Role != schema.User {
			i++
		}
		if i == len(msgs) {
			break
		}
		msgs = msgs[i:]
		if data, err = json.Marshal(msgs); err != nil {
			return nil, err
		}
	}
	return data, nil
}

func (a *Agent) ImportHistory(data []byte) error {
	var msgs []*schema.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return fmt.Errorf("import history: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.histMu.Lock()
	defer a.histMu.Unlock()
	a.history = msgs
	return nil
}

func (a *Agent) UndoLastTurn() {
	a.histMu.Lock()
	defer a.histMu.Unlock()
	for i := len(a.history) - 1; i >= 0; i-- {
		if a.history[i].Role == schema.User {
			a.history = a.history[:i]
			return
		}
	}
	a.history = nil
}

func UndoLastTurnJSON(data []byte) ([]byte, error) {
	var msgs []*schema.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, err
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == schema.User {
			return json.Marshal(msgs[:i])
		}
	}
	return json.Marshal([]*schema.Message{})
}

func (a *Agent) Usage() (usedTokens, maxTokens int) {
	a.histMu.Lock()
	defer a.histMu.Unlock()
	return int(countTokens(a.history, nil)), a.contextWindow
}

func (a *Agent) Close() {
	if a.tasks != nil {
		a.tasks.CancelAll()
	}
}

func (a *Agent) TaskSnapshots() []TaskSnapshot {
	if a.tasks == nil {
		return nil
	}
	return a.tasks.Snapshots()
}

func (a *Agent) CancelTask(id string) bool {
	if a.tasks == nil {
		return false
	}
	return a.tasks.CancelTask(id)
}

func (a *Agent) run(ctx context.Context, input string, ch chan<- Event) {
	send := func(ev Event) {
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
	}

	for {
		ok := a.runTurn(ctx, input, send)
		if a.queue != nil && ctx.Err() != nil {
			a.queue.clear()
		}
		if !ok || a.queue == nil {
			if ok {
				send(Event{Type: EventDone})
			}
			return
		}
		if ctx.Err() != nil {
			return
		}
		next, ok := a.queue.pop()
		if !ok {
			send(Event{Type: EventDone})
			return
		}
		send(Event{Type: EventSteer, Text: next})
		input = steerPrefix + next
	}
}

func (a *Agent) runTurn(ctx context.Context, input string, send func(Event)) bool {
	a.histMu.Lock()
	a.history = append(a.history, schema.UserMessage(input))
	msgs := slices.Clone(a.history)
	a.histMu.Unlock()
	defer func() {
		a.histMu.Lock()
		defer a.histMu.Unlock()
		a.history = trimHistory(a.history, a.historyBudget)
	}()

	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: a.agent, EnableStreaming: true})
	iterator := runner.Run(ctx, msgs)

	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			for {
				if _, ok := iterator.Next(); !ok {
					break
				}
			}
			send(Event{Type: EventError, Err: event.Err})
			return false
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		mo := event.Output.MessageOutput
		var msg *schema.Message
		if mo.IsStreaming {
			msg = consumeStream(mo, send)
		} else {
			msg = mo.Message
		}
		if msg == nil {
			continue
		}
		a.histMu.Lock()
		a.history = append(a.history, msg)
		a.histMu.Unlock()
		switch mo.Role {
		case schema.User:
			if strings.HasPrefix(msg.Content, steerPrefix) {
				send(Event{Type: EventSteer, Text: strings.TrimPrefix(msg.Content, steerPrefix)})
			}
		case schema.Assistant:
			for _, tc := range msg.ToolCalls {
				send(Event{Type: EventToolCall, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments})
			}
		case schema.Tool:
			send(Event{Type: EventToolResult, ToolName: mo.ToolName, Text: truncateRunes(msg.Content, 20000)})
		}
	}
	return true
}

func consumeStream(mo *adk.MessageVariant, send func(Event)) *schema.Message {
	defer mo.MessageStream.Close()
	var frames []*schema.Message
	for {
		frame, err := mo.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			send(Event{Type: EventError, Err: err})
			return nil
		}
		if frame == nil {
			continue
		}
		frames = append(frames, frame)
		if frame.Content != "" {
			send(Event{Type: EventTextDelta, Text: frame.Content})
		}
		if frame.ReasoningContent != "" {
			send(Event{Type: EventThinkingDelta, Text: frame.ReasoningContent})
		}
	}
	if len(frames) == 0 {
		return nil
	}
	msg, err := schema.ConcatMessages(frames)
	if err != nil {
		return nil
	}
	return msg
}

func trimHistory(msgs []*schema.Message, budget int64) []*schema.Message {
	if budget <= 0 {
		return msgs
	}
	for countTokens(msgs, nil) > budget {
		i := 1
		for i < len(msgs) && msgs[i].Role != schema.User {
			i++
		}
		if i == len(msgs) {
			break
		}
		msgs = msgs[i:]
	}
	return msgs
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
