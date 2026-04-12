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

func helpLabel(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	if len(keys) == 1 {
		return keys[0]
	}
	return keys[0] + "/" + keys[1]
}

// BuildListKeyMap constructs a listKeyMap from configurable key slices.
func BuildListKeyMap(sshConnect, sftpOpen, newHost, editHost, deleteHost, copySSH, cloneHost, search, toggleView []string) listKeyMap {
	return listKeyMap{
		SSHConnect: key.NewBinding(key.WithKeys(sshConnect...), key.WithHelp(helpLabel(sshConnect), "connect")),
		SFTPOpen:   key.NewBinding(key.WithKeys(sftpOpen...), key.WithHelp(helpLabel(sftpOpen), "sftp")),
		NewHost:    key.NewBinding(key.WithKeys(newHost...), key.WithHelp(helpLabel(newHost), "new")),
		EditHost:   key.NewBinding(key.WithKeys(editHost...), key.WithHelp(helpLabel(editHost), "edit")),
		DeleteHost: key.NewBinding(key.WithKeys(deleteHost...), key.WithHelp(helpLabel(deleteHost), "delete")),
		CopySSH:    key.NewBinding(key.WithKeys(copySSH...), key.WithHelp(helpLabel(copySSH), "copy ssh")),
		CloneHost:  key.NewBinding(key.WithKeys(cloneHost...), key.WithHelp(helpLabel(cloneHost), "clone")),
		Search:     key.NewBinding(key.WithKeys(search...), key.WithHelp(helpLabel(search), "search")),
		ToggleView: key.NewBinding(key.WithKeys(toggleView...), key.WithHelp(helpLabel(toggleView), "group/tag")),
	}
}
