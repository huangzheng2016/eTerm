package shareview

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
)

type Model struct {
	Peer      types.RemotePeer
	Target    string
	SessionID string
	Label     string
	hours     textinput.Model
	name      textinput.Model
	focus     int
	err       string
}

func New(peer types.RemotePeer, target, sessionID, label string, defaultHours int) *Model {
	if defaultHours < 1 || defaultHours > 168 {
		defaultHours = 4
	}
	hours := textinput.New()
	hours.Placeholder = "1-168"
	hours.CharLimit = 3
	hours.SetValue(strconv.Itoa(defaultHours))
	hours.CursorEnd()
	hours.SetWidth(44)
	hours.Focus()
	name := textinput.New()
	name.Placeholder = "optional"
	name.CharLimit = 128
	name.SetWidth(44)
	return &Model{Peer: peer, Target: target, SessionID: sessionID, Label: label, hours: hours, name: name}
}

func (m *Model) SetWidth(termW int) {
	w := 44
	if termW > 0 {
		w = max(24, termW-16)
		if w > 64 {
			w = 64
		}
	}
	m.hours.SetWidth(w)
	m.name.SetWidth(w)
}

func (m *Model) focusInput() *textinput.Model {
	if m.focus == 0 {
		return &m.hours
	}
	return &m.name
}

func (m *Model) focusNext() tea.Cmd {
	m.err = ""
	if m.focus == 0 {
		m.focus = 1
		m.hours.Blur()
		return m.name.Focus()
	}
	m.focus = 0
	m.name.Blur()
	return m.hours.Focus()
}

func (m *Model) Update(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		return true, nil
	case "tab", "shift+tab", "up", "down":
		return false, m.focusNext()
	case "enter":
		if m.focus == 0 {
			return false, m.focusNext()
		}
		n, err := strconv.Atoi(strings.TrimSpace(m.hours.Value()))
		if err != nil || n < 1 || n > 168 {
			m.err = "hours must be a number between 1 and 168"
			return false, nil
		}
		peer, target, sessionID, label := m.Peer, m.Target, m.SessionID, m.Label
		name := strings.TrimSpace(m.name.Value())
		return true, func() tea.Msg {
			return types.RemoteShareSubmitMsg{Peer: peer, Target: target, SessionID: sessionID, Label: label, Name: name, MaxHours: n}
		}
	}
	m.err = ""
	var cmd tea.Cmd
	in := m.focusInput()
	*in, cmd = in.Update(msg)
	return false, cmd
}

func (m *Model) Paste(msg tea.PasteMsg) {
	in := m.focusInput()
	*in = inputpaste.TextInput(*in, msg)
}

func (m *Model) View() string {
	target := "peer: " + m.Peer.Name
	if m.Target != "" && m.SessionID != "" {
		target = "tmux session: " + m.SessionID
	}
	hint := ui.DimStyle.Render("tab: switch field · enter: next/create · esc: cancel")

	var b strings.Builder
	b.WriteString(ui.TitleStyle.Render("Share shell link"))
	b.WriteString("\n")
	b.WriteString(ui.DimStyle.Render(target))
	b.WriteString("\n\n")
	b.WriteString(ui.DimStyle.Render("Expires (hours)"))
	b.WriteString("\n")
	b.WriteString(m.hours.View())
	b.WriteString("\n\n")
	b.WriteString(ui.DimStyle.Render("Name"))
	b.WriteString("\n")
	b.WriteString(m.name.View())
	if m.err != "" {
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true).Render(m.err))
	}
	b.WriteString("\n\n")
	b.WriteString(hint)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(b.String())
}
