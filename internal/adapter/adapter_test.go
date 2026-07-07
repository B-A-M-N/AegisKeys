package adapter

import (
	"fmt"
	"strings"
	"testing"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// truncate is a local helper for test output truncation.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// testAPIKey returns a SecretRecord with a realistic default policy that permits
// launch injection. Previously tests used an empty policy (which now correctly
// blocks injection), so they must use this helper to model a real secret.
func testAPIKey(secretValue string) *secret.SecretRecord {
	return &secret.SecretRecord{
		ID:     "k",
		Secret: secretValue,
		Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey),
	}
}

func TestRegistry_All(t *testing.T) {
	r := NewRegistry()
	all := r.All()
	if len(all) == 0 {
		t.Fatal("expected adapters")
	}
	// Should include generic, crush, aider, cline, hermes, qwen, claude, vibe, goose
	ids := make(map[string]bool)
	for _, a := range all {
		ids[a.ID()] = true
	}
	for _, want := range []string{"generic", "crush", "aider", "cline", "hermes", "qwen", "claude", "vibe", "goose"} {
		if !ids[want] {
			t.Errorf("missing adapter: %s", want)
		}
	}
}

func TestGenericAdapter_Render(t *testing.T) {
	a := GenericOpenAIAdapter{}
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openai",
		Target:       profile.TargetConfig{App: "generic", RenderMode: profile.RenderEnv, Command: "my-app"},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o", Source: profile.ModelSourceStatic}},
	}
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strategy.Plan.Env["OPENAI_API_KEY"] != "sk-test" {
		t.Errorf("env key not set")
	}
	if strategy.Plan.Env["OPENAI_MODEL"] != "gpt-4o" {
		t.Errorf("model not set")
	}
}

func TestAiderAdapter_ModelFormat(t *testing.T) {
	a := AiderAdapter{}
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openrouter",
		Target:       profile.TargetConfig{App: "aider", RenderMode: profile.RenderArgs},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "kimi-k2.7-code"}},
	}
	prov := provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	plan := strategy.Plan
	found := false
	for i, arg := range plan.Args {
		if arg == "--model" && i+1 < len(plan.Args) {
			if plan.Args[i+1] == "openrouter/kimi-k2.7-code" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected --model openrouter/kimi-k2.7-code, got %v", plan.Args)
	}
}

func TestResolveLaunchPlan(t *testing.T) {
	registry := NewRegistry()
	p := profile.Profile{
		Name:         "test-profile",
		ProviderSlug: "openai",
		Target:       profile.TargetConfig{App: "aider", RenderMode: profile.RenderEnv, Command: "my-app"},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o", Source: profile.ModelSourceStatic}},
	}
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test-key")
	plan, err := ResolveLaunchPlan(p, prov, key, registry)
	if err != nil {
		t.Fatalf("ResolveLaunchPlan: %v", err)
	}
	if plan.Env["OPENAI_API_KEY"] != "sk-test-key" {
		t.Errorf("key not in env")
	}
	if len(plan.Preview) == 0 {
		t.Errorf("expected preview lines")
	}
}

func TestProfile_TargetApp(t *testing.T) {
	p := profile.Profile{Target: profile.TargetConfig{App: "crush"}}
	if p.TargetApp() != "crush" {
		t.Errorf("TargetApp = %s", p.TargetApp())
	}
	p2 := profile.Profile{}
	if p2.TargetApp() != "generic" {
		t.Errorf("default TargetApp = %s", p2.TargetApp())
	}
}

func TestProfile_Command(t *testing.T) {
	p := profile.Profile{Target: profile.TargetConfig{App: "aider"}}
	if p.Command() != "aider" {
		t.Errorf("Command = %s", p.Command())
	}
	p2 := profile.Profile{Target: profile.TargetConfig{App: "crush", Command: "custom-crush"}}
	if p2.Command() != "custom-crush" {
		t.Errorf("Command = %s", p2.Command())
	}
}

// --- New tests for the refactor ---

func TestResolveLaunchStrategyRejectsProfileCredentialEnvOverride(t *testing.T) {
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openai",
		Target:       profile.TargetConfig{App: "generic", Command: "tool"},
		Env: map[string]string{
			"PROFILE_SPECIFIC": "from-profile",
			"OPENAI_API_KEY":   "profile-override",
		},
	}
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
		ExtraEnv: map[string]string{
			"OPENAI_BASE_URL": "https://api.openai.com/v1",
		},
	}
	key := testAPIKey("sk-from-vault")
	if _, err := ResolveLaunchStrategy(p, prov, key, NewRegistry()); err == nil {
		t.Fatal("expected error when profile env overrides credential env var")
	}
}

