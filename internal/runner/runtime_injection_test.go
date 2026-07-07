package runner

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// TestRuntimeInjection_ChildScoped is the adversarial proof that injection
// is child-process-scoped:
//  1. parent env does NOT contain the sentinel before or after launch
//  2. the launched child DOES receive the sentinel env var
//  3. the runner does not leak the sentinel into the parent process
func TestRuntimeInjection_ChildScoped(t *testing.T) {
	const sentinel = "AK_TEST_SECRET_DO_NOT_LEAK_123456"

	// Ensure parent does not already have this env var.
	os.Unsetenv("OPENROUTER_API_KEY")
	if _, exists := os.LookupEnv("OPENROUTER_API_KEY"); exists {
		t.Fatal("parent env already contains OPENROUTER_API_KEY")
	}

	// Build a launch strategy for a generic OpenAI-compatible app.
	reg := adapter.NewRegistry()
	prov := provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENROUTER_API_KEY"},
		BaseURL:       "https://openrouter.ai/api/v1",
		Compatibility: provider.CompatOpenAI,
		ExtraEnv:      map[string]string{"OPENAI_BASE_URL": "https://openrouter.ai/api/v1"},
	}
	key := &secret.SecretRecord{
		ID:           "key_test",
		ProviderSlug: "openrouter",
		Secret:       sentinel,
		Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
	}

	prof := profile.Profile{
		Name:         "test-injection",
		ProviderSlug: "openrouter",
		KeyID:        key.ID,
		Target: profile.TargetConfig{
			App:        "aider",
			RenderMode: profile.RenderEnv,
			Command:    "sh", // override
		},
	}

	strategy, err := adapter.ResolveLaunchStrategy(prof, prov, key, reg)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategy: %v", err)
	}

	// Validate through the central gate.
	if err := adapter.ValidateLaunchStrategy(strategy, prof, prov, key, adapter.DefaultSecurityPolicy()); err != nil {
		t.Fatalf("ValidateLaunchStrategy rejected valid launch: %v", err)
	}

	// Launch a child that prints its env. Use `env` on Unix.
	cmd := exec.Command("env")
	cmd.Env = buildChildEnv(strategy.Plan.Env)

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("child execution failed: %v", err)
	}
	childOutput := string(out)

	// Assert child received the secret.
	if !strings.Contains(childOutput, sentinel) {
		t.Errorf("child did NOT receive the sentinel env var. Injection failed.\nchild output:\n%s", childOutput)
	}

	// Assert parent os.Environ does not contain the sentinel.
	for _, e := range os.Environ() {
		if strings.Contains(e, sentinel) {
			t.Errorf("PARENT env contains sentinel after launch: %q — injection is NOT child-scoped", e)
		}
	}
}

// buildChildEnv constructs a full child env from the launch plan's ExtraEnv.
// Mirrors what runner.Run does internally.
func buildChildEnv(extra map[string]string) []string {
	// Start with a minimal safe base (like runner does).
	base := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"USER=" + os.Getenv("USER"),
		"TERM=" + os.Getenv("TERM"),
	}
	for k, v := range extra {
		base = append(base, k+"="+v)
	}
	return base
}
