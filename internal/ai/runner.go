package ai

import (
	"context"
	"errors"
	"io"
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
)

type Event struct {
	Type     EventType
	Text     string // delta text for EventTextDelta/EventThinkingDelta, tool output (capped, display-only) for EventToolResult
	ToolName string
	ToolArgs string
	Err      error
}

type Agent struct {
	agent   *adk.ChatModelAgent
	mu      sync.Mutex
	history []*schema.Message
	// historyBudget bounds the estimated tokens kept in history across
	// turns; oldest turns are evicted. Set below the middleware clear
	// threshold so compaction is not re-paid every turn.
	historyBudget int64
}

type Config struct {
	Provider       *Provider
	Model          string
	MaxContextSize int
	MaxIterations  int
	Executor       Executor
}

func NewAgent(ctx context.Context, cfg Config) (*Agent, error) {
	chatModel, err := NewChatModel(ctx, cfg.Provider, cfg.Model)
	if err != nil {
		return nil, err
	}
	tools, err := BuildTools(cfg.Executor)
	if err != nil {
		return nil, err
	}
	adkAgent, err := buildADKAgent(ctx, chatModel, tools, systemPrompt, cfg.MaxIterations, cfg.MaxContextSize)
	if err != nil {
		return nil, err
	}
	contextWindow := cfg.MaxContextSize
	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}
	return &Agent{agent: adkAgent, historyBudget: int64(float64(contextWindow) * historyBudgetRatio)}, nil
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

// Clear resets the conversation history.
func (a *Agent) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = nil
}

func (a *Agent) run(ctx context.Context, input string, ch chan<- Event) {
	a.history = append(a.history, schema.UserMessage(input))
	defer func() {
		a.history = trimHistory(a.history, a.historyBudget)
	}()

	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: a.agent, EnableStreaming: true})
	iterator := runner.Run(ctx, a.history)

	send := func(ev Event) {
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
	}

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
			return
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		mo := event.Output.MessageOutput
		var msg *schema.Message
		if mo.IsStreaming {
			msg = a.consumeStream(mo, send)
		} else {
			msg = mo.Message
		}
		if msg == nil {
			continue
		}
		a.history = append(a.history, msg)
		switch mo.Role {
		case schema.Assistant:
			for _, tc := range msg.ToolCalls {
				send(Event{Type: EventToolCall, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments})
			}
		case schema.Tool:
			// Display-only: the LLM already saw the full output; cap the panel copy.
			send(Event{Type: EventToolResult, ToolName: mo.ToolName, Text: truncateRunes(msg.Content, 20000)})
		}
	}
	send(Event{Type: EventDone})
}

// consumeStream reads one streaming message, forwarding text and thinking
// deltas, and returns the concatenated full message.
func (a *Agent) consumeStream(mo *adk.MessageVariant, send func(Event)) *schema.Message {
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
