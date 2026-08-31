package ai

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// steerToolModel blocks its first call until release, emits a tool call, then
// captures the second call's input and answers with text.
type steerToolModel struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
	calls   int
	second  []*schema.Message
}

func (m *steerToolModel) BindTools(tools []*schema.ToolInfo) error { return nil }

func (m *steerToolModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return nil, nil
}

func (m *steerToolModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.calls++
	if m.calls == 1 {
		m.once.Do(func() { close(m.entered) })
		select {
		case <-m.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return schema.StreamReaderFromArray([]*schema.Message{
			{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "c1", Function: schema.FunctionCall{Name: "list_tabs", Arguments: `{}`}}}},
		}), nil
	}
	m.second = input
	return schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: "done"},
	}), nil
}

func newSteerAgent(t *testing.T, m model.ChatModel, queue *steerQueue) *Agent {
	t.Helper()
	tools, err := BuildTools(fakeExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	adkAgent, err := buildADKAgent(context.Background(), m, tools, "test instruction", 4, 100000, queue)
	if err != nil {
		t.Fatal(err)
	}
	return &Agent{agent: adkAgent, queue: queue}
}

type runResult struct {
	steers  []string
	sawDone bool
	sawErr  bool
}

func drainRun(a *Agent, ctx context.Context, input string) <-chan runResult {
	out := make(chan runResult, 1)
	go func() {
		var r runResult
		for ev := range a.Run(ctx, input) {
			switch ev.Type {
			case EventSteer:
				r.steers = append(r.steers, ev.Text)
			case EventDone:
				r.sawDone = true
			case EventError:
				r.sawErr = true
			}
		}
		out <- r
	}()
	return out
}

func TestSteerInjectedAtStepBoundary(t *testing.T) {
	ctx := context.Background()
	m := &steerToolModel{release: make(chan struct{}), entered: make(chan struct{})}
	queue := &steerQueue{}
	a := newSteerAgent(t, m, queue)

	done := drainRun(a, ctx, "list tabs")
	<-m.entered // first model call in flight, turn is running
	a.Enqueue("focus on the second tab")
	a.Enqueue("then close it")
	close(m.release)
	r := <-done

	if r.sawErr || !r.sawDone {
		t.Fatalf("done=%v err=%v", r.sawDone, r.sawErr)
	}
	if len(r.steers) != 2 || r.steers[0] != "focus on the second tab" || r.steers[1] != "then close it" {
		t.Fatalf("steer events out of order: %v", r.steers)
	}
	if m.second == nil {
		t.Fatal("no second model call")
	}
	last := m.second[len(m.second)-2:]
	if last[0].Role != schema.User || last[0].Content != steerPrefix+"focus on the second tab" {
		t.Fatalf("second call input: %+v", last[0])
	}
	if last[1].Role != schema.User || last[1].Content != steerPrefix+"then close it" {
		t.Fatalf("second call input: %+v", last[1])
	}
	var steerHist []string
	for _, msg := range a.history {
		if msg.Role == schema.User && strings.HasPrefix(msg.Content, steerPrefix) {
			steerHist = append(steerHist, msg.Content)
		}
	}
	if len(steerHist) != 2 {
		t.Fatalf("history steer messages: %v", steerHist)
	}
}

// chainedModel blocks only its first call until release, then always answers
// text (no tool calls, one call per turn).
type chainedModel struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
	mu      sync.Mutex
	inputs  [][]*schema.Message
}

func (m *chainedModel) BindTools(tools []*schema.ToolInfo) error { return nil }

func (m *chainedModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return nil, nil
}

func (m *chainedModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.mu.Lock()
	m.inputs = append(m.inputs, input)
	call := len(m.inputs)
	m.mu.Unlock()
	if call == 1 {
		m.once.Do(func() { close(m.entered) })
		select {
		case <-m.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: "answer"},
	}), nil
}

// A message queued after the last model call of a turn is run as a chained
// turn instead of waiting for the user to resend it.
func TestSteerQueuedAtTurnEndChainsNewTurn(t *testing.T) {
	ctx := context.Background()
	m := &chainedModel{release: make(chan struct{}), entered: make(chan struct{})}
	queue := &steerQueue{}
	a := newSteerAgent(t, m, queue)

	done := drainRun(a, ctx, "first question")
	<-m.entered
	a.Enqueue("follow up")
	close(m.release)
	r := <-done

	if r.sawErr || !r.sawDone {
		t.Fatalf("done=%v err=%v", r.sawDone, r.sawErr)
	}
	if len(r.steers) != 1 || r.steers[0] != "follow up" {
		t.Fatalf("steer events: %v", r.steers)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.inputs) != 2 {
		t.Fatalf("model calls: %d, want 2", len(m.inputs))
	}
	last := m.inputs[1][len(m.inputs[1])-1]
	if last.Role != schema.User || last.Content != steerPrefix+"follow up" {
		t.Fatalf("chained turn input tail: %+v", last)
	}
	var roles []schema.RoleType
	for _, msg := range a.history {
		roles = append(roles, msg.Role)
	}
	want := []schema.RoleType{schema.User, schema.Assistant, schema.User, schema.Assistant}
	if len(roles) != len(want) {
		t.Fatalf("history roles: %v", roles)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("history roles: %v, want %v", roles, want)
		}
	}
}

func TestSteerCancelClearsQueue(t *testing.T) {
	m := &gatedModel{release: make(chan struct{}), entered: make(chan struct{})}
	defer close(m.release)
	queue := &steerQueue{}
	a := newSteerAgent(t, m, queue)

	ctx, cancel := context.WithCancel(context.Background())
	done := drainRun(a, ctx, "question")
	<-m.entered
	a.Enqueue("too late")
	cancel()
	<-done

	if _, ok := queue.pop(); ok {
		t.Fatal("queue not cleared after cancel")
	}
}
