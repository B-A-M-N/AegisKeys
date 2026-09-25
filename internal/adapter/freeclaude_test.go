package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

func TestReadFreeCodeCapabilitiesCachesByBinaryIdentity(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "free-code")
	counter := filepath.Join(tmp, "count")
	script := fmt.Sprintf("#!/bin/sh\nprintf x >> %q\nprintf '%%s\\n' '{\"openai_compatible_chat_completions\":true}'\n", counter)
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	freeCodeCapabilityCache.Lock()
	freeCodeCapabilityCache.values = make(map[freeCodeCapabilityCacheKey]freeCodeCapabilities)
	freeCodeCapabilityCache.Unlock()
	command := ResolvedCommand{Executable: path, ResolvedTarget: path}
	for range 2 {
		if _, err := readFreeCodeCapabilities(context.Background(), command); err != nil {
			t.Fatalf("read capabilities: %v", err)
		}
	}
	contents, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(contents), "x"); got != 1 {
		t.Fatalf("capability probe ran %d times, want 1", got)
	}

	if err := os.Chtimes(path, time.Now(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := readFreeCodeCapabilities(context.Background(), command); err != nil {
		t.Fatalf("re-read capabilities after replacement: %v", err)
	}
	contents, err = os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(contents), "x"); got != 2 {
		t.Fatalf("capability probe after binary change ran %d times, want 2", got)
	}
}

func writeFreeCodeCapabilitiesScript(t *testing.T, name, capabilityJSON string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = capabilities ] && [ \"$2\" = --json ]; then\n  printf '%%s\\n' '%s'\n  exit 0\nfi\nexit 2\n", capabilityJSON)
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestFreeClaudeAdapter_Registers verifies free-claude is discoverable and
// wire-compatible with Claude Code semantics.
func TestFreeClaudeAdapter_Registers(t *testing.T) {
	r := NewRegistry()
	a, ok := r.Get("free-claude")
	if !ok {
		t.Fatal("free-claude adapter not registered")
	}
	if a.DefaultCommand() != "free-code" {
		t.Errorf("expected default command free-code, got %q", a.DefaultCommand())
	}
	if a.Contract().SupportConfidence != ConfidenceExperimental {
		t.Errorf("expected experimental confidence, got %q", a.Contract().SupportConfidence)
	}
}

// TestFreeClaudeAdapter_Render verifies it injects ANTHROPIC_API_KEY and launches
// the free-code binary for an Anthropic provider.
func TestFreeClaudeAdapter_Render(t *testing.T) {
	a := FreeClaudeAdapter{}
	prov := provider.Provider{
		Slug:          "anthropic",
		Name:          "Anthropic",
		EnvVar:        "ANTHROPIC_API_KEY",
		BaseURL:       "https://api.anthropic.com",
		Compatibility: provider.CompatAnthropic,
	}
	p := profile.Profile{
		Name:         "fc-test",
		ProviderSlug: "anthropic",
		Target:       profile.TargetConfig{App: "free-claude", RenderMode: profile.RenderEnv},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "claude-sonnet-4-5"}},
	}
	key := testAPIKey("sk-ant-test123")
	strat, err := a.Render(p, prov, key)
	if err != nil {
		// Render returns an error when free-code is not installed anywhere
		// (PATH, ~/.local/bin, ~/.cargo/bin, /usr/local/bin). Skip the rest
		// of the assertions in that case — they require the binary present.
		if findFreeCodeBinary() == "" {
			t.Skip("free-code not installed; skipping render assertions")
		}
		t.Fatalf("render error: %v", err)
	}
	if !strings.HasPrefix(strat.Plan.Command, "/") {
		t.Errorf("expected absolute path to free-code binary, got %q", strat.Plan.Command)
	}
	if strat.Plan.Env["ANTHROPIC_API_KEY"] != "sk-ant-test123" {
		t.Errorf("expected ANTHROPIC_API_KEY injected, got %q", strat.Plan.Env["ANTHROPIC_API_KEY"])
	}
	if _, leaks := strat.Plan.Env["sk-ant-test123"]; leaks {
		t.Error("raw secret leaked as a key name")
	}
}