func TestValidateContract_Complete(t *testing.T) {
	c := AppSupportContract{
		ID:                "test",
		DisplayName:       "Test",
		CredentialControl: CredentialEnvInjected,
		SupportLevel:      SupportFullEnv,
		LaunchSurfaces:    []string{"cli"},
		CanInjectSecrets:  true,
		SupportConfidence: ConfidenceExperimental,
		CanPatchConfig:    false,
		CanManageModels:   false,
		CanLaunch:         false,
		ValidationChecks:  []string{"test_check"},
	}
	if err := ValidateContract(c); err != nil {
		t.Errorf("expected valid contract, got: %v", err)
	}
}

func TestValidateContract_MissingFields(t *testing.T) {
	cases := []struct {
		name string
		c    AppSupportContract
	}{
		{"missing ID", AppSupportContract{CredentialControl: CredentialEnvInjected, SupportLevel: SupportFullEnv, LaunchSurfaces: []string{"cli"}}},
		{"missing CredentialControl", AppSupportContract{ID: "x", SupportLevel: SupportFullEnv, LaunchSurfaces: []string{"cli"}}},
		{"missing SupportLevel", AppSupportContract{ID: "x", CredentialControl: CredentialEnvInjected, LaunchSurfaces: []string{"cli"}}},
		{"no LaunchSurfaces", AppSupportContract{ID: "x", CredentialControl: CredentialEnvInjected, SupportLevel: SupportFullEnv}},
		{"CanPatchConfig without ConfigFiles", AppSupportContract{ID: "x", CredentialControl: CredentialConfigPatched, SupportLevel: SupportEnvConfig, LaunchSurfaces: []string{"cli"}, CanPatchConfig: true}},
		{"CanInjectSecrets=false with no manual", AppSupportContract{ID: "x", CredentialControl: CredentialManualLogin, SupportLevel: SupportLauncherIsolation, LaunchSurfaces: []string{"cli"}}},
	}
	for _, c := range cases {
		if err := ValidateContract(c.c); err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}

func TestResolveLaunchStrategy_ManualApp_NoSecretEnv(t *testing.T) {
	// RooCode is a manual-credential adapter (CanInjectSecrets=false).
	// Even with a key provided, the launch plan must contain no raw secret env.
	reg := NewRegistry()
	a, ok := reg.Get("roo")
	if !ok {
		t.Skip("roo adapter not registered")
	}
	contract := a.Contract()
	if contract.CanInjectSecrets {
		t.Skip("roo adapter is not manual")
	}

	prof := profile.Profile{
		Name: "test",
		Target: profile.TargetConfig{
			App: "roo",
		},
	}
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-should-not-appear-anywhere")
	strategy, err := ResolveLaunchStrategy(prof, prov, key, reg)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategy: %v", err)
	}

	// No secret env vars should appear in the plan.
	if _, exists := strategy.Plan.Env["OPENAI_API_KEY"]; exists {
		t.Error("manual adapter plan contains raw OPENAI_API_KEY env — contract violated")
	}
	// No raw secret should appear anywhere in the output.
	planStr := fmt.Sprintf("%v", strategy.Plan.Env)
	if strings.Contains(planStr, "sk-should-not-appear-anywhere") {
		t.Error("manual adapter plan leaks raw secret: " + planStr)
	}
}

