package aiview

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func newSteerTestModel() (*Model, *FakeRunner) {
	fake := NewFakeRunner() // 20ms event delay keeps the run active
	m := New(fake, fake, fake)
	m.SetSize(100, 32)
	return m, fake
}

func TestEnterDuringRunQueuesDimThenSteerAck(t *testing.T) {
	m, fake := newSteerTestModel()

	m.input.SetValue("first")
	if cmd := m.send(); cmd == nil {
		t.Fatal("first send returned no cmd")
	}
	if m.status != statusRunning {
		t.Fatal("status not running after first send")
	}

	m.input.SetValue("second")
	if cmd := m.send(); cmd != nil {
		t.Fatal("queueing must not start a second run")
	}
	if m.status != statusRunning {
		t.Fatal("queueing must keep the run going")
	}
	if m.input.Value() != "" {
		t.Fatal("input not reset after queueing")
	}
	fake.mu.Lock()
	queued := append([]string(nil), fake.Queued...)
	fake.mu.Unlock()
	if len(queued) != 1 || queued[0] != "second" {
		t.Fatalf("runner queue: %v", queued)
	}
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockUser || !last.queued {
		t.Fatalf("last block: %+v", last)
	}
	if out := plain(last.cache); !strings.Contains(out, "Queued: second") {
		t.Fatalf("queued block must render dim: %q", out)
	}

	pumpEvents(t, m)
	for _, b := range m.blocks {
		if b.kind == blockUser && b.queued {
			t.Fatalf("block still queued after steer ack: %+v", b)
		}
	}
	if out := plain(m.blocks[1].cache); !strings.Contains(out, "You: second") {
		t.Fatalf("acked block must render as user block: %q", out)
	}
}

func TestCtrlCDuringRunDiscardsQueue(t *testing.T) {
	m, fake := newSteerTestModel()

	m.input.SetValue("first")
	m.send()
	m.input.SetValue("second")
	m.send()
	m.input.SetValue("third")
	m.send()

	m.chatKey(keyMsg('c', tea.ModCtrl))

	if m.status != statusIdle {
		t.Fatal("ctrl+c must end the run")
	}
	for _, b := range m.blocks {
		if b.kind == blockUser && b.queued {
			t.Fatalf("queued block survived ctrl+c: %+v", b)
		}
		if b.kind == blockSystem && b.cache == "" {
			t.Fatalf("system block %q was never rendered (blank gap)", b.text)
		}
	}
	fake.mu.Lock()
	remaining := fake.Queued
	fake.mu.Unlock()
	if len(remaining) != 0 {
		t.Fatalf("runner queue not cleared: %v", remaining)
	}
	var notes []string
	for _, b := range m.blocks {
		if b.kind == blockSystem {
			notes = append(notes, b.text)
		}
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "Interrupted by user (ctrl+c)") {
		t.Fatalf("missing interrupt note: %v", notes)
	}
	if !strings.Contains(joined, "2 queued message(s) discarded") {
		t.Fatalf("missing discard note: %v", notes)
	}
}

func TestEnqueueFailureKeepsInputAndShowsError(t *testing.T) {
	m, fake := newSteerTestModel()
	fake.EnqueueErr = errors.New("no run in progress")

	m.input.SetValue("first")
	m.send()
	m.input.SetValue("second")
	m.send()

	if m.errMsg == "" {
		t.Fatal("enqueue failure must surface an error")
	}
	if m.input.Value() != "second" {
		t.Fatalf("input must be kept on enqueue failure, got %q", m.input.Value())
	}
	for _, b := range m.blocks {
		if b.kind == blockUser && b.queued {
			t.Fatalf("failed enqueue must not add a queued block: %+v", b)
		}
	}
	fake.mu.Lock()
	remaining := fake.Queued
	fake.mu.Unlock()
	if len(remaining) != 0 {
		t.Fatalf("failed enqueue must not reach the runner queue: %v", remaining)
	}
}
