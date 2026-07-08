package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/config"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/runner"
	"aegiskeys/internal/secret"
	"aegiskeys/internal/security"
)

// Update routes messages. Blocking work never runs here; it returns tea.Cmd.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.matrix != nil {
			m.matrix.Resize(m.width, m.height)
		}
		return m, nil

	case matrixMsg:
		if m.matrix != nil {
			return m, m.matrix.Update(msg)
		}
		return m, nil

	case unlockResultMsg:
		if msg.err != nil {
			m.unlockError = msg.err.Error()
			m.passwordInput.Reset()
			return m, m.passwordInput.Focus()
		}
		m.unlocked = true
		m.unlockError = ""
		m.lastActivity = time.Now()
		m.keys = msg.keys
		m.vaultSession = &vaultSession{vault: msg.vault, key: msg.key, envelope: msg.envelope}
		m.passwordInput.Blur()
		m.passwordInput.Reset()
		m.matrix.TriggerSpark(matrixUnlock)
		// Start the periodic idle-check ticker so auto-lock works even without
		// keypresses. lockVault stops the ticker (via m.quit / re-lock).
		return m, autoLockTick()

	case doctorResultMsg:
		m.doctorResults = msg.results
		m.doctorRan = true
		if hasDoctorFailures(msg.results) {
			m.matrix.TriggerSpark(matrixDoctorFail)
		} else {
			m.matrix.TriggerSpark(matrixDoctorOK)
		}
		return m, nil

	case launchPreparedMsg:
		if msg.err != nil {
			m.statusMsg = "Launch failed: " + msg.err.Error()
			return m, nil
		}
		m.statusMsg = "Launching " + msg.profile + "..."
		return m, tea.ExecProcess(msg.cmd, func(err error) tea.Msg {
			usageErr := error(nil)
			cleanupErr := error(nil)
			if msg.vault != nil && msg.vault.vault != nil && childLikelyStarted(err) {
				msg.vault.vault.Touch(msg.keyID)
				if saveErr := secret.SaveVaultWithKey(config.VaultPath(msg.configDir), msg.vault.key, msg.vault.vault); saveErr != nil {
					usageErr = fmt.Errorf("save vault usage metadata: %w", saveErr)
				}
			}
			if msg.cleanup != nil {
				cleanupErr = msg.cleanup()
			}
			return launchFinishedMsg{err: err, usageErr: usageErr, cleanupErr: cleanupErr}
		})

	case launchFinishedMsg:
		if msg.cleanupErr != nil {
			// Preserve both child exit status and cleanup failure when both fail.
			if msg.err != nil {
				m.statusMsg = "Child exited: " + msg.err.Error() + "; config cleanup failed: " + msg.cleanupErr.Error()
			} else {
				m.statusMsg = "Child finished; config cleanup failed: " + msg.cleanupErr.Error()
			}
			return m, nil
		}
		if msg.usageErr != nil {
			m.statusMsg = "Child finished; usage metadata failed: " + msg.usageErr.Error()
			return m, nil
		}
		if msg.err != nil {
			m.statusMsg = "Child exited: " + msg.err.Error()
		} else {
			m.statusMsg = "Child process finished."
		}
		return m, nil

	case wizardModelsFetchedMsg:
		m.wizard.fetchingModels = false
		if msg.err != nil {
			// Non-fatal: show error in status but let the user type manually.
			m.wizard.errMsg = "Model fetch failed: " + msg.err.Error()
			return m, nil
		}
		// Only apply if the provider hasn't changed since the fetch started.
		if msg.providerSlug == m.wizard.draft.ProviderSlug {
			m.wizard.fetchedModels = msg.models
			// Pre-select first model for the active slot if input is empty.
			if len(msg.models) > 0 {
				m.ensureModelInputs()
				slotName := m.wizardActiveSlotName()
				if slotName != "" {
					if input, ok := m.wizard.modelSlotInputs[slotName]; ok {
						if strings.TrimSpace(input.Value()) == "" {
							input.SetValue(msg.models[0].ID)
							m.wizard.modelSlotInputs[slotName] = input
						}
					}
				}
				m.statusMsg = fmt.Sprintf("Loaded %d models for %s", len(msg.models), msg.providerSlug)
			}
		}
		return m, nil

	case scratchExternalEditedMsg:
		if m.scratchEditing && m.scratchEditingID != "" {
			m.scratchBodyInput.SetValue(msg.body)
			m.scratchDirty = true
			m.statusMsg = "Loaded from external editor (ctrl+s to save)."
		}
		return m, nil

	case statusMsgMsg:
		m.statusMsg = msg.msg
		return m, nil

	case modelCatalogLoadedMsg:
		m.modelCatalog.fetching = false
		if msg.err != nil {
			m.modelCatalog.errMsg = "Refresh failed: " + msg.err.Error()
			return m, nil
		}
		if msg.providerSlug != m.modelCatalog.providerSlug {
			return m, nil
		}
		// Merge fetched models with existing: prefer fetched metadata but
		// preserve the selected flag. New models are NOT auto-selected.
		existing := make(map[string]provider.ProviderModel, len(m.modelCatalog.models))
		for _, mod := range m.modelCatalog.models {
			existing[mod.ID] = mod
		}
		for _, fm := range msg.models {
			preSelected := m.modelCatalog.selected[fm.ID]
			if old, ok := existing[fm.ID]; ok {
				// Update metadata but preserve selection and mark as no longer
				// static-only (it is now confirmed from the live API).
				if fm.Name != "" {
					old.Name = fm.Name
				}
				if fm.ContextSize > 0 {
					old.ContextSize = fm.ContextSize
				}
				existing[fm.ID] = old
				m.modelCatalog.selected[fm.ID] = preSelected
			} else {
				existing[fm.ID] = fm
				m.modelCatalog.selected[fm.ID] = false
			}
		}
		merged := make([]provider.ProviderModel, 0, len(existing))
		for _, mod := range m.modelCatalog.models {
			if updated, ok := existing[mod.ID]; ok {
				merged = append(merged, updated)
				delete(existing, mod.ID)
			}
		}
		for _, mod := range existing {
			merged = append(merged, mod)
		}
		m.modelCatalog.models = merged

		// Persist the cache.
		cache := provider.NewModelCache(*m.providers.Find(msg.providerSlug), msg.models)
		_ = provider.SaveModelCache(m.configDir, cache)

		count := len(msg.models)
		m.logAudit("model_catalog_refresh", msg.providerSlug, m.modelCatalog.keyID)
		m.statusMsg = fmt.Sprintf("Loaded %d models for %s", count, msg.providerSlug)
		return m, nil

	case tea.KeyPressMsg:
		// Always allow ctrl+c to quit.
		if msg.String() == "ctrl+c" {
			m.quit = true
			return m, tea.Quit
		}
		// While locked, password field owns everything except ctrl+c.
		if !m.unlocked && m.vaultExists {
			return m.handleLockedKey(msg)
		}
		// Auto-lock check FIRST: any keypress while unlocked but idle past the
		// threshold locks the vault and discards the current keypress. This
		// must run before recording activity, otherwise the fresh timestamp
		// would reset the idle timer and auto-lock would never fire.
		if m.unlocked {
			m.autoLockIfIdle()
			if !m.unlocked {
				return m, nil
			}
			// Now record activity so the idle window resets on real input.
			m.lastActivity = time.Now()
		}
		return m.handleKey(msg)

	case autoLockMsg:
		// Timer-driven idle check: fires periodically regardless of keypresses
		// so an unattended terminal locks even without input. Stops ticking
		// once locked to avoid a leaked timer goroutine.
		if !m.unlocked {
			return m, nil
		}
		m.autoLockIfIdle()
		if !m.unlocked {
			return m, nil
		}
		return m, autoLockTick()

	case tea.PasteMsg:
		// Forward bracketed-paste content to whichever input currently owns it.
		// Without this case the root Update swallows paste events and the
		// textinput never receives them.
		return m.handlePaste(string(msg.Content))
	}
	return m, nil
}

// handlePaste routes a paste event to the focused text input. The key-add modal
// (specifically the secret field) is the main case, but delete-confirm and
// other modals ignore paste.
func (m *model) handlePaste(content string) (tea.Model, tea.Cmd) {
	if m.modal == modalAddKey {
		var cmd tea.Cmd
		m.addInput, cmd = m.addInput.Update(tea.PasteMsg{Content: content})
		m.syncKeyField()
		return m, cmd
	}
	// While locked, let the password field receive pastes too.
	if !m.unlocked && m.vaultExists {
		var cmd tea.Cmd
		m.passwordInput, cmd = m.passwordInput.Update(tea.PasteMsg{Content: content})
		return m, cmd
	}
	return m, nil
}

func hasDoctorFailures(results []security.CheckResult) bool {
	for _, r := range results {
		if r.Severity == security.SeverityFail {
			return true
		}
	}
	return false
}

