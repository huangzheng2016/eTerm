package batchresultview

import (
	"fmt"
	"io"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type oneByteReader struct {
	data []byte
	pos  int
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}

func TestCopySelectedOutputReturnsClipboardCommand(t *testing.T) {
	m := &Model{hosts: []hostState{{HostID: 1, Label: "host", Status: "failed"}}}
	m.hosts[0].Output.WriteString("failure output")

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c'}))

	if cmd == nil {
		t.Fatal("expected clipboard command")
	}
	if got := fmt.Sprint(cmd()); got != "failure output" {
		t.Fatalf("clipboard = %q", got)
	}
}

func TestEmitAfterAllDoneDoesNotPanic(t *testing.T) {
	m := &Model{jobID: 1, ch: make(chan tea.Msg, 4)}

	m.emit(AllDoneMsg{JobID: 1})
	m.emit(HostStartMsg{JobID: 1, HostID: 2})
}

func TestReadPipeCoalescesRapidOutput(t *testing.T) {
	m := &Model{jobID: 1, ch: make(chan tea.Msg, 4)}
	var wg sync.WaitGroup
	wg.Add(1)

	m.readPipe(2, &oneByteReader{data: []byte("abc")}, &wg)
	wg.Wait()

	msg, ok := (<-m.ch).(HostOutputMsg)
	if !ok {
		t.Fatalf("got %T want HostOutputMsg", msg)
	}
	if msg.JobID != 1 || msg.HostID != 2 || msg.Data != "abc" {
		t.Fatalf("msg = %+v", msg)
	}
	select {
	case extra := <-m.ch:
		t.Fatalf("unexpected extra message: %+v", extra)
	default:
	}
}
