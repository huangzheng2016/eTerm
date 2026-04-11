package app

import (
	"charm.land/bubbles/v2/key"
	"github.com/eterm/eterm/internal/keylabels"
)

type KeyMap struct {
	// QuitApp exits the TUI from any tab. Plain ctrl+c is sent to the remote shell on SSH.
	QuitApp      key.Binding
	Quit         key.Binding
	Help         key.Binding
	NewTab       key.Binding
	CloseTab     key.Binding
	CloseTabSafe key.Binding
	NextTab      key.Binding
	PrevTab      key.Binding
	SSHConnect   key.Binding
	SFTPOpen     key.Binding
	NewHost      key.Binding
	EditHost     key.Binding
	DeleteHost   key.Binding
	Search       key.Binding
	Lock         key.Binding
	LockApp      key.Binding
	ForwardTab   key.Binding
	SnippetsTab  key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		QuitApp: key.NewBinding(
			key.WithKeys("ctrl+shift+q", "ctrl+shift+c"),
			key.WithHelp("C-S-q", "quit"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("C-c", "quit (list only; SSH sends to host)"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		NewTab: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp(keylabels.KeysTab, "SSH keys"),
		),
		CloseTab: key.NewBinding(
			key.WithKeys("ctrl+w"),
			key.WithHelp("C-w", "close tab"),
		),
		CloseTabSafe: key.NewBinding(
			key.WithKeys("ctrl+shift+w"),
			key.WithHelp("C-S-w", "close tab"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("ctrl+tab", "ctrl+pgdown", "alt+n", "ctrl+right"),
			key.WithHelp("C-→/A-n", "next"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("ctrl+shift+tab", "ctrl+pgup", "alt+p", "ctrl+left"),
			key.WithHelp("C-←/A-p", "prev"),
		),
		SSHConnect: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "connect"),
		),
		SFTPOpen: key.NewBinding(
			key.WithKeys("ctrl+f", "s"),
			key.WithHelp("s/C-f", "open sftp"),
		),
		NewHost: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new host"),
		),
		EditHost: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit host"),
		),
		DeleteHost: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete host"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Lock: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("C-l", "lock"),
		),
		LockApp: key.NewBinding(
			key.WithKeys("ctrl+shift+l"),
			key.WithHelp("C-S-l", "lock"),
		),
		ForwardTab: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("C-p", "port fwds"),
		),
		SnippetsTab: key.NewBinding(
			key.WithKeys("ctrl+shift+b"),
			key.WithHelp("C-S-b", "snippets"),
		),
	}
}
