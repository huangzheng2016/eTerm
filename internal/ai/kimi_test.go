package ai

import (
	"os"
	"path/filepath"
	"testing"
)

const testKimiTOML = `
default_model = "free-tokens_kimi/kimi-k3-0829"

[providers.msh-public]
type = "kimi"
base_url = "https://api.example.com/v1"
api_key = "sk-public"

[providers.ds-red]
type = "openai"
base_url = "https://example.app.example.com/v1"
api_key = "sk-dsred"

[providers."managed:kimi-code"]
type = "kimi"
api_key = ""
base_url = "https://api.kimi.com/coding/v1"

[providers."managed:kimi-code".oauth]
storage = "file"
key = "oauth/kimi-code"

[providers.free-tokens_kimi]
type = "kimi"
base_url = "https://free-tokens.example.com/v1"
api_key = "sk-free"

[providers.free-tokens_kimi.source]
kind = "apiJson"
url = "https://free-tokens.example.com/v1/models/api.json"
apiKey = "source-key"

[providers.free-tokens_responses]
type = "openai_responses"
base_url = "https://free-tokens.example.com/v1"
api_key = "sk-resp"

[providers.anth]
type = "anthropic"
api_key = "sk-ant"
default_model = "claude-sonnet-4"

[models."free-tokens_kimi/kimi-k3-0829"]
provider = "free-tokens_kimi"
model = "kimi-k3-0829"
max_context_size = 1048576

[models."free-tokens_kimi/kimi-k3-highspeed"]
provider = "free-tokens_kimi"
model = "kimi-k3-highspeed"
max_context_size = 1048576

[models."kimi-code/kimi-for-coding"]
provider = "managed:kimi-code"
model = "kimi-for-coding"
max_context_size = 262144

[models."free-tokens_responses/gpt-5.5"]
provider = "free-tokens_responses"
model = "gpt-5.5"
max_context_size = 1050000
`

func writeKimiConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadKimiConfigMissingFile(t *testing.T) {
	cfg, err := LoadKimiConfig(filepath.Join(t.TempDir(), "nope", "config.toml"))
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if len(cfg.Providers) != 0 || len(cfg.Models) != 0 {
		t.Fatalf("missing file must return empty config: %+v", cfg)
	}
}

func TestLoadKimiConfigEmptyPath(t *testing.T) {
	cfg, err := LoadKimiConfig("")
	if err != nil || cfg == nil {
		t.Fatalf("empty path: cfg=%v err=%v", cfg, err)
	}
}