func TestFreeClaudeAdapter_OpenAICompatibleUsesNativeOpenAITransport(t *testing.T) {
	a := FreeClaudeAdapter{}
	prov := provider.Provider{
		Slug: "freeinference", Name: "FreeInference", EnvVar: "FREEINFERENCE_API_KEY",
		BaseURL: "https://freeinference.org/v1", Compatibility: provider.CompatOpenAI,
	}
	p := profile.Profile{Name: "fc-fi", ProviderSlug: "freeinference", Target: profile.TargetConfig{App: "free-claude"}}
	key := testAPIKey("fi-test-key")
	strat, err := a.Render(p, prov, key)
	if err != nil {
		if findFreeCodeBinary() == "" {
			t.Skip("free-code not installed; skipping render assertions")
		}
		t.Fatalf("render error: %v", err)
	}
	if got := strat.Plan.Env["CLAUDE_CODE_USE_OPENAI_COMPATIBLE"]; got != "1" {
		t.Errorf("CLAUDE_CODE_USE_OPENAI_COMPATIBLE = %q, want 1", got)
	}
	if got := strat.Plan.Env["OPENAI_BASE_URL"]; got != "https://freeinference.org/v1" {
		t.Errorf("OPENAI_BASE_URL = %q", got)
	}
	if got := strat.Plan.Env["OPENAI_API_KEY"]; got != "fi-test-key" {
		t.Errorf("OPENAI_API_KEY = %q", got)
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "FREEINFERENCE_API_KEY"} {
		if _, exists := strat.Plan.Env[name]; exists {
			t.Errorf("%s must be absent from OpenAI-compatible FreeClaude launch", name)
		}
	}
	if strat.Bridge != nil {
		t.Error("native OpenAI-compatible FreeClaude launch must not use a protocol bridge")
	}
	if strat.Plan.Transport != TransportOpenAIChat {
		t.Errorf("transport = %q, want %q", strat.Plan.Transport, TransportOpenAIChat)
	}
}

func TestFreeClaudeAdapter_ExplicitCommandOverridesDetectedBinary(t *testing.T) {
	capabilities := `{"build_time":"2026-07-22T19:16:00Z","executable":"/tmp/free-code-override","openai_compatible_chat_completions":true}`
	override := writeFreeCodeCapabilitiesScript(t, "override-free-code", capabilities)
	detectedDir := t.TempDir()
	detected := filepath.Join(detectedDir, "free-code")
	if err := os.WriteFile(detected, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", detectedDir)

	prov := provider.Provider{Slug: "freeinference", Name: "FreeInference", EnvVar: "FREEINFERENCE_API_KEY", BaseURL: "https://freeinference.org/v1", Compatibility: provider.CompatOpenAI}
	p := profile.Profile{Name: "override", ProviderSlug: prov.Slug, Target: profile.TargetConfig{App: "free-claude", Command: override}}
	strategy, err := (FreeClaudeAdapter{}).Render(p, prov, testAPIKey("fi-test-key"))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strategy.Plan.Command != override {
		t.Errorf("command = %q, want explicit override %q", strategy.Plan.Command, override)
	}
	if strategy.Plan.CommandResolution == nil || strategy.Plan.CommandResolution.Requested != override {
		t.Fatalf("command resolution = %#v, want requested override", strategy.Plan.CommandResolution)
	}
	if strategy.Plan.BuildTime != "2026-07-22T19:16:00Z" {
		t.Errorf("build time = %q", strategy.Plan.BuildTime)
	}
	if !strings.Contains(strings.Join(strategy.Plan.Preview, "\n"), "Transport: OpenAI Chat Completions") {
		t.Errorf("preview lacks sanitized transport details: %v", strategy.Plan.Preview)
	}
}

func TestFreeClaudeAdapter_RejectsBinaryWithoutOpenAICompatibleCapability(t *testing.T) {
	binary := writeFreeCodeCapabilitiesScript(t, "old-free-code", `{"openai_compatible_chat_completions":false}`)
	prov := provider.Provider{Slug: "freeinference", Name: "FreeInference", EnvVar: "FREEINFERENCE_API_KEY", BaseURL: "https://freeinference.org/v1", Compatibility: provider.CompatOpenAI}
	p := profile.Profile{Name: "old", ProviderSlug: prov.Slug, Target: profile.TargetConfig{App: "free-claude", Command: binary}}
	_, err := (FreeClaudeAdapter{}).Render(p, prov, testAPIKey("fi-test-key"))
	if err == nil || !strings.Contains(err.Error(), "does not support the generic OpenAI-compatible transport") {
		t.Fatalf("Render() error = %v, want capability failure", err)
	}
}

func TestFreeClaudeAdapter_UsesNativeTransportForOpenCodeGoChatOnlyModel(t *testing.T) {
	a := FreeClaudeAdapter{}
	p := profile.Profile{Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "opencode-go/deepseek-v4-flash"}}}
	prov := provider.Provider{Slug: "opencode-go", Name: "OpenCode Go", BaseURL: "https://opencode.ai/zen/go/v1", Compatibility: provider.CompatOpenAI}
	if _, err := a.Validate(p, prov); err != nil {
		t.Fatalf("expected chat-only OpenCode Go model to be bridgeable: %v", err)
	}
	p.Target.Command = writeFreeCodeCapabilitiesScript(t, "free-code-native", `{"openai_compatible_chat_completions":true}`)
	strat, err := a.Render(p, prov, &secret.SecretRecord{Secret: "test-go-key"})
	if err != nil || strat.Bridge != nil {
		t.Fatalf("expected native strategy without bridge, got %#v, %v", strat, err)
	}
	if got := strat.Plan.Env["OPENAI_API_KEY"]; got != "test-go-key" {
		t.Fatalf("native transport should inject upstream key, got %q", got)
	}

	p.Models.Main.ID = "opencode-go/minimax-m3"
	if _, err := a.Validate(p, prov); err != nil {
		t.Fatalf("expected Messages-capable OpenCode Go model to validate: %v", err)
	}
}

