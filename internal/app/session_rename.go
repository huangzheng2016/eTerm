package app

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

type sessionRenameKind int

const (
	renameRemoteTmux sessionRenameKind = iota
	renameTmuxSession
	renameTab
)

type sessionRenameModel struct {
	kind    sessionRenameKind
	input   textinput.Model
	peer    types.RemotePeer
	session string
	oldName string
	tab     int
}

type tabRenameMsg struct {
	Index int
	Title string
}

func newRemoteTmuxRenamePrompt(msg types.RemoteTmuxRenameRequestMsg) *sessionRenameModel {
	ti := newSessionRenameInput(msg.CurrentName)
	return &sessionRenameModel{kind: renameRemoteTmux, input: ti, peer: msg.Peer, session: msg.SessionID}
}

func newTmuxRenamePrompt(msg types.TmuxRenameRequestMsg) *sessionRenameModel {
	ti := newSessionRenameInput(msg.Name)
	return &sessionRenameModel{kind: renameTmuxSession, input: ti, oldName: msg.Name}
}

func newTabRenamePrompt(index int, title string) *sessionRenameModel {
	ti := newSessionRenameInput(title)
	return &sessionRenameModel{kind: renameTab, input: ti, tab: index}
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
		case renameRemoteTmux:
			peer := m.peer
			session := m.session
			return true, func() tea.Msg { return types.RemoteTmuxRenameMsg{Peer: peer, SessionID: session, Name: name} }
		case renameTmuxSession:
			oldName := m.oldName
			return true, func() tea.Msg { return types.TmuxRenameMsg{OldName: oldName, NewName: name} }
		case renameTab:
			idx := m.tab
			return true, func() tea.Msg { return tabRenameMsg{Index: idx, Title: name} }
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
	if m.kind == renameRemoteTmux {
		title = "Rename tmux session"
	} else if m.kind == renameTab {
		title = "Rename tab"
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("Enter apply · Esc cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, ui.TitleStyle.Render(title), "", m.input.View(), "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)
}

func (a App) openActiveTabRenamePrompt() (App, tea.Cmd) {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return a, nil
	}
	tab := a.tabs[a.activeTab]
	if sm, ok := tab.Model.(*sshview.Model); ok {
		if spec := sm.RemoteReconnect(); spec != nil && spec.Tmux {
			a.renamePrompt = newRemoteTmuxRenamePrompt(types.RemoteTmuxRenameRequestMsg{
				Peer:        spec.Peer,
				SessionID:   spec.SessionID,
				CurrentName: remoteTmuxPromptName(tab.Title, spec.Peer.Name, spec.SessionID),
			})
			a.renamePrompt.syncWidth(a.width)
			return a, textinput.Blink
		}
	}
	if tab.Type == LocalTab && tab.TmuxSession != "" {
		name := tab.TmuxSession
		a.renamePrompt = newTmuxRenamePrompt(types.TmuxRenameRequestMsg{Name: name})
		a.renamePrompt.syncWidth(a.width)
		return a, textinput.Blink
	}
	a.renamePrompt = newTabRenamePrompt(a.activeTab, tab.Title)
	a.renamePrompt.syncWidth(a.width)
	return a, textinput.Blink
}

func remoteTmuxPromptName(title, peerName, sessionID string) string {
	prefix := "[T]" + peerName + "-"
	if strings.HasPrefix(title, prefix) {
		if name := strings.TrimSpace(strings.TrimPrefix(title, prefix)); name != "" {
			return name
		}
	}
	return sessionID
}

func (a App) renameTab(msg tabRenameMsg) (App, tea.Cmd) {
	title := strings.TrimSpace(msg.Title)
	if title == "" || msg.Index < 0 || msg.Index >= len(a.tabs) {
		return a, nil
	}
	a.tabs[msg.Index].Title = title
	a.tabs[msg.Index].userRenamed = true
	a.syncTabBar()
	return a, nil
}
