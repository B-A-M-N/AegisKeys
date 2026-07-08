package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"aegiskeys/internal/config"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// modelCatalogState tracks the modal catalog overlay on the providers screen.
// It is NOT a separate screen enum — it is a mode on screenProviders, similar
// to how launchMode works on the launch screen.
type modelCatalogState struct {
	active       bool
	providerSlug string
	keyID        string
	source       provider.ModelSource

	fetching  bool
	filtering bool
	errMsg    string

	models   []provider.ProviderModel
	selected map[string]bool
	cursor   int
	filter   string
}

// openModelCatalog activates the catalog overlay for the currently selected
// provider. It initializes the selected set from the provider's existing
// static models and preloads any cached candidates. Returns a refresh cmd
// if the provider supports live fetching and a usable API key is available.
func (m *model) openModelCatalog() tea.Cmd {
	prov := m.selectedProvider()
	if prov == nil {
		return nil
	}

	slug := prov.Slug
	models := make([]provider.ProviderModel, len(prov.Models))
	copy(models, prov.Models)

	selected := make(map[string]bool)
	for _, pmod := range prov.Models {
		selected[pmod.ID] = true
	}

	// Preload from cache if available.
	if cache, err := provider.LoadModelCache(m.configDir, slug); err == nil && len(cache.Models) > 0 {
		existing := make(map[string]bool)
		for _, cm := range models {
			existing[cm.ID] = true
		}
		for _, cm := range cache.Models {
			if !existing[cm.ID] {
				models = append(models, cm)
			}
		}
	}

	m.modelCatalog = modelCatalogState{
		active:       true,
		providerSlug: slug,
		source:       providerModelSource(*prov),
		models:       models,
		selected:     selected,
		cursor:       0,
		filter:       "",
		filtering:    false,
		errMsg:       "",
	}

	// Auto-refresh only for providers already configured as dynamic. Static
	// catalogs should open without needing a network call or key-policy check.
	if m.modelCatalog.source == provider.ModelSourceDynamic {
		return m.refreshModelCatalog()
	}
	return nil
}

// closeModelCatalog deactivates the overlay and returns focus to the normal
// provider list.
func (m *model) closeModelCatalog() {
	m.modelCatalog.active = false
	m.modelCatalog.providerSlug = ""
	m.modelCatalog.models = nil
	m.modelCatalog.selected = nil
	m.modelCatalog.source = ""
	m.modelCatalog.cursor = 0
	m.modelCatalog.filter = ""
	m.modelCatalog.filtering = false
	m.modelCatalog.fetching = false
	m.modelCatalog.errMsg = ""
}

// refreshModelCatalog returns a tea.Cmd that fetches the live model catalog
// from the provider's API and updates the overlay (without auto-selecting
// newly discovered models). Returns nil when fetching is not possible.
func (m *model) refreshModelCatalog() tea.Cmd {
	prov := m.providers.Find(m.modelCatalog.providerSlug)
	if prov == nil {
		return nil
	}
	if !prov.CanRefreshModels() {
		return nil
	}

	// Resolve an API key if the provider needs one.
	var apiKey string
	keyID := m.modelCatalog.keyID
	if m.vaultSession != nil && m.vaultSession.vault != nil {
		if keyID == "" {
			keyID = m.resolveProviderKeyID(prov.Slug)
		}
		if keyID != "" {
			if rec := m.vaultSession.vault.Get(keyID); rec != nil {
				// Model refresh is an advisory policy surface: fetching a
				// non-secret model list is how users build static provider
				// catalogs. Never persist the key outside the encrypted vault.
				apiKey = rec.Secret
			}
		}
	}

	if apiKey == "" && prov.NeedsKey() {
		switch {
		case m.vaultSession == nil || m.vaultSession.vault == nil:
			m.modelCatalog.errMsg = "Vault not unlocked"
		case keyID == "":
			m.modelCatalog.errMsg = "No key found for provider " + prov.Slug + " (add a key first)"
		default:
			m.modelCatalog.errMsg = "Key " + keyID + " is unavailable for model refresh"
		}
		return nil
	}

	slug := prov.Slug
	pCopy := *prov
	m.modelCatalog.fetching = true
	m.modelCatalog.errMsg = ""

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15_000_000_000)
		defer cancel()
		models, err := provider.RefreshModels(ctx, pCopy, apiKey)
		return modelCatalogLoadedMsg{
			providerSlug: slug,
			models:       models,
			err:          err,
		}
	}
}

