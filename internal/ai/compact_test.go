package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// summaryModel records the last Generate request and replies with a fixed
// summary, or fails with a fixed error.
type summaryModel struct {
	summary string
	err     error
	calls   int
	input   []*schema.Message
}

func (m *summaryModel) BindTools(tools []*schema.ToolInfo) error { return nil }

func (m *summaryModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.calls++
	m.input = input
	if m.err != nil {
		return nil, m.err
	}
	return &schema.Message{Role: schema.Assistant, Content: m.summary}, nil
}

func (m *summaryModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not implemented")
}

func compactTestAgent(m *summaryModel, turns int) *Agent {
	a := &Agent{chatModel: m}
	for i := 1; i <= turns; i++ {
		a.history = append(a.history,
			schema.UserMessage(fmt.Sprintf("question %d", i)),
			&schema.Message{Role: schema.Assistant, Content: fmt.Sprintf("answer %d %s", i, strings.Repeat("a", 400))},
		)
	}
	return a
}

func TestCompactSummarizesAndKeepsRecentTurns(t *testing.T) {
	m := &summaryModel{summary: "SUMMARY: goal X, decided Y, path /tmp/z, next W"}
	a := compactTestAgent(m, 6)

	stats, err := a.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if m.calls != 1 {
		t.Fatalf("model calls: got %d, want 1", m.calls)
	}
	if len(m.input) != 2 || m.input[0].Role != schema.System || m.input[1].Role != schema.User {
		t.Fatalf("summary request shape: %+v", m.input)
	}
	if !strings.Contains(m.input[0].Content, "decisions") || !strings.Contains(m.input[0].Content, "paths") {
		t.Fatalf("system prompt must demand decisions and paths, got %q", m.input[0].Content)
	}
	// The transcript covers the dropped turns only, not the kept tail.
	if !strings.Contains(m.input[1].Content, "question 1") || !strings.Contains(m.input[1].Content, "answer 2") {
		t.Fatalf("transcript must cover the early turns, got %q", m.input[1].Content)
	}
	if strings.Contains(m.input[1].Content, "question 3") {
		t.Fatalf("transcript must exclude the kept tail, got %q", m.input[1].Content)
	}

	if stats.MessagesBefore != 12 || stats.MessagesAfter != 9 {
		t.Fatalf("stats messages: %d -> %d, want 12 -> 9", stats.MessagesBefore, stats.MessagesAfter)
	}
	if stats.TokensBefore <= 0 || stats.TokensAfter <= 0 || stats.TokensAfter >= stats.TokensBefore {
		t.Fatalf("stats tokens must shrink: %d -> %d", stats.TokensBefore, stats.TokensAfter)
	}

	if len(a.history) != 9 {
		t.Fatalf("history: got %d messages, want 9", len(a.history))
	}
	head := a.history[0]
	if head.Role != schema.User || !strings.Contains(head.Content, m.summary) {
		t.Fatalf("first message must be the user-role summary, got %+v", head)
	}
	if a.history[1].Role != schema.User || a.history[1].Content != "question 3" {
		t.Fatalf("kept tail must start at turn 3 verbatim, got %+v", a.history[1])
	}
	last := a.history[len(a.history)-1]
	if last.Role != schema.Assistant || !strings.HasPrefix(last.Content, "answer 6 ") {
		t.Fatalf("kept tail must end at turn 6 verbatim, got %+v", last)
	}
}

func TestCompactFailureKeepsHistory(t *testing.T) {
	wantErr := errors.New("model unreachable")
	m := &summaryModel{err: wantErr}
	a := compactTestAgent(m, 6)

	if _, err := a.Compact(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("error must propagate as-is, got %v", err)
	}
	if len(a.history) != 12 || a.history[0].Content != "question 1" {
		t.Fatalf("history must be untouched on failure, got %d messages", len(a.history))
	}
}

func TestCompactEmptySummaryKeepsHistory(t *testing.T) {
	m := &summaryModel{summary: "   "}
	a := compactTestAgent(m, 6)

	if _, err := a.Compact(context.Background()); err == nil {
		t.Fatal("empty summary must fail")
	}
	if len(a.history) != 12 {
		t.Fatalf("history must be untouched on empty summary, got %d messages", len(a.history))
	}
}

func TestCompactFewTurnsIsNoop(t *testing.T) {
	m := &summaryModel{summary: "unused"}
	a := compactTestAgent(m, 3)

	stats, err := a.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 0 {
		t.Fatalf("model must not be called when nothing to compact, got %d calls", m.calls)
	}
	if stats.MessagesBefore != 6 || stats.MessagesAfter != 6 || stats.TokensBefore != stats.TokensAfter {
		t.Fatalf("noop stats: %+v", stats)
	}
	if len(a.history) != 6 {
		t.Fatalf("history must be unchanged, got %d messages", len(a.history))
	}
}
