package batchresultview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCopySelectedOutputReturnsClipboardCommand(t *testing.T) {
	m := &Model{hosts: []hostState{{HostID: 1, Label: "host", Status: "failed"}}}
	m.hosts[0].Output.WriteString("failure output")

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c'}))

	if cmd == nil {
		t.Fatal("expected clipboard command")
	}
}

func TestEmitAfterAllDoneDoesNotPanic(t *testing.T) {
	m := &Model{jobID: 1, ch: make(chan tea.Msg, 4)}

	m.emit(AllDoneMsg{JobID: 1})
	m.emit(HostStartMsg{JobID: 1, HostID: 2})
}
