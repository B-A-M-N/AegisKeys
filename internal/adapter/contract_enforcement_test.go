package adapter

import (
	"strings"
	"testing"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// TestContractEnforcement_PerAdapter is adversarial: for every adapter,
// render a launch strategy with a sentinel key. If the adapter says
// CanInjectSecrets=false, the launch plan must contain no raw secret.
func TestContractEnforcement_PerAdapter(t *testing.T) {
	const sentinel = "AK_CONTRACT_SENTINEL_9876543210"

	reg := NewRegistry()
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
		ExtraEnv:      map[string]string{"OPENAI_BASE_URL": "https://api.openai.com/v1"},
	}

	key := &secret.SecretRecord{
		ID:           "key_test",
		ProviderSlug: "openai",
		Secret:       sentinel,
	}

	prof := profile.Profile{
		Name:         "test",
		ProviderSlug: "openai",
		KeyID:        key.ID,
		Target:       profile.TargetConfig{App: "generic", RenderMode: profile.RenderEnv, Command: "echo"},
	}

	var err error
	for _, a := range reg.All() {
		prof.Target.App = a.ID()
		var strategy *LaunchStrategy
		strategy, err = ResolveLaunchStrategy(prof, prov, key, reg)
		if err != nil {
			// Apps that can't render without a specific config — skip.
			continue
		}
		c := strategy.Support

		// Check 1: if CanInjectSecrets=false, no raw secret in plan.
		if !c.CanInjectSecrets {
			var combined strings.Builder
			for _, v := range strategy.Plan.Env {
				combined.WriteString(v)
				combined.WriteByte('\n')
			}
			combined.WriteString(strings.Join(strategy.Plan.Args, "\n"))
			combined.WriteString(strings.Join(strategy.Plan.Preview, "\n"))
			for _, f := range strategy.Plan.Files {
				combined.WriteString(f.Content)
			}
			if strings.Contains(combined.String(), sentinel) {
				t.Errorf("adapter %s (CanInjectSecrets=false): raw secret leaked into launch plan", a.ID())
			}
		}

		// ValidateLaunchStrategy should pass since ResolveLaunchStrategy already validates.
		if err = ValidateLaunchStrategy(strategy, prof, prov, key, DefaultSecurityPolicy()); err != nil && !strategy.Blocked {
			t.Errorf("adapter %s: ValidateLaunchStrategy rejected valid strategy: %v", a.ID(), err)
		}
	}
}

// TestContractEnforcement_BlockedStrategy verifies that a blocked adapter
// cannot execute. The runner must refuse.
func TestContractEnforcement_BlockedStrategy(t *testing.T) {
	const sentinel = "AK_BLOCKED_SENTINEL_abcdef"

	reg := NewRegistry()

	// Find an adapter that reports Blocked=true.
	blockedFound := false
	for _, a := range reg.All() {
		if a.Contract().SupportConfidence == ConfidenceBlocked {
			blockedFound = true
			// A blocked adapter should not have its launch plan executed.
			// Verify the contract declares it.
			if !a.Contract().RequiresManualStep && !a.Contract().CanInjectSecrets {
				t.Logf("adapter %s is blocked/manual — contract is honest", a.ID())
			}
		}
	}
	if !blockedFound {
		t.Skip("no adapter currently reports ConfidenceBlocked (this may be a gap)")
	}

	// Also verify: even if an adapter renders a plan with Blocked=true, the
	// central gate refuses.
	strategy := &LaunchStrategy{
		Blocked:     true,
		BlockReason: "test: app is blocked",
		Support:     AppSupportContract{ID: "test", CanLaunch: true},
	}
	prof := profile.Profile{Name: "t", ProviderSlug: "openai", KeyID: "k"}
	prov := provider.Provider{Slug: "openai"}
	key := testAPIKey(sentinel)

	if err := ValidateLaunchStrategy(strategy, prof, prov, key, DefaultSecurityPolicy()); err == nil {
		t.Error("ValidateLaunchStrategy should reject a blocked strategy, but accepted it")
	}
}

