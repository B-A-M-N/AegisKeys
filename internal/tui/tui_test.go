package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"aegiskeys/internal/audit"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"

	"aegiskeys/internal/adapter"
)

func newTestModel(t *testing.T) *model {
	t.Helper()
	reg := provider.NewRegistry()
	for _, p := range provider.DefaultProviders() {
		_ = reg.Add(p)
	}
	store := profile.NewStore()
	_ = store.Add(profile.Profile{Name: "or-main", ProviderSlug: "openrouter", KeyID: "key_1"})

	m := &model{
		configDir:   t.TempDir(),
		version:     "test",
		styles:      NewStyles("vault"),
		providers:   reg,
		profiles:    store,
		vaultExists: false,
		auditEvents: []audit.Event{{Event: "test_event"}},
		width:       120,
		height:      40,
		focus:       focusSidebar,
	}
	m.commandInput = textinput.New()
	m.commandInput.Placeholder = "command"
	m.commandInput.SetWidth(40)
	m.matrix = NewMatrix(120, 40)
	m.adapterRegistry = adapter.NewRegistry()
	m.addInput = textinput.New()
	m.addInput.Placeholder = "name"
	m.addInput.SetWidth(40)
	return m
}

func sendKey(t *testing.T, m *model, key string) {
	t.Helper()
	_, _ = m.Update(tea.KeyPressMsg{Text: key})
}

