package sshview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestANSITranscriptPreservesCellColor(t *testing.T) {
	m := &Model{emu: mkEmu(20, 3, "\x1b[31mred\x1b[0m")}
	got := m.ANSITranscript(MaxTranscriptBytes)
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("ANSI transcript has no style sequence: %q", got)
	}
	if !strings.Contains(ansi.Strip(got), "red") {
		t.Fatalf("ANSI transcript text = %q", ansi.Strip(got))
	}
}
