package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"charm.land/bubbles/v2/textinput"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// ---------------------------------------------------------------------------
// Wizard state machine
// ---------------------------------------------------------------------------

// WizardStep identifies a step in the profile creation wizard.
type WizardStep string

const (
	StepIntent     WizardStep = "intent"
	StepApp        WizardStep = "app"
	StepSurface    WizardStep = "surface"
	StepProvider   WizardStep = "provider"
	StepCredential WizardStep = "credential"
	StepModels     WizardStep = "models"
	StepRuntime    WizardStep = "runtime"
	StepHazards    WizardStep = "hazards"
	StepName       WizardStep = "name"
	StepPreview    WizardStep = "preview"
	StepSave       WizardStep = "save"
)

// IntentOption represents a top-level action the user can choose.
type IntentOption string

const (
	IntentCreateProfile IntentOption = "create_profile"
	IntentAddKey        IntentOption = "add_key"
	IntentAddProvider   IntentOption = "add_provider"
	IntentImportConfig  IntentOption = "import_config"
	IntentAddLocal      IntentOption = "add_local"
	IntentAddProxy      IntentOption = "add_proxy"
)

// ProfileDraft accumulates choices through the wizard.
type ProfileDraft struct {
	Name string

	AppID       string
	Surface     string
	SupportMode string

	ProviderSlug string
	KeyID        string
	NewKey       *KeyDraft

	Models profile.ModelSlots
	Env    map[string]string
	Args   []string
	Files  []adapter.FileWrite

	ManualSteps []adapter.ManualStep
	Hazards     []adapter.Hazard

	Options map[string]string
}

// KeyDraft holds inline key-creation data.
type KeyDraft struct {
	Label        string
	Secret       string
	ProviderSlug string
	Tags         []string
}

// Wizard holds the wizard state within the TUI model.
type wizardState struct {
	active  bool
	step    WizardStep
	draft   ProfileDraft
	hazards []adapter.Hazard
	manual  []adapter.ManualStep
	// For list-based steps, the selected index.
	selected int
	// The text input for the current step.
	input string
	// Error message for the current step.
	errMsg string
	// reg is the adapter registry used for contract lookups.
	reg *adapter.Registry

	// modelSlotInputs holds text inputs for each model slot during StepModels.
	modelSlotInputs map[string]textinput.Model
	activeModelSlot int

	// fetchedModels holds live-fetched models for the selected provider.
	// Populated asynchronously when entering StepModels for dynamic providers.
	fetchedModels []provider.ProviderModel
	// fetchingModels is true while a model-catalog fetch is in flight.
	fetchingModels bool

	// nameInput captures the desired profile name before saving.
	nameInput textinput.Model
}

// NextStep derives the next step from the draft + app contract.
func (w *wizardState) NextStep() WizardStep {
	c := adapter.AppSupportContract{}
	usesCatalog := false
	if a, ok := w.reg.Get(w.draft.AppID); ok {
		c = a.Contract()
		_, usesCatalog = a.(adapter.ProviderCatalogAdapter)
	}

	switch w.step {
	case StepIntent:
		if w.draft.AppID != "" {
			if usesCatalog {
				return StepRuntime
			}
			return StepProvider
		}
		return StepApp

	case StepApp:
		// Skip surface selection for now (most apps have one surface).
		if usesCatalog {
			return StepRuntime
		}
		return StepProvider

	case StepProvider:
		if c.CanInjectSecrets {
			return StepCredential
		}
		return StepModels

	case StepCredential:
		if len(c.ModelSlots) > 0 {
			return StepModels
		}
		return StepRuntime

	case StepModels:
		if c.CanIsolateProfile || c.CanPatchConfig || len(c.Hazards) > 0 || c.RequiresManualStep {
			return StepRuntime
		}
		return StepName

	case StepRuntime:
		if len(w.hazards) > 0 || len(w.manual) > 0 {
			return StepHazards
		}
		return StepName

	case StepHazards:
		return StepName

	case StepName:
		return StepPreview

	case StepPreview:
		return StepSave

	default:
		return StepPreview
	}
}

