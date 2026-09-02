package sshview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestOSC9NotificationReturnsRawCommand(t *testing.T) {
	cases := []struct {
		name string
		seq  string
		want string
	}{
		{"bel", "\x1b]9;build finished\a", "\x1b]9;build finished\a"},
		{"st", "\x1b]9;build finished\x1b\\", "\x1b]9;build finished\a"},
		{"semicolons", "\x1b]9;a;b\a", "\x1b]9;a;b\a"},
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

func TestOSC9SequenceStripsControlChars(t *testing.T) {
	got := OSC9Sequence("hi\x07\x1b\n\x7fthere")
	want := "\x1b]9;hithere\a"
	if got != want {
		t.Fatalf("osc9Sequence = %q want %q", got, want)
	}

	// C1 controls (U+0080-U+009F): U+009C would terminate the OSC envelope
	// early on C1-interpreting terminals.
	got = OSC9Sequence("hi\u0080\u009c\u009fthere")
	want = "\x1b]9;hithere\a"
	if got != want {
		t.Fatalf("osc9Sequence = %q want %q", got, want)
	}
}

func TestOSC9InvalidPayloadDoesNotNotify(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.ch <- []byte("next")

	_, cmd := m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("\x1b]9\a\x1b]9;\a")})
	if cmd == nil {
		t.Fatal("expected waitChunk command")
	}
	if _, ok := cmd().(ChunkMsg); !ok {
		t.Fatalf("unexpected notification command")
	}
}
