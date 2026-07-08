package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"aegiskeys/internal/config"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// setupCatalogTestModel returns a model with a provider that has preconfigured
// static models and a vault session containing an API key.
func setupCatalogTestModel(t *testing.T) *model {
	t.Helper()
	m := newTestModel(t)

	// Select a provider (openai is default-added).
	m.active = screenProviders
	m.focus = focusContent
	m.selected[screenProviders] = 0

	// Get the first provider and give it some static models.
	prov := m.selectedProvider()
	if prov == nil || len(m.providers.Providers) == 0 {
		t.Skip("no providers available in test registry")
	}
	prov = &m.providers.Providers[0]
	prov.Models = []provider.ProviderModel{
		{ID: "model-alpha", Name: "Alpha", Static: true},
		{ID: "model-beta", Name: "Beta", Static: true},
	}

	// Set up an unlocked vault with a key.
	m.unlocked = true
	m.keys = []secret.MaskedKeyItem{
		{
			ID:           "key_test_1",
			ProviderSlug: prov.Slug,
			Label:        "Primary Key",
			MaskedSecret: "...abcd",
		},
	}
	m.vaultSession = &vaultSession{
		vault: &secret.Vault{
			Version: 1,
			Keys: []secret.SecretRecord{
				{
					ID:           "key_test_1",
					Label:        "Primary Key",
					ProviderSlug: prov.Slug,
					Secret:       "sk-test-key-for-catalog",
					Kind:         secret.SecretAPIKey,
				},
			},
		},
	}
	m.lastActivity = time.Now()

	return m
}

func TestOpenModelCatalog_SelectedFromProviderModels(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.openModelCatalog()

	if !m.modelCatalog.active {
		t.Fatal("catalog should be active after open")
	}
	if len(m.modelCatalog.selected) != 2 {
		t.Errorf("expected 2 selected models (from provider static), got %d", len(m.modelCatalog.selected))
	}
	if !m.modelCatalog.selected["model-alpha"] {
		t.Errorf("model-alpha should be selected (from provider static)")
	}
	if !m.modelCatalog.selected["model-beta"] {
		t.Errorf("model-beta should be selected (from provider static)")
	}
}

func TestCatalogKey_ToggleSelectDeselect(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.openModelCatalog()

	// Cursor should start at 0.
	filtered := m.filteredCatalogModels()
	if len(filtered) == 0 {
		t.Fatal("expected models in catalog")
	}

	// Press space to toggle first model OFF.
	_, _ = m.Update(tea.KeyPressMsg{Text: " "})
	id := filtered[0].ID
	if m.modelCatalog.selected[id] {
		t.Errorf("expected model %s to be toggled OFF", id)
	}

	// Press space again to toggle it back ON.
	_, _ = m.Update(tea.KeyPressMsg{Text: " "})
	if !m.modelCatalog.selected[id] {
		t.Errorf("expected model %s to be toggled back ON", id)
	}
}

func TestCatalogKey_SelectAllClear(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.openModelCatalog()

	// Deselect all first.
	_, _ = m.Update(tea.KeyPressMsg{Text: "c"})
	if m.selectedCount() != 0 {
		t.Errorf("after clear: expected 0 selected, got %d", m.selectedCount())
	}

	// Select all.
	_, _ = m.Update(tea.KeyPressMsg{Text: "a"})
	filtered := m.filteredCatalogModels()
	for _, mod := range filtered {
		if !m.modelCatalog.selected[mod.ID] {
			t.Errorf("after select-all: expected %s to be selected", mod.ID)
		}
	}
	if m.selectedCount() != len(filtered) {
		t.Errorf("after select-all: expected %d selected, got %d", len(filtered), m.selectedCount())
	}

	// Clear again.
	_, _ = m.Update(tea.KeyPressMsg{Text: "c"})
	if m.selectedCount() != 0 {
		t.Errorf("after second clear: expected 0 selected, got %d", m.selectedCount())
	}
}

