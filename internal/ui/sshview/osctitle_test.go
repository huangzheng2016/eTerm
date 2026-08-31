package sshview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func titleMsgsForChunk(t *testing.T, m *Model, data string) []TitleMsg {
	t.Helper()
	m.ch <- []byte("next")
	_, cmd := m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte(data)})
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		// No title commands: the batch collapsed to the lone waitChunk.
		return nil
	}
	var out []TitleMsg
	for _, c := range batch {
		if tm, ok := c().(TitleMsg); ok {
			out = append(out, tm)
		}
	}
	return out
}

func TestOSCTitleEmitsTitleMsg(t *testing.T) {
	cases := []struct {
		name string
		seq  string
		want string
	}{
		{"osc0 bel", "\x1b]0;remote-host\a", "remote-host"},
		{"osc0 st", "\x1b]0;remote-host\x1b\\", "remote-host"},
		{"osc2 bel", "\x1b]2;remote-host\a", "remote-host"},
		{"osc1 icon name", "\x1b]1;remote-host\a", "remote-host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(nil, "test", 0, viewkeys.SSHKeys{})
			t.Cleanup(func() { _ = m.Close() })

			got := titleMsgsForChunk(t, m, tc.seq)
			if len(got) != 1 || got[0].Title != tc.want || got[0].StreamID != m.StreamID() {
				t.Fatalf("%q -> %#v", tc.seq, got)
			}
		})
	}
}

func TestOSCTitleZeroFiresOnce(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	// OSC 0 sets title and icon name; both callbacks fire with the same text.
	if got := titleMsgsForChunk(t, m, "\x1b]0;both\a"); len(got) != 1 {
		t.Fatalf("OSC 0 -> %d title msgs, want 1", len(got))
	}
}

func TestOSCTitleEmptyIgnored(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	if got := titleMsgsForChunk(t, m, "\x1b]2;\a"); len(got) != 0 {
		t.Fatalf("empty title -> %#v", got)
	}
}

func TestPushOSCTitleSanitizesAndDedups(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	m.pushOSCTitle("a\x07b\x1b[1mc\u009cd")
	m.pushOSCTitle("ab[1mcd")
	m.pushOSCTitle("  \t ")
	if len(m.oscTitles) != 1 || m.oscTitles[0] != "ab[1mcd" {
		t.Fatalf("oscTitles = %#v", m.oscTitles)
	}
}
