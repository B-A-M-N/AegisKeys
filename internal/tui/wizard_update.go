package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/config"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/resolve"
	"aegiskeys/internal/secret"
)

// providerCompatView pairs a provider with its compatibility status for the
// wizard provider step, so incompatible providers can be displayed with a
// reason and a repair action instead of being hidden.
type providerCompatView struct {
	Provider   provider.Provider
	Compatible bool
	Reason     string
}

// compatModeNames converts compatibility modes to readable strings.
func compatModeNames(modes []provider.CompatibilityMode) []string {
	names := make([]string, 0, len(modes))
	for _, mo := range modes {
		names = append(names, string(mo))
	}
	return names
}

// wizardProviderCompatViews returns every registered provider annotated with
// whether the selected app's adapter supports it. Compatible providers come
// first (in registry order), followed by incompatible ones. Selection indexes
// into this flat slice, so the view and the advance step never disagree.
func (m *model) wizardProviderCompatViews() []providerCompatView {
	appAdapter, ok := m.adapterRegistry.Get(m.wizard.draft.AppID)
	if !ok || appAdapter == nil {
		// No adapter matched — treat every provider as compatible so the user
		// is never blocked by a missing adapter registration.
		views := make([]providerCompatView, 0, len(m.providers.Providers))
		for i := range m.providers.Providers {
			views = append(views, providerCompatView{
				Provider:   m.providers.Providers[i],
				Compatible: true,
			})
		}
		return views
	}

	contract := appAdapter.Contract()
	accepted := contract.AcceptedCompatibility
	adapterName := appAdapter.DisplayName()

	var compat, incompat []providerCompatView
	for i := range m.providers.Providers {
		p := m.providers.Providers[i]
		p.Normalize()
		if appAdapter.SupportsProvider(p) {
			compat = append(compat, providerCompatView{Provider: p, Compatible: true})
			continue
		}
		reason := "missing or unsupported compatibility"
		if p.Compatibility != "" {
			reason = fmt.Sprintf("%s accepts %s; provider is %q",
				adapterName,
				strings.Join(compatModeNames(accepted), "/"),
				p.Compatibility,
			)
		}
		incompat = append(incompat, providerCompatView{
			Provider:   p,
			Compatible: false,
			Reason:     reason,
		})
	}
	return append(compat, incompat...)
}

// repairProviderAsOpenAI repairs a custom provider in place by marking it
// OpenAI-compatible and backfilling the auth metadata so it passes adapter
// filtering. Returns true on success.
func (m *model) repairProviderAsOpenAI(slug string) bool {
	p := m.providers.Find(slug)
	if p == nil {
		m.statusMsg = "Provider not found: " + slug
		return false
	}
	if p.Compatibility == provider.CompatAnthropic ||
		p.Compatibility == provider.CompatGoogle ||
		p.Compatibility == provider.CompatLocal {
		m.statusMsg = "Repair refused: known non-OpenAI provider protocols are not mutated"
		return false
	}
	if p.Protocol != "" && p.Protocol != provider.ProtocolOpenAI {
		m.statusMsg = "Repair refused: provider protocol is not OpenAI-compatible"
		return false
	}
	p.Compatibility = provider.CompatOpenAI
	p.Protocol = provider.ProtocolOpenAI
	if p.Auth.EnvVar == "" {
		p.Auth.EnvVar = p.EnvVar
	}
	if p.Auth.Type == "" {
		p.Auth.Type = "bearer"
	}
	if p.Auth.HeaderName == "" {
		p.Auth.HeaderName = "Authorization"
	}
	if p.Auth.Prefix == "" {
		p.Auth.Prefix = "Bearer "
	}
	if p.AuthHeader == "" {
		p.AuthHeader = "Authorization: Bearer ${KEY}"
	}
	p.Normalize()
	if err := p.ValidateStrict(); err != nil {
		m.statusMsg = "Repair failed: " + err.Error()
		return false
	}
	if err := m.providers.Save(config.ProvidersPath(m.configDir)); err != nil {
		m.statusMsg = "Save failed: " + err.Error()
		return false
	}
	m.statusMsg = "Provider repaired as OpenAI-compatible."
	return true
}

