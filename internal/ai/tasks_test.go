package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// gatedModel blocks its stream until release is closed, then answers.
type gatedModel struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (m *gatedModel) BindTools(tools []*schema.ToolInfo) error { return nil }

func (m *gatedModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return nil, nil
}

func (m *gatedModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.entered != nil {
		m.once.Do(func() { close(m.entered) })
	}
	select {
	case <-m.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: "gated answer"},
	}), nil
}

func testFactory(m model.ChatModel) agentFactory {
	return func(ctx context.Context) (*adk.ChatModelAgent, error) {
		tools, err := BuildTools(fakeExecutor{})
		if err != nil {
			return nil, err
		}
		return buildADKAgent(ctx, m, tools, "test instruction", 4, 100000)
	}
}

func TestTaskManagerSpawnWaitList(t *testing.T) {
	m := &gatedModel{release: make(chan struct{})}
	tm := NewTaskManager(testFactory(m))

	spawnOut, err := tm.spawn(context.Background(), &SpawnAgentInput{Task: "watch the build"})
	if err != nil {
		t.Fatal(err)
	}
	if spawnOut.ID != "task-1" || spawnOut.Error != "" {
		t.Fatalf("spawn: got %+v, want id task-1", spawnOut)
	}

	listOut, err := tm.list(context.Background(), &ListAgentsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listOut.Agents) != 1 || listOut.Agents[0].Status != string(TaskRunning) || listOut.Agents[0].Task != "watch the build" {
		t.Fatalf("list: got %+v", listOut.Agents)
	}

	// Still gated: wait times out with status running.
	waitOut, err := tm.wait(context.Background(), &WaitAgentInput{ID: "task-1", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if waitOut.Status != string(TaskRunning) || waitOut.Result != "" {
		t.Fatalf("wait while running: got %+v", waitOut)
	}

	// Canceled ctx must unblock wait immediately.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	waitOut, err = tm.wait(cancelCtx, &WaitAgentInput{ID: "task-1", TimeoutSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("wait must return promptly on ctx cancel")
	}
	if waitOut.Status != string(TaskRunning) {
		t.Fatalf("wait after cancel: got %+v", waitOut)
	}

	close(m.release)
	waitOut, err = tm.wait(context.Background(), &WaitAgentInput{ID: "task-1", TimeoutSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	if waitOut.Status != string(TaskDone) || waitOut.Result != "gated answer" {
		t.Fatalf("wait after finish: got %+v", waitOut)
	}

	listOut, _ = tm.list(context.Background(), &ListAgentsInput{})
	if listOut.Agents[0].Status != string(TaskDone) {
		t.Fatalf("list after finish: got %+v", listOut.Agents[0])
	}

	waitOut, _ = tm.wait(context.Background(), &WaitAgentInput{ID: "task-99"})
	if waitOut.Error == "" {
		t.Fatal("wait on unknown id must report an error")
	}
}

func TestTaskManagerRejectsBeyondMaxConcurrent(t *testing.T) {
	m := &gatedModel{release: make(chan struct{})}
	defer close(m.release)
	tm := NewTaskManager(testFactory(m))

	for i := 0; i < maxConcurrentTasks; i++ {
		out, err := tm.spawn(context.Background(), &SpawnAgentInput{Task: "task"})
		if err != nil || out.Error != "" {
			t.Fatalf("spawn %d: got %+v, err %v", i, out, err)
		}
	}
	out, err := tm.spawn(context.Background(), &SpawnAgentInput{Task: "one too many"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Error == "" || out.ID != "" {
		t.Fatalf("spawn beyond max must be rejected: got %+v", out)
	}
}
