package vt

import (
	"testing"
)

func TestOSC777NotifyMapsToNotification(t *testing.T) {
	term := newTestTerminal(t, 80, 24)
	var texts []string
	term.SetCallbacks(Callbacks{
		Notification: func(text string) { texts = append(texts, text) },
	})

	// BEL terminator.
	term.WriteString("\x1b]777;notify;Build;finished ok\a")
	if len(texts) != 1 || texts[0] != "Build: finished ok" {
		t.Fatalf("texts = %v", texts)
	}

	// ST terminator.
	term.WriteString("\x1b]777;notify;Deploy;done\x1b\\")
	if len(texts) != 2 || texts[1] != "Deploy: done" {
		t.Fatalf("texts = %v", texts)
	}

	// Empty body keeps the title alone.
	term.WriteString("\x1b]777;notify;Ping;\a")
	if len(texts) != 3 || texts[2] != "Ping" {
		t.Fatalf("texts = %v", texts)
	}
}

func TestOSC777InvalidIgnored(t *testing.T) {
	term := newTestTerminal(t, 80, 24)
	called := false
	term.SetCallbacks(Callbacks{
		Notification: func(string) { called = true },
	})
	term.WriteString("\x1b]777\a\x1b]777;notify\a\x1b]777;notify;\a\x1b]777;notify;;\a\x1b]777;other;a;b\a")
	if called {
		t.Fatal("unexpected callback for invalid OSC 777")
	}
}
