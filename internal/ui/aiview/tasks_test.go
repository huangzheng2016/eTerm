package aiview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func newTasksTestModel() (*Model, *FakeRunner) {
	fake := NewFakeRunner()
	fake.Delay = 0
	fake.TaskList = []TaskEntry{
		{ID: "task-1", Task: "watch the build", Status: "running", StartedSecAgo: 12, Tail: []TaskActivity{
			{Kind: "status", Text: "running"},
			{Kind: "tool", Text: "list_tabs {}"},
			{Kind: "text", Text: "build still going"},
		}},
		{ID: "task-2", Task: "tail the remote log", Status: "done", StartedSecAgo: 45, Tail: []TaskActivity{
			{Kind: "status", Text: "done"},
		}},
	}
	m := New(fake, fake, fake)
	m.SetSize(100, 32)
	return m, fake
}

func TestSlashTasksOpensView(t *testing.T) {
	m, _ := newTasksTestModel()
	sendSlash(t, m, "/tasks")
	if m.mode != modeTasks {
		t.Fatalf("mode = %v, want modeTasks", m.mode)
	}
	out := plain(m.View().Content)
	for _, s := range []string{"Tasks", "task-1", "running", "watch the build", "build still going", "task-2", "done", "tail the remote log"} {
		if !strings.Contains(out, s) {
			t.Fatalf("tasks view missing %q:\n%s", s, out)
		}
	}
}

func TestSlashTasksEmpty(t *testing.T) {
	m := newTestModel(nil)
	sendSlash(t, m, "/tasks")
	if m.mode != modeTasks {
		t.Fatalf("mode = %v, want modeTasks", m.mode)
	}
	if !strings.Contains(plain(m.View().Content), "No background tasks") {
		t.Fatal("missing empty state")
	}
}

func TestSlashTasksAllowedWhileRunning(t *testing.T) {
	m := newTestModel([]AgentEvent{
		{Kind: EventTextDelta, Text: "slow"},
		{Kind: EventDone},
	})
	m.input.SetValue("hi")
	m.send()
	sendSlash(t, m, "/tasks")
	if m.mode != modeTasks {
		t.Fatal("/tasks must open while a run is in progress")
	}
	m.Update(keyMsg(tea.KeyEscape, 0))
	pumpEvents(t, m)
}

func TestTasksViewNavigateInspectCancel(t *testing.T) {
	m, fake := newTasksTestModel()
	sendSlash(t, m, "/tasks")

	m.Update(keyMsg('j', 0))
	if m.tCursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.tCursor)
	}
	m.Update(keyMsg('k', 0))
	if m.tCursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.tCursor)
	}

	// x on a finished task is a no-op; on a running one it cancels.
	m.Update(keyMsg('j', 0))
	m.Update(keyMsg('x', 0))
	if len(fake.cancelledTasks) != 0 {
		t.Fatalf("finished task must not cancel: %v", fake.cancelledTasks)
	}
	m.Update(keyMsg('k', 0))
	m.Update(keyMsg('x', 0))
	if len(fake.cancelledTasks) != 1 || fake.cancelledTasks[0] != "task-1" {
		t.Fatalf("cancelled = %v, want [task-1]", fake.cancelledTasks)
	}
	if m.taskList[0].Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", m.taskList[0].Status)
	}

	// Inspect the activity tail.
	m.Update(keyMsg(tea.KeyEnter, 0))
	if m.mode != modeTaskDetail {
		t.Fatalf("mode = %v, want modeTaskDetail", m.mode)
	}
	out := plain(m.View().Content)
	for _, s := range []string{"task-1", "cancelled", "watch the build", "tool: list_tabs {}", "build still going"} {
		if !strings.Contains(out, s) {
			t.Fatalf("detail view missing %q:\n%s", s, out)
		}
	}

	m.Update(keyMsg(tea.KeyEscape, 0))
	if m.mode != modeTasks {
		t.Fatal("esc did not return to the list")
	}
	m.Update(keyMsg(tea.KeyEscape, 0))
	if m.mode != modeChat {
		t.Fatal("esc did not return to chat")
	}
}

func TestTasksTickRefreshesWhileOpen(t *testing.T) {
	m, fake := newTasksTestModel()
	sendSlash(t, m, "/tasks")
	seq := m.tasksSeq

	fake.TaskList[0].Status = "done"
	if cmd := m.tasksTick(seq); cmd == nil {
		t.Fatal("tick chain must continue while the view is open")
	}
	if m.taskList[0].Status != "done" {
		t.Fatalf("status = %q, want refreshed to done", m.taskList[0].Status)
	}

	// Stale and post-exit ticks end the chain.
	if cmd := m.tasksTick(seq + 1); cmd != nil {
		t.Fatal("stale tick must not reschedule")
	}
	m.Update(keyMsg(tea.KeyEscape, 0))
	if cmd := m.tasksTick(seq); cmd != nil {
		t.Fatal("tick must not reschedule after leaving the view")
	}
}
