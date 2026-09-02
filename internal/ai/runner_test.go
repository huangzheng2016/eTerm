package ai

import (
	"context"
	"encoding/json"
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
	tools, err := BuildTools(fakeExecutor{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	adkAgent, err := buildADKAgent(ctx, &fakeModel{}, tools, "test instruction", 4, 100000, nil)
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

func TestDequeueLast(t *testing.T) {
	a := &Agent{queue: &steerQueue{}}

	if text, ok := a.DequeueLast(); ok || text != "" {
		t.Fatalf("empty queue: got (%q, %v), want (\"\", false)", text, ok)
	}

	a.Enqueue("first")
	a.Enqueue("second")
	a.Enqueue("third")
	for _, want := range []string{"third", "second", "first"} {
		text, ok := a.DequeueLast()
		if !ok || text != want {
			t.Fatalf("got (%q, %v), want (%q, true)", text, ok, want)
		}
	}
	if _, ok := a.DequeueLast(); ok {
		t.Fatal("queue must be empty after popping all")
	}

	// Agents without a steer queue (sub-agents) must not panic.
	nilAgent := &Agent{}
	if _, ok := nilAgent.DequeueLast(); ok {
		t.Fatal("nil queue must report ok=false")
	}
}

// A recalled message must not be injected by the steer middleware's
// step-boundary drain.
func TestDequeueLastPreventsInjection(t *testing.T) {
	ctx := context.Background()
	m := &steerToolModel{release: make(chan struct{}), entered: make(chan struct{})}
	queue := &steerQueue{}
	a := newSteerAgent(t, m, queue)

	done := drainRun(a, ctx, "list tabs")
	<-m.entered // first model call in flight, turn is running
	a.Enqueue("keep")
	a.Enqueue("recall me")
	text, ok := a.DequeueLast()
	if !ok || text != "recall me" {
		t.Fatalf("DequeueLast: got (%q, %v), want (\"recall me\", true)", text, ok)
	}
	close(m.release)
	r := <-done

	if r.sawErr || !r.sawDone {
		t.Fatalf("done=%v err=%v", r.sawDone, r.sawErr)
	}
	if len(r.steers) != 1 || r.steers[0] != "keep" {
		t.Fatalf("steer events: %v", r.steers)
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

	done := make(chan struct{})
	go func() {
		consumeStream(mo, send)
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
	tools, err := BuildTools(fakeExecutor{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	adkAgent, err := buildADKAgent(ctx, &echoModel{}, tools, "test instruction", 4, 100000, nil)
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

func TestExportImportHistoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	tools, err := BuildTools(fakeExecutor{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	adkAgent, err := buildADKAgent(ctx, &echoModel{}, tools, "test instruction", 4, 100000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{agent: adkAgent}

	if data, err := a.ExportHistory(0); err != nil || data != nil {
		t.Fatalf("empty export: got %q, %v", data, err)
	}
	for range a.Run(ctx, "question one") {
	}
	data, err := a.ExportHistory(0)
	if err != nil || len(data) == 0 {
		t.Fatalf("export: %v, %d bytes", err, len(data))
	}

	b := &Agent{agent: adkAgent}
	if err := b.ImportHistory(data); err != nil {
		t.Fatal(err)
	}
	if len(b.history) != len(a.history) {
		t.Fatalf("imported %d messages, want %d", len(b.history), len(a.history))
	}
	for i := range a.history {
		if a.history[i].Role != b.history[i].Role || a.history[i].Content != b.history[i].Content {
			t.Fatalf("message %d mismatch: %+v vs %+v", i, a.history[i], b.history[i])
		}
	}
	if err := b.ImportHistory([]byte("{broken")); err == nil {
		t.Fatal("broken JSON must fail")
	}
}

func TestExportHistoryCapDropsOldestTurns(t *testing.T) {
	ctx := context.Background()
	tools, err := BuildTools(fakeExecutor{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	adkAgent, err := buildADKAgent(ctx, &echoModel{}, tools, "test instruction", 4, 100000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{agent: adkAgent}
	for i := 0; i < 5; i++ {
		for range a.Run(ctx, "question") {
		}
	}
	full, err := a.ExportHistory(0)
	if err != nil {
		t.Fatal(err)
	}
	capped, err := a.ExportHistory(len(full) / 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) >= len(full) {
		t.Fatalf("cap did not shrink export: %d vs %d", len(capped), len(full))
	}
	if len(capped) > len(full)/2 {
		t.Fatalf("cap exceeded: %d > %d", len(capped), len(full)/2)
	}
	var msgs []*schema.Message
	if err := json.Unmarshal(capped, &msgs); err != nil {
		t.Fatal(err)
	}
	if msgs[0].Role != schema.User {
		t.Fatalf("capped history must start on a turn boundary, got %s", msgs[0].Role)
	}
}

func TestUndoLastTurn(t *testing.T) {
	ctx := context.Background()
	tools, err := BuildTools(fakeExecutor{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	adkAgent, err := buildADKAgent(ctx, &echoModel{}, tools, "test instruction", 4, 100000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{agent: adkAgent}
	for range a.Run(ctx, "first") {
	}
	for range a.Run(ctx, "second") {
	}
	before := len(a.history)

	a.UndoLastTurn()
	if len(a.history) >= before {
		t.Fatalf("undo did not shrink history: %d -> %d", before, len(a.history))
	}
	for _, m := range a.history {
		if m.Content == "second" {
			t.Fatal("undone turn still in history")
		}
	}
	if a.history[len(a.history)-1].Role != schema.Assistant {
		t.Fatalf("remaining history must end on the first turn's answer, got %s", a.history[len(a.history)-1].Role)
	}

	a.UndoLastTurn()
	if len(a.history) != 0 {
		t.Fatalf("undo of last turn must empty history, got %d", len(a.history))
	}
	a.UndoLastTurn() // no-op on empty history
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

func TestUsageReflectsHistory(t *testing.T) {
	ctx := context.Background()
	tools, err := BuildTools(fakeExecutor{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	adkAgent, err := buildADKAgent(ctx, &echoModel{}, tools, "test instruction", 4, 100000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{agent: adkAgent, historyBudget: 100000, contextWindow: 100000}

	used, max := a.Usage()
	if used != 0 || max != 100000 {
		t.Fatalf("empty usage: got (%d, %d), want (0, 100000)", used, max)
	}
	for range a.Run(ctx, "question") {
	}
	used1, _ := a.Usage()
	if used1 <= used {
		t.Fatalf("usage must grow after a turn: %d -> %d", used, used1)
	}
	for range a.Run(ctx, "question") {
	}
	used2, _ := a.Usage()
	if used2 <= used1 {
		t.Fatalf("usage must keep growing: %d -> %d", used1, used2)
	}
	a.Clear()
	if used3, _ := a.Usage(); used3 != 0 {
		t.Fatalf("usage after Clear: got %d, want 0", used3)
	}
}

// Usage must not take the run mutex: a run blocked in the model still lets
// Usage report the history written so far.
func TestUsageDuringRunDoesNotBlock(t *testing.T) {
	ctx := context.Background()
	m := &gatedModel{release: make(chan struct{}), entered: make(chan struct{})}
	tools, err := BuildTools(fakeExecutor{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	adkAgent, err := buildADKAgent(ctx, m, tools, "test instruction", 4, 100000, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{agent: adkAgent, historyBudget: 100000, contextWindow: 100000}

	runDone := make(chan struct{})
	go func() {
		for range a.Run(ctx, "question") {
		}
		close(runDone)
	}()
	<-m.entered // run in flight, model blocked

	usageDone := make(chan struct{})
	go func() {
		a.Usage()
		close(usageDone)
	}()
	select {
	case <-usageDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Usage blocked while a run was in flight")
	}
	close(m.release)
	<-runDone
}
