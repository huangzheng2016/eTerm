package inputpaste

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func TextInput(m textinput.Model, msg tea.PasteMsg) textinput.Model {
	m.SetValue(m.Value() + singleLine(msg.Content))
	return m
}

func TextArea(m textarea.Model, msg tea.PasteMsg) textarea.Model {
	m.SetValue(m.Value() + msg.Content)
	return m
}

func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimRight(strings.ReplaceAll(s, "\n", ""), "\x00")
}
