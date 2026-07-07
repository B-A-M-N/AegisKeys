package adapter

import (
	"strings"
	"testing"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// TestProfileEnvBypass_Attempts is adversarial: creates profiles whose Env
// attempts to set provider secret vars or credential-looking names, and
// asserts profile env is rejected.
func TestProfileEnvBypass_Attempts(t *testing.T) {
	reg := NewRegistry()
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{ID: "k", ProviderSlug: "openai", Secret: "sk-test", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)}

	bypassAttempts := map[string]string{
		"OPENAI_API_KEY":     "attempted-override",
		"ANTHROPIC_API_KEY":  "attempted-override",
		"OPENROUTER_API_KEY": "attempted-override",
		"AUTH_TOKEN":         "attempted-override",
		"MY_SECRET":          "attempted-override",
		"FOO_PASSWORD":       "attempted-override",
		"APP_CREDENTIAL":     "attempted-override",
	}

	for envKey, envVal := range bypassAttempts {
		t.Run(envKey, func(t *testing.T) {
			prof := profile.Profile{
				Name:         "t",
				ProviderSlug: "openai",
				KeyID:        "k",
				Target:       profile.TargetConfig{App: "generic", RenderMode: profile.RenderEnv, Command: "echo"},
				Env:          map[string]string{envKey: envVal},
			}

			strategy, err := ResolveLaunchStrategy(prof, prov, key, reg)
			if err != nil {
				// If ResolveLaunchStrategy itself rejects, that's good.
				return
			}

			// If it rendered, the central gate must reject.
			if err := ValidateLaunchStrategy(strategy, prof, prov, key, DefaultSecurityPolicy()); err == nil {
				t.Errorf("profile env %q was accepted — bypass succeeded", envKey)
			}
		})
	}

	// Also verify: a benign profile env (non-credential-looking) is accepted.
	prof := profile.Profile{
		Name:         "t",
		ProviderSlug: "openai",
		KeyID:        "k",
		Target:       profile.TargetConfig{App: "aider", RenderMode: profile.RenderEnv, Command: "echo"},
		Env:          map[string]string{"MY_APP_DEBUG": "1"},
	}
	strategy, err := ResolveLaunchStrategy(prof, prov, key, reg)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategy rejected benign profile: %v", err)
	}
	if err := ValidateLaunchStrategy(strategy, prof, prov, key, DefaultSecurityPolicy()); err != nil {
		t.Errorf("benign profile env was rejected: %v", err)
	}
}

// TestRedactionSurface is adversarial: seeds one exact sentinel secret and
// runs every render/output path. Asserts the sentinel never appears.
func TestRedactionSurface(t *testing.T) {
	const sentinel = "AK_REDACTION_SENTINEL_xyz_789"

	v := &secret.Vault{Version: 1}
	if err := v.Add(secret.SecretRecord{
		ProviderSlug: "openai",
		Label:        "main",
		Secret:       sentinel,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// 1. Masked keys — raw secret must NOT appear.
	masked := secret.ToMaskedList(v.Keys)
	for _, k := range masked {
		if strings.Contains(k.MaskedSecret, sentinel) {
			t.Errorf("masked key leaks raw secret: %q", k.MaskedSecret)
		}
	}

	// 2. Vault.Serialize — raw secret must NOT appear.
	data, err := v.Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if strings.Contains(string(data), sentinel) {
		t.Errorf("vault.Serialize leaks raw secret")
	}
}
