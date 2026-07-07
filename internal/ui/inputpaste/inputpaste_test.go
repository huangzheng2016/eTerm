package inputpaste

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func TestTextInputPastesAtCursor(t *testing.T) {
	m := textinput.New()
	m.Focus()
	m.SetValue("abef")
	m.SetCursor(2)

	m = TextInput(m, tea.PasteMsg{Content: "cd"})

	if got := m.Value(); got != "abcdef" {
		t.Fatalf("value = %q", got)
	}
}

func TestTextAreaPastesAtCursor(t *testing.T) {
	m := textarea.New()
	m.Focus()
	m.SetValue("abef")
	m.SetCursorColumn(2)

	m = TextArea(m, tea.PasteMsg{Content: "cd"})

	if got := m.Value(); got != "abcdef" {
		t.Fatalf("value = %q", got)
	}
}