func normalizedKey(k tea.KeyPressMsg) string {
	key := k.String()
	if key == "" && k.Text != "" {
		key = k.Text
	}
	if len([]rune(key)) == 1 {
		key = strings.ToLower(key)
	}
	return key
}

func (m *model) handleLockedKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		if pw := m.passwordInput.Value(); pw != "" {
			return m, unlockCmd(m.configDir, pw)
		}
		return m, nil
	case "esc":
		// The locked screen prompts "Esc/Ctrl+C to quit" — honor it. Esc quits
		// the whole app; Ctrl+C is handled earlier in Update.
		m.quit = true
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.passwordInput, cmd = m.passwordInput.Update(k)
	return m, cmd
}

// handleKey is the central router. Order:
//  1. Modal open        -> modal owns keys (esc closes)
//  2. Form/input active -> input owns keys (esc/enter handled by caller)
//  3. Quit (only when not typing)
//  4. Global actions    -> r (doctor), z/n (add)
//  5. Global navigation -> screen jumps, tab, [, ]
//  6. Route to focused pane (sidebar or content)
func (m *model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := normalizedKey(k)

	// 1. Modal owns the keyboard when open. Pass the original KeyPressMsg so
	// bubbles/textinput receives real key events instead of inert strings.
	if m.focus == focusModal {
		return m.handleModalKey(k)
	}

	// 2. Active text input (filter, command) owns keys until esc/enter.
	if m.inputFocused {
		switch key {
		case "esc":
			m.inputFocused = false
			m.filterInput.Blur()
			return m, nil
		case "enter":
			m.filterText = m.filterInput.Value()
			m.inputFocused = false
			m.filterInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(k)
		return m, cmd
	}

	// Launch command input owns keys when active.
	if m.active == screenLaunch && m.launchMode == launchTypeCommand {
		switch key {
		case "esc":
			m.launchMode = launchSelectProfile
			m.commandInput.Blur()
			m.commandInput.Reset()
			return m, nil
		case "enter":
			m.launchCommand = m.commandInput.Value()
			m.commandInput.Blur()
			m.launchMode = launchSelectProfile
			return m, m.prepareTUILaunch(m.launchCommand)
		}
		// Let command input own all other keys.
		var cmd tea.Cmd
		m.commandInput, cmd = m.commandInput.Update(k)
		return m, cmd
	}

	// Wizard owns keys when active.
	if m.wizard.active {
		return m.handleWizardKey(k)
	}

	// Model catalog overlay owns keys when active — intercept before global
	// quit so 'q' closes the overlay instead of the whole app.
	if m.modelCatalog.active {
		return m.handleModelCatalogKey(k)
	}

	// 3. Quit only when not typing in any input.
	if key == "q" {
		m.lockVault()
		m.quit = true
		return m, tea.Quit
	}

	// 4. Global actions. These should work from either sidebar or content as long
	// as no text field/modal currently owns the keyboard.
	switch key {
	case "ctrl+l":
		m.lockVault()
		return m, nil
	case "r":
		// If on the keys screen, r rotates a key (screen-local action).
		if m.active == screenKeys && m.focus == focusContent {
			return m.startRotate()
		}
		m.active = screenDoctor
		m.focus = focusContent
		m.statusMsg = "Running security doctor..."
		return m, m.runDoctor()
	case "z", "n":
		return m.contextualAdd()
	}

	// 5. Global navigation (works regardless of focus pane).
	switch key {
	case "tab":
		m.cycleFocus()
		return m, nil
	case "[":
		m.prevScreen()
		return m, m.screenInitCmd(m.active)
	case "]":
		m.nextScreen()
		return m, m.screenInitCmd(m.active)
	case "1", "2", "3", "4", "5", "6", "7", "8":
		m.active = screen(key[0] - '1')
		m.focus = focusContent
		return m, m.screenInitCmd(m.active)
	case "?":
		m.active = screenHelp
		m.focus = focusSidebar
		return m, nil
	case "esc":
		if m.focus == focusContent {
			m.focus = focusSidebar
			return m, nil
		}
		return m, nil
	}

	// 6. Route to the focused pane.
	if m.focus == focusSidebar {
		return m.handleSidebarKey(key)
	}
	return m.handleContentKey(key)
}

// cycleFocus rotates sidebar -> content -> sidebar.
func (m *model) cycleFocus() {
	switch m.focus {
	case focusSidebar:
		m.focus = focusContent
	case focusContent:
		m.focus = focusSidebar
	default:
		m.focus = focusSidebar
	}
}

func (m *model) nextScreen() {
	m.active = screen((int(m.active) + 1) % screenCount())
	m.selected[m.active] = 0
}

func (m *model) prevScreen() {
	m.active = screen((int(m.active) - 1 + screenCount()) % screenCount())
	m.selected[m.active] = 0
}

// handleSidebarKey navigates the sidebar list.
func (m *model) handleSidebarKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "w", "up", "k":
		if int(m.active) > 0 {
			m.active = screen(int(m.active) - 1)
		}
	case "s", "down", "j":
		if int(m.active) < screenCount()-1 {
			m.active = screen(int(m.active) + 1)
		}
	case "d", "enter", "l", "right":
		m.focus = focusContent
		return m, m.screenInitCmd(m.active)
	case "z", "n":
		m.focus = focusContent
		return m.contextualAdd()
	case "a", "left", "h", "esc":
		// Already in sidebar; stay.
	}
	return m, nil
}

// handleContentKey dispatches to the active screen's local handler.
func (m *model) handleContentKey(key string) (tea.Model, tea.Cmd) {
	switch m.active {
	case screenDashboard:
		return m.handleDashboardKey(key)
	case screenProviders:
		return m.handleProvidersKey(key)
	case screenKeys:
		return m.handleKeysKey(key)
	case screenProfiles:
		return m.handleProfilesKey(key)
	case screenLaunch:
		return m.handleLaunchKey(key)
	case screenDoctor:
		return m.handleDoctorKey(key)
	case screenAudit:
		return m.handleAuditKey(key)
	case screenSettings:
		return m.handleSettingsKey(key)
	case screenScratch:
		return m.handleScratchKey(key)
	case screenHelp:
		return m.handleHelpKey(key)
	}
	return m, nil
}

// dashboardActionCount returns the number of selectable quick actions.
func dashboardActionCount() int { return 4 }

// handleDashboardKey processes keys on the dashboard.
func (m *model) handleDashboardKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "a", "left", "h":
		m.focus = focusSidebar
	case "d", "right", "l":
		m.statusMsg = "Dashboard: use number keys to jump to other screens."
	case "w", "up", "k":
		if m.selected[screenDashboard] > 0 {
			m.selected[screenDashboard]--
		}
	case "s", "down", "j":
		if m.selected[screenDashboard] < dashboardActionCount()-1 {
			m.selected[screenDashboard]++
		}
	case "enter":
		return m.runDashboardAction()
	}
	return m, nil
}

// runDashboardAction launches the selected quick action.
func (m *model) runDashboardAction() (tea.Model, tea.Cmd) {
	switch m.selected[screenDashboard] {
	case 0:
		m.startWizard()
		return m, nil
	case 1:
		return m.startKeyAdd()
	case 2:
		m.focus = focusContent
		return m.startAdd()
	case 3:
		m.active = screenDoctor
		m.focus = focusContent
		m.statusMsg = "Running security doctor..."
		return m, m.runDoctor()
	}
	return m, nil
}

// handleProvidersKey processes keys on the providers screen.
// Note: when the model catalog overlay is active, keys are already routed
// to handleModelCatalogKey by handleKey before reaching this function.
func (m *model) handleProvidersKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "w", "up", "k":
		if m.selected[screenProviders] > 0 {
			m.selected[screenProviders]--
		}
	case "s", "down", "j":
		m.selected[screenProviders] = m.clampSelected(m.selected[screenProviders] + 1)
	case "d", "right", "l", "enter":
		return m.openDetail()
	case "a", "left", "h":
		m.focus = focusSidebar
	case "e":
		return m.startEdit()
	case "z", "n":
		return m.contextualAdd()
	case "x":
		return m.startDelete()
	case "/":
		m.inputFocused = true
		m.filterInput.Focus()
		return m, nil
	case "m":
		return m, m.openModelCatalog()
	}
	return m, nil
}

// isSpaceKey reports whether the given key string represents a space press.
// Charm's KeyPressMsg returns "\x00" via String() for space, but the routing
// layer may also see it normalized differently depending on context.
func isSpaceKey(s string) bool {
	return s == " " || s == "\x00" || s == "space"
}

// handleModelCatalogKeyStr wraps the typed-string handler for callers that
// already have a normalized string key.
func (m *model) handleModelCatalogKeyStr(key string) (tea.Model, tea.Cmd) {
	return m.handleModelCatalogKey(tea.KeyPressMsg{Text: key})
}

