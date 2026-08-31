package ai

import "fmt"

const (
	ProviderOpenAI = "openai"
	ProviderClaude = "claude"
)

const SourceKimi = "kimi"

type Provider struct {
	Name         string `json:"name"`
	Type         string `json:"type"` // openai | claude
	APIKey       string `json:"api_key"`
	BaseURL      string `json:"base_url,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
	Source       string `json:"source,omitempty"` // "kimi" when imported from kimi-code config, empty for user-added
}

type ModelAlias struct {
	Alias          string `json:"alias"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	MaxContextSize int    `json:"max_context_size,omitempty"`
}

type Store struct {
	Providers      []Provider   `json:"providers"`
	Models         []ModelAlias `json:"models,omitempty"`
	ActiveProvider string       `json:"active_provider,omitempty"`
	ActiveModel    string       `json:"active_model,omitempty"`
}

func (s *Store) Get(name string) *Provider {
	for i := range s.Providers {
		if s.Providers[i].Name == name {
			return &s.Providers[i]
		}
	}
	return nil
}

func (s *Store) Upsert(p Provider) {
	if existing := s.Get(p.Name); existing != nil {
		*existing = p
		return
	}
	s.Providers = append(s.Providers, p)
}

func (s *Store) SetActive(provider, model string) error {
	if s.Get(provider) == nil {
		return fmt.Errorf("unknown provider: %s", provider)
	}
	s.ActiveProvider = provider
	s.ActiveModel = model
	return nil
}

// Resolve returns the effective provider, model id and max context size.
// ActiveModel may name a [models] alias or a raw model id.
func (s *Store) Resolve() (*Provider, string, int, error) {
	for _, m := range s.Models {
		if m.Alias == s.ActiveModel && s.ActiveModel != "" {
			p := s.Get(m.Provider)
			if p == nil {
				return nil, "", 0, fmt.Errorf("model alias %q points at unknown provider %q", m.Alias, m.Provider)
			}
			return p, m.Model, m.MaxContextSize, nil
		}
	}
	p := s.Get(s.ActiveProvider)
	if p == nil && len(s.Providers) > 0 {
		p = &s.Providers[0]
	}
	if p == nil {
		return nil, "", 0, fmt.Errorf("no provider configured")
	}
	model := s.ActiveModel
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return nil, "", 0, fmt.Errorf("no model selected for provider %q", p.Name)
	}
	return p, model, 0, nil
}
