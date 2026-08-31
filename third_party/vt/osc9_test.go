package vt

import (
	"testing"
)

func TestOSC9Notification(t *testing.T) {
	term := newTestTerminal(t, 80, 24)
	var texts []string
	term.SetCallbacks(Callbacks{
		Notification: func(text string) { texts = append(texts, text) },
	})

	// BEL terminator.
	term.WriteString("\x1b]9;build finished\a")
	if len(texts) != 1 || texts[0] != "build finished" {
		t.Fatalf("texts = %v", texts)
	}

	// ST terminator.
	term.WriteString("\x1b]9;done\x1b\\")
	if len(texts) != 2 || texts[1] != "done" {
		t.Fatalf("texts = %v", texts)
	}

	// Semicolons belong to the payload.
	term.WriteString("\x1b]9;a;b;c\a")
	if len(texts) != 3 || texts[2] != "a;b;c" {
		t.Fatalf("texts = %v", texts)
	}
}

func TestOSC9InvalidIgnored(t *testing.T) {
	term := newTestTerminal(t, 80, 24)
	called := false
	term.SetCallbacks(Callbacks{
		Notification: func(string) { called = true },
	})
	term.WriteString("\x1b]9\a\x1b]9;\a")
	if called {
		t.Fatal("unexpected callback for invalid OSC 9")
	}
}