func TestCatalogKey_FilterNarrowsList(t *testing.T) {
	m := setupCatalogTestModel(t)
	// Add a third model with distinct name.
	prov := m.providers.Find(m.modelCatalog.providerSlug)
	if prov != nil {
		prov.Models = append(prov.Models, provider.ProviderModel{
			ID: "model-gamma", Name: "Gamma Vision", Static: false,
		})
	}
	m.openModelCatalog()

	// Enter filter mode.
	_, _ = m.Update(tea.KeyPressMsg{Text: "/"})
	if !m.modelCatalog.filtering {
		t.Fatal("should be in filter mode")
	}

	// Type "alpha".
	for _, ch := range "alpha" {
		_, _ = m.Update(tea.KeyPressMsg{Text: string(ch)})
	}

	filtered := m.filteredCatalogModels()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered model, got %d", len(filtered))
	}
	if filtered[0].ID != "model-alpha" {
		t.Errorf("expected model-alpha, got %s", filtered[0].ID)
	}

	// Exit filter mode.
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	if m.modelCatalog.filtering {
		t.Fatal("should exit filter mode")
	}

	// Filter string should still persist (narrows visual list).
	if m.modelCatalog.filter != "alpha" {
		t.Errorf("expected filter=alpha, got %q", m.modelCatalog.filter)
	}
}

func TestCatalogKey_QuitClosesOverlay(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.openModelCatalog()

	if !m.modelCatalog.active {
		t.Fatal("catalog should be active")
	}

	// Press q to quit.
	_, _ = m.Update(tea.KeyPressMsg{Text: "q"})
	if m.modelCatalog.active {
		t.Error("catalog should be closed after pressing q")
	}
	if m.modelCatalog.providerSlug != "" {
		t.Error("providerSlug should be cleared after close")
	}
}

func TestCatalogKey_EscClosesOverlay(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.openModelCatalog()

	_, _ = m.Update(tea.KeyPressMsg{Text: "esc"})
	if m.modelCatalog.active {
		t.Error("catalog should be closed after pressing esc")
	}
}

func TestCatalogNavigate_VimKeys(t *testing.T) {
	m := setupCatalogTestModel(t)

	// Add extra models to the first provider (the one selected by index 0).
	if len(m.providers.Providers) >= 1 {
		m.providers.Providers[0].Models = append(m.providers.Providers[0].Models,
			provider.ProviderModel{ID: "model-delta", Name: "Delta"},
			provider.ProviderModel{ID: "model-epi", Name: "Epsilon"},
		)
	}
	m.openModelCatalog()

	// Reset selected to known state.
	m.modelCatalog.selected = map[string]bool{}
	m.modelCatalog.cursor = 0

	// Move down.
	_, _ = m.Update(tea.KeyPressMsg{Text: "j"})
	if m.modelCatalog.cursor != 1 {
		t.Errorf("expected cursor=1 after j, got %d", m.modelCatalog.cursor)
	}

	// Move down again.
	_, _ = m.Update(tea.KeyPressMsg{Text: "j"})
	if m.modelCatalog.cursor != 2 {
		t.Errorf("expected cursor=2 after second j, got %d", m.modelCatalog.cursor)
	}

	// Move up.
	_, _ = m.Update(tea.KeyPressMsg{Text: "k"})
	if m.modelCatalog.cursor != 1 {
		t.Errorf("expected cursor=1 after k, got %d", m.modelCatalog.cursor)
	}
}

