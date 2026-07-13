package keyview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
	"gorm.io/gorm"
)

func TestEnterOpensKeyDetailAndCopyPublicKey(t *testing.T) {
	m := New(nil, nil, viewkeys.KeyViewKeys{})
	m.loaded = true
	m.sshKeys = []db.SSHKey{{Model: gorm.Model{ID: 7}, Name: "deploy", PublicKeyData: "ssh-ed25519 AAAA"}}

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(Model)
	if m.mode != modeDetail || m.activeKeyID != 7 {
		t.Fatalf("detail state = mode %d key %d", m.mode, m.activeKeyID)
	}

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c'}))
	if cmd == nil {
		t.Fatal("copy public key command is nil")
	}
}

func TestEscapeDoesNotCloseKeyList(t *testing.T) {
	_, cmd := (Model{}).Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if cmd != nil {
		t.Fatal("escape closed the key list")
	}
}

func TestEditShortcutOpensEditInsteadOfExport(t *testing.T) {
	m := New(nil, nil, viewkeys.KeyViewKeys{Edit: []string{"e"}})
	m.loaded = true
	m.sshKeys = []db.SSHKey{{Model: gorm.Model{ID: 7}, Name: "deploy", CertificatePath: "/tmp/deploy-cert.pub"}}

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: 'e'}))
	m = updated.(Model)
	if m.mode != modeEdit || m.activeKeyID != 7 {
		t.Fatalf("edit state = mode %d key %d", m.mode, m.activeKeyID)
	}
	if m.nameInput.Value() != "deploy" || m.certPathInput.Value() != "/tmp/deploy-cert.pub" {
		t.Fatalf("edit fields = %q %q", m.nameInput.Value(), m.certPathInput.Value())
	}
}
