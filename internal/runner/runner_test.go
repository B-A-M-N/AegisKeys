package runner

import (
	"errors"
	"strings"
	"testing"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
)

func TestResolveEnv(t *testing.T) {
	prov := &provider.Provider{
		Slug:       "openrouter",
		EnvVar:     "OPENROUTER_API_KEY",
		ExtraEnv:   map[string]string{"OPENAI_BASE_URL": "https://openrouter.ai/api/v1"},
		AuthHeader: "Authorization: Bearer ${KEY}",
	}
	prof := &profile.Profile{
		Name:         "or-main",
		ProviderSlug: "openrouter",
		KeyID:        "key_1",
		Env:          map[string]string{"CUSTOM": "val"},
	}

	r, err := ResolveEnv(prof, prov, "sk-secret-value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Provider primary var.
	if r.EnvVars["OPENROUTER_API_KEY"] != "sk-secret-value" {
		t.Errorf("OPENROUTER_API_KEY = %q", r.EnvVars["OPENROUTER_API_KEY"])
	}
	// Extra env (base URL).
	if r.EnvVars["OPENAI_BASE_URL"] != "https://openrouter.ai/api/v1" {
		t.Errorf("OPENAI_BASE_URL = %q", r.EnvVars["OPENAI_BASE_URL"])
	}
	// Profile override wins.
	if r.EnvVars["CUSTOM"] != "val" {
		t.Errorf("CUSTOM = %q", r.EnvVars["CUSTOM"])
	}
}

func TestResolveEnvLocalProvider(t *testing.T) {
	// Local providers (Ollama) have no primary env var.
	prov := &provider.Provider{Slug: "ollama", ExtraEnv: map[string]string{"OPENAI_BASE_URL": "http://localhost:11434/v1"}}
	prof := &profile.Profile{Name: "local", ProviderSlug: "ollama", KeyID: "key_1"}
	r, err := ResolveEnv(prof, prov, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.EnvVars["OPENAI_BASE_URL"]; !ok {
		t.Error("expected OPENAI_BASE_URL for local provider")
	}
	// No key var should be injected.
	for k := range r.EnvVars {
		if k == "OPENAI_BASE_URL" {
			continue
		}
		t.Errorf("unexpected key %q in local provider env", k)
	}
}

func TestResolveEnv_NilGuard(t *testing.T) {
	_, err := ResolveEnv(nil, &provider.Provider{}, "secret")
	if err == nil {
		t.Error("expected error for nil profile")
	}
	_, err = ResolveEnv(&profile.Profile{}, nil, "secret")
	if err == nil {
		t.Error("expected error for nil provider")
	}
}

func TestMasked(t *testing.T) {
	prov := &provider.Provider{Slug: "openai", EnvVar: "OPENAI_API_KEY"}
	prof := &profile.Profile{Name: "p", ProviderSlug: "openai", KeyID: "k"}
	r, err := ResolveEnv(prof, prov, "sk-1234567890")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := r.Masked()
	if m["OPENAI_API_KEY"] != "sk-1...7890" {
		t.Errorf("masked key = %q, want sk-1...7890", m["OPENAI_API_KEY"])
	}
}

func TestBuildEnvString(t *testing.T) {
	redacted := BuildEnvString(map[string]string{"OPENAI_API_KEY": "sk-secret"}, true)
	if redacted != "OPENAI_API_KEY=<redacted>" {
		t.Errorf("redacted = %q", redacted)
	}
	full := BuildEnvString(map[string]string{"OPENAI_API_KEY": "sk-secret"}, false)
	if full != "OPENAI_API_KEY=sk-secret" {
		t.Errorf("full = %q", full)
	}
}

func TestMergedEnv_NoDuplicateKeys(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/root", "OPENAI_API_KEY=old"}
	overlay := map[string]string{"OPENAI_API_KEY": "new", "EXTRA": "val"}
	merged := mergedEnv(base, overlay)
	seen := map[string]string{}
	for _, kv := range merged {
		k, v, _ := strings.Cut(kv, "=")
		if prev, ok := seen[k]; ok {
			t.Errorf("duplicate key %s: %q and %q", k, prev, v)
		}
		seen[k] = v
	}
	if seen["OPENAI_API_KEY"] != "new" {
		t.Errorf("overlay should win, got %q", seen["OPENAI_API_KEY"])
	}
	if seen["PATH"] != "/usr/bin" {
		t.Errorf("base should be preserved, got %q", seen["PATH"])
	}
	if seen["EXTRA"] != "val" {
		t.Errorf("new key should be added, got %q", seen["EXTRA"])
	}
}

func TestExitError_Message(t *testing.T) {
	e := &ExitError{Code: 42, Err: errors.New("boom")}
	if !strings.Contains(e.Error(), "42") {
		t.Errorf("ExitError message should contain code: %q", e.Error())
	}
}
