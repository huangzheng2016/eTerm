package sshview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
			m := newTestModel(t, nil)

			batch := batchForChunk(t, m, tc.seq)

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
	m := newTestModel(t, nil)
	expectOnlyWaitChunk(t, m, "\x1b]777\a\x1b]777;notify\a\x1b]777;notify;;\a\x1b]777;other;a;b\a")
}
