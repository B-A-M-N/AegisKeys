package provider

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestModelRefreshURL_DeclaredURLs(t *testing.T) {
	cases := []struct {
		name string
		p    Provider
		want string
	}{
		{
			name: "catalog.refresh_url wins",
			p: Provider{
				Catalog:     ModelCatalogSpec{RefreshURL: "https://x.com/catalog"},
				ModelPolicy: ModelCatalogPolicy{RefreshURL: "https://x.com/policy"},
				Endpoints:   EndpointSpec{ModelsURL: "https://x.com/models"},
			},
			want: "https://x.com/catalog",
		},
		{
			name: "model_policy.refresh_url second",
			p: Provider{
				ModelPolicy: ModelCatalogPolicy{RefreshURL: "https://x.com/policy"},
				Endpoints:   EndpointSpec{ModelsURL: "https://x.com/models"},
			},
			want: "https://x.com/policy",
		},
		{
			name: "endpoints.models_url third",
			p: Provider{
				Endpoints: EndpointSpec{ModelsURL: "https://x.com/models"},
			},
			want: "https://x.com/models",
		},
		{
			name: "derived from base url for openai",
			p: Provider{
				Compatibility: CompatOpenAI,
				BaseURL:       "https://api.example.com/v1",
			},
			want: "https://api.example.com/v1/models",
		},
		{
			name: "no url for unknown compat without explicit",
			p: Provider{
				Compatibility: CompatAnthropic,
				BaseURL:       "https://api.anthropic.com",
			},
			want: "",
		},
		{
			name: "local loopback not derived",
			p: Provider{
				Compatibility: CompatLocal,
				BaseURL:       "http://localhost:11434/v1",
			},
			want: "",
		},
		{
			name: "empty provider",
			p:    Provider{},
			want: "",
		},
	}
	for _, tc := range cases {
		tc.p.Normalize()
		got := tc.p.ModelRefreshURL()
		if got != tc.want {
			t.Errorf("%s: ModelRefreshURL = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCanRefreshModels(t *testing.T) {
	cases := []struct {
		name string
		p    Provider
		want bool
	}{
		{
			name: "local provider always yes",
			p:    Provider{Slug: "ollama", Compatibility: CompatLocal, BaseURL: "http://localhost:11434/v1"},
			want: true,
		},
		{
			name: "catalog local source always yes",
			p:    Provider{Slug: "test", Catalog: ModelCatalogSpec{Source: "local"}},
			want: true,
		},
		{
			name: "explicit refresh url yes",
			p:    Provider{Slug: "test", Catalog: ModelCatalogSpec{RefreshURL: "https://x.com/models"}},
			want: true,
		},
		{
			name: "static with no url no",
			p:    Provider{Slug: "test", ModelPolicy: ModelCatalogPolicy{Source: "static"}},
			want: false,
		},
		{
			name: "static with refresh url yes",
			p: Provider{
				Slug:          "test",
				ModelPolicy:   ModelCatalogPolicy{Source: "static"},
				Catalog:       ModelCatalogSpec{RefreshURL: "https://x.com/models"},
				Compatibility: CompatOpenAI,
				BaseURL:       "https://api.example.com/v1",
			},
			want: true,
		},
		{
			name: "dynamic provider yes",
			p: Provider{
				Slug:          "openrouter",
				ModelPolicy:   ModelCatalogPolicy{Source: "dynamic", RefreshURL: "https://openrouter.ai/api/v1/models"},
				Compatibility: CompatOpenAI,
				BaseURL:       "https://openrouter.ai/api/v1",
			},
			want: true,
		},
	}
	for _, tc := range cases {
		tc.p.Normalize()
		if got := tc.p.CanRefreshModels(); got != tc.want {
			t.Errorf("%s: CanRefreshModels = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSetStaticModels_DedupAndMarksStatic(t *testing.T) {
	r := NewRegistry()
	_ = r.Add(Provider{Slug: "test", Name: "Test", Compatibility: CompatOpenAI, BaseURL: "https://api.example.com", Auth: AuthSpec{Type: "bearer", EnvVar: "TEST_KEY"}})

	models := []ProviderModel{
		{ID: "model-a", Name: "Model A"},
		{ID: "model-b", Name: "Model B"},
		{ID: "model-a", Name: "Duplicate A"},    // duplicate
		{ID: "", Name: "Empty"},                 // empty id
		{ID: "  model-c  ", Name: "Whitespace"}, // whitespace
	}

	if err := r.SetStaticModels("test", models); err != nil {
		t.Fatalf("SetStaticModels: %v", err)
	}

	p := r.Find("test")
	if len(p.Models) != 3 {
		t.Errorf("expected 3 models, got %d", len(p.Models))
	}
	seen := map[string]bool{}
	for _, m := range p.Models {
		if !m.Static {
			t.Errorf("model %q not marked Static", m.ID)
		}
		if seen[m.ID] {
			t.Errorf("duplicate model %q after dedup", m.ID)
		}
		seen[m.ID] = true
		if strings.TrimSpace(m.ID) != m.ID {
			t.Errorf("model %q not trimmed", m.ID)
		}
	}
	if p.ModelPolicy.Source != ModelSourceStatic {
		t.Errorf("ModelPolicy.Source = %q, want static", p.ModelPolicy.Source)
	}
}

func TestSetStaticModels_ProviderNotFound(t *testing.T) {
	r := NewRegistry()
	err := r.SetStaticModels("missing", []ProviderModel{{ID: "x"}})
	if err == nil {
		t.Error("expected error for missing provider")
	}
}

func TestModelCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cache := ModelCache{
		ProviderSlug: "openrouter",
		FetchedAt:    time.Now().UTC().Truncate(time.Second),
		RefreshURL:   "https://openrouter.ai/api/v1/models",
		Models: []ProviderModel{
			{ID: "anthropic/claude-sonnet-4-5", Name: "Claude Sonnet 4.5"},
			{ID: "deepseek/deepseek-chat", Name: "DeepSeek Chat"},
		},
	}

	if err := SaveModelCache(dir, cache); err != nil {
		t.Fatalf("SaveModelCache: %v", err)
	}

	loaded, err := LoadModelCache(dir, "openrouter")
	if err != nil {
		t.Fatalf("LoadModelCache: %v", err)
	}

	if len(loaded.Models) != 2 {
		t.Errorf("loaded %d models, want 2", len(loaded.Models))
	}
	if loaded.FetchedAt.Unix() != cache.FetchedAt.Unix() {
		t.Errorf("FetchedAt mismatch: %v vs %v", loaded.FetchedAt, cache.FetchedAt)
	}
	if loaded.RefreshURL != cache.RefreshURL {
		t.Errorf("RefreshURL = %q, want %q", loaded.RefreshURL, cache.RefreshURL)
	}

	// No secrets in the cache file — verify raw JSON contains no key material.
	raw, err := readTestFile(t, ModelCachePath(dir, "openrouter"))
	if err != nil {
		t.Fatalf("reading cache file: %v", err)
	}
	if strings.Contains(raw, "sk-") || strings.Contains(raw, "api_key") {
		t.Error("model cache contains secret-looking material")
	}
}

func TestModelCache_MissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	cache, err := LoadModelCache(dir, "openrouter")
	if err != nil {
		t.Fatalf("LoadModelCache: %v", err)
	}
	if cache.Models != nil {
		t.Errorf("expected nil Models for missing cache, got %v", cache.Models)
	}
}

func TestModelCache_IsCacheExpired(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		cache   ModelCache
		ttl     int
		expired bool
	}{
		{
			name:    "fresh within ttl",
			cache:   ModelCache{FetchedAt: now.Add(-5 * time.Minute)},
			ttl:     30,
			expired: false,
		},
		{
			name:    "expired past ttl",
			cache:   ModelCache{FetchedAt: now.Add(-60 * time.Minute)},
			ttl:     30,
			expired: true,
		},
		{
			name:    "zero ttl never expires",
			cache:   ModelCache{FetchedAt: now.Add(-24 * time.Hour)},
			ttl:     0,
			expired: false,
		},
		{
			name:    "negative ttl never expires",
			cache:   ModelCache{FetchedAt: now.Add(-24 * time.Hour)},
			ttl:     -1,
			expired: false,
		},
	}
	for _, tc := range cases {
		if got := tc.cache.IsCacheExpired(tc.ttl); got != tc.expired {
			t.Errorf("%s: IsCacheExpired = %v, want %v", tc.name, got, tc.expired)
		}
	}
}

func TestNewModelCache(t *testing.T) {
	p := Provider{Slug: "test", Catalog: ModelCatalogSpec{RefreshURL: "https://x.com/models"}}
	models := []ProviderModel{{ID: "a"}, {ID: "b"}}
	cache := NewModelCache(p, models)
	if cache.ProviderSlug != "test" {
		t.Errorf("slug = %q", cache.ProviderSlug)
	}
	if cache.RefreshURL != "https://x.com/models" {
		t.Errorf("RefreshURL = %q", cache.RefreshURL)
	}
	if len(cache.Models) != 2 {
		t.Errorf("len = %d", len(cache.Models))
	}
	if cache.FetchedAt.IsZero() {
		t.Error("FetchedAt not set")
	}
}

// readTestFile is a tiny helper so the test file doesn't need os import.
func readTestFile(t *testing.T, path string) (string, error) {
	t.Helper()
	data, err := osReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// indirection so we don't import os at the top just for this.
var osReadFile = os.ReadFile

// Silence unused import for context when tests don't all fire.
var _ = context.Background
