package adapter

import (
	"os"
	"path/filepath"
	"testing"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
)

func TestCheckDotEnvShadowing_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	hazards := CheckDotEnvShadowing(dir)
	if len(hazards) != 0 {
		t.Errorf("expected no hazards in empty dir, got %d", len(hazards))
	}
}

func TestCheckDotEnvShadowing_DetectsSecret(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := "OPENAI_API_KEY=sk-1234567890abcdef1234567890abcdef\nMODEL=gpt-4o\n"
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	hazards := CheckDotEnvShadowing(dir)
	if len(hazards) == 0 {
		t.Error("expected hazard for .env with API key")
	}
	if len(hazards) > 0 && hazards[0].Severity != "high" {
		t.Errorf("expected high severity, got %s", hazards[0].Severity)
	}
}

func TestCheckDotEnvShadowing_IgnoresCleanEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := "MODEL=gpt-4o\nDEBUG=true\n"
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	hazards := CheckDotEnvShadowing(dir)
	if len(hazards) != 0 {
		t.Error("clean .env should not produce hazards")
	}
}

func TestAiderPreflight_WarnsOnProjectDotEnvSecret(t *testing.T) {
	a := AiderAdapter{}
	c := a.Contract()
	if len(c.Hazards) == 0 {
		t.Error("Aider contract should declare .env shadowing hazard")
	}
}

func TestHermesAdapter_DoesNotWriteDotEnv(t *testing.T) {
	a := HermesAdapter{}
	p := profile.Profile{
		Name: "test", ProviderSlug: "openrouter",
		Target: profile.TargetConfig{App: "hermes"},
		Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}},
	}
	prov := provider.Provider{
		Name: "OpenRouter", Slug: "openrouter", EnvVar: "OPENROUTER_API_KEY",
		BaseURL: "https://openrouter.ai/api/v1", Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range strategy.Plan.Files {
		if filepath.Base(f.Path) == ".env" {
			t.Error("Hermes adapter should never write a .env file")
		}
	}
}

func TestZedAdapter_PartialSupport(t *testing.T) {
	a := ZedAdapter{}
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY", Compatibility: provider.CompatOpenAI,
	}
	if a.CanInjectCredential(prov) {
		t.Error("Zed should not support direct credential injection")
	}
	if !a.CanConfigureProvider(prov) {
		t.Error("Zed should support provider configuration")
	}
}

func TestIntelliJAdapter_PartialSupport(t *testing.T) {
	a := IntelliJAdapter{}
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY", Compatibility: provider.CompatOpenAI,
	}
	if a.CanInjectCredential(prov) {
		t.Error("IntelliJ should not support credential injection")
	}
	if !a.CanConfigureProvider(prov) {
		t.Error("IntelliJ should support provider configuration (guided)")
	}
}

func TestGuidedActions_ForAll(t *testing.T) {
	// Every app with partial support should have guided actions.
	for _, appID := range []string{"zed", "intellij", "cursor", "roo"} {
		actions := GuidedActions(appID)
		if len(actions) == 0 {
			t.Errorf("expected guided actions for %s", appID)
		}
	}
	// Unknown app should return nil.
	if GuidedActions("nonexistent") != nil {
		t.Error("unknown app should have no guided actions")
	}
}

func TestCursorAdapter_BlockedInPreflight(t *testing.T) {
	a := CursorAdapter{}
	p := profile.Profile{Name: "t", Target: profile.TargetConfig{App: "cursor"}}
	prov := provider.Provider{Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY", Compatibility: provider.CompatOpenAI}
	key := testAPIKey("sk-test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Blocked {
		t.Error("Cursor should be blocked (account-based auth)")
	}
	if s.BlockReason == "" {
		t.Error("blocked adapter should have a reason")
	}
}

func TestContinueAdapter_FullEnv(t *testing.T) {
	a := ContinueAdapter{}
	if a.Contract().SupportLevel != SupportFullEnv {
		t.Errorf("Continue support level = %s, want full_env", a.Contract().SupportLevel)
	}
	// Continue is env-only: it must not declare config patching.
	if a.Contract().CanPatchConfig {
		t.Error("Continue should not declare CanPatchConfig")
	}
}

func TestGeminiAdapter_GoogleCompat(t *testing.T) {
	a := GeminiCLIAdapter{}
	prov := provider.Provider{Name: "Google", Slug: "google", EnvVar: "GOOGLE_API_KEY", Compatibility: provider.CompatGoogle}
	if !a.SupportsProvider(prov) {
		t.Error("Gemini CLI should support Google providers")
	}
}
