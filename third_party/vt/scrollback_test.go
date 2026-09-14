package vt

import (
	"fmt"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestScrollback(t *testing.T) {
	t.Run("basic push and len", func(t *testing.T) {
		sb := NewScrollback(100)
		if sb.Len() != 0 {
			t.Errorf("expected len 0, got %d", sb.Len())
		}
		if sb.MaxLines() != 100 {
			t.Errorf("expected max 100, got %d", sb.MaxLines())
		}
	})

	t.Run("scrollback in emulator", func(t *testing.T) {
		e := NewEmulator(10, 5)

		for i := 0; i < 10; i++ {
			e.WriteString("\r\n")
		}

		sbLen := e.ScrollbackLen()
		t.Logf("scrollback length after 10 newlines: %d", sbLen)

		if sbLen == 0 {
			t.Error("expected scrollback to have captured lines, got 0")
		}
	})

	t.Run("scrollback with content", func(t *testing.T) {
		e := NewEmulator(20, 5)

		for i := 0; i < 10; i++ {
			e.WriteString("line\r\n")
		}

		sb := e.Scrollback()
		if sb == nil {
			t.Fatal("scrollback is nil")
		}

		if sb.Len() < 5 {
			t.Errorf("expected at least 5 lines in scrollback, got %d", sb.Len())
		}
	})

	t.Run("scrollback max lines", func(t *testing.T) {
		sb := NewScrollback(5)

		for i := 0; i < 10; i++ {
			sb.Push(nil)
		}

		if sb.Len() != 5 {
			t.Errorf("expected len 5 after overflow, got %d", sb.Len())
		}
	})

	t.Run("clear scrollback", func(t *testing.T) {
		e := NewEmulator(20, 5)

		for i := 0; i < 10; i++ {
			e.WriteString("line\r\n")
		}

		if e.ScrollbackLen() == 0 {
			t.Error("expected scrollback before clear")
		}

		e.ClearScrollback()

		if e.ScrollbackLen() != 0 {
			t.Errorf("expected empty scrollback after clear, got %d", e.ScrollbackLen())
		}
	})

	t.Run("alt screen does not have scrollback", func(t *testing.T) {
		e := NewEmulator(20, 5)

		for i := 0; i < 10; i++ {
			e.WriteString("line\r\n")
		}

		mainScrollbackLen := e.ScrollbackLen()
		if mainScrollbackLen == 0 {
			t.Error("expected scrollback on main screen")
		}

		e.WriteString("\x1b[?1049h")

		if e.ScrollbackLen() != mainScrollbackLen {
			t.Errorf("expected scrollback len %d in alt screen, got %d",
				mainScrollbackLen, e.ScrollbackLen())
		}

		for i := 0; i < 10; i++ {
			e.WriteString("alt\r\n")
		}

		if e.ScrollbackLen() != mainScrollbackLen {
			t.Errorf("expected scrollback len %d after alt screen writes, got %d",
				mainScrollbackLen, e.ScrollbackLen())
		}
	})

	t.Run("ED 2 saves to scrollback", func(t *testing.T) {
		e := NewEmulator(20, 5)

		e.WriteString("line 1\r\n")
		e.WriteString("line 2\r\n")
		e.WriteString("line 3\r\n")

		initialLen := e.ScrollbackLen()

		e.WriteString("\x1b[2J")

		newLen := e.ScrollbackLen()
		if newLen <= initialLen {
			t.Errorf("expected scrollback to grow after ED 2, was %d now %d", initialLen, newLen)
		}
		t.Logf("scrollback after ED 2: %d lines", newLen)
	})

	t.Run("ED 3 clears scrollback", func(t *testing.T) {
		e := NewEmulator(20, 5)

		for i := 0; i < 10; i++ {
			e.WriteString("line\r\n")
		}

		if e.ScrollbackLen() == 0 {
			t.Error("expected scrollback before ED 3")
		}

		e.WriteString("\x1b[3J")

		if e.ScrollbackLen() != 0 {
			t.Errorf("expected empty scrollback after ED 3, got %d", e.ScrollbackLen())
		}
	})

	t.Run("order preserved after overflow", func(t *testing.T) {
		sb := NewScrollback(5)

		for i := 0; i < 12; i++ {
			sb.PushWrapped(uv.Line{{Content: fmt.Sprintf("line%d", i)}}, i%3 == 0)
		}

		if sb.Len() != 5 {
			t.Fatalf("expected len 5 after overflow, got %d", sb.Len())
		}
		for i := 0; i < 5; i++ {
			line := sb.Line(i)
			if len(line) != 1 || line[0].Content != fmt.Sprintf("line%d", i+7) {
				t.Errorf("Line(%d) = %v, want line%d", i, line, i+7)
			}
			if got, want := sb.LineWrapped(i), (i+7)%3 == 0; got != want {
				t.Errorf("LineWrapped(%d) = %v, want %v", i, got, want)
			}
		}
	})

	t.Run("set max lines shrink after overflow", func(t *testing.T) {
		sb := NewScrollback(10)

		for i := 0; i < 15; i++ {
			sb.Push(uv.Line{{Content: fmt.Sprintf("line%d", i)}})
		}
		sb.SetMaxLines(4)

		if sb.Len() != 4 {
			t.Fatalf("expected len 4 after shrink, got %d", sb.Len())
		}
		sb.Push(uv.Line{{Content: "line15"}})
		for i := 0; i < 4; i++ {
			line := sb.Line(i)
			if len(line) != 1 || line[0].Content != fmt.Sprintf("line%d", i+12) {
				t.Errorf("Line(%d) = %v, want line%d", i, line, i+12)
			}
		}
	})

	t.Run("set max lines grow after overflow", func(t *testing.T) {
		sb := NewScrollback(5)

		for i := 0; i < 8; i++ {
			sb.Push(uv.Line{{Content: fmt.Sprintf("line%d", i)}})
		}
		sb.SetMaxLines(10)
		for i := 8; i < 13; i++ {
			sb.Push(uv.Line{{Content: fmt.Sprintf("line%d", i)}})
		}

		if sb.Len() != 10 {
			t.Fatalf("expected len 10 after grow, got %d", sb.Len())
		}
		for i := 0; i < 10; i++ {
			line := sb.Line(i)
			if len(line) != 1 || line[0].Content != fmt.Sprintf("line%d", i+3) {
				t.Errorf("Line(%d) = %v, want line%d", i, line, i+3)
			}
		}
	})
}

func BenchmarkPushWrappedFull(b *testing.B) {
	for _, maxLines := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("maxLines=%d", maxLines), func(b *testing.B) {
			sb := NewScrollback(maxLines)
			line := make(uv.Line, 80)
			for i := 0; i < maxLines; i++ {
				sb.Push(line)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sb.PushWrapped(line, i%2 == 0)
			}
		})
	}
}