// TestResolveLaunchStrategy_PreviewVsRun verifies the resolver mode split:
// Preview mode returns blocked/manual strategies (for display), while Run mode
// remains the hard gate. Both modes must still strip raw secrets from manual
// adapters (never expose raw secrets, even in preview).
func TestResolveLaunchStrategy_PreviewVsRun(t *testing.T) {
	reg := NewRegistry()
	prof := profile.Profile{
		Name:   "preview-test",
		Target: profile.TargetConfig{App: "roo"}, // manual-credential adapter
	}
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-preview-secret")
	// Preview mode returns the strategy (blocked or not) so the UI can show it.
	previewStrat, err := ResolveLaunchStrategyForMode(prof, prov, key, reg, ResolvePreview)
	if err != nil {
		t.Fatalf("preview mode should not error on manual adapter, got: %v", err)
	}
	if previewStrat == nil {
		t.Fatal("preview mode returned nil strategy")
	}
	// Raw secret must never appear in preview output.
	planStr := fmt.Sprintf("%v\n%v\n%v", previewStrat.Plan.Env, previewStrat.Plan.Args, previewStrat.Plan.Preview)
	if strings.Contains(planStr, "sk-preview-secret") {
		t.Error("preview mode leaks raw secret into launch plan")
	}

	// Run mode also resolves (manual adapter is not Blocked, just CanInjectSecrets=false).
	runStrat, err := ResolveLaunchStrategyForMode(prof, prov, key, reg, ResolveRun)
	if err != nil {
		t.Fatalf("run mode should not error on non-blocked manual adapter: %v", err)
	}
	if runStrat == nil {
		t.Fatal("run mode returned nil strategy")
	}
}

func TestValidateAllContracts_BuiltInAdaptersValid(t *testing.T) {
	r := NewRegistry()
	errs := r.ValidateAllContracts()
	for _, e := range errs {
		t.Errorf("contract validation error: %v", e)
	}
}

func TestBuildBaseEnv_ProfileEnv_AllowedNonCredential(t *testing.T) {
	// Non-credential profile env should merge cleanly.
	p := profile.Profile{
		Name: "t",
		Env: map[string]string{
			"PROFILE_SPECIFIC": "from-profile",
		},
	}
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
		ExtraEnv: map[string]string{
			"OPENAI_BASE_URL": "https://api.openai.com/v1",
		},
	}
	key := testAPIKey("sk-from-vault")
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Provider primary secret should be set from the vault.
	if env["OPENAI_API_KEY"] != "sk-from-vault" {
		t.Errorf("expected vault key, got %s", env["OPENAI_API_KEY"])
	}
	// Provider extra env should be present.
	if env["OPENAI_BASE_URL"] != "https://api.openai.com/v1" {
		t.Errorf("expected extra env, got %s", env["OPENAI_BASE_URL"])
	}
	if _, ok := env["PROFILE_SPECIFIC"]; ok {
		t.Error("profile env should be applied by resolver, not buildBaseEnv")
	}
}

func TestResolveLaunchStrategy_ProfileEnvAllowedNonCredential(t *testing.T) {
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openai",
		Target:       profile.TargetConfig{App: "generic", Command: "tool"},
		Env:          map[string]string{"PROFILE_SPECIFIC": "from-profile"},
	}
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
		ExtraEnv:      map[string]string{"OPENAI_BASE_URL": "https://api.openai.com/v1"},
	}
	key := testAPIKey("sk-from-vault")
	strategy, err := ResolveLaunchStrategy(p, prov, key, NewRegistry())
	if err != nil {
		t.Fatalf("ResolveLaunchStrategy: %v", err)
	}
	if strategy.Plan.Env["PROFILE_SPECIFIC"] != "from-profile" {
		t.Errorf("profile env not applied by resolver")
	}
}

func TestAiderAdapter_RendersMainWeakEditorAndEnvFileNull(t *testing.T) {
	a := AiderAdapter{}
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openrouter",
		Target:       profile.TargetConfig{App: "aider", RenderMode: profile.RenderArgs},
		Models: profile.ModelSlots{
			Main:   &profile.ModelRef{ID: "claude-sonnet-4-5"},
			Weak:   &profile.ModelRef{ID: "gpt-4o-mini"},
			Editor: &profile.ModelRef{ID: "claude-sonnet-4-5"},
		},
	}
	prov := provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	plan := strategy.Plan

	// Should have --env-file /dev/null.
	foundEnvFile := false
	for i, arg := range plan.Args {
		if arg == "--env-file" && i+1 < len(plan.Args) && plan.Args[i+1] == "/dev/null" {
			foundEnvFile = true
		}
	}
	if !foundEnvFile {
		t.Errorf("expected --env-file /dev/null in args, got %v", plan.Args)
	}

	// Should have --model, --weak-model, --editor-model.
	checks := map[string]string{
		"--model":        "openrouter/claude-sonnet-4-5",
		"--weak-model":   "openrouter/gpt-4o-mini",
		"--editor-model": "openrouter/claude-sonnet-4-5",
	}
	for flag, want := range checks {
		found := false
		for i, arg := range plan.Args {
			if arg == flag && i+1 < len(plan.Args) && plan.Args[i+1] == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %s %s in args, got %v", flag, want, plan.Args)
		}
	}
}

