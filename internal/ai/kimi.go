package ai

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type KimiConfig struct {
	DefaultProvider string                  `toml:"default_provider"`
	DefaultModel    string                  `toml:"default_model"`
	Providers       map[string]KimiProvider `toml:"providers"`
	Models          map[string]KimiModel    `toml:"models"`
}

type KimiProvider struct {
	Type         string     `toml:"type"`
	APIKey       string     `toml:"api_key"`
	BaseURL      string     `toml:"base_url"`
	DefaultModel string     `toml:"default_model"`
	OAuth        *KimiOAuth `toml:"oauth"`
}

type KimiOAuth struct {
	Storage   string `toml:"storage"`
	Key       string `toml:"key"`
	OAuthHost string `toml:"oauth_host"`
}

type KimiModel struct {
	Provider       string `toml:"provider"`
	Model          string `toml:"model"`
	MaxContextSize int    `toml:"max_context_size"`
}

func KimiConfigPath() string {
	if home := os.Getenv("KIMI_CODE_HOME"); home != "" {
		return filepath.Join(home, "config.toml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".kimi-code", "config.toml")
	}
	return ""
}

// LoadKimiConfig parses the kimi-code config.toml. A missing file is not an
// error: it returns an empty config.
func LoadKimiConfig(path string) (*KimiConfig, error) {
	cfg := &KimiConfig{}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ImportKimi merges kimi-code providers and model aliases into the store.
// Only providers with a plaintext api_key and a supported type are imported;
// oauth-based and unsupported-type providers are skipped. Existing entries
// (user-added) win on name conflict.
func (s *Store) ImportKimi(cfg *KimiConfig) {
	for name, kp := range cfg.Providers {
		if kp.APIKey == "" {
			continue
		}
		typ, ok := normalizeProviderType(kp.Type)
		if !ok || s.Get(name) != nil {
			continue
		}
		s.Providers = append(s.Providers, Provider{
			Name:         name,
			Type:         typ,
			APIKey:       kp.APIKey,
			BaseURL:      kp.BaseURL,
			DefaultModel: kp.DefaultModel,
			Source:       SourceKimi,
		})
	}
	for alias, km := range cfg.Models {
		if s.Get(km.Provider) == nil || s.modelAlias(alias) != nil {
			continue
		}
		s.Models = append(s.Models, ModelAlias{
			Alias:          alias,
			Provider:       km.Provider,
			Model:          km.Model,
			MaxContextSize: km.MaxContextSize,
		})
	}
	if s.ActiveProvider != "" {
		return
	}
	// default_provider may be absent; default_model then implies the provider
	// through its model alias (or its "provider/model" prefix).
	provider := cfg.DefaultProvider
	if provider == "" && cfg.DefaultModel != "" {
		if m := s.modelAlias(cfg.DefaultModel); m != nil {
			provider = m.Provider
		} else if prefix, _, ok := strings.Cut(cfg.DefaultModel, "/"); ok {
			provider = prefix
		}
	}
	if provider != "" && s.Get(provider) != nil {
		s.ActiveProvider = provider
		s.ActiveModel = cfg.DefaultModel
	}
}

func (s *Store) modelAlias(alias string) *ModelAlias {
	for i := range s.Models {
		if s.Models[i].Alias == alias {
			return &s.Models[i]
		}
	}
	return nil
}

// normalizeProviderType maps kimi-code provider types to eTerm provider types.
func normalizeProviderType(t string) (string, bool) {
	switch t {
	case "kimi", "openai":
		return ProviderOpenAI, true
	case "anthropic", "claude":
		return ProviderClaude, true
	default:
		return "", false
	}
}