// handleModelCatalogKey routes keys when the model catalog overlay is active
// on the providers screen. It accepts the raw KeyPressMsg so it can reliably
// detect space (which Charm encodes variably).
func (m *model) handleModelCatalogKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := normalizedKey(k)

	switch {
	case key == "esc" || key == "q":
		m.closeModelCatalog()
		return m, nil
	case key == "w" || key == "up" || key == "k":
		filtered := m.filteredCatalogModels()
		if m.modelCatalog.cursor > 0 && len(filtered) > 0 {
			m.modelCatalog.cursor--
		}
	case key == "down" || key == "j":
		filtered := m.filteredCatalogModels()
		if len(filtered) > 0 && m.modelCatalog.cursor < len(filtered)-1 {
			m.modelCatalog.cursor++
		}
	case isSpaceKey(key):
		filtered := m.filteredCatalogModels()
		if len(filtered) > 0 && m.modelCatalog.cursor < len(filtered) {
			id := filtered[m.modelCatalog.cursor].ID
			m.modelCatalog.selected[id] = !m.modelCatalog.selected[id]
		}
	case key == "a":
		// Select all currently visible (filtered) models.
		for _, mod := range m.filteredCatalogModels() {
			m.modelCatalog.selected[mod.ID] = true
		}
	case key == "c":
		// Deselect all currently visible (filtered) models.
		for _, mod := range m.filteredCatalogModels() {
			m.modelCatalog.selected[mod.ID] = false
		}
	case key == "/":
		// Toggle filter mode.
		m.modelCatalog.filtering = !m.modelCatalog.filtering
		if m.modelCatalog.filtering {
			m.modelCatalog.filter = ""
		}
		return m, nil
	case key == "r" && !m.modelCatalog.filtering:
		return m, m.refreshModelCatalog()
	case key == "s" && !m.modelCatalog.filtering:
		return m, m.saveModelCatalog()
	}

	// In filter mode: single chars append, backspace trims, enter/esc exits.
	if m.modelCatalog.filtering {
		switch {
		case key == "backspace":
			if len(m.modelCatalog.filter) > 0 {
				m.modelCatalog.filter = m.modelCatalog.filter[:len(m.modelCatalog.filter)-1]
			}
			return m, nil
		case key == "enter" || key == "esc":
			m.modelCatalog.filtering = false
			return m, nil
		case len(key) == 1 && !isSpaceKey(key):
			// Single printable rune appends to the filter string.
			m.modelCatalog.filter += key
			return m, nil
		}
	}
	return m, nil
}

// handleKeysKey processes keys on the keys screen.
func (m *model) handleKeysKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "w", "up", "k":
		if m.selected[screenKeys] > 0 {
			m.selected[screenKeys]--
		}
	case "s", "down", "j":
		m.selected[screenKeys] = m.clampSelected(m.selected[screenKeys] + 1)
	case "d", "right", "l", "enter":
		return m.openDetail()
	case "a", "left", "h":
		m.focus = focusSidebar
	case "e":
		return m.startEdit()
	case "r":
		return m.startRotate()
	case "z", "n":
		return m.contextualAdd()
	case "x":
		return m.startDelete()
	case "/":
		m.inputFocused = true
		m.filterInput.Focus()
		return m, nil
	}
	return m, nil
}

// handleProfilesKey processes keys on the profiles screen.
func (m *model) handleProfilesKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "w", "up", "k":
		if m.selected[screenProfiles] > 0 {
			m.selected[screenProfiles]--
		}
	case "s", "down", "j":
		m.selected[screenProfiles] = m.clampSelected(m.selected[screenProfiles] + 1)
	case "d", "right", "l", "enter":
		return m.openDetail()
	case "a", "left", "h":
		m.focus = focusSidebar
	case "e":
		return m.startEdit()
	case "z", "n":
		return m.contextualAdd()
	case "x":
		return m.startDelete()
	case "/":
		m.inputFocused = true
		m.filterInput.Focus()
		return m, nil
	}
	return m, nil
}

// handleDoctorKey processes keys on the doctor screen.
func (m *model) handleDoctorKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "w", "up", "k":
		if m.selected[screenDoctor] > 0 {
			m.selected[screenDoctor]--
		}
	case "s", "down", "j":
		m.selected[screenDoctor] = m.clampSelected(m.selected[screenDoctor] + 1)
	case "r":
		return m, m.runDoctor()
	case "d":
		// Restore missing default providers — self-heals a registry that was
		// trimmed to only custom/incompatible providers.
		return m.restoreDefaultProviders()
	case "a", "left", "h":
		m.focus = focusSidebar
	}
	return m, nil
}

// handleAuditKey processes keys on the audit screen.
func (m *model) handleAuditKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "w", "up", "k":
		if m.selected[screenAudit] > 0 {
			m.selected[screenAudit]--
		}
	case "s", "down", "j":
		m.selected[screenAudit] = m.clampSelected(m.selected[screenAudit] + 1)
	case "d", "right", "l", "enter":
		return m.openDetail()
	case "a", "left", "h":
		m.focus = focusSidebar
	}
	return m, nil
}

// handleSettingsKey processes keys on the settings screen.
func (m *model) handleSettingsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "w", "up", "k":
		if m.selected[screenSettings] > 0 {
			m.selected[screenSettings]--
		}
	case "s", "down", "j":
		m.selected[screenSettings] = m.clampSelected(m.selected[screenSettings] + 1)
	case "d", "right", "l", "enter":
		return m.adjustSetting(1)
	case "a", "left", "h":
		return m.adjustSetting(-1)
	}
	return m, nil
}

func (m *model) adjustSetting(dir int) (tea.Model, tea.Cmd) {
	switch m.selected[screenSettings] {
	case 0:
		m.cfg.AutoLock = cycleInt(m.cfg.AutoLock, []int{0, 5, 15, 30, 60}, dir)
		m.autoLockAfter = time.Duration(m.cfg.AutoLock) * time.Minute
		if m.cfg.AutoLock == 0 {
			m.statusMsg = "Auto-lock disabled"
		} else {
			m.statusMsg = fmt.Sprintf("Auto-lock: %d min", m.cfg.AutoLock)
		}
	case 1:
		m.themeName = cycleThemeBy(m.themeName, dir)
		m.styles = NewStyles(m.themeName)
		m.cfg.Theme = m.themeName
		m.statusMsg = "Theme: " + m.themeName
	case 2:
		m.cfg.DefaultProfile = cycleDefaultProfile(m.profiles.Profiles, m.cfg.DefaultProfile, dir)
		if m.cfg.DefaultProfile == "" {
			m.statusMsg = "Default profile cleared"
		} else {
			m.statusMsg = "Default profile: " + m.cfg.DefaultProfile
		}
	case 3:
		m.cfg.ClipboardTTLSeconds = cycleInt(m.cfg.ClipboardTTLSeconds, []int{0, 15, 30, 45, 60, 120}, dir)
		m.statusMsg = fmt.Sprintf("Clipboard TTL: %ds", m.cfg.ClipboardTTLSeconds)
	case 4:
		m.cfg.AdapterVerifyTimeoutSeconds = cycleInt(m.cfg.AdapterVerifyTimeoutSeconds, []int{5, 10, 20, 30, 60, 120}, dir)
		m.statusMsg = fmt.Sprintf("Adapter verify timeout: %ds", m.cfg.AdapterVerifyTimeoutSeconds)
	case 5:
		m.cfg.EnableAnimations = !m.cfg.EnableAnimations
		m.statusMsg = fmt.Sprintf("Animations: %t", m.cfg.EnableAnimations)
	case 6:
		m.cfg.EnableRiskyExport = !m.cfg.EnableRiskyExport
		m.statusMsg = fmt.Sprintf("Risky export: %t", m.cfg.EnableRiskyExport)
	case 7:
		m.cfg.RotationReminderDays = cycleInt(m.cfg.RotationReminderDays, []int{0, 30, 60, 90, 180, 365}, dir)
		if m.cfg.RotationReminderDays == 0 {
			m.statusMsg = "Rotation reminders disabled"
		} else {
			m.statusMsg = fmt.Sprintf("Rotation reminder: %d days", m.cfg.RotationReminderDays)
		}
	case 8:
		m.cfg.RuntimePolicy = cycleString(m.cfg.RuntimePolicy, []string{config.RuntimePolicyStrict, config.RuntimePolicyStandard, config.RuntimePolicyPermissive}, dir)
		if m.cfg.RuntimePolicy == config.RuntimePolicyStrict {
			// Strict policy forbids risky export; reflect that immediately.
			m.cfg.EnableRiskyExport = false
		}
		m.statusMsg = "Runtime policy: " + m.cfg.RuntimePolicy
	default:
		m.statusMsg = "Config path: " + config.ConfigPath(m.configDir)
		return m, nil
	}
	if err := config.SaveConfig(config.ConfigPath(m.configDir), m.cfg); err != nil {
		m.statusMsg += " (save failed: " + err.Error() + ")"
	}
	return m, nil
}

// handleHelpKey processes keys on the help screen.
func (m *model) handleHelpKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "a", "left", "h":
		m.focus = focusSidebar
	}
	return m, nil
}

