package cmd

import (
	"testing"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// TestRunIntegration_AdapterPath verifies that the run command's adapter
// integration resolves a launch strategy correctly.
func TestRunIntegration_AdapterPath(t *testing.T) {
	prof := &profile.Profile{
		Name:         "test-aider",
		ProviderSlug: "openrouter",
		KeyID:        "key_1",
		Target:       profile.TargetConfig{App: "aider", RenderMode: profile.RenderArgs},
		Models: profile.ModelSlots{
			Main: &profile.ModelRef{ID: "claude-sonnet-4-5", Source: profile.ModelSourceStatic},
		},
	}
	prov := &provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		BaseURL:       "https://openrouter.ai/api/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{Secret: "sk-test-key", ProviderSlug: "openrouter", ID: "key_1", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)}

	registry := adapter.NewRegistry()
	strategy, err := adapter.ResolveLaunchStrategy(*prof, *prov, key, registry)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategy: %v", err)
	}

	if strategy.Plan.Command != "aider" {
		t.Errorf("command = %s, want aider", strategy.Plan.Command)
	}
	if strategy.Plan.Env["OPENROUTER_API_KEY"] != "sk-test-key" {
		t.Errorf("key not in env")
	}
	// Aider should have --env-file /dev/null by default.
	found := false
	for i, arg := range strategy.Plan.Args {
		if arg == "--env-file" && i+1 < len(strategy.Plan.Args) && strategy.Plan.Args[i+1] == "/dev/null" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --env-file /dev/null, got %v", strategy.Plan.Args)
	}
}

// TestRunIntegration_HermesHomeIsolation verifies Hermes gets HERMES_HOME.
func TestRunIntegration_HermesHomeIsolation(t *testing.T) {
	prof := &profile.Profile{
		Name:         "test-hermes",
		ProviderSlug: "openrouter",
		KeyID:        "key_1",
		Target:       profile.TargetConfig{App: "hermes", RenderMode: profile.RenderEnv},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "claude-opus-4-5"}},
	}
	prov := &provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		BaseURL:       "https://openrouter.ai/api/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{Secret: "sk-test", ProviderSlug: "openrouter", ID: "key_1", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)}

	registry := adapter.NewRegistry()
	strategy, err := adapter.ResolveLaunchStrategy(*prof, *prov, key, registry)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategy: %v", err)
	}

	if strategy.Plan.Env["HERMES_HOME"] == "" {
		t.Error("HERMES_HOME not set")
	}
	if strategy.Plan.Env["HERMES_INFERENCE_PROVIDER"] == "" {
		t.Error("HERMES_INFERENCE_PROVIDER not set")
	}
	if len(strategy.Plan.Files) == 0 {
		t.Error("expected config file for Hermes")
	}
}

// TestRunIntegration_IntelliJNoSecretLeak verifies IntelliJ adapter never
// puts secrets in env or config files.
func TestRunIntegration_IntelliJNoSecretLeak(t *testing.T) {
	prof := &profile.Profile{
		Name:         "test-intellij",
		ProviderSlug: "openai",
		KeyID:        "key_1",
		Target:       profile.TargetConfig{App: "intellij", RenderMode: profile.RenderEnv},
	}
	prov := &provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{Secret: "sk-secret-leak-test", ProviderSlug: "openai", ID: "key_1", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)}

	registry := adapter.NewRegistry()
	strategy, err := adapter.ResolveLaunchStrategy(*prof, *prov, key, registry)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategy: %v", err)
	}

	// IntelliJ should not inject the secret via env.
	for _, v := range strategy.Plan.Env {
		if v == "sk-secret-leak-test" {
			t.Error("secret leaked to env for IntelliJ")
		}
	}
	// Config files should not contain the secret.
	for _, f := range strategy.Plan.Files {
		if len(f.Content) > 0 && len(f.Content) > 0 && containsString(f.Content, "sk-secret-leak-test") {
			t.Errorf("secret leaked to config file %s", f.Path)
		}
	}
}

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
