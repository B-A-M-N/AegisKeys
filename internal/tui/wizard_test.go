package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

func TestWizard_StartsAtIntent(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.startWizard()
	if !m.wizard.active {
		t.Fatal("wizard should be active")
	}
	if m.wizard.step != StepIntent {
		t.Errorf("expected StepIntent, got %s", m.wizard.step)
	}
}

func TestWizard_NavigateIntentToApp(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.startWizard()
	// Select first intent (CreateProfile) and press enter.
	m.wizard.selected = 0
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	if m.wizard.step != StepApp {
		t.Errorf("expected StepApp, got %s", m.wizard.step)
	}
}

func TestWizard_SelectAppSetsDraft(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.startWizard()
	m.wizard.step = StepApp
	m.wizard.selected = 0
	// Verify wizard is active and at app step.
	if !m.wizard.active {
		t.Fatal("wizard should be active")
	}
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	if m.wizard.draft.AppID == "" {
		t.Errorf("expected AppID to be set, step=%s active=%v", m.wizard.step, m.wizard.active)
	}
}

func TestWizard_BackFromAppReturnsToIntent(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.startWizard()
	m.wizard.step = StepApp
	_, _ = m.Update(tea.KeyPressMsg{Text: "esc"})
	if m.wizard.step != StepIntent {
		t.Errorf("expected StepIntent after esc, got %s", m.wizard.step)
	}
}

func TestWizard_EscFromIntentCancels(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.startWizard()
	_, _ = m.Update(tea.KeyPressMsg{Text: "esc"})
	if m.wizard.active {
		t.Error("wizard should be inactive after esc from intent")
	}
}

func TestWizard_PreviewShowsSaveHint(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.startWizard()
	m.wizard.step = StepPreview
	v := m.wizardView(NewStyles("vault"))
	if !strings.Contains(v, "Save") && !strings.Contains(v, "preview") {
		t.Errorf("expected preview to show save hint, got: %s", v[:min(len(v), 200)])
	}
}

func TestWizard_DraftInitializesMaps(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.startWizard()
	if m.wizard.draft.Env == nil {
		t.Error("draft.Env should be initialized")
	}
	if m.wizard.draft.Options == nil {
		t.Error("draft.Options should be initialized")
	}
}

// TestDashboard_ShowsDetectedApps verifies the dashboard renders app detection.
func TestDashboard_ShowsDetectedApps(t *testing.T) {
	m := newTestModel(t)
	m.unlocked = true
	v := stripANSIForTest(m.dashboardView(NewStyles("vault")))
	if !strings.Contains(v, "Detected apps") {
		t.Error("dashboard should show detected apps section")
	}
}

// TestDashboard_ShowsQuickActions verifies quick actions section.
func TestDashboard_ShowsQuickActions(t *testing.T) {
	m := newTestModel(t)
	v := stripANSIForTest(m.dashboardView(NewStyles("vault")))
	if !strings.Contains(v, "Quick actions") {
		t.Error("dashboard should show quick actions section")
	}
}

// TestDashboard_ShowsReadyProfiles verifies the ready profiles section.
func TestDashboard_ShowsReadyProfiles(t *testing.T) {
	m := newTestModel(t)
	v := stripANSIForTest(m.dashboardView(NewStyles("vault")))
	if !strings.Contains(v, "Ready profiles") {
		t.Error("dashboard should show ready profiles section")
	}
}

// TestDashboard_ShowsSecuritySection verifies the security summary.
func TestDashboard_ShowsSecuritySection(t *testing.T) {
	m := newTestModel(t)
	v := stripANSIForTest(m.dashboardView(NewStyles("vault")))
	if !strings.Contains(v, "Security") {
		t.Error("dashboard should show security section")
	}
}

// TestDetectInstalledApps_NoPanic verifies detection runs without panic.
func TestDetectInstalledApps_NoPanic(t *testing.T) {
	m := newTestModel(t)
	_ = m.detectInstalledApps()
}

// TestIsAppInstalled_KnownApp verifies installation check.
func TestIsAppInstalled_KnownApp(t *testing.T) {
	m := newTestModel(t)
	// Generic adapter has no command, so it should be false.
	if m.isAppInstalled("generic") {
		t.Error("generic app should not be 'installed' (no command)")
	}
}

// TestSupportBadge_KnownApps verifies badge assignment.
func TestSupportBadge_KnownApps(t *testing.T) {
	m := newTestModel(t)
	if m.supportBadge("aider") != "ENV/verified" {
		t.Errorf("Aider badge = %s, want ENV/verified", m.supportBadge("aider"))
	}
	if m.supportBadge("zed") != "KEYCHAIN/guided" {
		t.Errorf("Zed badge = %s, want KEYCHAIN/guided", m.supportBadge("zed"))
	}
	if m.supportBadge("intellij") != "ISOLATED/guided" {
		t.Errorf("IntelliJ badge = %s, want ISOLATED/guided", m.supportBadge("intellij"))
	}
}