// handleScratchKey processes keys on the encrypted scratchpad screen.
func (m *model) handleScratchKey(key string) (tea.Model, tea.Cmd) {
	// When editing, the textarea owns most keys; only control combos escape.
	if m.scratchEditing {
		switch key {
		case "ctrl+s":
			return m, m.saveScratchPad()
		case "ctrl+e":
			return m, m.editScratchInExternalEditor()
		case "esc":
			if m.scratchDirty {
				m.scratchDirty = false
				m.statusMsg = "Edit discarded."
			}
			m.scratchEditing = false
			m.scratchEditingID = ""
			return m, nil
		}
		return m, nil
	}

	switch key {
	case "w", "up", "k":
		if m.scratchListSelected > 0 {
			m.scratchListSelected--
		}
	case "s", "down", "j":
		if m.scratchListSelected < len(m.visibleScratchPads())-1 {
			m.scratchListSelected++
		}
	case "n":
		return m, m.newScratchPad()
	case "e", "enter":
		return m, m.editScratchPad()
	case "c":
		return m, m.copyScratchBody()
	case "x":
		return m, m.deleteScratchPad()
	case "/":
		m.inputFocused = true
		m.filterInput.Focus()
		return m, nil
	case "a", "left", "h":
		m.focus = focusSidebar
	case "esc", "q":
		m.active = screenKeys
		m.focus = focusSidebar
	}
	return m, nil
}

// newScratchPad creates a new encrypted scratchpad and opens it for editing.
func (m *model) newScratchPad() tea.Cmd {
	if m.vaultSession == nil || m.vaultSession.vault == nil {
		return nil
	}
	sp := secret.ScratchPadRecord{
		Kind:  secret.ScratchPadGeneral,
		Title: "New note",
	}
	if err := m.vaultSession.vault.AddScratchPad(sp); err != nil {
		return nil
	}
	m.scratchEditingID = sp.ID
	m.scratchTitleInput.SetValue(sp.Title)
	m.scratchBodyInput.SetValue("")
	m.scratchBodyInput.SetWidth(maxInt(40, m.width-10))
	m.scratchBodyInput.SetHeight(maxInt(8, m.height-12))
	m.scratchEditing = true
	m.scratchDirty = true
	m.logAudit("scratch.create", "", "")
	return nil
}

// editScratchPad opens the selected scratchpad for editing.
func (m *model) editScratchPad() tea.Cmd {
	sp := m.selectedScratchPad()
	if sp == nil {
		return nil
	}
	m.scratchEditingID = sp.ID
	m.scratchTitleInput.SetValue(sp.Title)
	m.scratchBodyInput.SetValue(sp.Body)
	m.scratchBodyInput.SetWidth(maxInt(40, m.width-10))
	m.scratchBodyInput.SetHeight(maxInt(8, m.height-12))
	m.scratchEditing = true
	m.scratchDirty = false
	return nil
}

// saveScratchPad persists the edited scratchpad to the vault.
func (m *model) saveScratchPad() tea.Cmd {
	sp := m.selectedScratchPad()
	if sp == nil {
		return nil
	}
	sp.Title = m.scratchTitleInput.Value()
	sp.Body = m.scratchBodyInput.Value()
	if err := m.vaultSession.vault.UpdateScratchPad(sp.ID, *sp); err != nil {
		return nil
	}
	if err := secret.SaveVaultWithKey(config.VaultPath(m.configDir), m.vaultSession.key, m.vaultSession.vault); err != nil {
		return nil
	}
	m.scratchEditing = false
	m.scratchDirty = false
	m.scratchEditingID = ""
	m.logAudit("scratch.update", "", "")
	return nil
}

// copyScratchBody copies the scratchpad body to clipboard (if policy allows).
func (m *model) copyScratchBody() tea.Cmd {
	sp := m.selectedScratchPad()
	if sp == nil {
		return nil
	}
	m.logAudit("scratch.copy", "", "")
	return tea.SetClipboard(sp.Body)
}

// deleteScratchPad removes the selected scratchpad from the vault.
func (m *model) deleteScratchPad() tea.Cmd {
	sp := m.selectedScratchPad()
	if sp == nil {
		return nil
	}
	if err := m.vaultSession.vault.RemoveScratchPad(sp.ID); err != nil {
		return nil
	}
	if err := secret.SaveVaultWithKey(config.VaultPath(m.configDir), m.vaultSession.key, m.vaultSession.vault); err != nil {
		return nil
	}
	if m.scratchListSelected > 0 {
		m.scratchListSelected--
	}
	m.logAudit("scratch.delete", "", "")
	return nil
}

// editScratchInExternalEditor opens the scratchpad body in $EDITOR.
func (m *model) editScratchInExternalEditor() tea.Cmd {
	sp := m.selectedScratchPad()
	if sp == nil {
		return nil
	}
	current := m.scratchBodyInput.Value()
	return func() tea.Msg {
		result, err := openInEditor(current)
		if err != nil {
			return statusMsgMsg{msg: "External editor failed: " + err.Error()}
		}
		return scratchExternalEditedMsg{body: result}
	}
}

type scratchExternalEditedMsg struct {
	body string
}

type statusMsgMsg struct {
	msg string
}

// settingKeys enumerates the adjustable settings on the Settings screen.
var settingKeys = []string{"Auto-lock", "Theme", "Default profile", "Clipboard TTL", "Verify timeout", "Animations", "Risky export", "Rotation reminders", "Runtime policy", "Config file"}

// cycleTheme returns the next theme name after the current one, wrapping
// around to the first. Used by the Settings screen to step through themes.
func cycleTheme(current string) string {
	return cycleThemeBy(current, 1)
}

func cycleThemeBy(current string, dir int) string {
	names := ThemeNames()
	if len(names) == 0 {
		return current
	}
	for i, n := range names {
		if n == current {
			return names[(i+dir+len(names))%len(names)]
		}
	}
	return names[0]
}

func cycleInt(current int, values []int, dir int) int {
	if len(values) == 0 {
		return current
	}
	for i, v := range values {
		if v == current {
			return values[(i+dir+len(values))%len(values)]
		}
	}
	return values[0]
}

// cycleString steps through a list of strings, wrapping around.
func cycleString(current string, values []string, dir int) string {
	if len(values) == 0 {
		return current
	}
	for i, v := range values {
		if v == current {
			return values[(i+dir+len(values))%len(values)]
		}
	}
	return values[0]
}

func cycleDefaultProfile(profiles []profile.Profile, current string, dir int) string {
	values := []string{""}
	for _, p := range profiles {
		values = append(values, p.Name)
	}
	for i, v := range values {
		if v == current {
			return values[(i+dir+len(values))%len(values)]
		}
	}
	if len(values) > 1 {
		return values[1]
	}
	return ""
}

// clampSelected keeps the cursor within the item count for a screen.
func (m *model) clampSelected(i int) int {
	count := m.itemCount(m.active)
	if count <= 0 {
		return 0
	}
	if i >= count {
		return count - 1
	}
	return i
}

// itemCount returns the number of selectable rows for a screen.
func (m *model) itemCount(s screen) int {
	switch s {
	case screenDashboard:
		return dashboardActionCount()
	case screenProviders:
		return len(m.providers.Providers)
	case screenKeys:
		return len(m.keys)
	case screenProfiles:
		return len(m.profiles.Profiles)
	case screenDoctor:
		return len(m.doctorResults)
	case screenAudit:
		return len(m.auditEvents)
	case screenSettings:
		return len(settingKeys)
	}
	return 0
}

// openDetail opens a detail modal for the selected item.
func (m *model) openDetail() (tea.Model, tea.Cmd) {
	m.modal = modalDetail
	m.focus = focusModal
	m.modalTarget = m.selectedItemKey()
	return m, nil
}

// startEdit opens an edit modal for the selected item.
func (m *model) startEdit() (tea.Model, tea.Cmd) {
	if m.itemCount(m.active) == 0 {
		m.statusMsg = "Nothing to edit."
		return m, nil
	}
	m.modal = modalEdit
	m.focus = focusModal
	m.addStep = 0
	m.addValues = nil
	m.addInput.Reset()
	m.addInput.Placeholder = m.editPlaceholder()
	m.modalPrompt = m.editPrompt()
	cmd := m.addInput.Focus()
	return m, cmd
}

// closeEditModal resets all edit-modal state so a failed commit can never
// leave a dirty, open modal behind (which previously caused an index panic
// on the next Enter in handleEditKey).
func (m *model) closeEditModal() {
	m.modal = modalNone
	m.focus = focusContent
	m.addInput.Blur()
	m.addValues = nil
	m.addStep = 0
}

