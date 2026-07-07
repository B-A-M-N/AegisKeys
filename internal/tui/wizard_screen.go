package tui

import (
	"fmt"
	"strings"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
)

// wizardView renders the active wizard step.
func (m *model) wizardView(s *Styles) string {
	if !m.wizard.active {
		return ""
	}
	var b strings.Builder
	b.WriteString(s.Title.Render(" Create Profile "))
	b.WriteString("\n\n")

	switch m.wizard.step {
	case StepIntent:
		b.WriteString(wizardIntentView(s, m))
	case StepApp:
		b.WriteString(wizardAppView(s, m))
	case StepProvider:
		b.WriteString(wizardProviderView(s, m))
	case StepCredential:
		b.WriteString(wizardCredentialView(s, m))
	case StepModels:
		b.WriteString(wizardModelsView(s, m))
	case StepRuntime:
		b.WriteString(wizardRuntimeView(s, m))
	case StepHazards:
		b.WriteString(wizardHazardsView(s, m))
	case StepName:
		b.WriteString(wizardNameView(s, m))
	case StepPreview:
		b.WriteString(wizardPreviewView(s, m))
	default:
		b.WriteString(s.Muted.Render("Unknown step"))
	}

	// Wizard error message (e.g. "Choose a compatible provider."). Rendered
	// prominently so the user sees why forward progress is refused instead of
	// the wizard silently ignoring Enter.
	if m.wizard.errMsg != "" {
		b.WriteString("\n\n")
		b.WriteString(s.Danger.Render("✗ " + m.wizard.errMsg))
	}

	// Navigation hints.
	b.WriteString("\n")
	b.WriteString(s.Muted.Render("← back  ·  →/Enter continue  ·  Esc cancel"))
	return b.String()
}

// wizardIntentView shows the top-level intent selection.
func wizardIntentView(s *Styles, m *model) string {
	var b strings.Builder
	b.WriteString(s.SectionHeader.Render("What do you want to add?"))
	b.WriteString("\n\n")
	for i, opt := range intentOptions {
		marker := " "
		style := s.Body
		if i == m.wizard.selected {
			marker = "›"
			style = s.SelectedRow
		}
		b.WriteString(fmt.Sprintf("%s %s\n", marker, style.Render(opt.title)))
		b.WriteString(fmt.Sprintf("    %s\n", s.Muted.Render(opt.desc)))
	}
	return b.String()
}