func TestHermesAdapter_WritesYamlConfigNotJson(t *testing.T) {
	a := HermesAdapter{}
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openrouter",
		Target:       profile.TargetConfig{App: "hermes", RenderMode: profile.RenderEnv},
		Models: profile.ModelSlots{
			Main: &profile.ModelRef{ID: "claude-opus-4-5"},
		},
	}
	prov := provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		BaseURL:       "https://openrouter.ai/api/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if len(strategy.Plan.Files) == 0 {
		t.Fatal("expected at least one file write")
	}
	f := strategy.Plan.Files[0]
	if f.Format != "yaml" {
		t.Errorf("expected yaml format, got %s", f.Format)
	}
	if f.Scope != ScopeProfile {
		t.Errorf("expected profile scope, got %s", f.Scope)
	}
	if f.MergePolicy != MergeYAML {
		t.Errorf("expected yaml_merge policy, got %s", f.MergePolicy)
	}
}

func TestHermesAdapter_UsesHermesHomeIsolation(t *testing.T) {
	a := HermesAdapter{}
	p := profile.Profile{
		Name:         "test-profile",
		ProviderSlug: "openrouter",
		Target:       profile.TargetConfig{App: "hermes", RenderMode: profile.RenderEnv},
		Models: profile.ModelSlots{
			Main: &profile.ModelRef{ID: "claude-opus-4-5"},
		},
	}
	prov := provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		BaseURL:       "https://openrouter.ai/api/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	env := strategy.Plan.Env
	if env["HERMES_HOME"] == "" {
		t.Error("expected HERMES_HOME to be set")
	}
	if env["HERMES_INFERENCE_PROVIDER"] == "" {
		t.Error("expected HERMES_INFERENCE_PROVIDER to be set")
	}
	if env["HERMES_INFERENCE_MODEL"] != "claude-opus-4-5" {
		t.Errorf("expected HERMES_INFERENCE_MODEL=claude-opus-4-5, got %s", env["HERMES_INFERENCE_MODEL"])
	}

	// File should be inside HERMES_HOME.
	if len(strategy.Plan.Files) == 0 {
		t.Fatal("expected file")
	}
	expectedDir := env["HERMES_HOME"]
	f := strategy.Plan.Files[0]
	if f.Path[:len(expectedDir)] != expectedDir {
		t.Errorf("expected file dir %s, got %s", expectedDir, f.Path)
	}
}

func TestHermesModelSlots_MainCompressionVisionWebExtract(t *testing.T) {
	a := HermesAdapter{}
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openrouter",
		Target:       profile.TargetConfig{App: "hermes", RenderMode: profile.RenderEnv},
		Models: profile.ModelSlots{
			Main:        &profile.ModelRef{ID: "claude-opus-4-5"},
			Compression: &profile.ModelRef{ID: "gemini-3-flash-preview"},
			Vision:      &profile.ModelRef{ID: "gpt-4o"},
			WebExtract:  &profile.ModelRef{ID: "gemini-3-flash-preview"},
		},
	}
	prov := provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		BaseURL:       "https://openrouter.ai/api/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if len(strategy.Plan.Files) == 0 {
		t.Fatal("expected file")
	}
	content := strategy.Plan.Files[0].Content
	// Should reference auxiliary roles.
	if !containsAny(content, "auxiliary", "compression", "vision", "web_extract") {
		t.Errorf("config missing auxiliary roles: %s", content)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) > 0 && len(sub) > 0 && containsSubstr(s, sub) {
			return true
		}
	}
	return false
}

func containsSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestZedAdapter_DarwinWarnsEnvInjectionPartial(t *testing.T) {
	a := ZedAdapter{}
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openrouter",
		Target:       profile.TargetConfig{App: "zed", RenderMode: profile.RenderEnv},
		Models: profile.ModelSlots{
			Main: &profile.ModelRef{ID: "claude-sonnet-4-5"},
		},
	}
	prov := provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Zed should warn about credential injection.
	if len(strategy.Hazards) == 0 && len(strategy.ManualSteps) == 0 {
		t.Error("expected hazards or manual steps for Zed")
	}
}