// restoreDefaultProviders merges the curated default provider list into the
// registry, rescuing a user stranded with only custom/incompatible providers.
func (m *model) restoreDefaultProviders() (tea.Model, tea.Cmd) {
	changed := m.providers.MergeDefaults(provider.DefaultProviders())
	if !changed {
		m.statusMsg = "Default providers already present."
		return m, nil
	}
	m.providers.NormalizeAll()
	for i := range m.providers.Providers {
		if err := m.providers.Providers[i].ValidateStrict(); err != nil {
			m.statusMsg = "Provider validation failed: " + err.Error()
			return m, nil
		}
	}
	if err := m.providers.Save(config.ProvidersPath(m.configDir)); err != nil {
		m.statusMsg = "Could not save providers: " + err.Error()
		return m, nil
	}
	m.statusMsg = "Restored missing default providers."
	return m, nil
}

// wizardActiveSlotName returns the name of the currently active model slot
// during StepModels, or "" if none.
func (m *model) wizardActiveSlotName() string {
	a, ok := m.adapterRegistry.Get(m.wizard.draft.AppID)
	if !ok {
		return ""
	}
	slots := a.Contract().ModelSlots
	if len(slots) == 0 {
		return ""
	}
	idx := m.wizard.activeModelSlot
	if idx < 0 || idx >= len(slots) {
		idx = 0
		m.wizard.activeModelSlot = 0
	}
	return slots[idx].Name
}

