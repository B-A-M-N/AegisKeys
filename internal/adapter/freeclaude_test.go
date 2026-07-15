package adapter

import (
	"strings"
	"testing"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

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

func TestFreeClaudeAdapter_OpenAICompatibleGatewaySetsBothAnthropicCredentialVars(t *testing.T) {
	a := FreeClaudeAdapter{}
	prov := provider.Provider{
		Slug: "opencode-go", Name: "OpenCode Go", EnvVar: "OPENCODE_GO_API_KEY",
		BaseURL: "https://opencode.ai/zen/go/v1", Compatibility: provider.CompatOpenAI,
	}
	p := profile.Profile{Name: "fc-go", ProviderSlug: "opencode-go", Target: profile.TargetConfig{App: "free-claude"}}
	key := testAPIKey("test-go-key")
	strat, err := a.Render(p, prov, key)
	if err != nil {
		if findFreeCodeBinary() == "" {
			t.Skip("free-code not installed; skipping render assertions")
		}
		t.Fatalf("render error: %v", err)
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if got := strat.Plan.Env[name]; got != "test-go-key" {
			t.Errorf("%s = %q, want injected gateway key", name, got)
		}
	}
}

func TestFreeClaudeAdapter_BridgesOpenCodeGoChatOnlyModel(t *testing.T) {
	a := FreeClaudeAdapter{}
	p := profile.Profile{Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "opencode-go/deepseek-v4-flash"}}}
	prov := provider.Provider{Slug: "opencode-go", Name: "OpenCode Go", BaseURL: "https://opencode.ai/zen/go/v1", Compatibility: provider.CompatOpenAI}
	if _, err := a.Validate(p, prov); err != nil {
		t.Fatalf("expected chat-only OpenCode Go model to be bridgeable: %v", err)
	}
	strat, err := a.Render(p, prov, &secret.SecretRecord{Secret: "test-go-key"})
	if err != nil || strat.Bridge == nil {
		t.Fatalf("expected bridge strategy, got %#v, %v", strat, err)
	}
	if got := strat.Plan.Env["ANTHROPIC_API_KEY"]; got != "aegiskeys-local-bridge" {
		t.Fatalf("bridge should not inject upstream key into child, got %q", got)
	}

	p.Models.Main.ID = "opencode-go/minimax-m3"
	if _, err := a.Validate(p, prov); err != nil {
		t.Fatalf("expected Messages-capable OpenCode Go model to validate: %v", err)
	}
}
