package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

const (
	maxConcurrentTasks = 4
	defaultWaitSeconds = 120
	maxWaitSeconds     = 600
	taskResultMaxRunes = 4000
	taskTailMax        = 50
	taskActivityRunes  = 120
)

type TaskStatus string

const (
	TaskRunning   TaskStatus = "running"
	TaskDone      TaskStatus = "done"
	TaskError     TaskStatus = "error"
	TaskCancelled TaskStatus = "cancelled"
)

// agentFactory builds a fresh sub-agent: same model, tools and middlewares as
// the parent, minus the task tools (no recursive spawn).
type agentFactory func(ctx context.Context) (*adk.ChatModelAgent, error)

type agentTask struct {
	id      string
	task    string
	status  TaskStatus
	result  string
	started time.Time
	cancel  context.CancelFunc
	done    chan struct{}
	tail    []TaskActivity
}

// TaskActivity is one entry in a task's activity tail: a text snippet, a tool
// call summary, or a status transition.
type TaskActivity struct {
	Kind string // text | tool | status
	Text string
}

// TaskSnapshot is a read-only view of one task for the panel's tasks browser.
type TaskSnapshot struct {
	ID            string
	Task          string
	Status        TaskStatus
	StartedSecAgo int
	Tail          []TaskActivity // oldest first, capped at taskTailMax
}

// TaskManager runs background sub-agents for an Agent. Sub-agent events are
// consumed internally; only the final text comes back via wait.
type TaskManager struct {
	mu      sync.Mutex // guards every agentTask field
	tasks   []*agentTask
	counter int
	factory agentFactory
}

func NewTaskManager(factory agentFactory) *TaskManager {
	return &TaskManager{factory: factory}
}

type SpawnAgentInput struct {
	Task    string `json:"task" jsonschema_description:"Complete instruction for the background agent: what to watch or do, and when to stop"`
	Context string `json:"context,omitempty" jsonschema_description:"Optional extra context the sub-agent needs: tab ids, commands, paths"`
}

