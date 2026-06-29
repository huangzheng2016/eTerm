package app

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
)

type sessionRenameKind int

const (
	renameRemoteShell sessionRenameKind = iota
	renameTmuxSession
)

type sessionRenameModel struct {
	kind    sessionRenameKind
	input   textinput.Model
	peer    types.RemotePeer
	shellID string
	oldName string
}

func newRemoteShellRenamePrompt(msg types.RemoteShellRenameRequestMsg) *sessionRenameModel {
	ti := newSessionRenameInput(msg.CurrentName)
	return &sessionRenameModel{kind: renameRemoteShell, input: ti, peer: msg.Peer, shellID: msg.ShellID}
}

func newTmuxRenamePrompt(msg types.TmuxRenameRequestMsg) *sessionRenameModel {
	ti := newSessionRenameInput(msg.Name)
	return &sessionRenameModel{kind: renameTmuxSession, input: ti, oldName: msg.Name}
}

func newSessionRenameInput(value string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "session name"
	ti.CharLimit = 256
	ti.SetValue(value)
	ti.CursorEnd()
	ti.SetWidth(44)
	ti.Focus()
	return ti
}

func (m *sessionRenameModel) syncWidth(termW int) {
	w := 44
	if termW > 0 {
		w = max(24, termW-16)
		if w > 64 {
			w = 64
		}
	}
	m.input.SetWidth(w)
}

func (m *sessionRenameModel) Update(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		return true, nil
	case "enter":
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			return true, nil
		}
		switch m.kind {
		case renameRemoteShell:
			peer := m.peer
			shellID := m.shellID
			return true, func() tea.Msg { return types.RemoteShellRenameMsg{Peer: peer, ShellID: shellID, Name: name} }
		case renameTmuxSession:
			oldName := m.oldName
			return true, func() tea.Msg { return types.TmuxRenameMsg{OldName: oldName, NewName: name} }
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return false, cmd
}

func (m *sessionRenameModel) paste(msg tea.PasteMsg) {
	m.input = inputpaste.TextInput(m.input, msg)
}

func (m *sessionRenameModel) View() string {
	title := "Rename session"
	if m.kind == renameRemoteShell {
		title = "Rename active shell"
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("Enter apply · Esc cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, ui.TitleStyle.Render(title), "", m.input.View(), "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)
}
