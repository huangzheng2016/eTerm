package app

import (
	"strings"
	"testing"
)

func TestApplyConnectProgressUpdatesToast(t *testing.T) {
	a := App{}
	next := make(chan string, 1)
	a, cmd := a.applyConnectProgress(connectProgressMsg{Text: "SSH connect - auth", Next: next})
	if got := a.toast.View(); !strings.Contains(got, "SSH connect - auth") {
		t.Fatalf("toast = %q", got)
	}
	if cmd == nil {
		t.Fatal("expected progress command")
	}
}
