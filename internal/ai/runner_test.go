package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// fakeModel streams a tool call on the first call and a text answer on the
// second, mimicking one ReAct iteration.
type fakeModel struct {
	calls int
}

func (f *fakeModel) BindTools(tools []*schema.ToolInfo) error { return nil }

func (f *fakeModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return nil, nil
}

func (f *fakeModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	f.calls++
	if f.calls == 1 {
		return schema.StreamReaderFromArray([]*schema.Message{
			{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "c1", Function: schema.FunctionCall{Name: "list_tabs", Arguments: `{}`}}}},
		}), nil
	}
	return schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: "I see "},
		{Role: schema.Assistant, Content: "one tab."},
	}), nil
}

type fakeExecutor struct {
	Executor // nil embedded; only ListTabs is exercised
}

func (fakeExecutor) ListTabs(ctx context.Context) ([]TabInfo, error) {
	return []TabInfo{{ID: "t1", Title: "shell", Type: "local", Active: true}}, nil
}

func TestAgentRunEmitsEventsAndHistory(t *testing.T) {
	ctx := context.Background()
	tools, err := BuildTools(fakeExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	adkAgent, err := buildADKAgent(ctx, &fakeModel{}, tools, "test instruction", 4, 100000)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{agent: adkAgent}

	var texts, toolCalls, toolResults []string
	var sawDone, sawErr bool
	for ev := range a.Run(ctx, "what tabs are open?") {
		switch ev.Type {
		case EventTextDelta:
			texts = append(texts, ev.Text)
		case EventToolCall:
			toolCalls = append(toolCalls, ev.ToolName)
		case EventToolResult:
			toolResults = append(toolResults, ev.ToolName+":"+ev.Text)
		case EventDone:
			sawDone = true
		case EventError:
			sawErr = true
			t.Errorf("unexpected error event: %v", ev.Err)
		}
	}
	if sawErr || !sawDone {
		t.Fatalf("done=%v err=%v", sawDone, sawErr)
	}
	if len(toolCalls) != 1 || toolCalls[0] != "list_tabs" {
		t.Fatalf("tool calls: %v", toolCalls)
	}
	if len(toolResults) != 1 || toolResults[0] == "" {
		t.Fatalf("tool results: %v", toolResults)
	}
	if got := joinStrings(texts); got != "I see one tab." {
		t.Fatalf("text deltas: %q", got)
	}
	// user + assistant(tool call) + tool result + assistant(final)
	if len(a.history) != 4 {
		t.Fatalf("history: got %d messages, want 4", len(a.history))
	}

	a.Clear()
	if len(a.history) != 0 {
		t.Fatalf("Clear must reset history, got %d", len(a.history))
	}
}

func joinStrings(parts []string) string {
	s := ""
	for _, p := range parts {
		s += p
	}
	return s
}

// Regression: consumeStream must route sends through the ctx-aware path.
// With a full event buffer (consumer stopped draining) and a canceled ctx,
// a blocking send would wedge the run goroutine holding a.mu forever
// (Esc during streaming in the TUI).
func TestConsumeStreamSendHonorsCancel(t *testing.T) {
	sr, sw := schema.Pipe[*schema.Message](0)
	go func() {
		defer sw.Close()
		for i := 0; i < 300; i++ {
			if sw.Send(&schema.Message{Role: schema.Assistant, Content: "delta text chunk "}, nil) {
				return
			}
		}
	}()
	mo := &adk.MessageVariant{IsStreaming: true, MessageStream: sr, Role: schema.Assistant}

	ch := make(chan Event, 64) // deliberately never drained
	ctx, cancel := context.WithCancel(context.Background())
	send := func(ev Event) {
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
	}

	a := &Agent{}
	done := make(chan struct{})
	go func() {
		a.consumeStream(mo, send)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond) // by now the buffer is full and send blocks
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("consumeStream blocked on send after cancel with a full event buffer")
	}
}

// echoModel answers every turn with the same 1000-char text.
type echoModel struct{}

func (m *echoModel) BindTools(tools []*schema.ToolInfo) error { return nil }

func (m *echoModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return nil, nil
}

func (m *echoModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: strings.Repeat("a", 1000)},
	}), nil
}

// Regression: history must stay bounded across many turns, evicting oldest
// whole turns (never splitting a tool-call/result pair).
func TestHistoryStaysBoundedAcrossTurns(t *testing.T) {
	ctx := context.Background()
	tools, err := BuildTools(fakeExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	adkAgent, err := buildADKAgent(ctx, &echoModel{}, tools, "test instruction", 4, 100000)
	if err != nil {
		t.Fatal(err)
	}
	// One turn costs ~252 estimated tokens (1000 ASCII chars + user text).
	a := &Agent{agent: adkAgent, historyBudget: 1000}

	for i := 0; i < 10; i++ {
		for range a.Run(ctx, "question") {
		}
	}

	if got := countTokens(a.history, nil); got > 1300 {
		t.Fatalf("history grew past budget+one turn: %d tokens, %d messages", got, len(a.history))
	}
	if len(a.history) < 2 {
		t.Fatalf("newest turn must be kept, got %d messages", len(a.history))
	}
	if a.history[0].Role != schema.User {
		t.Fatalf("history must start on a turn boundary, got role %s", a.history[0].Role)
	}
	last := a.history[len(a.history)-1]
	if last.Role != schema.Assistant || last.Content != strings.Repeat("a", 1000) {
		t.Fatalf("newest answer must be kept, got %+v", last)
	}
}

func TestTrimHistoryKeepsNewestTurnOverBudget(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage(strings.Repeat("x", 4000)),
		{Role: schema.Assistant, Content: strings.Repeat("y", 4000)},
	}
	got := trimHistory(msgs, 10)
	if len(got) != 2 {
		t.Fatalf("single turn over budget must be kept whole, got %d messages", len(got))
	}
}