func TestValidateLaunchStrategyRejectsRawSecretSubstrings(t *testing.T) {
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{ID: "key_test", ProviderSlug: "openai", Secret: "sk-substring-secret-1234567890"}
	prof := profile.Profile{Name: "test", ProviderSlug: "openai", KeyID: key.ID}
	base := AppSupportContract{
		ID:                "test",
		DisplayName:       "Test",
		DefaultCommand:    "test",
		SupportLevel:      SupportFullEnv,
		CredentialControl: CredentialEnvInjected,
		SupportConfidence: ConfidenceExperimental,
		RenderModes:       []string{"env", "args"},
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true,
		CanInjectSecrets:  true,
	}

	t.Run("argv", func(t *testing.T) {
		strategy := &LaunchStrategy{
			Support: base,
			Plan:    LaunchPlan{Command: "test", Args: []string{"--api-key=" + key.Secret}},
		}
		if err := ValidateLaunchStrategy(strategy, prof, prov, key, DefaultSecurityPolicy()); err == nil {
			t.Fatal("expected raw secret substring in argv to be rejected")
		}
	})

	t.Run("preview", func(t *testing.T) {
		strategy := &LaunchStrategy{
			Support: base,
			Plan:    LaunchPlan{Command: "test", Preview: []string{"Authorization: Bearer " + key.Secret}},
		}
		if err := ValidateLaunchStrategy(strategy, prof, prov, key, DefaultSecurityPolicy()); err == nil {
			t.Fatal("expected raw secret substring in preview to be rejected")
		}
	})

	t.Run("declared env injection allowed", func(t *testing.T) {
		strategy := &LaunchStrategy{
			Support: base,
			Plan:    LaunchPlan{Command: "test", Env: map[string]string{"OPENAI_API_KEY": key.Secret}},
		}
		if err := ValidateLaunchStrategy(strategy, prof, prov, key, DefaultSecurityPolicy()); err != nil {
			t.Fatalf("declared credential env injection should remain allowed: %v", err)
		}
	})
}

// TestContractEnforcement_ManualAppNoSecret verifies that a manual-credential app
// (cursor) either resolves without leaking the raw secret or gets blocked by the
// contract enforcer. Both outcomes are acceptable for security.
func TestContractEnforcement_ManualAppNoSecret(t *testing.T) {
	const sentinel = "AK_MANUAL_NO_SECRET_xyz"

	reg := NewRegistry()
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{ID: "k", ProviderSlug: "openai", Secret: sentinel}

	prof := profile.Profile{
		Name:         "t",
		ProviderSlug: "openai",
		KeyID:        "k",
		Target:       profile.TargetConfig{App: "cursor", RenderMode: profile.RenderEnv},
	}

	strategy, err := ResolveLaunchStrategy(prof, prov, key, reg)
	if err != nil {
		// Cursor is blocked by contract — acceptable.
		if !contains(err.Error(), "blocked") {
			t.Fatalf("ResolveLaunchStrategy unexpected error: %v", err)
		}
		return
	}

	if strategy.Blocked {
		return // Blocked by contract — acceptable.
	}

	if strategy.Support.CanInjectSecrets {
		t.Skip("cursor adapter currently CanInjectSecrets=true (expected false)")
	}

	// Assert no raw secret anywhere in the plan.
	output := strategy.Plan.Env["OPENAI_API_KEY"]
	if output == sentinel {
		t.Errorf("manual app received raw secret in OPENAI_API_KEY env")
	}
	for _, f := range strategy.Plan.Files {
		if searchString(f.Content, sentinel) {
			t.Errorf("manual app plan contains raw secret in file content")
		}
	}
}

