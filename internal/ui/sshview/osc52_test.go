package sshview

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
			m := newTestModel(t, nil)

			batch := batchForChunk(t, m, tc.seq)

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
	m := newTestModel(t, nil)
	expectOnlyWaitChunk(t, m, "\x1b]52;c;not-base64\x07")
}
