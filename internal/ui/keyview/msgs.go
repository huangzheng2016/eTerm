package keyview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/eterm/eterm/internal/db"
)

type keysLoadedMsg struct {
	keys []db.SSHKey
	err  error
}

type keyCreatedMsg struct {
	key *db.SSHKey
	err error
}

type keyDeletedMsg struct {
	err error
}

type keyImportedMsg struct {
	key *db.SSHKey
	err error
}

func isCtrlEnter(msg tea.KeyPressMsg) bool {
	if msg.String() == "ctrl+enter" {
		return true
	}
	k := msg.Key()
	return k.Code == tea.KeyEnter && k.Mod.Contains(tea.ModCtrl)
}
