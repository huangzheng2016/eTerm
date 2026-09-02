package ai

import (
	"context"
	"strings"
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
		tools, err := BuildTools(fakeExecutor{}, nil, false)
		if err != nil {
			return nil, err
		}
		return buildADKAgent(ctx, m, tools, "test instruction", 4, 100000, nil)
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

// Agent.Close must cancel running tasks so they cannot outlive the Agent
// (provider/model switch); blocked wait_agent callers unblock as cancelled.
func TestTaskManagerCancelAllOnClose(t *testing.T) {
	m := &gatedModel{release: make(chan struct{})} // never released
	tm := NewTaskManager(testFactory(m))
	a := &Agent{tasks: tm}

	spawnOut, err := tm.spawn(context.Background(), &SpawnAgentInput{Task: "watch"})
	if err != nil || spawnOut.ID == "" {
		t.Fatalf("spawn: %+v, %v", spawnOut, err)
	}

	a.Close()

	waitOut, err := tm.wait(context.Background(), &WaitAgentInput{ID: spawnOut.ID, TimeoutSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	if waitOut.Status != string(TaskCancelled) {
		t.Fatalf("wait after Close: got %+v, want status cancelled", waitOut)
	}

	listOut, _ := tm.list(context.Background(), &ListAgentsInput{})
	if len(listOut.Agents) != 1 || listOut.Agents[0].Status != string(TaskCancelled) {
		t.Fatalf("list after Close: got %+v", listOut.Agents)
	}
}

func TestTaskManagerActivityTail(t *testing.T) {
	tm := NewTaskManager(testFactory(&fakeModel{}))

	spawnOut, err := tm.spawn(context.Background(), &SpawnAgentInput{Task: "check tabs"})
	if err != nil || spawnOut.ID == "" {
		t.Fatalf("spawn: %+v, %v", spawnOut, err)
	}
	waitOut, err := tm.wait(context.Background(), &WaitAgentInput{ID: spawnOut.ID, TimeoutSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	if waitOut.Status != string(TaskDone) {
		t.Fatalf("wait: %+v", waitOut)
	}

	snaps := tm.Snapshots()
	if len(snaps) != 1 {
		t.Fatalf("snapshots: got %d, want 1", len(snaps))
	}
	s := snaps[0]
	if s.ID != "task-1" || s.Task != "check tabs" || s.Status != TaskDone {
		t.Fatalf("snapshot: %+v", s)
	}
	var kinds []string
	var toolText, textText string
	for _, a := range s.Tail {
		kinds = append(kinds, a.Kind)
		switch a.Kind {
		case "tool":
			toolText = a.Text
		case "text":
			textText = a.Text
		}
	}
	want := []string{"status", "tool", "text", "status"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("tail kinds: got %v, want %v", kinds, want)
	}
	if !strings.Contains(toolText, "list_tabs") {
		t.Fatalf("tool entry missing tool name: %q", toolText)
	}
	if textText != "I see one tab." {
		t.Fatalf("text entry: %q", textText)
	}
	if s.Tail[0].Text != string(TaskRunning) || s.Tail[len(s.Tail)-1].Text != string(TaskDone) {
		t.Fatalf("status transitions: %+v", s.Tail)
	}
}

func TestTaskActivityTailCapped(t *testing.T) {
	tm := NewTaskManager(nil)
	task := &agentTask{id: "task-1"}
	for i := 0; i < taskTailMax+10; i++ {
		tm.recordActivity(task, "text", "entry")
	}
	if len(task.tail) != taskTailMax {
		t.Fatalf("tail: got %d entries, want capped at %d", len(task.tail), taskTailMax)
	}
}

func TestTaskManagerCancelTask(t *testing.T) {
	m := &gatedModel{release: make(chan struct{})} // never released
	tm := NewTaskManager(testFactory(m))

	out1, err := tm.spawn(context.Background(), &SpawnAgentInput{Task: "one"})
	if err != nil || out1.ID == "" {
		t.Fatalf("spawn 1: %+v, %v", out1, err)
	}
	out2, err := tm.spawn(context.Background(), &SpawnAgentInput{Task: "two"})
	if err != nil || out2.ID == "" {
		t.Fatalf("spawn 2: %+v, %v", out2, err)
	}

	if tm.CancelTask("task-99") {
		t.Fatal("unknown id must not cancel")
	}
	if !tm.CancelTask(out1.ID) {
		t.Fatal("running task not cancelled")
	}

	waitOut, err := tm.wait(context.Background(), &WaitAgentInput{ID: out1.ID, TimeoutSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	if waitOut.Status != string(TaskCancelled) {
		t.Fatalf("wait after CancelTask: got %+v, want cancelled", waitOut)
	}
	if tm.CancelTask(out1.ID) {
		t.Fatal("cancelling a finished task must report false")
	}

	listOut, _ := tm.list(context.Background(), &ListAgentsInput{})
	if listOut.Agents[1].ID != out2.ID || listOut.Agents[1].Status != string(TaskRunning) {
		t.Fatalf("other task must keep running: %+v", listOut.Agents[1])
	}
}