// TestFreeInferenceAnthropicUsesAuthToken verifies that FreeInference's
// Anthropic endpoint receives the key via ANTHROPIC_AUTH_TOKEN (not
// ANTHROPIC_API_KEY), matching FreeInference's Claude Code docs. Otherwise the
// client treats the non-sk-ant key as a real Anthropic key and the gateway
// rejects it, prompting for interactive auth.
func TestFreeInferenceAnthropicUsesAuthToken(t *testing.T) {
	fiProv := provider.Provider{
		Slug: "freeinference-anthropic", Name: "FreeInference (Anthropic)",
		EnvVar: "FREEINFERENCE_API_KEY", BaseURL: "https://freeinference.org/anthropic",
		Compatibility: provider.CompatAnthropic,
	}
	key := testAPIKey("fi-test-key")

	// Claude Code adapter.
	cc := ClaudeCodeAdapter{}
	ccStrat, err := cc.Render(profile.Profile{Name: "cc-fi", ProviderSlug: fiProv.Slug, Target: profile.TargetConfig{App: "claude"}}, fiProv, key)
	if err == nil {
		if ccStrat.Plan.Env["ANTHROPIC_AUTH_TOKEN"] != "fi-test-key" {
			t.Errorf("Claude Code: expected ANTHROPIC_AUTH_TOKEN=fi-test-key, got %q", ccStrat.Plan.Env["ANTHROPIC_AUTH_TOKEN"])
		}
		if ccStrat.Plan.Env["ANTHROPIC_API_KEY"] != "" {
			t.Errorf("Claude Code: expected ANTHROPIC_API_KEY empty for FreeInference, got %q", ccStrat.Plan.Env["ANTHROPIC_API_KEY"])
		}
		if got := ccStrat.Plan.Env["ANTHROPIC_BASE_URL"]; got != "https://freeinference.org/anthropic" {
			t.Errorf("Claude Code: ANTHROPIC_BASE_URL = %q, want https://freeinference.org/anthropic", got)
		}
	}

	// FreeClaude adapter.
	fc := FreeClaudeAdapter{}
	fcStrat, err := fc.Render(profile.Profile{Name: "fc-fi", ProviderSlug: fiProv.Slug, Target: profile.TargetConfig{App: "free-claude"}}, fiProv, key)
	if err != nil {
		if findFreeCodeBinary() == "" {
			t.Skip("free-code not installed; skipping FreeClaude render assertions")
		}
		t.Fatalf("FreeClaude render error: %v", err)
	}
	if fcStrat.Plan.Env["ANTHROPIC_AUTH_TOKEN"] != "fi-test-key" {
		t.Errorf("FreeClaude: expected ANTHROPIC_AUTH_TOKEN=fi-test-key, got %q", fcStrat.Plan.Env["ANTHROPIC_AUTH_TOKEN"])
	}
	if fcStrat.Plan.Env["ANTHROPIC_API_KEY"] != "" {
		t.Errorf("FreeClaude: expected ANTHROPIC_API_KEY empty for FreeInference, got %q", fcStrat.Plan.Env["ANTHROPIC_API_KEY"])
	}
}

func TestClaudeCodeAdapter_BridgesFreeInferenceOpenAI(t *testing.T) {
	prov := provider.Provider{
		Slug: "freeinference", Name: "FreeInference",
		EnvVar: "FREEINFERENCE_API_KEY", BaseURL: "https://freeinference.org/v1",
		Compatibility: provider.CompatOpenAI,
	}
	p := profile.Profile{
		Name: "inclaude", ProviderSlug: prov.Slug,
		Target: profile.TargetConfig{App: "claude"},
		Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "deepseek-v4-flash"}},
	}
	strat, err := (ClaudeCodeAdapter{}).Render(p, prov, testAPIKey("fi-test-key"))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strat.Bridge == nil {
		t.Fatal("expected an Anthropic-to-OpenAI bridge")
	}
	if got, want := strat.Bridge.TargetBaseURL, "https://freeinference.org/v1"; got != want {
		t.Errorf("bridge target = %q, want %q", got, want)
	}
	if got, want := strat.Plan.Env["ANTHROPIC_AUTH_TOKEN"], "aegiskeys-local-bridge"; got != want {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q, want %q", got, want)
	}
}