// selectedWizardAppID returns the app ID at the current wizard selection.
func (m *model) selectedWizardAppID() string {
	idx := 0
	for _, group := range appGroups {
		for _, id := range group.adapterIDs {
			if _, ok := m.adapterRegistry.Get(id); ok {
				if idx == m.wizard.selected {
					return id
				}
				idx++
			}
		}
	}
	return ""
}
func (m *model) handleWizardKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := normalizedKey(k)

	// When in StepModels step, the active slot's text input owns character keys.
	// Tab/arrows/enter still navigate; all other keys go to the input.
	if m.wizard.step == StepModels {
		m.ensureModelInputs()
		slotName := m.wizardActiveSlotName()
		if slotName != "" {
			switch key {
			case "tab", "enter", "right", "left", "esc", "down", "up", "j", "k":
				// Navigation keys handled below.
			default:
				// Forward all other keys (including printable chars) to the slot input.
				var cmd tea.Cmd
				input := m.wizard.modelSlotInputs[slotName]
				input, cmd = input.Update(k)
				m.wizard.modelSlotInputs[slotName] = input
				return m, cmd
			}
		}
	}

	if m.wizard.step == StepName {
		switch key {
		case "tab", "enter", "esc", "up", "down":
			// Navigation handled below.
		default:
			var cmd tea.Cmd
			m.wizard.nameInput, cmd = m.wizard.nameInput.Update(k)
			return m, cmd
		}
	}

	// StepProvider action keys: repair selected provider or restore defaults.
	if m.wizard.step == StepProvider {
		switch key {
		case "u":
			views := m.wizardProviderCompatViews()
			if m.wizard.selected >= 0 && m.wizard.selected < len(views) {
				m.repairProviderAsOpenAI(views[m.wizard.selected].Provider.Slug)
			}
			return m, nil
		case "r":
			return m.restoreDefaultProviders()
		}
	}

	switch key {
	case "esc":
		// Go back a step, or cancel the wizard.
		if m.wizard.step == StepIntent {
			m.wizard.active = false
			return m, nil
		}
		m.wizard.step = m.wizard.PrevStep()
		m.wizard.errMsg = ""
		m.wizard.selected = 0
		return m, nil

	case "right":
		if m.wizard.step == StepModels {
			m.cycleWizardModel(1)
			return m, nil
		}
		ok, reason := m.wizardCanAdvance()
		if !ok {
			m.wizard.errMsg = reason
			return m, nil
		}
		return m.wizardAdvance()

	case "enter":
		if m.wizard.step == StepModels {
			// In models step, enter moves to the next slot or advances
			// past the last one.
			m.ensureModelInputs()
			if a, ok := m.adapterRegistry.Get(m.wizard.draft.AppID); ok {
				if slots := a.Contract().ModelSlots; m.wizard.activeModelSlot < len(slots)-1 {
					m.wizard.activeModelSlot++
					m.ensureModelInputs()
					return m, nil
				}
			}
		}
		ok, reason := m.wizardCanAdvance()
		if !ok {
			m.wizard.errMsg = reason
			return m, nil
		}
		return m.wizardAdvance()

	case "down", "j":
		m.wizard.selected++
		m.wizardClampSelection()
		return m, nil

	case "up", "k":
		if m.wizard.selected > 0 {
			m.wizard.selected--
		}
		return m, nil

	case "left":
		if m.wizard.step == StepModels {
			m.cycleWizardModel(-1)
			return m, nil
		}
		if m.wizard.step == StepIntent {
			m.wizard.active = false
			return m, nil
		}
		m.wizard.step = m.wizard.PrevStep()
		m.wizard.errMsg = ""
		m.wizard.selected = 0
		return m, nil

	case "tab":
		// In models step, tab moves between slot fields.
		if m.wizard.step == StepModels {
			m.wizard.activeModelSlot++
			a, ok := m.adapterRegistry.Get(m.wizard.draft.AppID)
			if ok {
				if m.wizard.activeModelSlot >= len(a.Contract().ModelSlots) {
					m.wizard.activeModelSlot = 0
				}
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *model) cycleWizardModel(dir int) {
	m.ensureModelInputs()
	slotName := m.wizardActiveSlotName()
	if slotName == "" {
		return
	}
	candidates := m.wizardModelCandidates(slotName)
	if len(candidates) == 0 {
		return
	}
	input := m.wizard.modelSlotInputs[slotName]
	current := strings.TrimSpace(input.Value())
	idx := 0
	for i, candidate := range candidates {
		if candidate.ID == current {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(candidates)) % len(candidates)
	input.SetValue(candidates[idx].ID)
	m.wizard.modelSlotInputs[slotName] = input
	m.statusMsg = slotName + " model: " + candidates[idx].ID
}

// wizardCanAdvance reports whether the current step has valid input to proceed,
// returning (false, reason) if the user needs to take action first.
func (m *model) wizardCanAdvance() (bool, string) {
	switch m.wizard.step {
	case StepApp:
		if m.selectedWizardAppID() == "" {
			return false, "Choose a supported app."
		}
	case StepProvider:
		views := m.wizardProviderCompatViews()
		if len(views) == 0 {
			return false, "No providers. Add one or restore defaults."
		}
		if m.wizard.selected < 0 || m.wizard.selected >= len(views) {
			return false, "Choose a provider."
		}
		if !views[m.wizard.selected].Compatible {
			return false, views[m.wizard.selected].Reason + " Press u to explicitly repair unknown/custom OpenAI-compatible provider metadata."
		}
	case StepCredential:
		if !m.unlocked {
			return false, "Unlock the vault first."
		}
		keys := m.wizardVisibleKeys()
		if len(keys) == 0 {
			return false, "No key exists for this provider. Add a key first."
		}
		if m.wizard.selected < 0 || m.wizard.selected >= len(keys) {
			return false, "Choose a key."
		}
	case StepModels:
		// Ensure slot inputs exist before validating.
		m.ensureModelInputs()
		// Model slots are optional but validate required ones are set.
		if err := m.commitWizardModels(); err != nil {
			return false, err.Error()
		}
	}
	return true, ""
}

// commitWizardModels converts per-slot input values into profile.ModelSlots.
func (m *model) commitWizardModels() error {
	a, ok := m.adapterRegistry.Get(m.wizard.draft.AppID)
	if !ok {
		return nil
	}
	c := a.Contract()
	if len(c.ModelSlots) == 0 {
		return nil
	}

	var models profile.ModelSlots
	for _, slot := range c.ModelSlots {
		val := ""
		if mi, ok := m.wizard.modelSlotInputs[slot.Name]; ok {
			val = strings.TrimSpace(mi.Value())
		}
		if val == "" {
			val = slot.Default
		}
		if val == "" && !slot.Optional && !m.wizardUsesProviderCatalog() {
			return fmt.Errorf("required model slot %q is empty", slot.Name)
		}
		if val == "" {
			continue
		}

		ref := &profile.ModelRef{
			ID:     val,
			Source: profile.ModelSourceManual,
			Locked: true,
		}

		switch slot.Name {
		case "main":
			models.Main = ref
		case "fast":
			models.Fast = ref
		case "weak":
			models.Weak = ref
		case "editor":
			models.Editor = ref
		case "planner":
			models.Planner = ref
		case "actor":
			models.Actor = ref
		case "subagent":
			models.Subagent = ref
		case "inline_assistant":
			models.InlineAssistant = ref
		case "commit_message":
			models.CommitMessage = ref
		case "thread_summary":
			models.ThreadSummary = ref
		case "compression":
			models.Compression = ref
		case "vision":
			models.Vision = ref
		case "web_extract":
			models.WebExtract = ref
		default:
			if models.Custom == nil {
				models.Custom = map[string]profile.ModelRef{}
			}
			models.Custom[slot.Name] = *ref
		}
	}
	m.wizard.draft.Models = models
	return nil
}

// ensureModelInputs initializes per-slot text inputs for the models step.
func (m *model) ensureModelInputs() {
	if m.wizard.modelSlotInputs == nil {
		m.wizard.modelSlotInputs = map[string]textinput.Model{}
	}
	a, ok := m.adapterRegistry.Get(m.wizard.draft.AppID)
	if !ok {
		return
	}
	// Ensure each slot has a text input and set focus/blur state so only the
	// active slot shows the cursor.
	for i, slot := range a.Contract().ModelSlots {
		input, exists := m.wizard.modelSlotInputs[slot.Name]
		if !exists {
			input = textinput.New()
			input.Placeholder = "model id"
			input.CharLimit = 120
			if slot.Default != "" {
				input.SetValue(slot.Default)
			}
			m.wizard.modelSlotInputs[slot.Name] = input
		}
		if i == m.wizard.activeModelSlot {
			// Discard the Focus() cmd here — ensureModelInputs has no cmd
			// return path. The cursor will render on the next keypress
			// cycle; the focus bool is what actually gates key input.
			_ = input.Focus()
		} else {
			input.Blur()
		}
		m.wizard.modelSlotInputs[slot.Name] = input
	}
}

// wizardAdvance moves to the next step based on current selection.
func (m *model) wizardAdvance() (tea.Model, tea.Cmd) {
	switch m.wizard.step {
	case StepIntent:
		if m.wizard.selected < len(intentOptions) {
			selected := intentOptions[m.wizard.selected]
			switch selected.id {
			case IntentCreateProfile:
				return m, m.enterWizardStep(StepApp)
			case IntentAddKey:
				m.wizard.active = false
				m.startKeyAdd()
			case IntentAddProvider:
				m.wizard.active = false
				_, _ = m.startAdd()
			default:
				m.wizard.active = false
			}
		}

	case StepApp:
		// Find the app ID at the selected index.
		idx := 0
		for _, group := range appGroups {
			for _, id := range group.adapterIDs {
				if _, ok := m.adapterRegistry.Get(id); ok {
					if idx == m.wizard.selected {
						m.wizard.draft.AppID = id
						if m.wizardUsesProviderCatalog() {
							if err := m.selectWizardCatalogDefault(); err != nil {
								m.wizard.errMsg = err.Error()
								return m, nil
							}
						}
						return m, m.enterWizardStep(m.wizard.NextStep())
					}
					idx++
				}
			}
		}

	case StepProvider:
		views := m.wizardProviderCompatViews()
		if m.wizard.selected < 0 || m.wizard.selected >= len(views) {
			m.wizard.errMsg = "Choose a provider."
			return m, nil
		}
		sel := views[m.wizard.selected]
		if !sel.Compatible {
			m.wizard.errMsg = sel.Reason + " Press u to explicitly repair unknown/custom OpenAI-compatible provider metadata."
			return m, nil
		}
		m.wizard.draft.ProviderSlug = sel.Provider.Slug
		// Skip credential step if the selected provider doesn't need a key
		// (e.g. local/no-key providers like Ollama).
		selProvider := m.providers.Find(m.wizard.draft.ProviderSlug)
		nextStep := m.wizard.NextStep()
		if nextStep == StepCredential && (selProvider == nil || !selProvider.NeedsKey()) {
			nextStep = StepModels
		}
		return m, m.enterWizardStep(nextStep)

	case StepCredential:
		if m.unlocked {
			keys := m.wizardVisibleKeys()
			if m.wizard.selected < len(keys) {
				m.wizard.draft.KeyID = keys[m.wizard.selected].ID
				// Initialize model slot inputs when entering models step.
				return m, m.enterWizardStep(m.wizard.NextStep())
			}
		}

	case StepModels:
		m.ensureModelInputs()
		if err := m.commitWizardModels(); err != nil {
			m.wizard.errMsg = err.Error()
			return m, nil
		}
		return m, m.enterWizardStep(m.wizard.NextStep())

	case StepRuntime:
		// Collect hazards from the adapter contract.
		if a, ok := m.adapterRegistry.Get(m.wizard.draft.AppID); ok {
			c := a.Contract()
			m.wizard.hazards = c.Hazards
		}
		return m, m.enterWizardStep(m.wizard.NextStep())

	case StepHazards:
		return m, m.enterWizardStep(StepName)

	case StepName:
		name := strings.TrimSpace(m.wizard.nameInput.Value())
		if name == "" {
			m.wizard.errMsg = "Profile name cannot be empty."
			return m, nil
		}
		if p := m.profiles.Find(name); p != nil {
			_ = p
			m.wizard.errMsg = fmt.Sprintf("Name %q is already taken — edit the name above.", name)
			return m, nil
		}
		m.wizard.draft.Name = name
		return m, m.enterWizardStep(StepPreview)

	case StepPreview:
		return m.wizardSave()
	}
	return m, nil
}

// wizardClampSelection ensures the selected index is valid for the current step.
func (m *model) wizardClampSelection() {
	max := m.wizardMaxSelection()
	if m.wizard.selected > max {
		m.wizard.selected = max
	}
	if m.wizard.selected < 0 {
		m.wizard.selected = 0
	}
}

// wizardMaxSelection returns the max valid index for the current step.
func (m *model) wizardMaxSelection() int {
	switch m.wizard.step {
	case StepIntent:
		return len(intentOptions) - 1
	case StepApp:
		return len(m.wizardVisibleApps()) - 1
	case StepProvider:
		return len(m.wizardProviderCompatViews()) - 1
	case StepCredential:
		keys := m.wizardVisibleKeys()
		return len(keys) - 1
	default:
		return 0
	}
}

// wizardSave persists the profile from the draft to disk.
func (m *model) wizardSave() (tea.Model, tea.Cmd) {
	d := m.wizard.draft
	name := d.Name
	if name == "" {
		name = m.wizard.deriveProfileName()
	}

	renderMode := profile.RenderEnv
	if a, ok := m.adapterRegistry.Get(d.AppID); ok {
		c := a.Contract()
		switch {
		case c.CanPatchConfig && c.CanInjectSecrets:
			renderMode = profile.RenderEnvConfig
		case c.CanPatchConfig:
			renderMode = profile.RenderConfigFile
		case c.CanInjectSecrets:
			renderMode = profile.RenderEnv
		}
	}

	p := profile.Profile{
		Name:         name,
		ProviderSlug: d.ProviderSlug,
		KeyID:        d.KeyID,
		Target: profile.TargetConfig{
			App:        d.AppID,
			RenderMode: renderMode,
		},
		Models: d.Models,
		Env:    d.Env,
		Args:   d.Args,
		Files:  targetConfigFiles(d.Files),
	}

	// Validate the profile resolves before saving so broken adapters or
	// missing keys never surface only at launch time.
	var vault *secret.Vault
	if m.vaultSession != nil {
		vault = m.vaultSession.vault
	}
	if err := resolve.ValidateResolution(p, m.providers, vault, m.adapterRegistry); err != nil {
		m.wizard.errMsg = "Cannot save: " + err.Error()
		return m, nil
	}

	if err := m.profiles.Add(p); err != nil {
		m.wizard.errMsg = err.Error()
		return m, nil
	}

	if err := profile.SaveStore(config.ProfilesPath(m.configDir), m.profiles); err != nil {
		m.wizard.errMsg = "save failed: " + err.Error()
		return m, nil
	}

	m.logAudit("profile.add", "", name)
	m.wizard.active = false
	m.statusMsg = "Profile saved: " + name
	return m, nil
}

// targetConfigFiles converts adapter FileWrites to profile TargetConfigFiles.
func targetConfigFiles(writes []adapter.FileWrite) []profile.TargetConfigFile {
	if len(writes) == 0 {
		return nil
	}
	out := make([]profile.TargetConfigFile, 0, len(writes))
	for _, w := range writes {
		out = append(out, profile.TargetConfigFile{
			Path:         w.Path,
			Format:       w.Format,
			Content:      w.Content,
			MergePolicy:  string(w.MergePolicy),
			BackupPolicy: string(w.BackupPolicy),
			Scope:        string(w.Scope),
			RedactCheck:  w.RedactCheck,
			Description:  w.Description,
		})
	}
	return out
}

// enterWizardStep transitions the wizard to a step, resetting per-step
// state and initializing step-specific resources (e.g. model slot inputs)
// before the next View render sees the step.
func (m *model) enterWizardStep(step WizardStep) tea.Cmd {
	m.wizard.step = step
	m.wizard.selected = 0
	m.wizard.errMsg = ""
	if step == StepModels {
		m.ensureModelInputs()
		// Reset any previously-fetched catalog so stale results never leak
		// across provider changes.
		m.wizard.fetchedModels = nil
		m.wizard.fetchingModels = false
		if m.wizardUsesProviderCatalog() {
			m.statusMsg = "Provider catalog mode: default provider selected automatically."
			return nil
		}
		// If the provider is dynamic and we have a key in the vault session,
		// kick off a live fetch in the background.
		if cmd := m.fetchWizardModelsCmd(); cmd != nil {
			m.wizard.fetchingModels = true
			return cmd
		}
	}
	if step == StepName {
		m.wizard.nameInput = textinput.New()
		m.wizard.nameInput.CharLimit = 80
		// Pre-fill with a unique name so the user can simply confirm or edit
		// rather than immediately hitting a uniqueness error.
		base := m.wizard.deriveProfileName()
		suggested := base
		for i := 2; m.profiles.Find(suggested) != nil; i++ {
			suggested = fmt.Sprintf("%s-%d", base, i)
		}
		m.wizard.nameInput.SetValue(suggested)
		// Focus() returns a tea.Cmd that starts cursor blinking — must be
		// returned to the runtime or the cursor never renders.
		return m.wizard.nameInput.Focus()
	}
	return nil
}

// fetchWizardModelsCmd returns a tea.Cmd that fetches the live model catalog
// for the wizard's selected provider, or nil if the provider is not dynamic
// or no API key is available.
func (m *model) fetchWizardModelsCmd() tea.Cmd {
	if m.wizardUsesProviderCatalog() {
		return nil
	}
	prov := m.providers.Find(m.wizard.draft.ProviderSlug)
	if prov == nil {
		return nil
	}
	// Fetch for any provider that can refresh its model catalog — including
	// static-configured providers that expose a refresh endpoint. Static models
	// are the configured allowlist; the refresh URL is the discovery source.
	if !prov.CanRefreshModels() {
		return nil
	}
	// We need an unlocked vault to resolve the key secret.
	if m.vaultSession == nil || m.vaultSession.vault == nil {
		return nil
	}
	// Resolve the API key for this provider.
	keyID := m.wizard.draft.KeyID
	var apiKey string
	for _, rec := range m.vaultSession.vault.Keys {
		if rec.ID == keyID {
			apiKey = rec.Secret
			break
		}
	}
	// For providers that don't need a key, still try fetching.
	if apiKey == "" && prov.NeedsKey() {
		return nil
	}
	slug := prov.Slug
	pCopy := *prov
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15_000_000_000) // 15s
		defer cancel()
		models, err := provider.RefreshModels(ctx, pCopy, apiKey)
		return wizardModelsFetchedMsg{
			providerSlug: slug,
			models:       models,
			err:          err,
		}
	}
}

// startWizard activates the profile creation wizard.
func (m *model) startWizard() {
	m.wizard = wizardState{
		active: true,
		step:   StepIntent,
		draft:  ProfileDraft{Env: map[string]string{}, Options: map[string]string{}},
		reg:    m.adapterRegistry,
	}
	m.wizard.selected = 0
}