// TestWizardView_RendersEachStep verifies the view renders every step.
func TestWizardView_RendersEachStep(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	steps := []WizardStep{StepIntent, StepApp, StepProvider, StepCredential, StepModels, StepRuntime, StepHazards, StepPreview}
	for _, step := range steps {
		m.wizard.active = true
		m.wizard.step = step
		v := m.wizardView(NewStyles("vault"))
		if len(v) == 0 {
			t.Errorf("wizardView returned empty for step %s", step)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestWizard_ProviderCompatViews_IncludesIncompatible verifies the provider
// step shows ALL providers (compatible first, then incompatible) instead of
// hiding incompatible ones — the core fix for the dead-end wizard.
func TestWizard_ProviderCompatViews_IncludesIncompatible(t *testing.T) {
	m := newTestModel(t)
	m.startWizard()
	// Select Crush, which only accepts OpenAI/Local providers. (Navigating via
	// keypress would land on Aider, the first app, which accepts everything.)
	m.wizard.step = StepProvider
	m.wizard.draft.AppID = "crush"
	m.wizard.active = true

	// Add a provider explicitly marked Google-compatible. Crush rejects it, so
	// this provider is genuinely incompatible — and because its Compatibility
	// field is already set, Normalize won't overwrite it. This is the scenario
	// that previously dead-ended the wizard.
	_ = m.providers.Add(provider.Provider{
		Name: "gemini-clone", Slug: "gemini-clone", EnvVar: "GEMINI_API_KEY",
		BaseURL:       "https://generativelanguage.googleapis.com",
		Compatibility: provider.CompatGoogle,
	})

	views := m.wizardProviderCompatViews()
	if len(views) != len(m.providers.Providers) {
		t.Errorf("compat views count = %d, want %d (all providers)", len(views), len(m.providers.Providers))
	}
	// Compatible must come before incompatible.
	sawIncompatible := false
	for _, v := range views {
		if !v.Compatible {
			sawIncompatible = true
		} else if sawIncompatible {
			t.Error("compatible provider appeared after an incompatible one")
		}
	}
	if !sawIncompatible {
		t.Error("expected at least one incompatible provider (gemini-clone should be filtered by Crush)")
	}
	// The incompatible entry must explain why.
	found := false
	for _, v := range views {
		if v.Provider.Slug == "gemini-clone" {
			found = true
			if v.Reason == "" {
				t.Error("incompatible provider should have a reason")
			}
		}
	}
	if !found {
		t.Error("gemini-clone provider not found in compat views")
	}
}

// TestWizard_RepairProviderAsOpenAI verifies the u-key repair path.
func TestWizard_RepairProviderAsOpenAI(t *testing.T) {
	m := newTestModel(t)
	// Add a bare custom provider that Crush rejects (no compat).
	_ = m.providers.Add(provider.Provider{
		Name: "beepboop", Slug: "beepboop",
		EnvVar: "BEEPBOOP_API_KEY", BaseURL: "https://api.beepboop.com/v1",
	})

	if ok := m.repairProviderAsOpenAI("beepboop"); !ok {
		t.Fatalf("repair should succeed, status: %s", m.statusMsg)
	}
	p := m.providers.Find("beepboop")
	if p == nil {
		t.Fatal("beepboop not found after repair")
	}
	if p.Compatibility != provider.CompatOpenAI {
		t.Errorf("Compatibility = %q, want openai", p.Compatibility)
	}
	// After repair it must pass strict validation.
	if err := p.ValidateStrict(); err != nil {
		t.Errorf("repaired provider failed validation: %v", err)
	}
}

// TestWizard_ProviderView_NoDeadEnd verifies the provider view never shows a
// bare "No providers match" — it must show the list with repair hints.
func TestWizard_ProviderView_NoDeadEnd(t *testing.T) {
	m := newTestModel(t)
	m.startWizard()
	m.wizard.step = StepApp
	m.wizard.selected = 0
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	if m.wizard.step != StepProvider {
		t.Fatalf("expected StepProvider, got %s", m.wizard.step)
	}
	v := stripANSIForTest(wizardProviderView(NewStyles("vault"), m))
	if strings.Contains(v, "No providers match this app") {
		t.Error("provider view still shows the dead-end message")
	}
	// Should show the section header and repair/restore hints.
	if !strings.Contains(v, "provider") {
		t.Errorf("provider view should mention provider, got: %s", v[:min(len(v), 200)])
	}
}

// TestWizard_EnterWizardStep_InitsModelModels verifies entering StepModels
// initializes slot inputs before render.
func TestWizard_EnterWizardStep_InitsModelModels(t *testing.T) {
	m := newTestModel(t)
	m.startWizard()
	_ = m.enterWizardStep(StepModels)
	if m.wizard.modelSlotInputs == nil {
		t.Error("modelSlotInputs should be initialized on entering StepModels")
	}
}

// Ensure these types are actually referenced.
var (
	_ = adapter.ActionLaunch
	_ = adapter.ActionPatchConfig
	_ = secret.MaskedKeyItem{}
)
