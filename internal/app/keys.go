package app

import (
	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	// QuitApp exits the TUI from any tab. Plain ctrl+c is sent to the remote shell on SSH.
	QuitApp        key.Binding
	Quit           key.Binding
	Help           key.Binding
	NewTab         key.Binding
	CloseTab       key.Binding
	CloseTabSafe   key.Binding
	NextTab        key.Binding
	PrevTab        key.Binding
	TabPageLeft    key.Binding
	TabPageRight   key.Binding
	SSHConnect     key.Binding
	SFTPOpen       key.Binding
	NewHost        key.Binding
	EditHost       key.Binding
	DeleteHost     key.Binding
	Search         key.Binding
	Lock           key.Binding
	LockApp        key.Binding
	ForwardTab     key.Binding
	SnippetsTab    key.Binding
	CommandPalette key.Binding
	AIOverlay      key.Binding
	VoiceInput     key.Binding
	LocalTerminal  key.Binding
	RenameTab      key.Binding
	PasteImageURL  key.Binding
}

func DefaultKeyMap() KeyMap {
	return BuildKeyMap(DefaultKeyBindingConfig())
}