func TestLoadKimiConfigParses(t *testing.T) {
	cfg, err := LoadKimiConfig(writeKimiConfig(t, testKimiTOML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "free-tokens_kimi/kimi-k3-0829" {
		t.Fatalf("default_model: %q", cfg.DefaultModel)
	}
	if len(cfg.Providers) != 6 {
		t.Fatalf("providers: got %d, want 6", len(cfg.Providers))
	}
	p := cfg.Providers["free-tokens_kimi"]
	if p.Type != "kimi" || p.APIKey != "sk-free" || p.BaseURL != "https://free-tokens.example.com/v1" {
		t.Fatalf("free-tokens_kimi: %+v", p)
	}
	oauth := cfg.Providers["managed:kimi-code"]
	if oauth.OAuth == nil || oauth.OAuth.Key != "oauth/kimi-code" {
		t.Fatalf("managed oauth: %+v", oauth)
	}
	m := cfg.Models["free-tokens_kimi/kimi-k3-0829"]
	if m.Provider != "free-tokens_kimi" || m.Model != "kimi-k3-0829" || m.MaxContextSize != 1048576 {
		t.Fatalf("model alias: %+v", m)
	}
}

func TestLoadKimiConfigInvalidTOML(t *testing.T) {
	if _, err := LoadKimiConfig(writeKimiConfig(t, "not = [valid")); err == nil {
		t.Fatal("invalid TOML must error")
	}
}

func TestKimiConfigPathOverride(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", "/tmp/kimi-home-test")
	if got := KimiConfigPath(); got != "/tmp/kimi-home-test/config.toml" {
		t.Fatalf("KIMI_CODE_HOME override: %q", got)
	}
}

func TestImportKimi(t *testing.T) {
	cfg, err := LoadKimiConfig(writeKimiConfig(t, testKimiTOML))
	if err != nil {
		t.Fatal(err)
	}
	var s Store
	s.ImportKimi(cfg)

	wantProviders := map[string]string{
		"msh-public":       ProviderOpenAI,
		"ds-red":           ProviderOpenAI,
		"free-tokens_kimi": ProviderOpenAI,
		"anth":             ProviderClaude,
	}
	if len(s.Providers) != len(wantProviders) {
		t.Fatalf("imported providers: got %d, want %d (%+v)", len(s.Providers), len(wantProviders), s.Providers)
	}
	for _, p := range s.Providers {
		wantType, ok := wantProviders[p.Name]
		if !ok {
			t.Fatalf("unexpected provider imported: %s", p.Name)
		}
		if p.Type != wantType {
			t.Fatalf("provider %s type: got %s, want %s", p.Name, p.Type, wantType)
		}
		if p.Source != SourceKimi {
			t.Fatalf("provider %s source: %q", p.Name, p.Source)
		}
	}

	// Aliases for skipped providers (managed:kimi-code oauth, openai_responses) are dropped.
	if len(s.Models) != 2 {
		t.Fatalf("imported aliases: got %d, want 2 (%+v)", len(s.Models), s.Models)
	}
	for _, m := range s.Models {
		if m.Provider != "free-tokens_kimi" || m.MaxContextSize != 1048576 {
			t.Fatalf("alias: %+v", m)
		}
	}

	// default_provider absent: derived from the default_model alias.
	if s.ActiveProvider != "free-tokens_kimi" || s.ActiveModel != "free-tokens_kimi/kimi-k3-0829" {
		t.Fatalf("active: %s / %s", s.ActiveProvider, s.ActiveModel)
	}

	p, model, maxCtx, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "free-tokens_kimi" || model != "kimi-k3-0829" || maxCtx != 1048576 {
		t.Fatalf("resolve: %s / %s / %d", p.Name, model, maxCtx)
	}
}

func TestImportKimiDefaultProviderPresent(t *testing.T) {
	cfg, err := LoadKimiConfig(writeKimiConfig(t, `
default_provider = "anth"
default_model = "claude-sonnet-4"

[providers.anth]
type = "anthropic"
api_key = "sk-ant"
default_model = "claude-sonnet-4"
`))
	if err != nil {
		t.Fatal(err)
	}
	var s Store
	s.ImportKimi(cfg)
	if s.ActiveProvider != "anth" || s.ActiveModel != "claude-sonnet-4" {
		t.Fatalf("active: %s / %s", s.ActiveProvider, s.ActiveModel)
	}
	p, model, _, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != ProviderClaude || model != "claude-sonnet-4" {
		t.Fatalf("resolve: %s / %s", p.Type, model)
	}
}

func TestImportKimiUserProviderWins(t *testing.T) {
	cfg, err := LoadKimiConfig(writeKimiConfig(t, testKimiTOML))
	if err != nil {
		t.Fatal(err)
	}
	s := Store{Providers: []Provider{{Name: "ds-red", Type: ProviderClaude, APIKey: "user-key"}}}
	s.ImportKimi(cfg)
	p := s.Get("ds-red")
	if p.APIKey != "user-key" || p.Type != ProviderClaude || p.Source != "" {
		t.Fatalf("user provider must win: %+v", p)
	}
}

func TestStoreResolveFallbacks(t *testing.T) {
	var empty Store
	if _, _, _, err := empty.Resolve(); err == nil {
		t.Fatal("empty store must error")
	}

	s := Store{Providers: []Provider{{Name: "a", Type: ProviderOpenAI, APIKey: "k", DefaultModel: "gpt-x"}}}
	p, model, maxCtx, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "a" || model != "gpt-x" || maxCtx != 0 {
		t.Fatalf("fallback to first provider default model: %s / %s / %d", p.Name, model, maxCtx)
	}

	s.Models = []ModelAlias{{Alias: "gone", Provider: "missing", Model: "m"}}
	s.ActiveModel = "gone"
	if _, _, _, err := s.Resolve(); err == nil {
		t.Fatal("alias pointing at unknown provider must error")
	}

	if err := s.SetActive("nope", "m"); err == nil {
		t.Fatal("SetActive with unknown provider must error")
	}
	if err := s.SetActive("a", "gpt-y"); err != nil {
		t.Fatal(err)
	}
	_, model, _, err = s.Resolve()
	if err != nil || model != "gpt-y" {
		t.Fatalf("raw model id resolve: %s err=%v", model, err)
	}
}

func TestStoreUpsert(t *testing.T) {
	var s Store
	s.Upsert(Provider{Name: "a", Type: ProviderOpenAI, APIKey: "k1"})
	s.Upsert(Provider{Name: "a", Type: ProviderOpenAI, APIKey: "k2"})
	if len(s.Providers) != 1 || s.Providers[0].APIKey != "k2" {
		t.Fatalf("upsert: %+v", s.Providers)
	}
}