// startRotate opens a modal to enter a new secret value for key rotation.
func (m *model) startRotate() (tea.Model, tea.Cmd) {
	if m.vaultSession == nil || m.vaultSession.vault == nil {
		m.statusMsg = "Vault session unavailable."
		return m, nil
	}
	if m.itemCount(m.active) == 0 {
		m.statusMsg = "Nothing to rotate."
		return m, nil
	}
	k := m.selectedKey()
	if k == nil {
		return m, nil
	}
	m.modal = modalRotate
	m.focus = focusModal
	m.modalTarget = k.ID
	m.addStep = 0
	m.addValues = nil
	m.addInput.Reset()
	m.addInput.Placeholder = "new secret value"
	m.addInput.EchoMode = textinput.EchoPassword
	m.modalPrompt = "Rotate secret for " + k.Label
	cmd := m.addInput.Focus()
	return m, cmd
}

func (m *model) editFields() []addField {
	switch m.active {
	case screenProviders:
		p := m.selectedProvider()
		if p == nil {
			return nil
		}
		return []addField{
			{label: "Provider name", placeholder: p.Name},
			{label: "Slug", placeholder: p.Slug},
			{label: "Env var", placeholder: p.CanonicalEnvVar()},
			{label: "Base URL", placeholder: p.CanonicalBaseURL()},
		}
	case screenProfiles:
		p := m.selectedProfile()
		if p == nil {
			return nil
		}
		return []addField{
			{label: "Profile name", placeholder: p.Name},
			{label: "Provider slug", placeholder: p.ProviderSlug},
			{label: "Key ID", placeholder: p.KeyID},
		}
	case screenKeys:
		k := m.selectedKey()
		if k == nil {
			return nil
		}
		return []addField{
			{label: "Label", placeholder: k.Label},
			{label: "Provider slug", placeholder: k.ProviderSlug},
			{label: "Tags (comma-sep)", placeholder: ""},
		}
	default:
		return nil
	}
}

func (m *model) editPrompt() string {
	fields := m.editFields()
	if len(fields) == 0 || m.addStep >= len(fields) {
		return "Edit"
	}
	return fields[m.addStep].label
}

func (m *model) editPlaceholder() string {
	fields := m.editFields()
	if len(fields) == 0 || m.addStep >= len(fields) {
		return "value"
	}
	return fields[m.addStep].placeholder
}

func (m *model) selectedProvider() *provider.Provider {
	if m.selected[screenProviders] < len(m.providers.Providers) {
		return &m.providers.Providers[m.selected[screenProviders]]
	}
	return nil
}

func (m *model) selectedProfile() *profile.Profile {
	if m.selected[screenProfiles] < len(m.profiles.Profiles) {
		return &m.profiles.Profiles[m.selected[screenProfiles]]
	}
	return nil
}

// selectedKey returns the currently selected key from the masked list.
func (m *model) selectedKey() *secret.MaskedKeyItem {
	if m.selected[screenKeys] < len(m.keys) {
		return &m.keys[m.selected[screenKeys]]
	}
	return nil
}

// contextualAdd routes the z/n key to the right action for the active screen.
// Dashboard/Launch → app-first profile wizard. Profiles → wizard. Keys → key add.
// Providers → provider add. Other screens → informative message.
func (m *model) contextualAdd() (tea.Model, tea.Cmd) {
	switch m.active {
	case screenDashboard, screenLaunch:
		m.startWizard()
		return m, nil

	case screenProfiles:
		m.startWizard()
		return m, nil

	case screenKeys:
		return m.startKeyAdd()

	case screenProviders:
		m.focus = focusContent
		return m.startAdd()

	default:
		m.statusMsg = "Nothing to add on this screen. Use Providers, Keys, or Profiles."
		return m, nil
	}
}

func (m *model) startAdd() (tea.Model, tea.Cmd) {
	if m.active != screenProviders && m.active != screenProfiles {
		m.statusMsg = "Add is available for Providers and Profiles. Keys use: aegiskeys key add"
		return m, nil
	}
	m.modal = modalAdd
	m.focus = focusModal
	m.addStep = 0
	m.addValues = nil
	m.addInput.Reset()
	m.addInput.Placeholder = m.addPlaceholder()
	m.modalPrompt = m.addPrompt()
	cmd := m.addInput.Focus()
	return m, cmd
}

// startKeyAdd opens the key add modal with empty fields.
func (m *model) startKeyAdd() (tea.Model, tea.Cmd) {
	m.modal = modalAddKey
	m.focus = focusModal
	m.keyForm = keyFormState{}
	m.keyFormActive = 0
	m.keyForm.providerIdx = 0
	if len(m.providers.Providers) > 0 {
		m.keyForm.providerSlug = m.providers.Providers[0].Slug
	}
	m.addInput.Reset()
	m.addInput.Placeholder = "provider (↑/↓ to select)"
	m.modalPrompt = "Provider"
	return m, nil
}

// commitKeyAdd validates and persists the new key to the encrypted vault.

func (m *model) startDelete() (tea.Model, tea.Cmd) {
	if m.itemCount(m.active) == 0 {
		return m, nil
	}
	m.modal = modalConfirmDelete
	m.focus = focusModal
	m.modalTarget = m.selectedItemKey()
	m.deleteBlockReason = ""
	m.deleteConfirmWait = false
	m.deleteConfirmed = false
	m.deleteConfirmWait = false
	m.deleteConfirmed = false
	switch m.active {
	case screenProviders:
		// If the provider has referencing keys, open straight into the
		// cascade-delete confirmation (key count shown) instead of a generic
		// "are you sure". No keys → fall through to the normal confirm prompt.
		var refCount int
		if m.vaultSession != nil && m.vaultSession.vault != nil {
			for _, k := range m.vaultSession.vault.Keys {
				if k.ProviderSlug == m.modalTarget {
					refCount++
				}
			}
		}
		if refCount > 0 {
			m.deleteConfirmWait = true
			m.deleteBlockReason = fmt.Sprintf("Provider %q is used by %d key(s). Delete provider and its keys?", m.modalTarget, refCount)
			return m, nil
		}
		m.modalPrompt = "Delete provider " + m.modalTarget + "?"
	case screenKeys:
		// Count profiles referencing this key to warn the user.
		refCount := 0
		for _, p := range m.profiles.Profiles {
			if p.KeyID == m.modalTarget {
				refCount++
			}
		}
		m.modalPrompt = "Delete key " + m.modalTarget + "?"
		if refCount == 1 {
			m.modalPrompt += " (referenced by 1 profile)"
		} else if refCount > 1 {
			m.modalPrompt += fmt.Sprintf(" (referenced by %d profiles)", refCount)
		}
	case screenProfiles:
		m.modalPrompt = "Delete profile " + m.modalTarget + "?"
		// Profile deletion is always allowed; the profile only references
		// metadata + a key id. No dangling pointers result.
	}
	return m, nil
}

// selectedItemKey returns a stable key for the selected item.
func (m *model) selectedItemKey() string {
	switch m.active {
	case screenProviders:
		if m.selected[screenProviders] < len(m.providers.Providers) {
			return m.providers.Providers[m.selected[screenProviders]].Slug
		}
	case screenKeys:
		if m.selected[screenKeys] < len(m.keys) {
			return m.keys[m.selected[screenKeys]].ID
		}
	case screenProfiles:
		if m.selected[screenProfiles] < len(m.profiles.Profiles) {
			return m.profiles.Profiles[m.selected[screenProfiles]].Name
		}
	}
	return ""
}

// handleModalKey processes keys while a modal is open.
func (m *model) handleModalKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := normalizedKey(k)
	// Add/Edit modals own the input field.
	if m.modal == modalAdd {
		return m.handleAddKey(k)
	}
	if m.modal == modalEdit {
		return m.handleEditKey(k)
	}
	if m.modal == modalAddKey {
		return m.handleKeyAddKey(k)
	}
	if m.modal == modalRotate {
		return m.handleRotateKey(k)
	}
	switch key {
	case "esc", "n":
		m.modal = modalNone
		m.modalTarget = ""
		m.deleteBlockReason = ""
		m.deleteConfirmWait = false
		m.deleteConfirmed = false
		m.focus = focusContent
		return m, nil
	case "enter", "y":
		if m.modal == modalConfirmDelete {
			if m.deleteConfirmWait {
				// User confirmed the cascade delete of provider + keys.
				m.deleteConfirmed = true
				m.deleteConfirmWait = false
				m.deleteBlockReason = ""
				blocked := m.applyDelete()
				_ = blocked // cascade path always succeeds
				m.modal = modalNone
				m.modalTarget = ""
				m.focus = focusContent
				return m, nil
			}
			blocked := m.applyDelete()
			if blocked {
				// Keep the modal open so the user sees the reason via
				// renderDeleteModal; clear it on their next Esc.
				return m, nil
			}
		}
		m.modal = modalNone
		m.modalTarget = ""
		m.focus = focusContent
		return m, nil
	}
	return m, nil
}