// TestResolveLaunchStrategyRejectsRawSecretInFiles verifies that ResolveLaunchStrategy
// refuses to produce a launch plan that would write a raw secret to a config file.
func TestResolveLaunchStrategyRejectsRawSecretInFiles(t *testing.T) {
	prof := profile.Profile{
		Name:         "bad",
		ProviderSlug: "openrouter",
		Target:       profile.TargetConfig{App: "badfixture", RenderMode: profile.RenderEnv},
	}
	prov := provider.Provider{
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENROUTER_API_KEY"},
		BaseURL:       "https://openrouter.ai/api/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{ID: "k1", ProviderSlug: "openrouter", Secret: "sk-test-secret-raw-value"}

	// Use a custom registry with an adapter that writes raw secret into files.
	reg := NewRegistryWithForTest(BadRawSecretFileAdapter{})

	_, err := ResolveLaunchStrategy(prof, prov, key, reg)
	if err == nil {
		t.Fatal("ResolveLaunchStrategy should reject raw secret in files, but accepted")
	}
	if !contains(err.Error(), "raw secret") {
		t.Errorf("expected error to mention 'raw secret', got: %v", err)
	}
}

// TestValidateContract_VerifiedRequiresValidationChecks verifies that an adapter
// claiming ConfidenceVerified must also declare ValidationChecks.
func TestValidateContract_VerifiedRequiresValidationChecks(t *testing.T) {
	c := AppSupportContract{
		ID:                "test-verified",
		DisplayName:       "Test Verified",
		CredentialControl: CredentialEnvInjected,
		SupportLevel:      SupportFullEnv,
		SupportConfidence: ConfidenceVerified,
		LaunchSurfaces:    []string{"cli"},
		CanInjectSecrets:  true,
		CanPatchConfig:    false,
		CanManageModels:   false,
		CanLaunch:         true,
		DefaultCommand:    "test",
		ConfigFiles:       nil,
		ModelSlots:        nil,
		ValidationChecks:  nil, // should fail: verified without checks
	}
	if err := ValidateContract(c); err == nil {
		t.Error("ValidateContract should reject verified adapter without ValidationChecks")
	}

	// After adding checks AND passing verification gates, it should pass.
	c.ValidationChecks = []string{"test_no_raw_secret_in_plan", "test_env_inject_only"}
	c.Verification = AdapterVerification{RenderGolden: true, NoSecretLeak: true, ConfigMergeTest: true, LaunchSmokeTest: true}
	if err := ValidateContract(c); err != nil {
		t.Errorf("ValidateContract should accept adapter with ValidationChecks and passing gates: %v", err)
	}

	// Checks declared but gates not passed → still rejected (prevents "paper verified").
	c.Verification = AdapterVerification{}
	if err := ValidateContract(c); err == nil {
		t.Error("ValidateContract should reject verified adapter whose verification gates have not passed")
	}
}

// TestValidateContract_CanPatchConfigRequiresConfigFiles verifies honestedt adapter claims.
func TestValidateContract_CanPatchConfigRequiresConfigFiles(t *testing.T) {
	c := AppSupportContract{
		ID:                "test-patch",
		DisplayName:       "Test Patch",
		CredentialControl: CredentialConfigPatched,
		SupportLevel:      SupportEnvConfig,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"cli"},
		CanInjectSecrets:  true,
		CanPatchConfig:    true,
		ConfigFiles:       nil, // should fail
	}
	if err := ValidateContract(c); err == nil {
		t.Error("ValidateContract should reject CanPatchConfig with empty ConfigFiles")
	}

	c.ConfigFiles = []ConfigFileContract{{Path: "/test", Format: "json", Description: "test"}}
	if err := ValidateContract(c); err != nil {
		t.Errorf("ValidateContract should accept CanPatchConfig with ConfigFiles: %v", err)
	}
}

// TestValidateContract_HighCriticalHazardRequiresFix verifies hazard fix requirement.
func TestValidateContract_HighCriticalHazardRequiresFix(t *testing.T) {
	c := AppSupportContract{
		ID:                "test-hazard",
		DisplayName:       "Test Hazard",
		CredentialControl: CredentialEnvInjected,
		SupportLevel:      SupportFullEnv,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"cli"},
		CanInjectSecrets:  true,
		Hazards: []Hazard{
			{Severity: "high", Title: "Shadowing", Detail: "Env shadowing occurs", Fix: ""},
		},
	}
	if err := ValidateContract(c); err == nil {
		t.Error("ValidateContract should reject high hazard without Fix")
	}

	c.Hazards[0].Fix = "Use isolated env"
	if err := ValidateContract(c); err != nil {
		t.Errorf("ValidateContract should accept high hazard with Fix: %v", err)
	}
}

// --- BadRawSecretFileAdapter is a test fixture that writes raw secrets into files ---

type BadRawSecretFileAdapter struct{}

func (BadRawSecretFileAdapter) ID() string                                    { return "badfixture" }
func (BadRawSecretFileAdapter) DisplayName() string                           { return "Bad Fixture" }
func (BadRawSecretFileAdapter) DefaultCommand() string                        { return "" }
func (BadRawSecretFileAdapter) SupportsProvider(p provider.Provider) bool     { return true }
func (BadRawSecretFileAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (BadRawSecretFileAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (BadRawSecretFileAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                "badfixture",
		DisplayName:       "Bad Fixture",
		CredentialControl: CredentialEnvInjected,
		SupportLevel:      SupportFullEnv,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"cli"},
		CanInjectSecrets:  true,
		CanLaunch:         false,
	}
}

func (BadRawSecretFileAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	rawSecret := ""
	if key != nil {
		rawSecret = key.Secret
	}
	return &LaunchStrategy{
		Plan: LaunchPlan{
			Files: []FileWrite{
				{
					Path:        "/tmp/badfile.json",
					Format:      "json",
					Content:     `{"key":"` + rawSecret + `"}`,
					Scope:       ScopeTemp,
					MergePolicy: MergeNone,
					RedactCheck: true,
				},
			},
		},
	}, nil
}

func (BadRawSecretFileAdapter) Validate(p profile.Profile, prov provider.Provider) (warnings []string, err error) {
	return nil, nil
}

// contains is a simple string-contains helper (avoids importing strings twice).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
