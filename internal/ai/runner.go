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
	// EventSteer reports a queued user message entering the turn (injected at
	// a step boundary, or run as a chained turn when the queue outlived it).
	EventSteer
)

type Event struct {
	Type     EventType
	Text     string // delta text for EventTextDelta/EventThinkingDelta, tool output (capped, display-only) for EventToolResult
	ToolName string
	ToolArgs string
	Err      error
}

type Agent struct {
	agent *adk.ChatModelAgent
	mu    sync.Mutex // serializes runs
	// histMu guards history alone, so Usage can read it mid-run without
	// waiting for the whole turn to finish.
	histMu  sync.Mutex
	history []*schema.Message
	// historyBudget bounds the estimated tokens kept in history across
	// turns; oldest turns are evicted. Set below the middleware clear
	// threshold so compaction is not re-paid every turn.
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
	// Daemons binds the remote-daemon tools and their prompt section; false
	// when no daemon is registered (they could not do anything anyway).
	Daemons bool
	// Cron schedules wake-ups for this session; owned by the app bridge so
	// jobs survive agent rebuilds. Nil disables the cron tools.
	Cron *CronScheduler
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
	// Sub-agents get the base tools only: no spawn_agent, so no recursion.
	// Steer is main-agent only: queued input targets the user's turn.
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
		historyBudget: int64(float64(contextWindow) * historyBudgetRatio),
		contextWindow: contextWindow,
		tasks:         tm,
		queue:         queue,
	}, nil
}

// Run starts one agent turn with the given user input and returns a channel
// of streaming events. The channel is closed after EventDone or EventError.
// Concurrent runs are serialized.
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

// Enqueue queues a user message submitted while a run is in flight. It is
// injected into the turn at the next model call, or run as a chained turn
// when the current one ends first.
func (a *Agent) Enqueue(text string) {
	if a.queue != nil {
		a.queue.enqueue(text)
	}
}

// ClearQueue drops all queued messages without injecting them.
func (a *Agent) ClearQueue() {
	if a.queue != nil {
		a.queue.clear()
	}
}

// Clear resets the conversation history.
func (a *Agent) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.histMu.Lock()
	defer a.histMu.Unlock()
	a.history = nil
	a.ClearQueue()
}

// ExportHistory serializes the conversation history as JSON for session
// persistence. Empty history exports as nil. When capBytes > 0, oldest whole
// turns are dropped until the JSON fits (the newest turn is always kept).
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

// ImportHistory replaces the conversation history with a previously exported
// one. It blocks on the run mutex, so callers must not invoke it mid-run.
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

// UndoLastTurn truncates the history to just before the last user message,
// rewinding one turn.
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

// UndoLastTurnJSON rewinds one turn in an exported history, for callers that
// hold the JSON form (a resumed session not yet loaded into an Agent).
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

// Usage returns the estimated token count of the current history and the
// configured context window. Safe to call while a run is in flight.
func (a *Agent) Usage() (usedTokens, maxTokens int) {
	a.histMu.Lock()
	defer a.histMu.Unlock()
	return int(countTokens(a.history, nil)), a.contextWindow
}

// Close cancels all running background tasks. The app layer must call it when
// replacing the Agent (aiBridge.agentFor on provider/model switch), otherwise
// orphaned sub-agents keep running on the old provider's credentials.
func (a *Agent) Close() {
	if a.tasks != nil {
		a.tasks.CancelAll()
	}
}

// TaskSnapshots returns every background task with its activity tail, for the
// panel's tasks browser.
func (a *Agent) TaskSnapshots() []TaskSnapshot {
	if a.tasks == nil {
		return nil
	}
	return a.tasks.Snapshots()
}

// CancelTask cancels one running background task.
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
			// Cancelled run: drop everything still queued.
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
		// The turn ended before this queued message could be injected; run it
		// as a chained turn so the user does not have to resend.
		send(Event{Type: EventSteer, Text: next})
		input = steerPrefix + next
	}
}

// runTurn executes one agent turn, streaming events via send. It returns
// false when the turn failed (EventError already sent).
func (a *Agent) runTurn(ctx context.Context, input string, send func(Event)) bool {
	a.histMu.Lock()
	a.history = append(a.history, schema.UserMessage(input))
	// The runner keeps reading the slice while history grows below.
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
			// Steer injection surfaced by the steer middleware via
			// adk.SendEvent; the append above already recorded it in exact
			// stream order.
			if strings.HasPrefix(msg.Content, steerPrefix) {
				send(Event{Type: EventSteer, Text: strings.TrimPrefix(msg.Content, steerPrefix)})
			}
		case schema.Assistant:
			for _, tc := range msg.ToolCalls {
				send(Event{Type: EventToolCall, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments})
			}
		case schema.Tool:
			// Display-only: the LLM already saw the full output; cap the panel copy.
			send(Event{Type: EventToolResult, ToolName: mo.ToolName, Text: truncateRunes(msg.Content, 20000)})
		}
	}
	return true
}

// consumeStream reads one streaming message, forwarding text and thinking
// deltas via send, and returns the concatenated full message.
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

// trimHistory evicts oldest whole turns while the estimated token count
// exceeds budget. A turn is a user message plus everything up to the next
// user message, so eviction never splits a tool-call/result pair. The
// newest turn is always kept, even if it alone exceeds the budget.
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
