package ai

import (
	"context"
	"testing"

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
