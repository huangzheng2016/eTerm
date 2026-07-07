package sshview

import (
	"encoding/base64"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestOSC52SystemClipboardReturnsSetClipboardCommand(t *testing.T) {
	cases := []struct {
		name string
		seq  string
	}{
		{"bel", ansi.SetSystemClipboard("remote copy")},
		{"st", "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("remote copy")) + "\x1b\\"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(nil, "test", 0, viewkeys.SSHKeys{})
			t.Cleanup(func() { _ = m.Close() })
			m.ch <- []byte("next")

			_, cmd := m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte(tc.seq)})
			if cmd == nil {
				t.Fatal("expected command")
			}
			msg := cmd()
			batch, ok := msg.(tea.BatchMsg)
			if !ok {
				t.Fatalf("msg = %T want tea.BatchMsg", msg)
			}

			found := false
			for _, c := range batch {
				if fmt.Sprint(c()) == "remote copy" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing clipboard command in %#v", batch)
			}
		})
	}
}

func TestOSC52InvalidPayloadDoesNotSetClipboard(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.ch <- []byte("next")

	_, cmd := m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("\x1b]52;c;not-base64\x07")})
	if cmd == nil {
		t.Fatal("expected waitChunk command")
	}
	if _, ok := cmd().(ChunkMsg); !ok {
		t.Fatalf("unexpected clipboard command")
	}
}
