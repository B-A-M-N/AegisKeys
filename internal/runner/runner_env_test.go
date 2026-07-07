package runner

import (
	"testing"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

func TestBaseEnvForClass_CLI(t *testing.T) {
	env := baseEnvForClass("cli")
	if !env["PATH"] {
		t.Error("CLI env should include PATH")
	}
	if env["DISPLAY"] {
		t.Error("CLI env should NOT include DISPLAY")
	}
}

func TestBaseEnvForClass_GUI(t *testing.T) {
	env := baseEnvForClass("gui")
	if !env["PATH"] {
		t.Error("GUI env should include PATH")
	}
	if !env["DISPLAY"] {
		t.Error("GUI env should include DISPLAY")
	}
	if !env["WAYLAND_DISPLAY"] {
		t.Error("GUI env should include WAYLAND_DISPLAY")
	}
}

func TestBaseEnvForClass_IDE(t *testing.T) {
	env := baseEnvForClass("ide")
	if !env["DBUS_SESSION_BUS_ADDRESS"] {
		t.Error("IDE env should include DBUS_SESSION_BUS_ADDRESS")
	}
}

func TestCleanBaseEnvWithAllowlist_GUIIncludesDisplay(t *testing.T) {
	// Simulate parent env with DISPLAY and a secret.
	parent := []string{
		"PATH=/usr/bin",
		"DISPLAY=:0",
		"SECRET_KEY=should-be-stripped",
		"HOME=/home/user",
	}
	allowlist := baseEnvEnvForClass("gui")
	got := cleanBaseEnvWithAllowlist(parent, allowlist)
	found := false
	for _, v := range got {
		if v == "DISPLAY=:0" {
			found = true
		}
		if v == "SECRET_KEY=should-be-stripped" {
			t.Error("secret-looking var should be stripped")
		}
	}
	if !found {
		t.Error("GUI allowlist should include DISPLAY")
	}
}

func baseEnvEnvForClass(class string) map[string]bool {
	if class == "gui" || class == "ide" {
		merged := make(map[string]bool)
		for k := range safeBaseEnv {
			merged[k] = true
		}
		for k := range guiSafeEnv {
			merged[k] = true
		}
		return merged
	}
	return safeBaseEnv
}

func TestRunConfig_AppClassField(t *testing.T) {
	cfg := RunConfig{AppClass: "gui"}
	if cfg.AppClass != "gui" {
		t.Error("AppClass field should be settable")
	}
}

// TestResolveRunConfig_AppClass verifies the runner's resolve flow preserves
// the run config structure.
func TestResolveRunConfig_AppClass(t *testing.T) {
	registry := adapter.NewRegistry()
	p := &profile.Profile{
		Name:         "test",
		ProviderSlug: "openai",
		KeyID:        "key_1",
		Target:       profile.TargetConfig{App: "aider"},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}},
	}
	prov := &provider.Provider{
		Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY",
		BaseURL: "https://api.openai.com/v1", Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{Secret: "sk-test", ProviderSlug: "openai", ID: "key_1", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)}

	cfg, files, err := ResolveRunConfig(p, prov, key, registry)
	if err != nil {
		t.Fatalf("ResolveRunConfig: %v", err)
	}
	if cfg.Command != "aider" {
		t.Errorf("command = %s", cfg.Command)
	}
	if len(files) != 0 {
		t.Errorf("aider should have no files, got %d", len(files))
	}
}