// handleKeyAddKey processes keys while the key add modal is open.
// Tab/Shift+Tab move between fields, Enter advances or saves on last field.
// When the provider field is active, up/down arrows select from existing providers.
func (m *model) handleKeyAddKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := normalizedKey(k)

	// Provider selection mode: up/down to browse providers.
	if m.keyFormActive == 0 && len(m.providers.Providers) > 0 {
		switch key {
		case "up", "k":
			if m.keyForm.providerIdx > 0 {
				m.keyForm.providerIdx--
			}
			m.keyForm.providerSlug = m.providers.Providers[m.keyForm.providerIdx].Slug
			return m, nil
		case "down", "j":
			if m.keyForm.providerIdx < len(m.providers.Providers)-1 {
				m.keyForm.providerIdx++
			}
			m.keyForm.providerSlug = m.providers.Providers[m.keyForm.providerIdx].Slug
			return m, nil
		}
	}

	switch key {
	case "esc":
		m.modal = modalNone
		m.focus = focusContent
		m.addInput.Blur()
		m.keyForm = keyFormState{}
		return m, nil
	case "tab":
		return m.advanceKeyField(1)
	case "shift+tab":
		return m.advanceKeyField(-1)
	case "enter":
		// On last field, save. Otherwise advance.
		if m.keyFormActive == 3 {
			return m.commitKeyAdd()
		}
		return m.advanceKeyField(1)
	default:
		var cmd tea.Cmd
		m.addInput, cmd = m.addInput.Update(k)
		m.syncKeyField()
		return m, cmd
	}
}

// syncKeyField copies the current input value into the active key form field.
func (m *model) syncKeyField() {
	val := m.addInput.Value()
	switch m.keyFormActive {
	case 1:
		m.keyForm.label = val
	case 2:
		m.keyForm.secret = val
	case 3:
		m.keyForm.tags = val
	}
}

// advanceKeyField saves the current field, moves to the next/prev field, and
// updates the input to show that field's current value.
func (m *model) advanceKeyField(dir int) (tea.Model, tea.Cmd) {
	m.syncKeyField()
	m.keyFormActive += dir
	if m.keyFormActive < 0 {
		m.keyFormActive = 3
	}
	if m.keyFormActive > 3 {
		m.keyFormActive = 0
	}
	// Load the new active field into the input.
	var val string
	switch m.keyFormActive {
	case 0:
		// Provider selector: sync index to current slug.
		m.addInput.Blur()
		m.modalPrompt = "Provider (↑/↓ to select)"
		for i, p := range m.providers.Providers {
			if p.Slug == m.keyForm.providerSlug {
				m.keyForm.providerIdx = i
				break
			}
		}
		return m, nil
	case 1:
		val = m.keyForm.label
		m.modalPrompt = "Label"
		m.addInput.Placeholder = "e.g. main, backup"
	case 2:
		val = ""
		m.modalPrompt = "Secret"
		m.addInput.Placeholder = "API key (hidden)"
	case 3:
		val = m.keyForm.tags
		m.modalPrompt = "Tags"
		m.addInput.Placeholder = "comma-separated (optional)"
	}
	m.addInput.SetValue(val)
	// Secret field uses password echo; every other field uses normal echo so a
	// buffered secret isn't inadvertently displayed.
	if m.keyFormActive == 2 {
		m.addInput.EchoMode = textinput.EchoPassword
		m.addInput.EchoCharacter = '•'
	} else {
		m.addInput.EchoMode = textinput.EchoNormal
	}
	if m.keyFormActive != 0 {
		return m, m.addInput.Focus()
	}
	return m, nil
}

type addField struct {
	label       string
	placeholder string
	required    bool
}

func (m *model) addFields() []addField {
	switch m.active {
	case screenProviders:
		return []addField{
			{label: "Provider name", placeholder: "", required: true},
			{label: "Slug", placeholder: "auto from name", required: false},
			{label: "Env var", placeholder: "MY_LLM_API_KEY", required: true},
			{label: "Base URL", placeholder: "https://api.example.com/v1", required: false},
		}
	default:
		return nil
	}
}

func (m *model) addPrompt() string {
	fields := m.addFields()
	if len(fields) == 0 || m.addStep >= len(fields) {
		return "Add"
	}
	return fields[m.addStep].label
}

func (m *model) addPlaceholder() string {
	fields := m.addFields()
	if len(fields) == 0 || m.addStep >= len(fields) {
		return "value"
	}
	return fields[m.addStep].placeholder
}

// handleAddKey processes keys while the add modal is open.
func (m *model) handleAddKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := normalizedKey(k)
	switch key {
	case "esc":
		m.modal = modalNone
		m.focus = focusContent
		m.addInput.Blur()
		m.addValues = nil
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.addInput.Value())
		fields := m.addFields()
		if len(fields) == 0 {
			m.modal = modalNone
			m.focus = focusContent
			m.statusMsg = "Nothing to add on this screen."
			return m, nil
		}
		if val == "" {
			if m.active == screenProviders && (m.addStep == 1 || m.addStep == 2) && len(m.addValues) > 0 {
				val = defaultSlugOrEnv(m.addValues[0], m.addStep)
			} else {
				m.statusMsg = fields[m.addStep].label + " cannot be empty."
				return m, nil
			}
		}
		m.addValues = append(m.addValues, val)
		m.addStep++
		if m.addStep < len(fields) {
			m.addInput.Reset()
			m.addInput.Placeholder = m.addPlaceholder()
			m.modalPrompt = m.addPrompt()
			return m, m.addInput.Focus()
		}
		return m.commitAdd()
	default:
		var cmd tea.Cmd
		m.addInput, cmd = m.addInput.Update(k)
		return m, cmd
	}
}

func defaultSlugOrEnv(name string, step int) string {
	slug := sanitizeSlug(name)
	if step == 1 {
		return slug
	}
	envSlug := strings.ToUpper(strings.ReplaceAll(slug, "-", "_"))
	return envSlug + "_API_KEY"
}

func sanitizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func (m *model) commitAdd() (tea.Model, tea.Cmd) {
	vals := m.addValues
	var err error
	switch m.active {
	case screenProviders:
		name, slug, envVar, baseURL := vals[0], vals[1], vals[2], vals[3]
		p := provider.Provider{
			Name:    name,
			Slug:    sanitizeSlug(slug),
			EnvVar:  envVar,
			BaseURL: baseURL,
		}
		p.Normalize()
		if vErr := p.ValidateStrict(); vErr != nil {
			m.statusMsg = "Provider invalid: " + vErr.Error()
			return m, nil
		}
		err = m.providers.Add(p)
		if err == nil {
			err = m.providers.Save(config.ProvidersPath(m.configDir))
		}
		if err == nil {
			m.logAudit("provider.add", p.Slug, "")
		}
	case screenProfiles:
		name, provSlug, keyID := vals[0], vals[1], vals[2]
		err = m.profiles.Add(profile.Profile{
			Name:         name,
			ProviderSlug: sanitizeSlug(provSlug),
			KeyID:        keyID,
		})
		if err == nil {
			err = profile.SaveStore(config.ProfilesPath(m.configDir), m.profiles)
		}
		if err == nil {
			m.logAudit("profile.add", "", name)
		}
	}

	m.modal = modalNone
	m.focus = focusContent
	m.addInput.Blur()
	m.addValues = nil
	if err != nil {
		m.statusMsg = "Add failed: " + err.Error()
		return m, nil
	}
	m.statusMsg = "Added."
	return m, nil
}

// applyDelete removes the targeted item from the in-memory model.
// Deletion is blocked (with a status message) when dependent items exist,
// to prevent dangling references and broken launch profiles. Returns true if
// the deletion was blocked (the modal stays open so the user sees why).
func (m *model) applyDelete() bool {
	m.deleteBlockReason = ""
	switch m.active {
	case screenProviders:
		// Find keys that reference this provider.
		var keyIDs []string
		if m.vaultSession != nil && m.vaultSession.vault != nil {
			for _, k := range m.vaultSession.vault.Keys {
				if k.ProviderSlug == m.modalTarget {
					keyIDs = append(keyIDs, k.ID)
				}
			}
		}
		// If keys reference this provider and the user hasn't confirmed the
		// cascade yet, prompt them. A hard block frustrates cleanup; instead
		// we warn and let them delete the provider and its keys together.
		if len(keyIDs) > 0 && !m.deleteConfirmed {
			m.deleteConfirmWait = true
			m.deleteBlockReason = fmt.Sprintf("Provider %q is used by %d key(s). Delete provider and its keys?", m.modalTarget, len(keyIDs))
			return true
		}
		// Confirmed (or no refs): cascade-delete the provider and its keys.
		for _, id := range keyIDs {
			_ = m.vaultSession.vault.Remove(id)
		}
		_ = m.providers.Remove(m.modalTarget)
		_ = m.providers.Save(config.ProvidersPath(m.configDir))
		if m.vaultSession != nil && m.vaultSession.vault != nil {
			m.keys = secret.ToMaskedList(m.vaultSession.vault.Keys)
		}
		m.logAudit("provider.delete", m.modalTarget, "")
		m.statusMsg = "Provider deleted."
		m.deleteConfirmWait = false
		m.deleteConfirmed = false
	case screenProfiles:
		_ = m.profiles.Remove(m.modalTarget)
		_ = profile.SaveStore(config.ProfilesPath(m.configDir), m.profiles)
		m.logAudit("profile.delete", "", m.modalTarget)
		m.statusMsg = "Profile deleted."
	case screenKeys:
		if m.vaultSession == nil || m.vaultSession.vault == nil {
			m.deleteBlockReason = "Vault session unavailable."
			return true
		}
		// Block deletion if any profile references this key.
		var profRefs []string
		for _, p := range m.profiles.Profiles {
			if p.KeyID == m.modalTarget {
				profRefs = append(profRefs, p.Name)
			}
		}
		if len(profRefs) > 0 {
			m.deleteBlockReason = fmt.Sprintf("Key is used by %d profile(s): %s. Delete or reassign profiles first.", len(profRefs), strings.Join(profRefs, ", "))
			return true
		}
		var deletedKeyProvider string
		if rec := m.vaultSession.vault.Get(m.modalTarget); rec != nil {
			deletedKeyProvider = rec.ProviderSlug
		}
		if err := m.vaultSession.vault.Remove(m.modalTarget); err != nil {
			m.statusMsg = "Delete failed: " + err.Error()
			return false
		}
		if err := secret.SaveVaultWithKey(config.VaultPath(m.configDir), m.vaultSession.key, m.vaultSession.vault); err != nil {
			m.statusMsg = "Vault save failed: " + err.Error()
			return false
		}
		m.keys = secret.ToMaskedList(m.vaultSession.vault.Keys)
		m.logAudit("key.delete", deletedKeyProvider, "")
		m.statusMsg = "Key deleted."
	}
	m.selected[m.active] = m.clampSelected(m.selected[m.active])
	return false
}