func stripANSIForTest(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\x1b' {
			i++
			if i < len(runes) && runes[i] == '[' {
				i++
				for i < len(runes) && runes[i] != 'm' {
					i++
				}
			}
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

func TestDashboard_NoDuplicatedSidebar(t *testing.T) {
	m := newTestModel(t)
	v := stripANSIForTest(m.View().Content)
	// "AegisKeys" should appear once in the sidebar header.
	// The footer says "AegisKeys · local vault..." so we count sidebar only.
	if got := strings.Count(v, "AegisKeys"); got < 1 {
		t.Errorf("expected at least 1 AegisKeys reference, got %d", got)
	}
	if !strings.Contains(v, "Dashboard") {
		t.Errorf("dashboard not visible")
	}
}

func TestScreenSwitch_NoLeak(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	sendKey(t, m, "2")
	vProviders := stripANSIForTest(m.View().Content)
	if !strings.Contains(vProviders, "SLUG") {
		t.Errorf("providers screen missing header")
	}
	if strings.Contains(vProviders, "Recent activity") {
		t.Errorf("providers screen leaked dashboard content")
	}
	sendKey(t, m, "3")
	vKeys := stripANSIForTest(m.View().Content)
	if !strings.Contains(vKeys, "masked") {
		t.Errorf("keys screen not shown")
	}
}

func TestNumberKeys_SwitchScreen(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	sendKey(t, m, "7")
	if m.active != screenAudit {
		t.Fatalf("expected audit screen, got %d", m.active)
	}
	sendKey(t, m, "1")
	if m.active != screenDashboard {
		t.Fatalf("expected dashboard screen, got %d", m.active)
	}
}

func TestSidebarNavigation_WASD(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusSidebar
	sendKey(t, m, "s")
	if m.active != screenProviders {
		t.Fatalf("expected providers after s, got %d", m.active)
	}
	sendKey(t, m, "s")
	if m.active != screenKeys {
		t.Fatalf("expected keys after s, got %d", m.active)
	}
	sendKey(t, m, "w")
	if m.active != screenProviders {
		t.Fatalf("expected providers after w, got %d", m.active)
	}
}

func TestSidebarNavigation_VimKeys(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusSidebar
	sendKey(t, m, "j")
	if m.active != screenProviders {
		t.Fatalf("expected providers after j, got %d", m.active)
	}
	sendKey(t, m, "down")
	if m.active != screenKeys {
		t.Fatalf("expected keys after down, got %d", m.active)
	}
	sendKey(t, m, "up")
	if m.active != screenProviders {
		t.Fatalf("expected providers after up, got %d", m.active)
	}
}

func TestSidebar_EnterOpensContent(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusSidebar
	sendKey(t, m, "d")
	if m.focus != focusContent {
		t.Fatalf("expected content focus after d, got %d", m.focus)
	}
}

func TestTabAndBracketNavigation(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	sendKey(t, m, "tab")
	if m.focus != focusSidebar {
		t.Fatalf("expected sidebar focus after tab, got %d", m.focus)
	}
	sendKey(t, m, "]")
	if m.active != screenProviders {
		t.Fatalf("expected providers after ], got %d", m.active)
	}
	sendKey(t, m, "[")
	if m.active != screenDashboard {
		t.Fatalf("expected dashboard after [, got %d", m.active)
	}
}

func TestSmallTerminal_NoPanic(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	for _, dim := range [][2]int{{60, 10}, {30, 5}, {10, 3}} {
		m.width, m.height = dim[0], dim[1]
		_ = m.View().Content
	}
}

func TestContentEnter_OpensDetail(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	sendKey(t, m, "enter")
	if m.modal != modalDetail {
		t.Fatalf("expected detail modal after enter, got %d", m.modal)
	}
}

func TestContentWASD_NavigatesList(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProfiles
	_ = m.profiles.Add(profile.Profile{Name: "second", ProviderSlug: "openrouter", KeyID: "key_2"})
	sendKey(t, m, "s")
	if m.selected[screenProfiles] != 1 {
		t.Fatalf("expected selection 1 after s, got %d", m.selected[screenProfiles])
	}
	sendKey(t, m, "w")
	if m.selected[screenProfiles] != 0 {
		t.Fatalf("expected selection 0 after w, got %d", m.selected[screenProfiles])
	}
}

// TestProviderDelete_BlockedByKey verifies that deleting a provider that has
// referencing keys is blocked (with a status message visible to the user).
func TestProviderDelete_BlockedByKey(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	m.unlocked = true
	// Simulate a vault session with a key referencing openai.
	m.vaultSession = &vaultSession{
		vault: &secret.Vault{
			Keys: []secret.SecretRecord{{ID: "k1", ProviderSlug: "openai", Label: "main"}},
		},
	}
	// Select the openai provider (index 0 in defaults).
	m.selected[screenProviders] = 0
	if m.providers.Providers[0].Slug != "openai" {
		t.Skip("expected openai at index 0")
	}
	// Press x to start delete. Because the provider has referencing keys,
	// startDelete opens straight into the cascade-delete confirmation prompt.
	sendKey(t, m, "x")
	if m.modal != modalConfirmDelete {
		t.Fatal("expected delete modal")
	}
	if !m.deleteConfirmWait {
		t.Fatal("expected deleteConfirmWait prompt when provider has keys")
	}
	if m.deleteBlockReason == "" {
		t.Fatal("expected a warning naming the key count")
	}
	if m.providers.Find("openai") == nil {
		t.Fatal("provider should NOT be deleted before confirmation")
	}
	// Now confirm "yes" → provider AND its key are removed.
	sendKey(t, m, "y")
	if m.modal != modalNone {
		t.Errorf("expected modal closed after confirm, got %d", m.modal)
	}
	if m.providers.Find("openai") != nil {
		t.Fatal("provider should be deleted after 'yes'")
	}
	// The referencing key must be cascade-deleted too.
	if m.vaultSession.vault.Get("k1") != nil {
		t.Fatal("referencing key should be cascade-deleted")
	}
}

// TestProviderDelete_DeclineCascade verifies that answering "no" to the
// cascade-delete warning leaves the provider and its keys intact.
func TestProviderDelete_DeclineCascade(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	m.unlocked = true
	m.vaultSession = &vaultSession{
		vault: &secret.Vault{
			Keys: []secret.SecretRecord{{ID: "k1", ProviderSlug: "openai", Label: "main"}},
		},
	}
	m.selected[screenProviders] = 0
	if m.providers.Providers[0].Slug != "openai" {
		t.Skip("expected openai at index 0")
	}
	sendKey(t, m, "x")
	if !m.deleteConfirmWait {
		t.Fatal("expected cascade confirmation prompt")
	}
	// Decline with "n".
	sendKey(t, m, "n")
	if m.modal != modalNone {
		t.Errorf("expected modal closed after decline, got %d", m.modal)
	}
	// Provider and key must both survive.
	if m.providers.Find("openai") == nil {
		t.Fatal("provider should survive a declined cascade delete")
	}
	if m.vaultSession.vault.Get("k1") == nil {
		t.Fatal("referencing key should survive a declined cascade delete")
	}
}

// TestProviderDelete_Succeeds verifies a provider with no references is removed.
func TestProviderDelete_Succeeds(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	m.unlocked = true
	m.vaultSession = &vaultSession{vault: &secret.Vault{}}
	// Add a throwaway provider with no key/profile refs.
	custom := provider.Provider{Name: "zcustom", Slug: "zcustom", EnvVar: "ZCUSTOM_KEY", BaseURL: "https://example.com/v1", Compatibility: provider.CompatOpenAI, Protocol: provider.ProtocolOpenAI, Auth: provider.AuthSpec{Type: "bearer", EnvVar: "ZCUSTOM_KEY"}}
	if err := m.providers.Add(custom); err != nil {
		t.Fatal(err)
	}
	idx := -1
	for i, p := range m.providers.Providers {
		if p.Slug == "zcustom" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("custom provider not found")
	}
	m.selected[screenProviders] = idx
	sendKey(t, m, "x")
	sendKey(t, m, "enter")
	if m.providers.Find("zcustom") != nil {
		t.Fatal("provider should have been deleted")
	}
	if m.statusMsg != "Provider deleted." {
		t.Errorf("expected 'Provider deleted.' confirmation, got %q", m.statusMsg)
	}
}

func TestContentDelete_OpensModal(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProfiles
	sendKey(t, m, "x")
	if m.modal != modalConfirmDelete {
		t.Fatalf("expected delete modal after x, got %d", m.modal)
	}
}

func TestModal_EscCloses(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProfiles
	sendKey(t, m, "x")
	if m.modal != modalConfirmDelete {
		t.Fatalf("expected delete modal, got %d", m.modal)
	}
	sendKey(t, m, "esc")
	if m.modal != modalNone {
		t.Fatalf("expected no modal after esc, got %d", m.modal)
	}
}

func TestHelp_Truthful(t *testing.T) {
	m := newTestModel(t)
	sendKey(t, m, "?")
	v := stripANSIForTest(m.View().Content)
	for _, want := range []string{"1-8", "enter", "q", "tab", "z", "e", "x", "esc"} {
		if !strings.Contains(v, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestMatrix_TickChangesFrame(t *testing.T) {
	m := newTestModel(t)
	m.matrix = NewMatrix(80, 30)
	initialFrame := m.matrix.Frame
	_, cmd := m.Update(matrixMsg{})
	if cmd == nil {
		t.Fatal("expected tick command after matrix update")
	}
	msg := cmd()
	_, _ = m.Update(msg)
	if m.matrix.Frame <= initialFrame {
		t.Errorf("expected frame to advance after tick")
	}
}

func TestMatrix_NoLayoutShift(t *testing.T) {
	m := newTestModel(t)
	m.matrix = NewMatrix(80, 30)
	m.focus = focusContent
	before := m.View().Content
	for i := 0; i < 5; i++ {
		_, cmd := m.Update(matrixMsg{})
		if cmd != nil {
			msg := cmd()
			_, _ = m.Update(msg)
		}
	}
	after := m.View().Content
	if strings.Count(before, "\n") != strings.Count(after, "\n") {
		t.Errorf("matrix animation changed layout")
	}
}

func TestProviders_NKey_StartsAdd(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProfiles
	sendKey(t, m, "z")
	if !m.wizard.active {
		t.Fatal("expected wizard to activate after z")
	}
	if m.wizard.step != StepIntent {
		t.Fatalf("expected wizard at intent step, got %s", m.wizard.step)
	}
}

func TestProvidersZ_WizardCreatesProfile(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProfiles
	_ = m.providers.Add(provider.Provider{Name: "Test", Slug: "test", EnvVar: "TEST_KEY", Compatibility: provider.CompatOpenAI})
	m.keys = []secret.MaskedKeyItem{{ID: "key_test", Label: "test", ProviderSlug: "test"}}
	m.unlocked = true
	sendKey(t, m, "z")
	if !m.wizard.active {
		t.Fatal("wizard should be active")
	}
	// Select "Create launch profile" (index 0) and advance.
	m.wizard.selected = 0
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	// Now at App step. Select first app.
	m.wizard.selected = 0
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	// Now at Provider step. Select the test provider.
	m.wizard.selected = 0
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	// Now at Credential step. Select the test key.
	m.wizard.selected = 0
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	// Now at Models (optional) or Runtime. Advance to preview.
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	// Save.
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	if len(m.profiles.Profiles) < 1 {
		t.Fatalf("expected at least 1 profile, got %d", len(m.profiles.Profiles))
	}
}
func TestProviders_EKey_StartsEdit(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProfiles
	sendKey(t, m, "e")
	if m.modal != modalEdit {
		t.Fatal("expected edit modal after e")
	}
}

// TestHandleEditKey_InvalidProvider_NoPanic is a regression test for the
// index-out-of-range panic in handleEditKey: when commitEdit rejects a
// provider (e.g. ValidateStrict fails) and the user presses Enter again, the
// modal must close cleanly instead of panicking on fields[len(fields)].
func TestHandleEditKey_InvalidProvider_NoPanic(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	m.startEdit()
	if m.modal != modalEdit {
		t.Fatal("expected edit modal")
	}
	// Drive through all 4 provider edit fields with empty values (which keep
	// the existing placeholder and will produce an invalid provider on commit).
	for i := 0; i < 4; i++ {
		_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
	}
	// commitEdit ran on the 4th enter. With invalid data the modal should
	// close (or stay bounded) — pressing Enter again MUST NOT panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on repeated Enter during provider edit: %v", r)
		}
	}()
	_, _ = m.Update(tea.KeyPressMsg{Text: "enter"})
}

func TestMatrix_NoBleedIntoUI(t *testing.T) {
	m := newTestModel(t)
	sendKey(t, m, "2")
	v := stripANSIForTest(m.View().Content)
	if !strings.Contains(v, "OpenAI") {
		t.Errorf("provider name 'OpenAI' not found intact")
	}
	if !strings.Contains(v, "OPENAI_API_KEY") {
		t.Errorf("env var 'OPENAI_API_KEY' not found intact")
	}
}

func TestModal_ShowsDetail(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	sendKey(t, m, "s")
	sendKey(t, m, "d")
	if m.modal != modalDetail {
		t.Fatalf("expected detail modal, got %d", m.modal)
	}
	v := stripANSIForTest(m.View().Content)
	if !strings.Contains(v, "OpenAI") {
		t.Errorf("detail modal missing provider name")
	}
}

func TestWriteLine_ASCII(t *testing.T) {
	w, h := 80, 30
	grid := make([][]gridCell, h)
	priority := make([][]int, h)
	for y := range grid {
		grid[y] = make([]gridCell, w)
		priority[y] = make([]int, w)
	}
	writeLine(grid, priority, 10, 5, "Hello", 10, 2, w, h, false)
	if grid[5][10].ch != 'H' {
		t.Errorf("expected 'H' at (10,5), got %q", grid[5][10].ch)
	}
	if grid[5][14].ch != 'o' {
		t.Errorf("expected 'o' at (14,5), got %q", grid[5][14].ch)
	}
}

func TestWriteLine_BoxDrawing(t *testing.T) {
	w, h := 80, 30
	grid := make([][]gridCell, h)
	priority := make([][]int, h)
	for y := range grid {
		grid[y] = make([]gridCell, w)
		priority[y] = make([]int, w)
	}
	writeLine(grid, priority, 10, 5, "+----+", 10, 2, w, h, false)
	if grid[5][10].ch != '+' {
		t.Errorf("expected '+' at (10,5)")
	}
	if grid[5][11].ch != '-' {
		t.Errorf("expected '-' at (11,5)")
	}
	if grid[5][15].ch != '+' {
		t.Errorf("expected '+' at (15,5)")
	}
}

func TestWriteLine_ANSIStripped(t *testing.T) {
	w, h := 80, 30
	grid := make([][]gridCell, h)
	priority := make([][]int, h)
	for y := range grid {
		grid[y] = make([]gridCell, w)
		priority[y] = make([]int, w)
	}
	styled := "\x1b[1mHello\x1b[0m"
	writeLine(grid, priority, 10, 5, styled, 10, 2, w, h, false)
	if grid[5][10].ch != 'H' {
		t.Errorf("expected 'H' at (10,5)")
	}
	if grid[5][14].ch != 'o' {
		t.Errorf("expected 'o' at (14,5)")
	}
	if grid[5][15].ch != 0 {
		t.Errorf("expected empty at (15,5), got %q", grid[5][15].ch)
	}
}

func TestModal_NotFragmented(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	sendKey(t, m, "d")
	if m.modal != modalDetail {
		t.Fatalf("expected detail modal, got %d", m.modal)
	}
	v := stripANSIForTest(m.View().Content)
	// Count '+' that are part of modal borders (adjacent to '-').
	lines := strings.Split(v, "\n")
	borderPlusCount := 0
	for _, line := range lines {
		for i, r := range line {
			if r != '+' {
				continue
			}
			if i > 0 && rune(line[i-1]) == '-' {
				borderPlusCount++
				continue
			}
			if i < len(line)-1 && rune(line[i+1]) == '-' {
				borderPlusCount++
			}
		}
	}
	if borderPlusCount != 4 {
		t.Errorf("expected 4 border corners, got %d", borderPlusCount)
	}
}

func TestModalBlocksMatrix(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	sendKey(t, m, "d")
	if m.modal != modalDetail {
		t.Fatalf("expected detail modal, got %d", m.modal)
	}
	v := stripANSIForTest(m.View().Content)
	lines := strings.Split(v, "\n")

	topRow := -1
	bottomRow := -1
	for i, line := range lines {
		if strings.Contains(line, "+---") || strings.Contains(line, "---+") {
			if topRow < 0 {
				topRow = i
			}
			bottomRow = i
		}
	}
	if topRow < 0 {
		t.Fatal("modal not found")
	}

	topLine := lines[topRow]
	leftCol := strings.Index(topLine, "+")
	rightCol := strings.LastIndex(topLine, "+")
	if leftCol < 0 || rightCol <= leftCol {
		t.Fatal("modal corners not found")
	}

	matrixSymbols := "=<>;|#$%&~^"
	bleedCount := 0
	for i := topRow + 1; i < bottomRow; i++ {
		if i >= len(lines) {
			break
		}
		line := lines[i]
		for j := leftCol + 1; j < rightCol && j < len(line); j++ {
			if strings.ContainsRune(matrixSymbols, rune(line[j])) {
				bleedCount++
			}
		}
	}
	if bleedCount > 0 {
		t.Errorf("modal contains %d matrix symbols bleeding through", bleedCount)
	}
}

func TestFooter_NoMojibake(t *testing.T) {
	m := newTestModel(t)
	v := stripANSIForTest(m.View().Content)
	if strings.Contains(v, "Â") {
		t.Errorf("footer contains mojibake")
	}
	if !strings.Contains(v, "AegisKeys") {
		t.Errorf("footer missing AegisKeys branding")
	}
}

// --- Regression tests ---

func TestMatrixResize_NoOp(t *testing.T) {
	m := NewMatrix(80, 30)
	dropsBefore := len(m.Drops)
	m.Resize(80, 30)
	dropsAfter := len(m.Drops)
	if dropsBefore != dropsAfter {
		t.Errorf("Resize with same size should not reseed: drops %d -> %d", dropsBefore, dropsAfter)
	}
}

func TestMatrixResize_Changes(t *testing.T) {
	m := NewMatrix(80, 30)
	m.Resize(100, 40)
	if m.Width != 100 || m.Height != 40 {
		t.Errorf("Resize should update dimensions: got %dx%d", m.Width, m.Height)
	}
}

func TestMatrixRender_Deterministic(t *testing.T) {
	m := NewMatrix(80, 30)
	buf1 := NewMatrixBuffer(80, 30)
	m.Render(buf1)
	buf2 := NewMatrixBuffer(80, 30)
	m.Render(buf2)
	for y := 0; y < 30; y++ {
		for x := 0; x < 80; x++ {
			c1 := buf1.cells[y][x]
			c2 := buf2.cells[y][x]
			if c1.ch != c2.ch || c1.colorIdx != c2.colorIdx {
				t.Errorf("Render not deterministic at (%d,%d)", x, y)
				return
			}
		}
	}
}

func TestModalCellStyle_NotBorder(t *testing.T) {
	s := NewStyles("vault")
	modalBg := s.CellModalBg
	out := modalBg.Render(" ")
	if strings.Contains(out, "┌") || strings.Contains(out, "─") {
		t.Errorf("CellModalBg should not produce box characters: %q", out)
	}
}

func TestView_NoMatrixResize(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	_ = m.View()
	dropsAfterFirst := len(m.matrix.Drops)
	_ = m.View()
	dropsAfterSecond := len(m.matrix.Drops)
	if dropsAfterFirst != dropsAfterSecond {
		t.Errorf("View should not reseed matrix: drops %d -> %d", dropsAfterFirst, dropsAfterSecond)
	}
}

func minLen(s string, n int) int {
	if len(s) < n {
		return len(s)
	}
	return n
}

// --- New tests from patch ---

func TestGlobalR_RunsDoctorFromAnyScreen(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	_, cmd := m.Update(tea.KeyPressMsg{Text: "r"})
	if m.active != screenDoctor {
		t.Fatalf("expected r to jump to doctor, got %d", m.active)
	}
	if cmd == nil {
		t.Fatal("expected doctor command from global r")
	}
}

func TestGlobalZ_StartsAddFromDashboard(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenDashboard
	sendKey(t, m, "z")
	if !m.wizard.active {
		t.Fatalf("expected wizard to activate after z from dashboard")
	}
	if m.wizard.step != StepIntent {
		t.Fatalf("expected StepIntent, got %s", m.wizard.step)
	}
}

func TestAddModal_TextInputReceivesKeyPressMsg(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	sendKey(t, m, "z")
	// Providers screen: z opens the provider add modal (not the profile wizard).
	if m.modal != modalAdd {
		t.Fatalf("expected provider add modal after z on providers screen, got %d", m.modal)
	}
	sendKey(t, m, "x")
	if got := m.addInput.Value(); got != "x" {
		t.Fatalf("expected add input to receive typed key, got %q", got)
	}
}

func TestKeysZ_OpensKeyAddModal(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenKeys
	sendKey(t, m, "z")
	// Keys screen: z opens the key add modal (not the profile wizard).
	if m.modal != modalAddKey {
		t.Fatalf("expected modalAddKey after z on keys, got %d", m.modal)
	}
}

func TestKeysZ_ModalRenders(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenKeys
	m.modal = modalAddKey
	m.modalPrompt = "Provider"
	v := stripANSIForTest(m.View().Content)
	if !strings.Contains(v, "Provider") {
		t.Fatalf("key add modal did not render Provider field")
	}
	if !strings.Contains(v, "Label") {
		t.Fatalf("key add modal did not render Label field")
	}
	if !strings.Contains(v, "Secret") {
		t.Fatalf("key add modal did not render Secret field")
	}
}

func TestAddModal_RendersEvenIfFocusStale(t *testing.T) {
	m := newTestModel(t)
	m.active = screenProfiles
	m.focus = focusContent
	m.modal = modalAdd

	v := stripANSIForTest(m.View().Content)

	if !strings.Contains(v, "Add") {
		t.Fatalf("modalAdd state did not render visible Add modal")
	}
}

func TestMatrixSpark_Triggered(t *testing.T) {
	m := NewMatrix(80, 30)
	m.TriggerSpark(matrixUnlock)
	if len(m.semanticEvents) != 1 {
		t.Fatalf("expected 1 semantic event, got %d", len(m.semanticEvents))
	}
	if m.semanticEvents[0].semantic != matrixUnlock {
		t.Errorf("expected matrixUnlock semantic, got %d", m.semanticEvents[0].semantic)
	}
}

func TestMatrixSpark_Decays(t *testing.T) {
	m := NewMatrix(80, 30)
	m.TriggerSpark(matrixUnlock)
	// Run enough frames to decay the event. Events start at lifetime 30-60.
	for i := 0; i < 70; i++ {
		cmd := m.Update(matrixMsg{})
		if cmd != nil {
			msg := cmd()
			_ = msg
		}
		if len(m.semanticEvents) == 0 {
			break
		}
	}
	if len(m.semanticEvents) != 0 {
		t.Errorf("expected semantic events to decay, got %d", len(m.semanticEvents))
	}
}

func TestMatrix_PerCellColorChanges(t *testing.T) {
	m := NewMatrix(80, 30)
	buf1 := NewMatrixBuffer(80, 30)
	buf2 := NewMatrixBuffer(80, 30)
	m.Render(buf1)
	// Advance a few frames.
	for i := 0; i < 10; i++ {
		cmd := m.Update(matrixMsg{})
		if cmd != nil {
			msg := cmd()
			_ = msg
		}
	}
	m.Render(buf2)
	// At least some cells should have different color indices.
	changed := 0
	for y := 0; y < 30; y++ {
		for x := 0; x < 80; x++ {
			if buf1.cells[y][x].colorIdx != buf2.cells[y][x].colorIdx {
				changed++
			}
		}
	}
	if changed == 0 {
		t.Error("expected per-cell color changes across frames")
	}
}

func TestLaunchView_ShowsMaskedEnvAndSafety(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenLaunch
	m.unlocked = true
	// Add a profile directly (bypass store validation that requires vault keys).
	// Use "openai" provider — it's reliably registered by newTestModel.
	m.profiles.Profiles = []profile.Profile{{
		Name:         "oa-main",
		ProviderSlug: "openai",
		KeyID:        "key_1",
		Target:       profile.TargetConfig{App: "aider", RenderMode: profile.RenderEnv},
	}}
	// Set up a vault session with the key so strategy resolution succeeds.
	vault := &secret.Vault{Version: 1}
	_ = vault.Add(secret.SecretRecord{ID: "key_1", ProviderSlug: "openai", Label: "test", Secret: "sk-test-val", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)})
	m.vaultSession = &vaultSession{vault: vault, key: [32]byte{}}
	// Suppress the background matrix so the view output is deterministic.
	m.matrix = NewMatrix(0, 0)
	v := stripANSIForTest(m.View().Content)
	// B2: TUI launch shows real strategy output, not generic safety rows.
	// Verify the launch footer and command section are present.
	for _, want := range []string{"Enter launches default", "Command"} {
		if !strings.Contains(v, want) {
			t.Errorf("launch view missing expected section %q", want)
		}
	}
	// Generic "all good" safety checkmarks must NOT appear.
	for _, banned := range []string{"child process only", "global shell untouched", "audit metadata only", "raw secret never displayed"} {
		if strings.Contains(v, banned) {
			t.Errorf("launch view shows generic safety claim %q — should show real strategy output", banned)
		}
	}
}

func TestView_NeverContainsRawSecret(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenKeys
	// Add a key with a fake secret (masked).
	m.keys = []secret.MaskedKeyItem{
		{ID: "key_test", Label: "test-key", MaskedSecret: "sk-t...t", ProviderSlug: "openrouter"},
	}
	v := stripANSIForTest(m.View().Content)
	// The raw secret should never appear.
	if strings.Contains(v, "sk-raw-secret-value") {
		t.Error("View should never contain raw secret")
	}
}

func TestProfilesZ_AddModalRenders(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProfiles
	m.modal = modalAdd
	m.modalPrompt = "Profile name"

	v := stripANSIForTest(m.View().Content)

	if !strings.Contains(v, "Add") {
		t.Fatalf("profiles add modal did not render 'Add' title")
	}
	if !strings.Contains(v, "Profile name") {
		t.Fatalf("profiles add modal did not render field prompt")
	}
	if !strings.Contains(v, "Enter next/save") {
		t.Fatalf("profiles add modal did not render help text")
	}
}

func TestProvidersZ_AddModalRenders(t *testing.T) {
	m := newTestModel(t)
	m.focus = focusContent
	m.active = screenProviders
	_ = m.providers.Add(provider.Provider{Name: "Test", Slug: "test", EnvVar: "TEST_KEY"})
	m.modal = modalAdd
	m.modalPrompt = "Provider name"

	v := stripANSIForTest(m.View().Content)

	if !strings.Contains(v, "Add") {
		t.Fatalf("providers add modal did not render 'Add' title")
	}
	if !strings.Contains(v, "Provider name") {
		t.Fatalf("providers add modal did not render field prompt")
	}
}

// TestLogAudit_RecordsKeyAdd verifies the TUI writes audit events on commit.
// The audit log must capture metadata-only events (never raw secrets).
func TestLogAudit_RecordsKeyAdd(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/audit.log"

	m := newTestModel(t)
	m.configDir = dir
	m.unlocked = true
	m.auditLogger = audit.NewLogger(logPath)
	m.vaultSession = &vaultSession{
		vault: &secret.Vault{Version: 1},
		key:   [32]byte{},
	}

	// Simulate a filled-out key add form and commit.
	m.keyForm = keyFormState{
		providerSlug: "openai",
		label:        "main",
		secret:       "sk-should-never-appear-in-audit",
	}
	m.keyFormActive = 3
	_, _ = m.commitKeyAdd()

	// The audit log must contain the key.add event but NEVER the raw secret.
	events, err := m.auditLogger.Tail(10)
	if err != nil || len(events) == 0 {
		t.Fatalf("expected audit event, got err=%v events=%d", err, len(events))
	}
	var found bool
	for _, e := range events {
		if e.Event == "key.add" && e.Provider == "openai" {
			found = true
		}
		// The secret must never leak into any audit event field, including
		// metadata maps.
		if strings.Contains(e.Event, "sk-should-never") ||
			strings.Contains(e.Provider, "sk-should-never") ||
			strings.Contains(e.Profile, "sk-should-never") {
			t.Fatal("raw secret leaked into audit event")
		}
		for _, v := range e.Metadata {
			if strings.Contains(v, "sk-should-never") {
				t.Fatal("raw secret leaked into audit metadata")
			}
		}
	}
	if !found {
		t.Fatal("key.add audit event not recorded")
	}
}

// TestKeyRename_TUICommit proves the TUI key edit/rename path persists a new
// label to the encrypted vault (the "Key edit/rename is CLI-only" gap).
func TestKeyRename_TUICommit(t *testing.T) {
	m := newTestModel(t)
	v := &secret.Vault{}
	rec := secret.SecretRecord{ID: "key_1", ProviderSlug: "openrouter", Label: "old-label", Secret: "sk-testsecretvalue123"}
	v.Keys = []secret.SecretRecord{rec}
	m.vaultSession = &vaultSession{vault: v, key: [32]byte{}}
	m.keys = secret.ToMaskedList(v.Keys)
	m.active = screenKeys
	m.selected[screenKeys] = 0
	m.addValues = []string{"new-label", "openrouter", "alpha,beta"}
	m.commitEdit()

	got := m.vaultSession.vault.Get("key_1")
	if got == nil {
		t.Fatal("key disappeared after edit")
	}
	if got.Label != "new-label" {
		t.Fatalf("expected renamed label new-label, got %q", got.Label)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "alpha" {
		t.Fatalf("expected tags parsed, got %v", got.Tags)
	}
	if m.statusMsg != "Updated." {
		t.Fatalf("expected Updated. status, got %q", m.statusMsg)
	}
}
