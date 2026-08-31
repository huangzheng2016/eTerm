package sshview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestOSC777NotifyReturnsSameRawCommandAsOSC9(t *testing.T) {
	cases := []struct {
		name string
		seq  string
		want string
	}{
		{"bel", "\x1b]777;notify;Build;finished ok\a", "\x1b]9;Build: finished ok\a"},
		{"st", "\x1b]777;notify;Build;finished ok\x1b\\", "\x1b]9;Build: finished ok\a"},
		{"empty body", "\x1b]777;notify;Deploy;\a", "\x1b]9;Deploy\a"},
		{"del stripped", "\x1b]777;notify;hi;the\x7fre\a", "\x1b]9;hi: there\a"},
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
				if raw, ok := c().(tea.RawMsg); ok && raw.Msg == tc.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing raw notification %q in %#v", tc.want, batch)
			}
		})
	}
}

func TestOSC777InvalidPayloadDoesNotNotify(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.ch <- []byte("next")

	_, cmd := m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("\x1b]777\a\x1b]777;notify\a\x1b]777;notify;;\a\x1b]777;other;a;b\a")})
	if cmd == nil {
		t.Fatal("expected waitChunk command")
	}
	if _, ok := cmd().(ChunkMsg); !ok {
		t.Fatalf("unexpected notification command")
	}
}