// handleEditKey processes keys while the edit modal is open.
func (m *model) handleEditKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := normalizedKey(k)
	switch key {
	case "esc":
		m.modal = modalNone
		m.focus = focusContent
		m.addInput.Blur()
		m.addValues = nil
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.addInput.Value())
		fields := m.editFields()
		if len(fields) == 0 {
			m.modal = modalNone
			m.focus = focusContent
			m.statusMsg = "Nothing to edit."
			return m, nil
		}
		// Guard: if addStep is already past the last field (a prior commit
		// attempt failed and left dirty state), abort the modal rather than
		// indexing out of range or looping on commit forever.
		if m.addStep >= len(fields) {
			m.modal = modalNone
			m.focus = focusContent
			m.addInput.Blur()
			m.addValues = nil
			if m.statusMsg == "" {
				m.statusMsg = "Edit failed. Please retry."
			}
			return m, nil
		}
		// If empty, keep existing value.
		if val == "" {
			val = fields[m.addStep].placeholder
		}
		m.addValues = append(m.addValues, val)
		m.addStep++
		if m.addStep < len(fields) {
			m.addInput.Reset()
			m.addInput.Placeholder = m.editPlaceholder()
			m.modalPrompt = m.editPrompt()
			return m, m.addInput.Focus()
		}
		return m.commitEdit()
	default:
		var cmd tea.Cmd
		m.addInput, cmd = m.addInput.Update(k)
		return m, cmd
	}
}

// handleRotateKey processes keys while the rotate modal is open.
func (m *model) handleRotateKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := normalizedKey(k)
	switch key {
	case "esc":
		m.modal = modalNone
		m.modalTarget = ""
		m.focus = focusContent
		m.addInput.Blur()
		m.addInput.EchoMode = textinput.EchoNormal
		m.addValues = nil
		return m, nil
	case "enter":
		return m.commitRotate()
	default:
		var cmd tea.Cmd
		m.addInput, cmd = m.addInput.Update(k)
		return m, cmd
	}
}

// commitRotate rotates the secret for the selected key.
func (m *model) commitRotate() (tea.Model, tea.Cmd) {
	if m.vaultSession == nil || m.vaultSession.vault == nil {
		m.statusMsg = "Vault session unavailable."
		return m, nil
	}
	newSecret := strings.TrimSpace(m.addInput.Value())
	if newSecret == "" {
		m.statusMsg = "Rotation cancelled (empty secret)."
		m.modal = modalNone
		m.focus = focusContent
		m.addInput.EchoMode = textinput.EchoNormal
		return m, nil
	}
	var rotatedKeyProvider string
	if rec := m.vaultSession.vault.Get(m.modalTarget); rec != nil {
		rotatedKeyProvider = rec.ProviderSlug
	}
	if err := m.vaultSession.vault.Rotate(m.modalTarget, newSecret); err != nil {
		m.statusMsg = "Rotation failed: " + err.Error()
		return m, nil
	}
	if err := secret.SaveVaultWithKey(config.VaultPath(m.configDir), m.vaultSession.key, m.vaultSession.vault); err != nil {
		m.statusMsg = "Vault save failed: " + err.Error()
		return m, nil
	}
	m.keys = secret.ToMaskedList(m.vaultSession.vault.Keys)
	m.modal = modalNone
	m.focus = focusContent
	m.addInput.EchoMode = textinput.EchoNormal
	msg := "Key rotated: " + m.modalTarget
	// Clear the rotated secret from the input buffer + reset echo mode.
	m.addInput.Reset()
	m.addValues = nil
	m.logAudit("key.rotate", rotatedKeyProvider, "")
	m.modalTarget = ""
	m.statusMsg = msg
	return m, nil
}

// commitKeyAdd validates and persists the new key to the encrypted vault.
func (m *model) commitKeyAdd() (tea.Model, tea.Cmd) {
	if m.vaultSession == nil || m.vaultSession.vault == nil {
		m.statusMsg = "Vault session unavailable. Lock and unlock again."
		return m, nil
	}
	f := m.keyForm
	if strings.TrimSpace(f.label) == "" || strings.TrimSpace(f.secret) == "" {
		m.statusMsg = "Label and secret are required."
		return m, nil
	}
	if strings.TrimSpace(f.providerSlug) == "" {
		f.providerSlug = "default"
	}
	// Validate the provider exists and has enough metadata to ever be used by
	// an adapter. A key attached to a broken/orphan provider is an unusable
	// encrypted blob — reject it early instead of storing dead weight.
	providerSlug := strings.ToLower(strings.TrimSpace(f.providerSlug))
	if providerSlug != "default" {
		p := m.providers.Find(providerSlug)
		if p == nil {
			m.statusMsg = "Provider " + providerSlug + " does not exist. Add it first."
			return m, nil
		}
		p.Normalize()
		if err := p.ValidateStrict(); err != nil {
			m.statusMsg = "Provider " + providerSlug + " is incomplete: " + err.Error()
			return m, nil
		}
	}
	rec := secret.SecretRecord{
		Kind:         secret.SecretAPIKey,
		ProviderSlug: strings.ToLower(strings.TrimSpace(f.providerSlug)),
		Label:        strings.TrimSpace(f.label),
		Secret:       f.secret,
		Tags:         parseTags(f.tags),
		RevealPolicy: secret.RevealConfirm,
		Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
	}
	if err := m.vaultSession.vault.Add(rec); err != nil {
		m.statusMsg = "Key add failed: " + err.Error()
		return m, nil
	}
	if err := secret.SaveVaultWithKey(config.VaultPath(m.configDir), m.vaultSession.key, m.vaultSession.vault); err != nil {
		m.statusMsg = "Key save failed: " + err.Error()
		return m, nil
	}
	m.keys = secret.ToMaskedList(m.vaultSession.vault.Keys)
	m.modal = modalNone
	m.focus = focusContent
	m.addInput.Blur()
	// Clear secret material from form state immediately after commit. Go strings
	// cannot be reliably zeroized, but clearing the fields reduces the window
	// where the raw secret lingers in model state.
	m.keyForm.secret = ""
	m.keyForm = keyFormState{}
	m.keyFormActive = 0
	m.addInput.Reset()
	m.matrix.TriggerSpark(matrixAddKey)
	m.logAudit("key.add", rec.ProviderSlug, "")
	m.statusMsg = "Key added and encrypted."
	return m, nil
}

