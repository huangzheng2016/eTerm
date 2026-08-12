package vt

import (
	"testing"
)

func TestOSC133CommandLifecycle(t *testing.T) {
	term := newTestTerminal(t, 80, 24)
	type event struct {
		name     string
		exitCode int
	}
	var events []event
	term.SetCallbacks(Callbacks{
		CommandStart: func() { events = append(events, event{name: "start"}) },
		CommandEnd:   func(code int) { events = append(events, event{name: "end", exitCode: code}) },
	})

	term.WriteString("\x1b]133;A\a\x1b]133;B\a\x1b]133;C\a\x1b]133;D;0\a")
	if len(events) != 2 {
		t.Fatalf("events = %v", events)
	}
	if events[0].name != "start" {
		t.Fatalf("first event = %+v", events[0])
	}
	if events[1].name != "end" || events[1].exitCode != 0 {
		t.Fatalf("second event = %+v", events[1])
	}

	// Non-zero exit code, ST terminator.
	term.WriteString("\x1b]133;C\x1b\\\x1b]133;D;127\x1b\\")
	if len(events) != 4 || events[3].exitCode != 127 {
		t.Fatalf("events = %v", events)
	}

	// D without an exit code reports -1.
	term.WriteString("\x1b]133;D\a")
	if len(events) != 5 || events[4].exitCode != -1 {
		t.Fatalf("events = %v", events)
	}
}

func TestOSC133InvalidIgnored(t *testing.T) {
	term := newTestTerminal(t, 80, 24)
	called := false
	term.SetCallbacks(Callbacks{
		CommandStart: func() { called = true },
		CommandEnd:   func(int) { called = true },
	})
	term.WriteString("\x1b]133\a\x1b]133;\a\x1b]133;X\a")
	if called {
		t.Fatal("unexpected callback for invalid OSC 133")
	}
}
