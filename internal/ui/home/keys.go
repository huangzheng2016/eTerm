package home

import "charm.land/bubbles/v2/key"

// listKeyMap holds shortcuts for the host list; runtime matching uses internal/keymatch (Keystroke + ultraviolet).
type listKeyMap struct {
	SSHConnect key.Binding
	SFTPOpen   key.Binding
	NewHost    key.Binding
	EditHost   key.Binding
	DeleteHost key.Binding
	CopySSH    key.Binding
	CloneHost  key.Binding
	Search     key.Binding
	ToggleView key.Binding
}

func defaultListKeyMap() listKeyMap {
	return listKeyMap{
		SSHConnect: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "connect"),
		),
		SFTPOpen: key.NewBinding(
			key.WithKeys("ctrl+f", "s"),
			key.WithHelp("s/C-f", "sftp"),
		),
		NewHost: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new"),
		),
		EditHost: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit"),
		),
		DeleteHost: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		CopySSH: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "copy ssh"),
		),
		CloneHost: key.NewBinding(
			key.WithKeys("C"),
			key.WithHelp("C", "clone"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		ToggleView: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "group/tag"),
		),
	}
}