// parseTags splits a comma-separated tag string into a trimmed slice.
func parseTags(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (m *model) commitEdit() (tea.Model, tea.Cmd) {
	vals := m.addValues
	var err error
	switch m.active {
	case screenProviders:
		p := m.selectedProvider()
		if p != nil && len(vals) >= 4 {
			p.Name = vals[0]
			p.Slug = sanitizeSlug(vals[1])
			p.EnvVar = vals[2]
			p.BaseURL = vals[3]
			p.Normalize()
			if vErr := p.ValidateStrict(); vErr != nil {
				m.statusMsg = "Provider invalid: " + vErr.Error()
				m.closeEditModal()
				return m, nil
			}
			err = m.providers.Save(config.ProvidersPath(m.configDir))
			if err == nil {
				m.logAudit("provider.edit", p.Slug, "")
			}
		}
	case screenProfiles:
		p := m.selectedProfile()
		if p != nil && len(vals) >= 3 {
			newProviderSlug := sanitizeSlug(vals[1])
			// Validate provider exists.
			prov := m.providers.Find(newProviderSlug)
			if prov == nil {
				m.statusMsg = "Unknown provider: " + newProviderSlug
				m.closeEditModal()
				return m, nil
			}
			// Validate key exists and matches provider.
			var key *secret.SecretRecord
			if m.vaultSession != nil && m.vaultSession.vault != nil {
				key = m.vaultSession.vault.Get(vals[2])
				if key == nil {
					m.statusMsg = "Unknown key: " + vals[2]
					m.closeEditModal()
					return m, nil
				}
				if key.ProviderSlug != "" && key.ProviderSlug != prov.Slug {
					m.statusMsg = fmt.Sprintf("Key provider %q does not match profile provider %q", key.ProviderSlug, prov.Slug)
					m.closeEditModal()
					return m, nil
				}
			}
			p.Name = vals[0]
			p.ProviderSlug = newProviderSlug
			p.KeyID = vals[2]
			// If we resolved the adapter, update the target RenderMode.
			if a, ok := m.adapterRegistry.Get(p.TargetApp()); ok {
				c := a.Contract()
				switch {
				case c.CanPatchConfig && c.CanInjectSecrets:
					p.Target.RenderMode = profile.RenderEnvConfig
				case c.CanPatchConfig:
					p.Target.RenderMode = profile.RenderConfigFile
				case c.CanInjectSecrets:
					p.Target.RenderMode = profile.RenderEnv
				}
			}
			_ = key
			err = profile.SaveStore(config.ProfilesPath(m.configDir), m.profiles)
			if err == nil {
				m.logAudit("profile.edit", "", p.Name)
			}
		}
	case screenKeys:
		k := m.selectedKey()
		if k != nil && m.vaultSession != nil && m.vaultSession.vault != nil && len(vals) >= 3 {
			rec := m.vaultSession.vault.Get(k.ID)
			if rec != nil {
				rec.Label = vals[0]
				rec.ProviderSlug = sanitizeSlug(vals[1])
				rec.Tags = parseTags(vals[2])
				m.vaultSession.vault.Touch(k.ID)
				err = secret.SaveVaultWithKey(config.VaultPath(m.configDir), m.vaultSession.key, m.vaultSession.vault)
				if err == nil {
					m.keys = secret.ToMaskedList(m.vaultSession.vault.Keys)
					m.logAudit("key.edit", rec.ProviderSlug, "")
				}
			}
		}
	}

	m.modal = modalNone
	m.focus = focusContent
	m.addInput.Blur()
	m.addValues = nil
	if err != nil {
		m.statusMsg = "Edit failed: " + err.Error()
		return m, nil
	}
	m.statusMsg = "Updated."
	return m, nil
}

// handleLaunchKey routes keys on the Launch screen.
func (m *model) handleLaunchKey(key string) (tea.Model, tea.Cmd) {
	if m.launchMode == launchTypeCommand {
		return m, nil
	}
	switch m.launchMode {
	case launchSelectProfile:
		switch key {
		case "w", "up", "k":
			if m.selected[screenLaunch] > 0 {
				m.selected[screenLaunch]--
			}
		case "s", "down", "j":
			if m.selected[screenLaunch] < len(m.profiles.Profiles)-1 {
				m.selected[screenLaunch]++
			}
		case "enter":
			if len(m.profiles.Profiles) > 0 {
				m.matrix.TriggerSpark(matrixLaunch)
				return m, m.prepareTUILaunch("")
			}
		case "d", "right", "l":
			if len(m.profiles.Profiles) > 0 {
				m.launchMode = launchTypeCommand
				m.commandInput.Focus()
				m.matrix.TriggerSpark(matrixLaunch)
			}
			return m, nil
		case "a", "left", "h", "esc":
			m.focus = focusSidebar
			return m, nil
		}
	}
	return m, nil
}

// screenInitCmd runs setup when switching to a screen.
func (m *model) screenInitCmd(s screen) tea.Cmd {
	if s == screenDoctor && !m.doctorRan {
		return m.runDoctor()
	}
	return nil
}

// --- async commands ---

func unlockCmd(configDir, password string) tea.Cmd {
	return func() tea.Msg {
		v, key, err := secret.LoadVaultWithKey(config.VaultPath(configDir), password)
		if err != nil {
			return unlockResultMsg{err: err}
		}
		// Re-read the envelope to capture KDF metadata for rekey checks.
		var env *secret.VaultEnvelope
		if raw, rerr := os.ReadFile(config.VaultPath(configDir)); rerr == nil {
			var e secret.VaultEnvelope
			if jerr := json.Unmarshal(raw, &e); jerr == nil {
				env = &e
			}
		}
		return unlockResultMsg{
			vault:    v,
			envelope: env,
			key:      key,
			keys:     secret.ToMaskedList(v.Keys),
		}
	}
}

// runDoctor runs the right doctor for the current state: locked-only
// checks when the vault is sealed, full unlocked checks otherwise. This
// is the single entry point the TUI uses.
func (m *model) runDoctor() tea.Cmd {
	return func() tea.Msg {
		if m.unlocked && m.vaultSession != nil && m.vaultSession.vault != nil {
			results := security.RunDoctorUnlocked(m.configDir, m.vaultSession.vault, m.profiles)
			return doctorResultMsg{results: results}
		}
		return doctorResultMsg{results: security.RunDoctor(m.configDir)}
	}
}

func runDoctorCmd(configDir string) tea.Cmd {
	return func() tea.Msg {
		return doctorResultMsg{results: security.RunDoctor(configDir)}
	}
}

// prepareTUILaunch resolves and materializes the selected launch strategy
// asynchronously. The returned launchPreparedMsg is then executed via
// tea.ExecProcess so Bubble Tea can release/restore the terminal.
func (m *model) prepareTUILaunch(commandLine string) tea.Cmd {
	idx := m.selected[screenLaunch]
	if idx < 0 || idx >= len(m.profiles.Profiles) {
		return func() tea.Msg { return launchPreparedMsg{err: fmt.Errorf("no profile selected")} }
	}
	prof := m.profiles.Profiles[idx]
	prov := m.providers.Find(prof.ProviderSlug)
	if prov == nil {
		return func() tea.Msg {
			return launchPreparedMsg{profile: prof.Name, err: fmt.Errorf("profile %q references missing provider %q", prof.Name, prof.ProviderSlug)}
		}
	}
	var key *secret.SecretRecord
	if m.vaultSession != nil && m.vaultSession.vault != nil {
		key = m.vaultSession.vault.Get(prof.KeyID)
	}
	vault := m.vaultSession
	configDir := m.configDir
	registry := m.adapterRegistry
	fields, parseErr := splitCommandLine(commandLine)
	if parseErr != nil {
		return func() tea.Msg { return launchPreparedMsg{profile: prof.Name, err: parseErr} }
	}

	return func() tea.Msg {
		strategy, err := adapter.ResolveLaunchStrategyCatalog(prof, *prov, key, registry, m.providers, m.vaultSession.vault, adapter.ResolveRun)
		if err != nil {
			return launchPreparedMsg{profile: prof.Name, err: err}
		}
		extraArgs := []string(nil)
		if len(fields) > 0 {
			if !strategy.Support.CanLaunchArbitraryCommand {
				return launchPreparedMsg{
					profile: prof.Name,
					err:     fmt.Errorf("adapter %s does not allow arbitrary command override", strategy.Support.ID),
				}
			}
			strategy.Plan.Command = fields[0]
			extraArgs = fields[1:]
			if err := adapter.ValidateLaunchStrategyForMode(strategy, prof, *prov, key, adapter.DefaultSecurityPolicy(), adapter.ResolveRun); err != nil {
				return launchPreparedMsg{profile: prof.Name, err: err}
			}
		}
		prepared, err := runner.PrepareCommandWithCleanup(context.Background(), strategy, runner.RunOptions{
			ProfileName:  prof.Name,
			ConfigDir:    configDir,
			ExtraArgs:    extraArgs,
			InheritStdio: true,
		})
		if err != nil {
			return launchPreparedMsg{profile: prof.Name, err: err}
		}
		return launchPreparedMsg{profile: prof.Name, cmd: prepared.Cmd, cleanup: prepared.Cleanup, vault: vault, configDir: configDir, keyID: prof.KeyID}
	}
}

func childLikelyStarted(err error) bool {
	if err == nil {
		return true
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return false
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return false
	}
	return true
}

func splitCommandLine(s string) ([]string, error) {
	var fields []string
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t', '\n':
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if escaped {
		b.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted command argument")
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields, nil
}

// autoLockMsg is a periodic timer tick that triggers an idle check. It lets
// auto-lock fire even when the user isn't typing (an unattended terminal).
type autoLockMsg struct{}

// autoLockTick schedules the next idle-check tick. The interval is a fraction
// of the auto-lock window so the lock fires close to the configured threshold.
func autoLockTick() tea.Cmd {
	return tea.Tick(time.Minute, func(time.Time) tea.Msg {
		return autoLockMsg{}
	})
}