// PrevStep returns the previous step.
func (w *wizardState) PrevStep() WizardStep {
	usesCatalog := false
	if a, ok := w.reg.Get(w.draft.AppID); ok {
		_, usesCatalog = a.(adapter.ProviderCatalogAdapter)
	}
	switch w.step {
	case StepApp:
		return StepIntent
	case StepProvider:
		if w.draft.AppID != "" {
			return StepApp
		}
		return StepIntent
	case StepCredential:
		return StepProvider
	case StepModels:
		if usesCatalog {
			return StepApp
		}
		if _, ok := w.reg.Get(w.draft.AppID); ok {
			return StepProvider
		}
		return StepProvider
	case StepRuntime:
		if usesCatalog {
			return StepApp
		}
		return StepModels
	case StepHazards:
		return StepRuntime
	case StepName:
		if len(w.hazards) > 0 {
			return StepHazards
		}
		if _, ok := w.reg.Get(w.draft.AppID); ok {
			return StepRuntime
		}
		return StepModels
	case StepPreview:
		return StepName
	default:
		return StepIntent
	}
}

// CanAdvance reports whether the current step has valid input to proceed.
func (w *wizardState) CanAdvance() bool {
	switch w.step {
	case StepIntent:
		return true // selection always valid
	case StepApp:
		return w.selected >= 0 // app selection is always valid (has items)
	case StepProvider:
		return w.selected >= 0
	case StepCredential:
		return w.selected >= 0 || w.draft.NewKey != nil
	case StepModels:
		return true // optional
	case StepRuntime:
		return true // optional
	case StepHazards:
		return true
	case StepPreview:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Intent options
// ---------------------------------------------------------------------------

// intentOptions lists the top-level intents shown when the wizard starts.
// Only implemented options are listed — dead options that close the wizard
// without action are intentionally omitted until their workflows exist.
var intentOptions = []struct {
	id    IntentOption
	title string
	desc  string
}{
	{IntentCreateProfile, "Create launch profile", "App-first guided profile creation"},
	{IntentAddKey, "Add API key", "Store a new encrypted credential"},
	{IntentAddProvider, "Add provider/router", "Register a new API provider"},
}

// ---------------------------------------------------------------------------
// App selection groups
// ---------------------------------------------------------------------------

type appGroup struct {
	title       string
	description string
	adapterIDs  []string
}

// appGroups organizes adapters by support tier for the wizard.
var appGroups = []appGroup{
	{
		title:       "FULL ENV / CLI",
		description: "AegisKeys injects secrets and launches",
		adapterIDs:  []string{"aider", "hermes", "crush", "qwen", "claude", "free-claude", "cline", "goose", "vibe", "codex", "mimo", "opencode", "openhands", "gemini", "copilot", "continue"},
	},
	{
		title:       "ADVANCED GUI / IDE",
		description: "AegisKeys configures app but may need keychain/manual step",
		adapterIDs:  []string{"zed"},
	},
	{
		title:       "GUIDED",
		description: "AegisKeys guides launch but app controls auth",
		adapterIDs:  []string{"intellij", "roo", "kilo", "cursor"},
	},
	{
		title:       "CUSTOM / LOCAL",
		description: "Generic or no-key",
		adapterIDs:  []string{"generic"},
	},
}

// ---------------------------------------------------------------------------
// App detection
// ---------------------------------------------------------------------------

// DetectedApp represents an app found on the local system.
type DetectedApp struct {
	AppID     string
	Name      string
	Command   string
	Installed bool
	ConfigDir string
}

// detectInstalledApps scans for known coding agents on the system.
func (m *model) detectInstalledApps() []DetectedApp {
	found := []DetectedApp{}
	for _, group := range appGroups {
		for _, id := range group.adapterIDs {
			if id == "generic" {
				continue
			}
			if a, ok := m.adapterRegistry.Get(id); ok {
				cmd := a.DefaultCommand()
				if cmd == "" {
					continue
				}
				installed := isCommandAvailable(cmd)
				found = append(found, DetectedApp{
					AppID:     id,
					Name:      a.DisplayName(),
					Command:   cmd,
					Installed: installed,
				})
			}
		}
	}
	return found
}

// isCommandAvailable checks if a binary is on PATH.
func isCommandAvailable(name string) bool {
	if name == "" {
		return false
	}
	return adapter.FindBinary(name) != ""
}

// isAppInstalled checks if an app is installed by ID.
func (m *model) isAppInstalled(appID string) bool {
	if a, ok := m.adapterRegistry.Get(appID); ok {
		return isCommandAvailable(a.DefaultCommand())
	}
	return false
}

// appConfigDir returns the config directory for an app, if detectable.
func appConfigDir(appID string) string {
	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}
	switch appID {
	case "crush":
		return filepath.Join(home, ".config", "crush")
	case "aider":
		return filepath.Join(home, ".aider")
	case "cline":
		return filepath.Join(home, ".cline")
	case "hermes":
		return filepath.Join(home, ".hermes")
	case "qwen":
		return filepath.Join(home, ".qwen")
	case "goose":
		return filepath.Join(home, ".config", "goose")
	case "vibe":
		return filepath.Join(home, ".vibe")
	case "opencode":
		return filepath.Join(home, ".config", "opencode")
	case "zed":
		return filepath.Join(home, ".config", "zed")
	case "intellij":
		return filepath.Join(home, ".config", "JetBrains")
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Draft helpers
// ---------------------------------------------------------------------------

// deriveProfileName auto-generates a profile name from app + provider.
func (w *wizardState) deriveProfileName() string {
	if w.draft.Name != "" {
		return w.draft.Name
	}
	app := w.draft.AppID
	prov := w.draft.ProviderSlug
	if app != "" && prov != "" {
		return fmt.Sprintf("%s-%s", app, prov)
	}
	if app != "" {
		return app
	}
	return "new-profile"
}

// supportBadge returns a display badge for the app's support level.
func (m *model) supportBadge(appID string) string {
	if a, ok := m.adapterRegistry.Get(appID); ok {
		c := a.Contract()
		mode := "CUSTOM"
		switch c.SupportLevel {
		case adapter.SupportFullEnv:
			mode = "ENV"
		case adapter.SupportEnvConfig:
			mode = "ENV+CONFIG"
		case adapter.SupportConfigKeychain:
			mode = "KEYCHAIN"
		case adapter.SupportLauncherIsolation:
			mode = "ISOLATED"
		case adapter.SupportManualCredential:
			mode = "MANUAL"
		}
		if _, ok := a.(adapter.ProviderCatalogAdapter); ok {
			mode = "CATALOG"
		}
		return fmt.Sprintf("%s/%s", mode, adapter.DemotedConfidence(c))
	}
	return "CUSTOM"
}

// wizardUsesProviderCatalog reports whether the selected app renders a
// multi-provider catalog instead of a single provider-only launch config.
func (m *model) wizardUsesProviderCatalog() bool {
	a, ok := m.adapterRegistry.Get(m.wizard.draft.AppID)
	if !ok || a == nil {
		return false
	}
	_, ok = a.(adapter.ProviderCatalogAdapter)
	return ok
}

type wizardCatalogProviderStatus struct {
	Name     string
	Slug     string
	Included bool
	Reason   string
}

// wizardCatalogProviderStatuses mirrors the catalog resolver's inclusion rules
// without exposing secret values: compatible local/no-auth providers are
// included, compatible keyed providers are included when an injectable key is
// present, and everything else is shown as skipped with a reason.
func (m *model) wizardCatalogProviderStatuses() []wizardCatalogProviderStatus {
	a, ok := m.adapterRegistry.Get(m.wizard.draft.AppID)
	if !ok || a == nil {
		return nil
	}
	out := make([]wizardCatalogProviderStatus, 0, len(m.providers.Providers))
	for i := range m.providers.Providers {
		prov := m.providers.Providers[i]
		prov.Normalize()
		status := wizardCatalogProviderStatus{
			Name: prov.DisplayName(),
			Slug: prov.Slug,
		}
		if !a.SupportsProvider(prov) {
			status.Reason = "unsupported provider type"
			out = append(out, status)
			continue
		}
		if !prov.NeedsKey() {
			status.Included = true
			status.Reason = "local/no credential"
			out = append(out, status)
			continue
		}
		if m.wizardHasInjectableKey(prov.Slug) {
			status.Included = true
			status.Reason = "credential available"
		} else {
			status.Included = true
			status.Reason = "credential not injected"
		}
		out = append(out, status)
	}
	return out
}

func (m *model) wizardHasInjectableKey(providerSlug string) bool {
	return m.wizardFirstInjectableKeyID(providerSlug) != ""
}

func (m *model) wizardFirstInjectableKeyID(providerSlug string) string {
	if m.vaultSession != nil && m.vaultSession.vault != nil {
		for i := range m.vaultSession.vault.Keys {
			rec := &m.vaultSession.vault.Keys[i]
			if provider.CredentialCompatible(providerSlug, rec.ProviderSlug) && rec.AllowAccess(secret.AccessInjectEnv) == nil {
				return rec.ID
			}
		}
		return ""
	}
	return ""
}

func (m *model) selectWizardCatalogDefault() error {
	a, ok := m.adapterRegistry.Get(m.wizard.draft.AppID)
	if !ok || a == nil {
		return fmt.Errorf("adapter %q is not registered", m.wizard.draft.AppID)
	}

	var localDefault string
	for i := range m.providers.Providers {
		prov := m.providers.Providers[i]
		prov.Normalize()
		if !a.SupportsProvider(prov) {
			continue
		}
		if !prov.NeedsKey() {
			if localDefault == "" {
				localDefault = prov.Slug
			}
			continue
		}
		if keyID := m.wizardFirstInjectableKeyID(prov.Slug); keyID != "" {
			m.wizard.draft.ProviderSlug = prov.Slug
			m.wizard.draft.KeyID = keyID
			return nil
		}
	}

	if localDefault != "" {
		m.wizard.draft.ProviderSlug = localDefault
		m.wizard.draft.KeyID = ""
		return nil
	}

	return fmt.Errorf("No launch-ready providers for this catalog. Unlock the vault and add a key for a compatible provider, or add a local/no-key provider.")
}

// wizardVisibleKeys returns keys matching the selected provider (or all keys
// if no provider is selected yet). Used for both display and selection so the
// index always resolves to the correct key.
func (m *model) wizardVisibleKeys() []secret.MaskedKeyItem {
	if m.wizard.draft.ProviderSlug == "" {
		return m.keys
	}
	out := []secret.MaskedKeyItem{}
	for _, k := range m.keys {
		if provider.CredentialCompatible(m.wizard.draft.ProviderSlug, k.ProviderSlug) {
			out = append(out, k)
		}
	}
	return out
}

// wizardVisibleApps returns the ordered list of registered adapter IDs shown
// in the wizard app step. Used for render, clamp, and advance so the index
// always resolves to the same list.
func (m *model) wizardVisibleApps() []string {
	var out []string
	for _, group := range appGroups {
		for _, id := range group.adapterIDs {
			if _, ok := m.adapterRegistry.Get(id); ok {
				out = append(out, id)
			}
		}
	}
	return out
}

// wizardModelCandidates returns the provider catalog models suitable for the
// given slot name. For dynamic providers, it prefers live-fetched models over
// the static list stored in providers.json. Falls back to static if neither
// is available.
func (m *model) wizardModelCandidates(_ string) []provider.ProviderModel {
	if m.wizardUsesProviderCatalog() {
		var models []provider.ProviderModel
		for _, st := range m.wizardCatalogProviderStatuses() {
			if !st.Included {
				continue
			}
			prov := m.providers.Find(st.Slug)
			if prov == nil {
				continue
			}
			models = append(models, prov.Models...)
		}
		return models
	}
	if m.wizard.draft.ProviderSlug == "" {
		return nil
	}
	// Prefer live-fetched models (dynamic catalog).
	if len(m.wizard.fetchedModels) > 0 {
		return m.wizard.fetchedModels
	}
	prov := m.providers.Find(m.wizard.draft.ProviderSlug)
	if prov == nil {
		return nil
	}
	if len(prov.Models) == 0 {
		return nil
	}
	return prov.Models
}
