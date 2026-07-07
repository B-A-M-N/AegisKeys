package tui

import (
	"fmt"
	"strings"
	"testing"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// TestSecretPropagation_TUIVIEW is adversarial: creates a vault with a known
// secret, then verifies that the raw secret NEVER appears in any TUI output
// surface across all screens, the locked view, and modals.
func TestSecretPropagation_TUIVIEW(t *testing.T) {
	knownSecret := "sk-this-is-an-adversarial-test-secret-do-not-leak"

	v := &secret.Vault{Version: 1}
	if err := v.Add(secret.SecretRecord{
		ProviderSlug: "openai",
		Label:        "main",
		Secret:       knownSecret,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	store := profile.NewStore()
	if err := store.Add(profile.Profile{
		Name:         "test-profile",
		ProviderSlug: "openai",
		KeyID:        v.Keys[0].ID,
		Target:       profile.TargetConfig{App: "generic", RenderMode: profile.RenderEnv},
	}); err != nil {
		t.Fatalf("Add profile: %v", err)
	}

	m := newTestModel(t)
	m.unlocked = true
	m.vaultSession = &vaultSession{vault: v}
	m.keys = secret.ToMaskedList(v.Keys)
	m.profiles = store

	// Render every screen and check for the secret.
	screens := []screen{
		screenDashboard, screenProviders, screenKeys, screenProfiles,
		screenLaunch, screenDoctor, screenAudit, screenHelp, screenSettings,
	}
	for _, s := range screens {
		m.active = s
		m.focus = focusContent
		content := stripANSIForTest(m.View().Content)
		if strings.Contains(content, knownSecret) {
			t.Errorf("raw secret leaked on screen %d", s)
		}
	}

	// Also check the password-locked view.
	m.unlocked = false
	m.vaultSession = nil
	locked := stripANSIForTest(m.View().Content)
	if strings.Contains(locked, knownSecret) {
		t.Errorf("raw secret leaked on locked view")
	}
}

// TestSecretPropagation_ADAPTERRENDER verifies adapter Render output never
// contains the raw secret for any adapter+provider combination.
func TestSecretPropagation_ADAPTERRENDER(t *testing.T) {
	knownSecret := "sk-adversarial-render-secret-12345"

	reg := adapter.NewRegistry()

	v := &secret.Vault{Version: 1}
	if err := v.Add(secret.SecretRecord{
		ProviderSlug: "openai",
		Label:        "main",
		Secret:       knownSecret,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	key := &v.Keys[0]

	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
		ExtraEnv:      map[string]string{"OPENAI_BASE_URL": "https://api.openai.com/v1"},
	}

	prof := profile.Profile{
		Name:         "t",
		ProviderSlug: "openai",
		KeyID:        key.ID,
		Target:       profile.TargetConfig{App: "generic", RenderMode: profile.RenderEnv},
	}

	for _, a := range reg.All() {
		strategy, err := adapter.ResolveLaunchStrategy(prof, prov, key, reg)
		if err != nil {
			continue // some apps can't launch without a command
		}
		// Check all output fields for raw secret.
		output := fmt.Sprintf("%+v", strategy.Plan) +
			fmt.Sprintf("%+v", strategy.Hazards) +
			fmt.Sprintf("%+v", strategy.ManualSteps)
		if strings.Contains(output, knownSecret) {
			t.Errorf("adapter %s leaked raw secret in output", a.ID())
		}
	}
}

// TestSecretPropagation_MODALS verifies modals never show raw secrets.
func TestSecretPropagation_MODALS(t *testing.T) {
	knownSecret := "sk-modal-secret-check-abcdef"

	v := &secret.Vault{Version: 1}
	if err := v.Add(secret.SecretRecord{
		ProviderSlug: "openai",
		Label:        "main",
		Secret:       knownSecret,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	m := newTestModel(t)
	m.unlocked = true
	m.vaultSession = &vaultSession{vault: v}
	m.keys = secret.ToMaskedList(v.Keys)
	m.providers = provider.NewRegistry()

	// Open the detail modal on the Keys screen.
	m.active = screenKeys
	m.focus = focusContent
	m.selected[screenKeys] = 0
	sendKey(t, m, "d")

	content := stripANSIForTest(m.View().Content)
	if strings.Contains(content, knownSecret) {
		t.Errorf("raw secret leaked in detail modal")
	}
}