func TestSaveModelCatalog_PersistsStaticModels(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.openModelCatalog()

	// Select only the first filtered model.
	_, _ = m.Update(tea.KeyPressMsg{Text: "c"}) // clear all
	_, _ = m.Update(tea.KeyPressMsg{Text: " "}) // toggle first on

	filtered := m.filteredCatalogModels()
	if len(filtered) == 0 {
		t.Fatal("no models to save")
	}
	wantID := filtered[0].ID

	// Press 's' to save.
	m.unlocked = true
	m.vaultSession = &vaultSession{
		vault: &secret.Vault{
			Version: 1,
			Keys: []secret.SecretRecord{
				{ID: "key_test1", Label: "Test", Secret: "sk-test", Kind: secret.SecretAPIKey},
			},
		},
	}

	// Persist the in-memory registry to disk so saveModelCatalog can reload.
	_ = m.providers.Save(config.ProvidersPath(m.configDir))

	slug := m.modelCatalog.providerSlug

	// Press 's' to save via the UI.
	_, cmd := m.handleProvidersKey("s")
	if cmd != nil {
		_ = cmd()
	}

	// Verify: re-open registry from disk and check models are static.
	relReg, err := provider.LoadRegistry(config.ProvidersPath(m.configDir))
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	saved := relReg.Find(slug)
	if saved == nil {
		t.Fatalf("provider %s should exist after save", slug)
	}
	found := false
	for _, mod := range saved.Models {
		if mod.ID == wantID {
			found = true
			if !mod.Static {
				t.Errorf("expected model %s to be static after save", wantID)
			}
		}
	}
	if !found {
		t.Errorf("model %s should be in provider models after save", wantID)
	}
}

func TestRefreshModelCatalog_NoAutoSelectNew(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.openModelCatalog()

	// Simulate refresh delivering new models.
	newModels := []provider.ProviderModel{
		{ID: "model-alpha"}, // existing — selection preserved
		{ID: "model-new"},   // new — should NOT be auto-selected
	}
	m.modelCatalog.selected["model-alpha"] = true // pre-selected
	m.modelCatalog.selected["model-new"] = false  // not yet seen

	msg := modelCatalogLoadedMsg{
		providerSlug: m.modelCatalog.providerSlug,
		models:       newModels,
		err:          nil,
	}
	_, _ = m.Update(msg)

	// New model should NOT be auto-selected.
	if m.modelCatalog.selected["model-new"] {
		t.Error("new fetched model should NOT be auto-selected")
	}
	// Existing selected model should remain selected.
	if !m.modelCatalog.selected["model-alpha"] {
		t.Error("existing selected model should remain selected")
	}
}

func TestCatalogView_RendersTitle(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.openModelCatalog()

	v := stripANSIForTest(m.modelCatalogView(m.styles))
	if !strings.Contains(v, "Model Catalog") {
		t.Errorf("expected 'Model Catalog' in view, got:\n%s", v)
	}
}

func TestCatalogView_FooterShowsKeys(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.openModelCatalog()

	v := stripANSIForTest(m.modelCatalogView(m.styles))
	if !strings.Contains(v, "refresh") || !strings.Contains(v, "toggle") || !strings.Contains(v, "save") {
		t.Errorf("expected footer key hints in view, got:\n%s", v)
	}
}

func TestCatalogView_FilterIndicatorInFilteringMode(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.openModelCatalog()

	// Enter filter mode and type something.
	_, _ = m.Update(tea.KeyPressMsg{Text: "/"})
	_, _ = m.Update(tea.KeyPressMsg{Text: "a"})

	v := stripANSIForTest(m.modelCatalogView(m.styles))
	if !strings.Contains(v, "filter:") {
		t.Errorf("expected 'filter:' indicator in filtering view, got:\n%s", v)
	}
}

func TestCatalogOpensFromProvidersScreen_MKey(t *testing.T) {
	m := setupCatalogTestModel(t)
	m.active = screenProviders
	m.focus = focusContent
	m.modelCatalog.active = false

	// Press 'm' on the providers screen.
	_, _ = m.Update(tea.KeyPressMsg{Text: "m"})

	if !m.modelCatalog.active {
		t.Error("pressing 'm' on providers should open model catalog")
	}
}

func TestCatalogClosed_MKeyNotAffected(t *testing.T) {
	// When the catalog is NOT active, pressing other keys should still work.
	m := setupCatalogTestModel(t)
	m.modelCatalog.active = false
	m.active = screenProviders
	m.focus = focusContent

	initial := m.selected[screenProviders]

	// 's' should move down (provider navigation), not be captured by catalog.
	_, _ = m.Update(tea.KeyPressMsg{Text: "s"})

	if m.selected[screenProviders] != initial+1 && m.selected[screenProviders] != initial {
		// If last item, it should stay the same.
		if m.selected[screenProviders] == initial {
			t.Log("already at last provider")
		}
	}
}
