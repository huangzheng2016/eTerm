package sshview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestDisconnectedViewShowsTopRightBadge(t *testing.T) {
	m := &Model{emu: mkEmu(40, 5, "shell output"), disconnected: true}

	view := m.View().Content
	lines := strings.Split(ansi.Strip(view), "\n")
	if !strings.HasSuffix(lines[0], " DISCONNECTED ") {
		t.Fatalf("first line missing disconnected badge: %q", lines[0])
	}
	if got := lipgloss.Width(lines[0]); got != 40 {
		t.Fatalf("first line width = %d, want 40", got)
	}
	if !strings.Contains(view, "Connection lost") {
		t.Fatalf("view missing reconnect reason: %q", view)
	}
}

func TestConnectedViewDoesNotShowDisconnectedBadge(t *testing.T) {
	m := &Model{emu: mkEmu(40, 5, "shell output")}
	if strings.Contains(m.View().Content, "DISCONNECTED") {
		t.Fatal("connected view contains disconnected badge")
	}
}