func TestZedAdapter_GeneratesSettingsPatchWithoutSecrets(t *testing.T) {
	a := ZedAdapter{}
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openrouter",
		Target:       profile.TargetConfig{App: "zed", RenderMode: profile.RenderEnv},
		Models: profile.ModelSlots{
			Main:     &profile.ModelRef{ID: "claude-sonnet-4-5"},
			Subagent: &profile.ModelRef{ID: "claude-haiku-4"},
		},
	}
	prov := provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		BaseURL:       "https://openrouter.ai/api/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if len(strategy.Plan.Files) == 0 {
		t.Fatal("expected settings patch file")
	}
	content := strategy.Plan.Files[0].Content
	if containsSubstr(content, "sk-test") {
		t.Error("settings patch must not contain raw secret")
	}
	if !containsSubstr(content, "aegiskeys-router") {
		t.Error("expected provider key in settings patch")
	}
}

func TestIntelliJAdapter_RejectsCredentialProviderInjection(t *testing.T) {
	a := IntelliJAdapter{}
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}

	if a.CanInjectCredential(prov) {
		t.Error("IntelliJ should not support credential injection")
	}
	if !a.SupportsProvider(prov) {
		t.Error("IntelliJ should support providers for guided setup")
	}

	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openai",
		Target:       profile.TargetConfig{App: "intellij", RenderMode: profile.RenderEnv},
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strategy.Plan.Command != "idea" {
		t.Errorf("expected command=idea, got %s", strategy.Plan.Command)
	}
	if len(strategy.ManualSteps) == 0 {
		t.Error("IntelliJ should have manual steps for credential handoff")
	}
	if len(strategy.Plan.Files) == 0 {
		t.Fatal("expected VM options file")
	}
	content := strategy.Plan.Files[0].Content
	if containsSubstr(content, "sk-test") {
		t.Error("VM options must not contain raw secret")
	}
}

func TestFileWrite_RedactCheckField_Present(t *testing.T) {
	// Verify FileWrite has RedactCheck field (compile-time check).
	f := FileWrite{
		Path:        "/tmp/test.json",
		Format:      "json",
		RedactCheck: true,
	}
	if !f.RedactCheck {
		t.Error("RedactCheck field should be settable")
	}
}

func TestLaunchStrategy_WrapsLaunchPlan(t *testing.T) {
	strategy := &LaunchStrategy{
		Plan: LaunchPlan{
			Command: "aider",
			Env:     map[string]string{"KEY": "value"},
		},
		ManualSteps: []ManualStep{{Title: "Step 1"}},
		Hazards:     []Hazard{{Title: "Hazard 1"}},
	}
	if strategy.Plan.Command != "aider" {
		t.Error("Plan should be accessible")
	}
	if len(strategy.ManualSteps) != 1 {
		t.Error("ManualSteps should be accessible")
	}
	if len(strategy.Hazards) != 1 {
		t.Error("Hazards should be accessible")
	}
}

func TestResolveLaunchStrategy_PlanOverrides(t *testing.T) {
	registry := NewRegistry()
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openai",
		Target:       profile.TargetConfig{App: "aider", RenderMode: profile.RenderArgs},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}},
		Env:          map[string]string{"CUSTOM_VAR": "custom-value"},
		Args:         []string{"--flag"},
	}
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := ResolveLaunchStrategy(p, prov, key, registry)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategy: %v", err)
	}
	if strategy.Plan.Env["CUSTOM_VAR"] != "custom-value" {
		t.Errorf("profile env override not applied")
	}
	// Profile args should be appended.
	found := false
	for _, arg := range strategy.Plan.Args {
		if arg == "--flag" {
			found = true
		}
	}
	if !found {
		t.Errorf("profile args not appended, got %v", strategy.Plan.Args)
	}
}