type SpawnAgentOutput struct {
	ID    string `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}

type WaitAgentInput struct {
	ID             string `json:"id" jsonschema_description:"Task id from spawn_agent"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema_description:"Max seconds to block waiting for the agent to finish (default 120, max 600)"`
}

type WaitAgentOutput struct {
	Status string `json:"status"` // running | done | error | cancelled
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type ListAgentsInput struct{}

type AgentInfo struct {
	ID            string `json:"id"`
	Task          string `json:"task"`
	Status        string `json:"status"` // running | done | error | cancelled
	StartedSecAgo int    `json:"started_sec_ago"`
}

type ListAgentsOutput struct {
	Agents []AgentInfo `json:"agents"`
}

func (tm *TaskManager) Tools() ([]tool.BaseTool, error) {
	spawn, err := utils.InferTool("spawn_agent", "Start a background sub-agent on a task. It gets a fresh conversation with the same terminal/daemon tools you have (but cannot spawn further agents), runs in the background, and its progress is NOT streamed to you - only its final text comes back via wait_agent. Use it for long watches (monitor a build, tail a remote log) or independent work that can run in parallel while you do something else; prefer doing simple quick things yourself. Max 4 concurrent", tm.spawn)
	if err != nil {
		return nil, fmt.Errorf("build spawn_agent: %w", err)
	}
	wait, err := utils.InferTool("wait_agent", "Block until a background agent finishes or the timeout elapses, then return its status (running/done/error/cancelled) and, when finished, its final text. If it is still running, wait again later or do other work and check back with list_agents", tm.wait)
	if err != nil {
		return nil, fmt.Errorf("build wait_agent: %w", err)
	}
	list, err := utils.InferTool("list_agents", "List the background agents you spawned with their id, task, status (running/done/error/cancelled) and how many seconds ago each started", tm.list)
	if err != nil {
		return nil, fmt.Errorf("build list_agents: %w", err)
	}
	return []tool.BaseTool{spawn, wait, list}, nil
}

func (tm *TaskManager) spawn(ctx context.Context, in *SpawnAgentInput) (*SpawnAgentOutput, error) {
	tm.mu.Lock()
	running := 0
	for _, t := range tm.tasks {
		if t.status == TaskRunning {
			running++
		}
	}
	if running >= maxConcurrentTasks {
		tm.mu.Unlock()
		return &SpawnAgentOutput{Error: fmt.Sprintf("%d background agents already running (max %d); wait for one to finish", running, maxConcurrentTasks)}, nil
	}
	tm.counter++
	ctx, cancel := context.WithCancel(context.Background())
	t := &agentTask{
		id:      fmt.Sprintf("task-%d", tm.counter),
		task:    in.Task,
		status:  TaskRunning,
		started: time.Now(),
		cancel:  cancel,
		done:    make(chan struct{}),
		tail:    []TaskActivity{{Kind: "status", Text: string(TaskRunning)}},
	}
	tm.tasks = append(tm.tasks, t)
	tm.mu.Unlock()

	go tm.runTask(ctx, t, in.Context)
	return &SpawnAgentOutput{ID: t.id}, nil
}

// CancelAll cancels every running task; their wait callers unblock with
// status cancelled. Called when the owning Agent is replaced.
func (tm *TaskManager) CancelAll() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	for _, t := range tm.tasks {
		if t.status == TaskRunning {
			t.cancel()
		}
	}
}

// CancelTask cancels one running task; its wait caller unblocks with status
// cancelled. Returns false when the id is unknown or not running.
func (tm *TaskManager) CancelTask(id string) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	for _, t := range tm.tasks {
		if t.id == id {
			if t.status != TaskRunning {
				return false
			}
			t.cancel()
			return true
		}
	}
	return false
}

// recordActivity appends one entry to the task's activity tail; the tail is a
// ring buffer capped at taskTailMax (oldest dropped). Text is collapsed to
// one line and truncated.
func (tm *TaskManager) recordActivity(t *agentTask, kind, text string) {
	text = strings.Join(strings.Fields(text), " ")
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t.tail = append(t.tail, TaskActivity{Kind: kind, Text: truncateRunes(text, taskActivityRunes)})
	if len(t.tail) > taskTailMax {
		t.tail = t.tail[len(t.tail)-taskTailMax:]
	}
}

// Snapshots returns every task with its activity tail, oldest spawn first.
func (tm *TaskManager) Snapshots() []TaskSnapshot {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := make([]TaskSnapshot, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		tail := make([]TaskActivity, len(t.tail))
		copy(tail, t.tail)
		out = append(out, TaskSnapshot{
			ID:            t.id,
			Task:          t.task,
			Status:        t.status,
			StartedSecAgo: int(time.Since(t.started).Seconds()),
			Tail:          tail,
		})
	}
	return out
}

func (tm *TaskManager) wait(ctx context.Context, in *WaitAgentInput) (*WaitAgentOutput, error) {
	timeout := in.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultWaitSeconds
	}
	if timeout > maxWaitSeconds {
		timeout = maxWaitSeconds
	}
	tm.mu.Lock()
	var t *agentTask
	for _, task := range tm.tasks {
		if task.id == in.ID {
			t = task
			break
		}
	}
	tm.mu.Unlock()
	if t == nil {
		return &WaitAgentOutput{Error: "unknown task id: " + in.ID}, nil
	}

	timer := time.NewTimer(time.Duration(timeout) * time.Second)
	defer timer.Stop()
	select {
	case <-t.done:
	case <-timer.C:
	case <-ctx.Done():
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := &WaitAgentOutput{Status: string(t.status)}
	if t.status != TaskRunning {
		out.Result = truncateRunes(t.result, taskResultMaxRunes)
	}
	return out, nil
}

func (tm *TaskManager) list(ctx context.Context, in *ListAgentsInput) (*ListAgentsOutput, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := &ListAgentsOutput{}
	for _, t := range tm.tasks {
		out.Agents = append(out.Agents, AgentInfo{
			ID:            t.id,
			Task:          t.task,
			Status:        string(t.status),
			StartedSecAgo: int(time.Since(t.started).Seconds()),
		})
	}
	return out, nil
}

// runTask executes one sub-agent turn on a detached ctx (the task keeps
// running after the spawning tool call returns), stoppable via CancelAll.
func (tm *TaskManager) runTask(ctx context.Context, t *agentTask, taskCtx string) {
	result, err := tm.runAgent(ctx, t, taskCtx)
	tm.mu.Lock()
	switch {
	case ctx.Err() != nil:
		t.status = TaskCancelled
	case err != nil:
		t.status = TaskError
		if result == "" {
			result = err.Error()
		}
	default:
		t.status = TaskDone
	}
	t.result = result
	status := t.status
	tm.mu.Unlock()
	text := string(status)
	if status == TaskError && err != nil {
		text = "error: " + err.Error()
	}
	tm.recordActivity(t, "status", text)
	close(t.done)
}

func (tm *TaskManager) runAgent(ctx context.Context, t *agentTask, taskCtx string) (string, error) {
	agent, err := tm.factory(ctx)
	if err != nil {
		return "", err
	}
	input := t.task
	if taskCtx != "" {
		input = t.task + "\n\nContext:\n" + taskCtx
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	iterator := runner.Run(ctx, []*schema.Message{schema.UserMessage(input)})

	var lastText string
	var runErr error
	send := func(ev Event) {
		if ev.Type == EventError {
			runErr = ev.Err
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
			return lastText, event.Err
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
		if msg == nil || mo.Role != schema.Assistant {
			continue
		}
		for _, tc := range msg.ToolCalls {
			tm.recordActivity(t, "tool", tc.Function.Name+" "+tc.Function.Arguments)
		}
		if msg.Content != "" {
			lastText = msg.Content
			tm.recordActivity(t, "text", msg.Content)
		}
	}
	return lastText, runErr
}
