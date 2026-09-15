package syncview

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
	"gorm.io/gorm"
)

func newModelWithSecrets(t *testing.T) (*Model, *gorm.DB) {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.Setup([]byte("pw"))
	k := mk.GetKey()
	if k == nil {
		t.Fatal("master key unavailable")
	}
	defer k.Clear()
	for key, plain := range map[string]string{
		"sync_api_key":    "topsecretkey",
		"sync_passphrase": "topsecretpass",
	} {
		enc, err := security.Encrypt([]byte(plain), k.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetSetting(database, key, enc); err != nil {
			t.Fatal(err)
		}
	}
	return New(database, mk), database
}

func TestViewShowsSectionsAndMasksSecrets(t *testing.T) {
	m, _ := newModelWithSecrets(t)
	m.SetSize(100, 40)

	view := m.View().Content
	for _, want := range []string{"Connection", "Encryption", "Behavior", "sync data encryption passphrase", "(set)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	for _, leak := range []string{"topsecretkey", "topsecretpass"} {
		if strings.Contains(view, leak) {
			t.Fatalf("view leaks secret %q:\n%s", leak, view)
		}
	}
}

func TestViewShowsNotSetForEmptySecrets(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(database, security.NewMasterKeyManager(nil, nil, time.Minute))
	m.SetSize(100, 40)

	view := m.View().Content
	if !strings.Contains(view, "(not set)") {
		t.Fatalf("view missing (not set):\n%s", view)
	}
	if strings.Contains(view, "(set)") {
		t.Fatalf("view shows (set) for empty secrets:\n%s", view)
	}
}

func TestSecretEditCommitStagesPendingValue(t *testing.T) {
	m, _ := newModelWithSecrets(t)
	m.SetSize(100, 40)
	enter := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})

	m.focused = 5 // Passphrase in HTTP mode
	m.Update(enter)
	if m.editing != inPassphrase {
		t.Fatalf("editing = %d, want %d", m.editing, inPassphrase)
	}
	if got := m.inputs[inPassphrase].Value(); got != "" {
		t.Fatalf("edit input preloaded with %q", got)
	}

	m.inputs[inPassphrase].SetValue("newpass")
	if view := m.View().Content; strings.Contains(view, "newpass") || !strings.Contains(view, "*******") {
		t.Fatalf("secret echoed while editing:\n%s", view)
	}

	m.Update(enter)
	if m.editing >= 0 {
		t.Fatal("still editing after commit")
	}
	if !m.passDirty || m.pendPass != "newpass" {
		t.Fatalf("pendPass = %q dirty = %v", m.pendPass, m.passDirty)
	}
	if view := m.View().Content; strings.Contains(view, "newpass") || !strings.Contains(view, "(set)") {
		t.Fatalf("committed secret not masked:\n%s", view)
	}
}

func TestSecretEditEscDiscardsPendingValue(t *testing.T) {
	m, _ := newModelWithSecrets(t)
	m.SetSize(100, 40)

	m.focused = 5 // Passphrase in HTTP mode
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m.inputs[inPassphrase].SetValue("newpass")
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if cmd != nil {
		t.Fatal("esc in edit mode must not emit a command")
	}
	if m.editing >= 0 {
		t.Fatal("still editing after esc")
	}
	if m.passDirty || m.pendPass != "" {
		t.Fatalf("esc should discard edit: pendPass = %q dirty = %v", m.pendPass, m.passDirty)
	}
}

func TestTypingOutsideEditDoesNotTouchSecretInput(t *testing.T) {
	m, _ := newModelWithSecrets(t)
	m.SetSize(100, 40)

	m.focused = 4 // API Key in HTTP mode
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
	if got := m.inputs[inAPIKey].Value(); got != "" {
		t.Fatalf("secret input changed outside edit mode: %q", got)
	}
	if m.apiKeyDirty {
		t.Fatal("api key marked dirty without edit")
	}
}

func TestSaveKeepsUntouchedSecrets(t *testing.T) {
	m, database := newModelWithSecrets(t)
	beforeKey, _ := db.GetSetting(database, "sync_api_key")
	beforePass, _ := db.GetSetting(database, "sync_passphrase")

	m.enableIdx = 1
	m.modeIdx = 0
	m.inputs[inServerURL].SetValue("https://sync.example.com")
	msg := m.save()()
	if _, ok := msg.(types.SuccessMsg); !ok {
		t.Fatalf("got %T want types.SuccessMsg", msg)
	}

	afterKey, _ := db.GetSetting(database, "sync_api_key")
	afterPass, _ := db.GetSetting(database, "sync_passphrase")
	if afterKey != beforeKey || afterPass != beforePass {
		t.Fatal("untouched secrets were rewritten")
	}
}

func TestSaveOverwritesEditedSecret(t *testing.T) {
	m, database := newModelWithSecrets(t)
	beforeKey, _ := db.GetSetting(database, "sync_api_key")

	m.enableIdx = 1
	m.modeIdx = 0
	m.inputs[inServerURL].SetValue("https://sync.example.com")
	m.pendPass = "newpass"
	m.passDirty = true
	msg := m.save()()
	if _, ok := msg.(types.SuccessMsg); !ok {
		t.Fatalf("got %T want types.SuccessMsg", msg)
	}

	enc, _ := db.GetSetting(database, "sync_passphrase")
	k := m.masterKey.GetKey()
	if k == nil {
		t.Fatal("master key unavailable")
	}
	defer k.Clear()
	plain, err := security.Decrypt(enc, k.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "newpass" {
		t.Fatalf("passphrase = %q, want newpass", plain)
	}
	if afterKey, _ := db.GetSetting(database, "sync_api_key"); afterKey != beforeKey {
		t.Fatal("untouched api key was rewritten")
	}
}

func TestSaveClearsEditedSecret(t *testing.T) {
	m, database := newModelWithSecrets(t)

	m.pendAPIKey = ""
	m.apiKeyDirty = true
	msg := m.save()()
	if _, ok := msg.(types.SuccessMsg); !ok {
		t.Fatalf("got %T want types.SuccessMsg", msg)
	}
	if v, _ := db.GetSetting(database, "sync_api_key"); v != "" {
		t.Fatalf("sync_api_key = %q, want cleared", v)
	}
}

func TestSaveRequiresEffectivePassphrase(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(database, security.NewMasterKeyManager(nil, nil, time.Minute))
	m.enableIdx = 1
	m.modeIdx = 0
	m.inputs[inServerURL].SetValue("https://sync.example.com")

	if cmd := m.save(); cmd != nil {
		t.Fatal("expected validation failure without passphrase")
	}
	if m.err == "" {
		t.Fatal("expected validation error")
	}

	m.passDirty = true // cleared via edit
	if cmd := m.save(); cmd != nil {
		t.Fatal("expected validation failure with cleared passphrase")
	}
}
