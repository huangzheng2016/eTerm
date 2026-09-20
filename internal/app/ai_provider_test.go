package app

import (
	"strings"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/ai"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
)

func TestBridgeUpdateDeletePersist(t *testing.T) {
	database := aiTestDB(t)
	mk := security.NewMasterKeyManager(nil, nil, 0)
	mk.Setup([]byte("pw"))

	bridge := &aiBridge{store: &ai.Store{}, db: database, mk: mk}
	bridge.store.Upsert(ai.Provider{Name: "kimi-src", Type: ai.ProviderOpenAI, APIKey: "k", Source: ai.SourceKimi})
	bridge.Add(aiview.Provider{Name: "mine", Type: "openai", BaseURL: "https://a", APIKey: "sk-1", Model: "gpt-5"})
	bridge.Switch("mine", "gpt-5")

	if err := bridge.Update("kimi-src", aiview.Provider{Name: "kimi-src"}); err == nil {
		t.Fatal("kimi provider update accepted")
	}
	if err := bridge.Update("mine", aiview.Provider{Name: "renamed", Type: "openai", Model: "gpt-5-mini"}); err != nil {
		t.Fatal(err)
	}

	loaded := loadAIStore(database, mk)
	p := loaded.Get("renamed")
	if p == nil || p.APIKey != "sk-1" || p.DefaultModel != "gpt-5-mini" {
		t.Fatalf("persisted provider = %+v", p)
	}
	if loaded.ActiveProvider != "renamed" {
		t.Fatalf("persisted active = %q", loaded.ActiveProvider)
	}

	if err := bridge.Delete("kimi-src"); err == nil {
		t.Fatal("kimi provider delete accepted")
	}
	if err := bridge.Delete("renamed"); err != nil {
		t.Fatal(err)
	}

	loaded = loadAIStore(database, mk)
	if loaded.Get("renamed") != nil {
		t.Fatal("deleted provider still persisted")
	}
	raw, err := db.GetSetting(database, aiActiveSettingKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"provider":""`) {
		t.Fatalf("persisted active after delete = %s", raw)
	}
}