// resolveProviderKeyID find the first usable key that matches a provider slug.
// Falls back to any key when the provider accepts keys without provider linkage.
func (m *model) resolveProviderKeyID(slug string) string {
	if m.vaultSession == nil || m.vaultSession.vault == nil {
		return ""
	}
	for i := range m.vaultSession.vault.Keys {
		k := &m.vaultSession.vault.Keys[i]
		if k.ProviderSlug == slug {
			return k.ID
		}
	}
	// Fallback: any key whose policy allows model refresh.
	for i := range m.vaultSession.vault.Keys {
		k := &m.vaultSession.vault.Keys[i]
		if k.ProviderSlug == "" {
			if err := k.AllowAccess(secret.AccessRefreshModels); err == nil {
				return k.ID
			}
		}
	}
	return ""
}

// toggleModelCatalogSource switches the pending save mode between dynamic and
// static. Selection only matters in static mode.
func (m *model) toggleModelCatalogSource() {
	if m.modelCatalog.source == provider.ModelSourceStatic {
		m.modelCatalog.source = provider.ModelSourceDynamic
	} else {
		m.modelCatalog.source = provider.ModelSourceStatic
	}
	m.modelCatalog.errMsg = ""
}

// saveModelCatalog persists either the user's selected static allowlist or the
// loaded dynamic catalog and writes the on-disk providers file.
func (m *model) saveModelCatalog() tea.Cmd {
	slug := m.modelCatalog.providerSlug
	selected := make([]provider.ProviderModel, 0, len(m.modelCatalog.selected))
	for _, mod := range m.modelCatalog.models {
		if m.modelCatalog.selected[mod.ID] {
			selected = append(selected, mod)
		}
	}
	if len(selected) == 0 {
		m.statusMsg = "Save failed: catalog requires at least one selected model"
		m.modelCatalog.errMsg = "Select at least one model before saving"
		return nil
	}

	var count int
	var err error
	if m.modelCatalog.source == provider.ModelSourceDynamic {
		models := make([]provider.ProviderModel, 0, len(selected))
		for _, mod := range selected {
			mod.Static = false
			models = append(models, mod)
		}
		count = len(models)
		err = m.providers.SetDynamicModels(slug, models)
	} else {
		staticModels := make([]provider.ProviderModel, 0, len(selected))
		for _, mod := range selected {
			mod.Static = true
			staticModels = append(staticModels, mod)
		}
		count = len(staticModels)
		err = m.providers.SetStaticModels(slug, staticModels)
	}
	if err != nil {
		m.statusMsg = "Save failed: " + err.Error()
		return nil
	}
	if err := m.providers.Save(config.ProvidersPath(m.configDir)); err != nil {
		m.statusMsg = "Write failed: " + err.Error()
		return nil
	}

	m.logAudit("model_catalog_save", slug, m.modelCatalog.keyID)
	m.statusMsg = fmt.Sprintf("Saved %s catalog with %d models for %s", m.modelCatalog.source, count, slug)
	m.closeModelCatalog()
	return nil
}

func providerModelSource(p provider.Provider) provider.ModelSource {
	if p.ModelPolicy.Source != "" {
		return p.ModelPolicy.Source
	}
	if p.Catalog.Source != "" {
		return provider.ModelSource(p.Catalog.Source)
	}
	if p.Compatibility == provider.CompatLocal || p.Protocol == provider.ProtocolLocal {
		return provider.ModelSourceLocal
	}
	if len(p.Models) > 0 {
		return provider.ModelSourceStatic
	}
	return provider.ModelSourceManual
}

// filteredCatalogModels returns the models that match the current filter
// (case-insensitive substring on ID or name). When filter is empty, all
// models are returned.
func (m *model) filteredCatalogModels() []provider.ProviderModel {
	if m.modelCatalog.filter == "" {
		return m.modelCatalog.models
	}
	pat := strings.ToLower(m.modelCatalog.filter)
	out := make([]provider.ProviderModel, 0, len(m.modelCatalog.models))
	for _, mod := range m.modelCatalog.models {
		if strings.Contains(strings.ToLower(mod.ID), pat) ||
			strings.Contains(strings.ToLower(strings.TrimSpace(mod.Name)), pat) {
			out = append(out, mod)
		}
	}
	return out
}

// selectedCount returns the number of models currently toggled on.
func (m *model) selectedCount() int {
	n := 0
	for _, v := range m.modelCatalog.selected {
		if v {
			n++
		}
	}
	return n
}

// cacheAgeString returns a human-readable cache age, or an empty string if
// no cache is available.
func cacheAgeString(m *model) string {
	cache, err := provider.LoadModelCache(m.configDir, m.modelCatalog.providerSlug)
	if err != nil || len(cache.Models) == 0 {
		return ""
	}
	d := time.Since(cache.FetchedAt)
	switch {
	case d < time.Minute:
		return "cached just now"
	case d < time.Hour:
		return fmt.Sprintf("cached %dmin ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("cached %dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("cached %dd ago", int(d.Hours()/24))
	}
}