// TestAdapterGoldenFixes_NoSecretLeak asserts that no adapter writes the raw
// secret to plaintext config files or includes it in preview lines. The
// secret is EXPECTED in env vars — that's the credential-injection contract.
// What must never happen: the secret sitting in a user-visible config file or
// echoed in preview output.
func TestAdapterGoldenFixes_NoSecretLeak(t *testing.T) {
	registry := NewRegistry()
	secretValue := "sk-this-is-a-real-secret-value-1234567890abcdef"
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		Endpoints:     provider.EndpointSpec{BaseURL: "https://api.openai.com/v1"},
	}
	key := &secret.SecretRecord{ID: "key_test", ProviderSlug: "openai", Secret: secretValue, Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)}

	cases := []struct {
		id   string
		prof profile.Profile
	}{
		{"crush", profile.Profile{Name: "c", ProviderSlug: "openai", Target: profile.TargetConfig{App: "crush", RenderMode: profile.RenderEnvConfig}, Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-5"}}}},
		{"aider", profile.Profile{Name: "a", ProviderSlug: "openai", Target: profile.TargetConfig{App: "aider", RenderMode: profile.RenderEnvArgs}, Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-5"}}}},
		{"cline", profile.Profile{Name: "cl", ProviderSlug: "openai", Target: profile.TargetConfig{App: "cline", RenderMode: profile.RenderEnvConfig}, Models: profile.ModelSlots{Planner: &profile.ModelRef{ID: "gpt-5"}}}},
		{"hermes", profile.Profile{Name: "h", ProviderSlug: "openai", Target: profile.TargetConfig{App: "hermes", RenderMode: profile.RenderEnvConfig}, Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-5"}}}},
		{"qwen", profile.Profile{Name: "q", ProviderSlug: "openai", Target: profile.TargetConfig{App: "qwen", RenderMode: profile.RenderEnvConfig}, Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "qwen-plus"}}}},
		{"goose", profile.Profile{Name: "gs", ProviderSlug: "openai", Target: profile.TargetConfig{App: "goose", RenderMode: profile.RenderEnvConfig}, Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-5"}}}},
	}

	for _, c := range cases {
		c := c
		t.Run(c.id, func(t *testing.T) {
			a, ok := registry.Get(c.id)
			if !ok {
				t.Skipf("adapter %s not registered", c.id)
				return
			}
			rendered, err := a.Render(c.prof, prov, key)
			if err != nil {
				t.Skipf("adapter %s cannot render with this provider: %v", c.id, err)
				return
			}
			if rendered == nil {
				t.Fatalf("%s: nil strategy", c.id)
			}
			// Config files must not carry the secret in plaintext.
			for _, f := range rendered.Plan.Files {
				if strings.Contains(f.Content, secretValue) {
					t.Errorf("%s: file %s content contains raw secret", c.id, f.Path)
				}
			}
			// Preview lines must not leak the secret.
			for _, p := range rendered.Plan.Preview {
				if strings.Contains(p, secretValue) {
					t.Errorf("%s: preview line leaks secret: %s", c.id, truncateStr(p, 50))
				}
			}
			// Manual step descriptions must not leak.
			for _, ms := range rendered.ManualSteps {
				if strings.Contains(ms.Title, secretValue) || strings.Contains(ms.Description, secretValue) {
					t.Errorf("%s: manual step leaks secret", c.id)
				}
			}
		})
	}
}

func TestClaudeCodeAdapter(t *testing.T) {
	a := ClaudeCodeAdapter{}
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openrouter",
		Target:       profile.TargetConfig{App: "claude", RenderMode: profile.RenderEnv},
		Models: profile.ModelSlots{
			Main:     &profile.ModelRef{ID: "openrouter/claude-3.5-sonnet"},
			Fast:     &profile.ModelRef{ID: "openrouter/claude-3-haiku"},
			Planner:  &profile.ModelRef{ID: "openrouter/claude-3-opus"},
			Subagent: &profile.ModelRef{ID: "openrouter/claude-3.5-haiku"},
		},
	}
	prov := provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		EnvVar:        "OPENROUTER_API_KEY",
		BaseURL:       "https://openrouter.ai/api/v1",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	strategy, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Must map models to specific Anthropic env vars.
	envChecks := map[string]string{
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "openrouter/claude-3.5-sonnet",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "openrouter/claude-3-haiku",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "openrouter/claude-3-opus",
		"CLAUDE_CODE_SUBAGENT_MODEL":     "openrouter/claude-3.5-haiku",
		"ANTHROPIC_AUTH_TOKEN":           "sk-test",
		"ANTHROPIC_API_KEY":              "",
		"ANTHROPIC_BASE_URL":             "https://openrouter.ai/api",
	}

	for k, want := range envChecks {
		if got := strategy.Plan.Env[k]; got != want {
			t.Errorf("expected %s=%q, got %q", k, want, got)
		}
	}

	// Should not have --model
	for _, arg := range strategy.Plan.Args {
		if arg == "--model" {
			t.Errorf("expected no --model arg, but got one")
		}
	}
}
