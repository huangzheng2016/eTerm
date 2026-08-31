package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/ai"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"gorm.io/gorm"
)

func TestDecodeSendKeys(t *testing.T) {
	cases := map[string]string{
		`\n`:            "\n",            // backslash+n -> one LF byte
		`\\n`:           `\n`,            // escaped backslash -> literal backslash+n
		"real\nnewline": "real\nnewline", // raw control bytes pass through
		`\t`:            "\t",
		`\r`:            "\r",
		`\\`:            `\`,
		`\x41\x42`:      "AB",
		`\x0a`:          "\n",
		`\x4`:           `\x4`,  // incomplete hex passes through
		`\xzz`:          `\xzz`, // invalid hex passes through
		`\q`:            `\q`,   // unknown escape passes through
		`a\`:            `a\`,   // trailing backslash
		"plain":         "plain",
	}
	for in, want := range cases {
		if got := decodeSendKeys(in); got != want {
			t.Errorf("decodeSendKeys(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWindowTranscript(t *testing.T) {
	full := "0123456789"
	text, total := windowTranscript(full, 4, 0)
	if text != "6789" || total != 10 {
		t.Fatalf("got %q/%d", text, total)
	}
	text, _ = windowTranscript(full, 4, 2)
	if text != "4567" {
		t.Fatalf("skip window got %q", text)
	}
	text, _ = windowTranscript(full, 100, 0)
	if text != full {
		t.Fatalf("short transcript got %q", text)
	}
	text, _ = windowTranscript(full, 4, 100)
	if text != "" {
		t.Fatalf("skip beyond end got %q", text)
	}
	text, _ = windowTranscript(full, 0, 0)
	if text != full {
		t.Fatalf("default maxBytes got %q", text)
	}
}

func TestWindowTranscriptRuneBoundary(t *testing.T) {
	full := "ab界cd_efgh"
	text, total := windowTranscript(full, 6, 0)
	if total != len(full) {
		t.Fatalf("total = %d", total)
	}
	if !strings.Contains(text, "d_efgh") || strings.ContainsRune(text, '\uFFFD') {
		t.Fatalf("got %q", text)
	}
	if text != "d_efgh" {
		t.Fatalf("got %q", text)
	}
}

func TestTranscriptTail(t *testing.T) {
	if got := transcriptTail("abc", 10); got != "abc" {
		t.Fatalf("short got %q", got)
	}
	if got := transcriptTail("0123456789", 4); got != "6789" {
		t.Fatalf("got %q", got)
	}
	if got := transcriptTail("ab界cd", 4); got != "cd" {
		t.Fatalf("rune-aligned got %q", got)
	}
}

func TestAITabInfosAndLookup(t *testing.T) {
	sv := sshview.New(nil, "prod", 0, BuildSSHKeys(DefaultKeyBindingConfig()))
	a := App{
		tabs: []Tab{
			{Type: HomeTab, Title: "List"},
			{Type: SSHTab, Title: "prod", Model: sv},
		},
		activeTab: 1,
	}
	infos := a.aiTabInfos()
	if len(infos) != 2 {
		t.Fatalf("infos = %#v", infos)
	}
	if infos[0].ID != "tab-0" || infos[0].Active {
		t.Fatalf("list tab info = %+v", infos[0])
	}
	if infos[1].ID == "" || infos[1].ID == "tab-1" || !infos[1].Active {
		t.Fatalf("ssh tab info = %+v", infos[1])
	}
	if a.sshViewByAITabID(infos[1].ID) != sv {
		t.Fatal("sshViewByAITabID did not find the ssh tab")
	}
	if a.sshViewByAITabID("tab-0") != nil {
		t.Fatal("non-terminal tab resolved to a sshview")
	}
	if a.sshViewByAITabID("999999") != nil {
		t.Fatal("unknown stream id resolved")
	}
}

func TestAISharedStatePeers(t *testing.T) {
	s := &aiSharedState{}
	s.setPeers([]types.RemotePeer{{ID: "p1", Name: "daemon-a"}})
	peer, ok := s.peerByName("daemon-a")
	if !ok || peer.ID != "p1" {
		t.Fatalf("peer = %+v ok=%v", peer, ok)
	}
	if _, ok := s.peerByName("nope"); ok {
		t.Fatal("unknown name resolved")
	}
	infos := s.daemonInfos()
	if len(infos) != 1 || infos[0].Name != "daemon-a" || infos[0].Status != "online" {
		t.Fatalf("infos = %#v", infos)
	}
	s.setPeers(nil)
	if len(s.daemonInfos()) != 0 {
		t.Fatal("peers not cleared")
	}
}

func TestAIStorePersistenceRoundTrip(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, 0)
	mk.Setup([]byte("pw"))

	bridge := &aiBridge{store: &ai.Store{}, db: database, mk: mk}
	bridge.store.Upsert(ai.Provider{Name: "mine", Type: ai.ProviderOpenAI, APIKey: "sk-secret", BaseURL: "https://api.example.com", DefaultModel: "gpt-5"})
	bridge.store.Upsert(ai.Provider{Name: "free-tokens_kimi", Type: ai.ProviderOpenAI, APIKey: "kimi-key", Source: ai.SourceKimi})
	bridge.persistProviders()
	if err := bridge.store.SetActive("mine", "gpt-5"); err != nil {
		t.Fatal(err)
	}
	bridge.persistActive()

	// The stored blob must not contain plaintext keys.
	v, err := db.GetSetting(database, aiProvidersSettingKey)
	if err != nil || v == "" {
		t.Fatalf("providers setting missing: %v", err)
	}
	if strings.Contains(v, "sk-secret") || strings.Contains(v, "kimi-key") {
		t.Fatal("provider keys stored in plaintext")
	}

	// Only user-added providers are persisted; kimi-sourced ones are not.
	k := mk.GetKey()
	plain, err := security.Decrypt(v, k.Bytes())
	k.Clear()
	if err != nil {
		t.Fatal(err)
	}
	var persisted []ai.Provider
	if err := json.Unmarshal(plain, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0].Name != "mine" {
		t.Fatalf("persisted = %+v", persisted)
	}

	loaded := loadAIStore(database, mk)
	p := loaded.Get("mine")
	if p == nil || p.APIKey != "sk-secret" || p.BaseURL != "https://api.example.com" || p.DefaultModel != "gpt-5" {
		t.Fatalf("loaded provider = %+v", p)
	}
	if loaded.ActiveProvider != "mine" || loaded.ActiveModel != "gpt-5" {
		t.Fatalf("active = %q/%q", loaded.ActiveProvider, loaded.ActiveModel)
	}
}

func TestBridgeModelsAndSwitch(t *testing.T) {
	store := &ai.Store{}
	store.Upsert(ai.Provider{Name: "free-tokens_kimi", Type: ai.ProviderOpenAI, APIKey: "k"})
	store.Upsert(ai.Provider{Name: "mine", Type: ai.ProviderOpenAI, APIKey: "k2", DefaultModel: "gpt-5"})
	store.Models = append(store.Models, ai.ModelAlias{Alias: "free-tokens_kimi/kimi-k3-highspeed", Provider: "free-tokens_kimi", Model: "kimi-k3-highspeed"})
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	bridge := &aiBridge{store: store, db: database, mk: security.NewMasterKeyManager(nil, nil, 0)}

	// The aliased provider appears only as its alias; the unaliased one as itself.
	models := bridge.Models()
	if len(models) != 2 {
		t.Fatalf("Models() = %v", models)
	}
	if models[0].Label != "free-tokens_kimi/kimi-k3-highspeed" || models[0].Provider != "free-tokens_kimi" {
		t.Fatalf("alias entry = %+v", models[0])
	}
	if models[1].Label != "mine" || models[1].Model != "gpt-5" {
		t.Fatalf("provider entry = %+v", models[1])
	}

	bridge.Switch("free-tokens_kimi", "free-tokens_kimi/kimi-k3-highspeed")
	if store.ActiveModel != "free-tokens_kimi/kimi-k3-highspeed" {
		t.Fatalf("ActiveModel = %q", store.ActiveModel)
	}
	if bridge.Active() != "free-tokens_kimi/kimi-k3-highspeed" {
		t.Fatalf("Active() = %q", bridge.Active())
	}
	p, model, _, err := store.Resolve()
	if err != nil || p.Name != "free-tokens_kimi" || model != "kimi-k3-highspeed" {
		t.Fatalf("Resolve = %v %q %v", p, model, err)
	}

	bridge.Switch("mine", "gpt-5")
	if bridge.Active() != "mine" {
		t.Fatalf("Active() after raw switch = %q", bridge.Active())
	}
}

func TestMigrateAIKeyBindings(t *testing.T) {
	// Saved pre-AI config: palette on ctrl+k, forwards on ctrl+p, no ai_overlay.
	cfg := DefaultKeyBindingConfig()
	cfg.CommandPalette = []string{"ctrl+k"}
	cfg.ForwardTab = []string{"ctrl+p"}
	migrateAIKeyBindings(&cfg, "linux")
	if cfg.CommandPalette[0] != "ctrl+p" {
		t.Fatalf("palette = %v", cfg.CommandPalette)
	}
	if cfg.ForwardTab[0] != "ctrl+shift+f" {
		t.Fatalf("forward = %v", cfg.ForwardTab)
	}
	if cfg.AIOverlay[0] != "ctrl+k" {
		t.Fatalf("ai = %v", cfg.AIOverlay)
	}

	// Windows migration uses the windows forwards default.
	cfg = DefaultKeyBindingConfig()
	cfg.CommandPalette = []string{"ctrl+k"}
	cfg.ForwardTab = []string{"ctrl+p"}
	migrateAIKeyBindings(&cfg, "windows")
	if cfg.ForwardTab[0] != "alt+shift+f" {
		t.Fatalf("windows forward = %v", cfg.ForwardTab)
	}

	// Custom bindings without conflicts stay untouched.
	cfg = DefaultKeyBindingConfig()
	cfg.CommandPalette = []string{"f1"}
	migrateAIKeyBindings(&cfg, "linux")
	if cfg.CommandPalette[0] != "f1" || cfg.ForwardTab[0] != "ctrl+shift+f" {
		t.Fatalf("custom cfg mangled: %v %v", cfg.CommandPalette, cfg.ForwardTab)
	}
}
