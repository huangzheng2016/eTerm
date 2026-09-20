package ai

import "testing"

func TestStoreUpdateRenameKeepsKeyAndRefs(t *testing.T) {
	s := &Store{}
	s.Upsert(Provider{Name: "mine", Type: ProviderOpenAI, APIKey: "sk-1", BaseURL: "https://a", DefaultModel: "gpt-5"})
	s.Models = append(s.Models, ModelAlias{Alias: "fast", Provider: "mine", Model: "gpt-5-mini"})
	if err := s.SetActive("mine", "gpt-5"); err != nil {
		t.Fatal(err)
	}

	err := s.Update("mine", Provider{Name: "renamed", Type: ProviderClaude, BaseURL: "https://b", DefaultModel: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	p := s.Get("renamed")
	if p == nil || p.APIKey != "sk-1" || p.Type != ProviderClaude || p.BaseURL != "https://b" {
		t.Fatalf("updated provider = %+v", p)
	}
	if s.Get("mine") != nil {
		t.Fatal("old name still present")
	}
	if s.Models[0].Provider != "renamed" {
		t.Fatalf("alias provider = %q", s.Models[0].Provider)
	}
	if s.ActiveProvider != "renamed" {
		t.Fatalf("active = %q", s.ActiveProvider)
	}
}

func TestStoreUpdateRejectsKimiAndConflict(t *testing.T) {
	s := &Store{}
	s.Upsert(Provider{Name: "kimi-src", Type: ProviderOpenAI, APIKey: "k", Source: SourceKimi})
	s.Upsert(Provider{Name: "other", Type: ProviderOpenAI, APIKey: "o"})
	s.Upsert(Provider{Name: "mine", Type: ProviderOpenAI, APIKey: "m"})

	if err := s.Update("kimi-src", Provider{Name: "kimi-src"}); err == nil {
		t.Fatal("kimi provider update accepted")
	}
	if err := s.Update("mine", Provider{Name: "other"}); err == nil {
		t.Fatal("rename to existing name accepted")
	}
	if err := s.Update("missing", Provider{Name: "x"}); err == nil {
		t.Fatal("unknown provider update accepted")
	}
}

func TestStoreDeleteRemovesAliasesAndActive(t *testing.T) {
	s := &Store{}
	s.Upsert(Provider{Name: "mine", Type: ProviderOpenAI, APIKey: "m"})
	s.Upsert(Provider{Name: "keep", Type: ProviderOpenAI, APIKey: "k"})
	s.Models = append(s.Models,
		ModelAlias{Alias: "a1", Provider: "mine", Model: "m1"},
		ModelAlias{Alias: "a2", Provider: "keep", Model: "m2"})
	if err := s.SetActive("mine", "a1"); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete("mine"); err != nil {
		t.Fatal(err)
	}
	if s.Get("mine") != nil {
		t.Fatal("provider not deleted")
	}
	if len(s.Models) != 1 || s.Models[0].Alias != "a2" {
		t.Fatalf("aliases = %+v", s.Models)
	}
	if s.ActiveProvider != "" || s.ActiveModel != "" {
		t.Fatalf("active = %q/%q", s.ActiveProvider, s.ActiveModel)
	}

	if err := s.Delete("mine"); err == nil {
		t.Fatal("delete of missing provider accepted")
	}
	s.Providers[0].Source = SourceKimi
	if err := s.Delete("keep"); err == nil {
		t.Fatal("kimi provider delete accepted")
	}
}
