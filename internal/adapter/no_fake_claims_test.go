package adapter

import (
	"strings"
	"testing"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// --- Phase 4: no-fake-security-claims tests ---
// These enforce the product's security contract structurally, so a regression
// is caught by the build rather than by review.

// TestNoVerifiedAdapterWithoutVerificationGates is the hard rule: an adapter
// may not claim ConfidenceVerified unless ALL verification gates pass.
func TestNoVerifiedAdapterWithoutVerificationGates(t *testing.T) {
	reg := NewRegistry()
	for _, a := range reg.All() {
		c := a.Contract()
		if c.SupportConfidence == ConfidenceVerified && !c.Verification.Verified() {
			t.Errorf("adapter %q claims ConfidenceVerified but verification gates are not all passed (RenderGolden=%v NoSecretLeak=%v ConfigMergeTest=%v LaunchSmokeTest=%v)",
				c.ID, c.Verification.RenderGolden, c.Verification.NoSecretLeak, c.Verification.ConfigMergeTest, c.Verification.LaunchSmokeTest)
		}
	}
}

// TestNoManualAdapterReceivesRawSecret verifies that adapters with
// CanInjectSecrets=false never receive the raw secret — not in env, args,
// preview, or config files. This is the C2 boundary.
func TestNoManualAdapterReceivesRawSecret(t *testing.T) {
	const sentinel = "AK_MANUAL_SENTINEL_abcdef123456"
	reg := NewRegistry()
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{ID: "k", Secret: sentinel}

	for _, a := range reg.All() {
		contract := a.Contract()
		if contract.CanInjectSecrets {
			continue
		}
		prof := profile.Profile{
			Name:   "t",
			Target: profile.TargetConfig{App: a.ID()},
		}
		strategy, err := ResolveLaunchStrategyForMode(prof, prov, key, reg, ResolveRun)
		if err != nil {
			// Some manual adapters can't render without config — skip.
			continue
		}
		// Raw secret must not appear anywhere in the plan.
		combined := strings.Builder{}
		for _, v := range strategy.Plan.Env {
			combined.WriteString(v + "\n")
		}
		for _, v := range strategy.Plan.Args {
			combined.WriteString(v + "\n")
		}
		for _, v := range strategy.Plan.Preview {
			combined.WriteString(v + "\n")
		}
		for _, f := range strategy.Plan.Files {
			combined.WriteString(f.Content)
		}
		if strings.Contains(combined.String(), sentinel) {
			t.Errorf("adapter %q (CanInjectSecrets=false) received raw secret in plan", a.ID())
		}
	}
}

// TestNoProfileEnvCredentialNames blocks profile env from overriding the
// provider secret var or setting credential-looking variables.
func TestNoProfileEnvCredentialNames(t *testing.T) {
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		Compatibility: provider.CompatOpenAI,
	}
	reg := NewRegistry()

	// Profile env overriding the provider secret var → rejected.
	prof := profile.Profile{
		Name:         "t",
		ProviderSlug: "openai",
		Target:       profile.TargetConfig{App: "aider", RenderMode: profile.RenderEnv, Command: "echo"},
		Env:          map[string]string{"OPENAI_API_KEY": "steal-me"},
	}
	key := &secret.SecretRecord{ID: "k", Secret: "sk-real"}
	if _, err := ResolveLaunchStrategy(prof, prov, key, reg); err == nil {
		t.Error("profile env overriding provider secret var should be rejected")
	}
}