// wizardAppView shows app cards grouped by support tier.
func wizardAppView(s *Styles, m *model) string {
	var b strings.Builder
	b.WriteString(s.SectionHeader.Render("Choose target app"))
	b.WriteString("\n\n")
	idx := 0
	for _, group := range appGroups {
		b.WriteString(s.SectionHeader.Render(group.title))
		b.WriteString("\n")
		b.WriteString(s.Muted.Render(group.description))
		b.WriteString("\n")
		for _, id := range group.adapterIDs {
			if a, ok := m.adapterRegistry.Get(id); ok {
				marker := " "
				style := s.Body
				if idx == m.wizard.selected {
					marker = "›"
					style = s.SelectedRow
				}
				badge := m.supportBadge(id)
				b.WriteString(fmt.Sprintf("%s %s  %s  %s\n", marker, style.Render(a.DisplayName()), s.Muted.Render(fmt.Sprintf("[%s]", badge)), s.Muted.Render(strings.Join(slotsPreview(a.Contract()), "/"))))
				idx++
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// slotsPreview returns a short list of slot names for display.
func slotsPreview(c adapter.AppSupportContract) []string {
	if len(c.ModelSlots) == 0 {
		return []string{"main"}
	}
	slots := make([]string, 0, len(c.ModelSlots))
	for _, s := range c.ModelSlots {
		slots = append(slots, s.Name)
		if len(slots) >= 4 {
			slots = append(slots, "...")
			break
		}
	}
	return slots
}

// wizardProviderView shows every registered provider, grouped into compatible
// and incompatible sections. Incompatible providers show why they don't match
// and can be repaired inline (u) instead of being hidden. Selection indexes
// into the flat compat-view slice (wizardProviderCompatViews).
func wizardProviderView(s *Styles, m *model) string {
	var b strings.Builder
	b.WriteString(s.SectionHeader.Render("Choose provider"))
	b.WriteString("\n\n")
	if len(m.providers.Providers) == 0 {
		b.WriteString(s.Muted.Render("No providers registered."))
		b.WriteString("\n\n")
		b.WriteString(s.Muted.Render("r: restore default providers  ·  Esc: back"))
		return b.String()
	}

	views := m.wizardProviderCompatViews()
	compatCount := 0
	for _, v := range views {
		if v.Compatible {
			compatCount++
		}
	}

	// Compatible providers section.
	if compatCount > 0 {
		b.WriteString(s.SectionHeader.Render("Compatible providers"))
		b.WriteString("\n")
		for i, v := range views {
			if !v.Compatible {
				continue
			}
			writeProviderRow(&b, s, i, v, m.wizard.selected)
		}
		b.WriteString("\n")
	}

	// Incompatible providers section — shown with reasons, not hidden.
	if compatCount < len(views) {
		b.WriteString(s.SectionHeader.Render("Other providers"))
		b.WriteString("\n")
		for i, v := range views {
			if v.Compatible {
				continue
			}
			writeProviderRow(&b, s, i, v, m.wizard.selected)
			b.WriteString(fmt.Sprintf("      %s\n", s.Muted.Render(v.Reason)))
		}
		b.WriteString("\n")
	}

	// Hints.
	if compatCount == 0 {
		b.WriteString(s.Warning.Render("No compatible providers found for this app."))
		b.WriteString("\n")
	}
	b.WriteString(s.Muted.Render("↑/↓ select · Enter continue"))
	if compatCount < len(views) {
		b.WriteString(s.Muted.Render(" · u: repair as OpenAI-compatible"))
	}
	b.WriteString(s.Muted.Render(" · r: restore defaults · Esc: back"))
	return b.String()
}

// writeProviderRow renders a single provider row in the wizard provider view.
func writeProviderRow(b *strings.Builder, s *Styles, i int, v providerCompatView, selected int) {
	marker := " "
	style := s.Body
	if i == selected {
		marker = "›"
		style = s.SelectedRow
	}
	tag := ""
	if !v.Compatible {
		tag = s.Muted.Render(" (incompatible)")
	}
	b.WriteString(fmt.Sprintf("%s %s%s  %s\n", marker, style.Render(v.Provider.Name), tag, s.Muted.Render(v.Provider.Slug)))
}

// wizardCredentialView shows key selection or inline key creation.
func wizardCredentialView(s *Styles, m *model) string {
	var b strings.Builder
	b.WriteString(s.SectionHeader.Render("Choose credential"))
	b.WriteString("\n\n")
	if !m.unlocked {
		b.WriteString(s.Muted.Render("Unlock the vault to select a key."))
		return b.String()
	}
	keys := m.wizardVisibleKeys()
	if len(keys) == 0 {
		b.WriteString(s.Muted.Render("No matching keys. Add one first with `z` → Add API key."))
		return b.String()
	}
	for i, k := range keys {
		marker := " "
		style := s.Body
		if i == m.wizard.selected {
			marker = "›"
			style = s.SelectedRow
		}
		b.WriteString(fmt.Sprintf("%s %s  %s  %s\n", marker, style.Render(k.Label), s.KeyMasked.Render(k.MaskedSecret), s.Muted.Render(k.ProviderSlug)))
	}
	return b.String()
}

// wizardModelsView shows model slot configuration. Provider catalogs are the
// primary selection source; manual text remains as a fallback for local/manual
// providers that do not expose a known catalog.
func wizardModelsView(s *Styles, m *model) string {
	var b strings.Builder
	b.WriteString(s.SectionHeader.Render("Configure models"))
	b.WriteString("\n\n")
	appID := m.wizard.draft.AppID
	a, ok := m.adapterRegistry.Get(appID)
	if !ok {
		b.WriteString(s.Muted.Render("No model slots for this app."))
		return b.String()
	}
	c := a.Contract()
	if len(c.ModelSlots) == 0 {
		b.WriteString(s.Muted.Render("No model slots for this app."))
		return b.String()
	}

	// Show live fetch status.
	if m.wizard.fetchingModels {
		b.WriteString(s.Warning.Render("⟳ Fetching model catalog…"))
		b.WriteString("\n\n")
	} else if len(m.wizard.fetchedModels) > 0 {
		b.WriteString(s.Success.Render(fmt.Sprintf("✓ %d models loaded", len(m.wizard.fetchedModels))))
		b.WriteString("  ")
		b.WriteString(s.Muted.Render("←/→ to cycle"))
		b.WriteString("\n\n")
	}

	m.ensureModelInputs()
	for i, slot := range c.ModelSlots {
		current := ""
		if mi, ok := m.wizard.modelSlotInputs[slot.Name]; ok {
			current = mi.Value()
		}
		optional := ""
		if slot.Optional {
			optional = s.Muted.Render(" (optional)")
		}
		active := i == m.wizard.activeModelSlot
		marker := " "
		if active {
			marker = "›"
		}
		b.WriteString(fmt.Sprintf("%s %s%s\n", marker, s.Body.Render(slot.Name+":"), optional))
		// Render the actual text input so the user can type directly into it.
		if input, ok := m.wizard.modelSlotInputs[slot.Name]; ok {
			b.WriteString("    " + input.View() + "\n")
		} else if current != "" {
			b.WriteString(fmt.Sprintf("    %s %s\n", s.Value.Render("→"), s.Value.Render(current)))
		} else {
			b.WriteString(fmt.Sprintf("    %s\n", s.Muted.Render("  [←/→ select from catalog, or type custom model]")))
		}

		// Show catalog candidates for this slot, if the provider has any.
		candidates := m.wizardModelCandidates(slot.Name)
		if len(candidates) > 0 {
			show := candidates
			maxShow := 12
			if len(show) > maxShow {
				show = show[:maxShow]
			}
			names := make([]string, 0, len(show))
			for _, cm := range show {
				label := cm.ID
				if cm.Name != "" {
					label = cm.Name
				}
				names = append(names, label)
			}
			b.WriteString(fmt.Sprintf("    %s %s\n", s.Muted.Render("catalog:"), s.Muted.Render(strings.Join(names, ", "))))
			if len(candidates) > maxShow {
				b.WriteString(fmt.Sprintf("      %s\n", s.Muted.Render(fmt.Sprintf("...and %d more · ←/→ to cycle through all", len(candidates)-maxShow))))
			}
		}
	}
	b.WriteString("\n")
	b.WriteString(s.Muted.Render("←/→ select model · Tab/Enter next slot · type only for custom/manual models"))
	return b.String()
}

// modelSlotValue returns the model ID for a given slot name.
func modelSlotValue(models profile.ModelSlots, slot string) string {
	switch slot {
	case "main":
		if models.Main != nil {
			return models.Main.ID
		}
	case "fast":
		if models.Fast != nil {
			return models.Fast.ID
		}
	case "weak":
		if models.Weak != nil {
			return models.Weak.ID
		}
	case "editor":
		if models.Editor != nil {
			return models.Editor.ID
		}
	case "planner":
		if models.Planner != nil {
			return models.Planner.ID
		}
	case "actor":
		if models.Actor != nil {
			return models.Actor.ID
		}
	case "compression":
		if models.Compression != nil {
			return models.Compression.ID
		}
	case "vision":
		if models.Vision != nil {
			return models.Vision.ID
		}
	case "web_extract":
		if models.WebExtract != nil {
			return models.WebExtract.ID
		}
	}
	return ""
}

// wizardRuntimeView shows runtime/config isolation options.
func wizardRuntimeView(s *Styles, m *model) string {
	var b strings.Builder
	b.WriteString(s.SectionHeader.Render("Runtime / config isolation"))
	b.WriteString("\n\n")
	appID := m.wizard.draft.AppID
	if a, ok := m.adapterRegistry.Get(appID); ok {
		c := a.Contract()
		if c.CanIsolateProfile {
			b.WriteString(fmt.Sprintf("  %s Isolated profile directory\n", s.Success.Render("✓")))
		}
		if c.CanPatchConfig {
			b.WriteString(fmt.Sprintf("  %s Config file patching\n", s.Success.Render("✓")))
		}
		if c.CanInjectSecrets {
			b.WriteString(fmt.Sprintf("  %s Secret injection\n", s.Success.Render("✓")))
		} else {
			b.WriteString(fmt.Sprintf("  %s Manual credential handoff\n", s.Warning.Render("⚠")))
		}
	}
	b.WriteString("\n")
	b.WriteString(s.Muted.Render("Continue to preview."))
	return b.String()
}

// wizardHazardsView shows hazards and manual steps.
func wizardHazardsView(s *Styles, m *model) string {
	var b strings.Builder
	b.WriteString(s.SectionHeader.Render("Warnings & manual steps"))
	b.WriteString("\n\n")
	if len(m.wizard.hazards) > 0 {
		b.WriteString(s.SectionHeader.Render("Warnings"))
		b.WriteString("\n")
		for _, h := range m.wizard.hazards {
			icon := s.Warning.Render("⚠")
			if h.Severity == "critical" {
				icon = s.Danger.Render("✗")
			}
			b.WriteString(fmt.Sprintf("  %s %s\n", icon, s.Warning.Render(h.Title)))
			if h.Detail != "" {
				b.WriteString(fmt.Sprintf("    %s\n", s.Muted.Render(h.Detail)))
			}
			if h.Fix != "" {
				b.WriteString(fmt.Sprintf("    %s %s\n", s.Muted.Render("fix:"), s.Muted.Render(h.Fix)))
			}
		}
		b.WriteString("\n")
	}
	if len(m.wizard.manual) > 0 {
		b.WriteString(s.SectionHeader.Render("Manual steps"))
		b.WriteString("\n")
		for i, step := range m.wizard.manual {
			b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, s.Body.Render(step.Title)))
			if step.Description != "" {
				b.WriteString(fmt.Sprintf("     %s\n", s.Muted.Render(step.Description)))
			}
		}
	}
	return b.String()
}

func wizardNameView(s *Styles, m *model) string {
	var b strings.Builder
	b.WriteString(s.SectionHeader.Render("Profile Name"))
	b.WriteString("\n\n")
	b.WriteString(s.Body.Render("Choose a unique name for this profile."))
	b.WriteString("\n\n")
	b.WriteString("  " + m.wizard.nameInput.View() + "\n")
	return b.String()
}

// wizardPreviewView shows the finalized profile summary before saving.
func wizardPreviewView(s *Styles, m *model) string {
	var b strings.Builder
	d := m.wizard.draft
	b.WriteString(s.SectionHeader.Render("Preview"))
	b.WriteString("\n\n")
	name := d.Name
	if name == "" {
		name = m.wizard.deriveProfileName()
	}
	b.WriteString(fmt.Sprintf("  %s %s\n", s.Body.Render("Profile:"), s.KeyLabel.Render(name)))
	b.WriteString(fmt.Sprintf("  %s %s\n", s.Body.Render("App:"), s.Value.Render(d.AppID)))
	b.WriteString(fmt.Sprintf("  %s %s\n", s.Body.Render("Provider:"), s.Value.Render(d.ProviderSlug)))
	if d.KeyID != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Body.Render("Key:"), s.KeyMasked.Render(d.KeyID)))
	}
	if d.Models.Main != nil {
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Body.Render("Main model:"), s.Value.Render(d.Models.Main.ID)))
	}
	if d.Models.Weak != nil {
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Body.Render("Weak model:"), s.Value.Render(d.Models.Weak.ID)))
	}
	if d.Models.Editor != nil {
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Body.Render("Editor model:"), s.Value.Render(d.Models.Editor.ID)))
	}
	if len(d.Env) > 0 {
		b.WriteString(fmt.Sprintf("  %s\n", s.Body.Render("Env:")))
		for k, v := range d.Env {
			b.WriteString(fmt.Sprintf("    %s=%s\n", s.KeyMasked.Render(k), s.Muted.Render(v)))
		}
	}
	b.WriteString("\n")
	b.WriteString(s.Muted.Render("[Enter] Save profile  ·  [esc] Back"))
	return b.String()
}
