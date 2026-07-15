// Package tui implements the interactive AegisKeys terminal UI using the
// Charmbracelet v2 stack (bubbletea + lipgloss + bubbles + huh).
//
// v2 notes (terminal-tui-engineering SKILL):
//   - View() returns tea.View; AltScreen is set on that View.
//   - tea.Quit (the function value) quits.
package tui

import (
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/colorprofile"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/audit"
	"aegiskeys/internal/config"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
	"aegiskeys/internal/security"
)

// Run launches the interactive TUI against the given config directory.
func Run(configDir, version string) error {
	reg, err := provider.LoadRegistry(config.ProvidersPath(configDir))
	if err != nil {
		reg = provider.NewRegistry()
	}
	// Self-heal: merge missing default providers and backfill structural
	// fields. A partial or hand-edited providers.json must never strand the
	// user with only custom providers and no viable profile path.
	if reg.MergeDefaults(provider.DefaultProviders()) {
		_ = reg.Save(config.ProvidersPath(configDir))
	}
	store, err := profile.LoadStore(config.ProfilesPath(configDir))
	if err != nil {
		store = profile.NewStore()
	}

	cfg, cfgErr := config.LoadConfig(config.ConfigPath(configDir))
	if cfgErr != nil {
		cfg = config.DefaultConfig()
	}
	themeName := normalizeTheme(cfg.Theme)

	m := &model{
		configDir:       configDir,
		version:         version,
		styles:          NewStyles(themeName),
		themeName:       themeName,
		cfg:             cfg,
		providers:       reg,
		profiles:        store,
		vaultExists:     secret.VaultExists(config.VaultPath(configDir)),
		auditEvents:     nil,
		adapterRegistry: adapter.NewRegistry(),
		autoLockAfter:   time.Duration(cfg.AutoLock) * time.Minute,
	}
	m.auditLogger = audit.NewLogger(config.AuditPath(configDir))
	m.auditEvents, _ = m.auditLogger.Tail(10)
	m.passwordInput = newPasswordInput()
	m.matrix = NewMatrix(80, 30)

	m.addInput = textinput.New()
	m.addInput.Placeholder = "name"
	m.addInput.SetWidth(40)

	m.commandInput = textinput.New()
	m.commandInput.Placeholder = "command (blank = adapter default)"
	m.commandInput.SetWidth(48)

	m.scratchTitleInput = textinput.New()
	m.scratchTitleInput.Placeholder = "title"
	m.scratchTitleInput.SetWidth(40)

	m.scratchBodyInput = textarea.New()
	m.scratchBodyInput.SetWidth(60)
	m.scratchBodyInput.SetHeight(10)

	opts := []tea.ProgramOption{}
	if os.Getenv("FORCE_COLOR") != "" || os.Getenv("COLORTERM") == "truecolor" || os.Getenv("CLICOLOR_FORCE") != "" {
		opts = append(opts, tea.WithColorProfile(colorprofile.TrueColor))
	}
	p := tea.NewProgram(m, opts...)
	_, err = p.Run()
	return err
}

func newPasswordInput() textinput.Model {
	ti := textinput.New()
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Placeholder = "master password"
	ti.SetWidth(30)
	ti.Focus()
	return ti
}

// --- focus model ---------------------------------------------------------

// focusZone identifies which pane currently owns navigation/input.
type focusZone int

const (
	focusSidebar focusZone = iota
	focusContent
	focusForm
	focusModal
)

// screen identifies the active panel.
type screen int

const (
	screenDashboard screen = iota
	screenProviders
	screenKeys
	screenProfiles
	screenLaunch
	screenDoctor
	screenAudit
	screenSettings
	screenScratch
	screenHelp
)

var screenDefs = []struct {
	id    screen
	label string
	key   string
}{
	{screenDashboard, "Dashboard", "1"},
	{screenProviders, "Providers", "2"},
	{screenKeys, "Keys", "3"},
	{screenProfiles, "Profiles", "4"},
	{screenLaunch, "Launch", "5"},
	{screenDoctor, "Doctor", "6"},
	{screenAudit, "Audit", "7"},
	{screenSettings, "Settings", "8"},
	{screenScratch, "Scratch", "9"},
	{screenHelp, "Help", "?"},
}

func screenCount() int { return len(screenDefs) }

// modalKind identifies an active modal.
type modalKind int

const (
	modalNone modalKind = iota
	modalConfirmDelete
	modalDetail
	modalAdd
	modalEdit
	modalAddKey
	modalRotate
)

type launchMode int

const (
	launchSelectProfile launchMode = iota
	launchTypeCommand
)

// model is the single root Bubble Tea model.
type model struct {
	configDir   string
	version     string
	styles      *Styles
	providers   *provider.Registry
	profiles    *profile.Store
	vaultExists bool
	auditEvents []audit.Event

	width, height int

	active screen
	focus  focusZone
	quit   bool

	// Per-screen selected row index.
	selected [9]int

	// Vault / unlock state.
	unlocked      bool
	unlockError   string
	passwordInput textinput.Model

	// Loaded masked keys.
	keys          []secret.MaskedKeyItem
	doctorResults []security.CheckResult
	doctorRan     bool

	// auditLogger persists the append-only audit log handle. Stored (not
	// discarded) so TUI commits can record metadata-only events.
	auditLogger *audit.Logger

	// Vault session for encrypted key operations.
	vaultSession *vaultSession

	// Auto-lock session management (P0-9).
	lastActivity  time.Time
	autoLockAfter time.Duration

	// Adapter registry for rendering launch plans.
	adapterRegistry *adapter.Registry

	// Profile creation wizard.
	wizard wizardState

	// Modal state.
	modal       modalKind
	modalPrompt string
	// Key of the item targeted by the modal (provider slug / key id / profile name).
	modalTarget string
	// deleteBlockReason is set when a delete was blocked; the modal stays open
	// and shows this reason so the user can't miss why deletion was refused.
	deleteBlockReason string
	// deleteConfirmWait is set when a provider has referencing keys and we're
	// waiting for the user to confirm the cascade delete (yes/no).
	deleteConfirmWait bool
	// deleteConfirmed is set when the user answered "yes" to a cascade-delete
	// warning, so applyDelete proceeds instead of re-prompting.
	deleteConfirmed bool

	// Add form state.
	addInput  textinput.Model
	addStep   int
	addValues []string

	// Key add form state (multi-field).
	keyForm       keyFormState
	keyFormActive int // index of active field in key form

	// Theme: name of the active color theme (see styles.ThemeNames).
	themeName string

	// Loaded on-disk config (so theme/auto-lock changes can be persisted).
	cfg config.Config

	// Launch screen.
	launchMode    launchMode
	launchCommand string
	commandInput  textinput.Model

	// Form / input filter for lists.
	filterText   string
	inputFocused bool
	filterInput  textinput.Model

	// Status line (transient).
	statusMsg string

	// Model catalog overlay on the providers screen.
	modelCatalog modelCatalogState

	// Scratchpad screen state.
	scratchListSelected int
	scratchEditing      bool
	scratchEditingID    string
	scratchEditingTitle bool // true = title focused, false = body focused
	scratchTitleInput   textinput.Model
	scratchBodyInput    textarea.Model
	scratchDirty        bool
	scratchRevision     uint64
	scratchSaveInFlight bool
	scratchBodyCursor   int
	scratchSelecting    bool
	scratchSelectAnchor int

	// Last key press message, saved so screen handlers that need to forward
	// it to child input components (e.g. scratchpad editor) have access.
	lastKeyPress tea.KeyPressMsg

	// Background animation.
	matrix *Matrix
	// Track whether the matrix has been started.
	animStarted bool
}

// vaultSession holds the decrypted vault, the derived encryption key, and
// the last-seen envelope metadata in memory so the TUI can perform encrypted
// key operations (add, rotate, delete) and KDF-freshness checks without
// re-prompting for the master password. The raw password is NOT stored;
// only the derived [32] byte key is retained, and only for the unlocked session.
type vaultSession struct {
	vault    *secret.Vault
	envelope *secret.VaultEnvelope
	key      [32]byte
}

// keyFormState captures the fields for adding a new API key.
type keyFormState struct {
	providerSlug string
	label        string
	secret       string
	tags         string
	providerIdx  int // index into providers list for selection
	// setup holds non-secret provider setup param values keyed by SetupParam.Key
	// (e.g. Azure resource/deployment/api-version, Bedrock region).
	setup map[string]string
	// secretSetup holds secondary-secret setup param values keyed by Key
	// (e.g. an AWS secret access key).
	secretSetup map[string]string
}

// Zero overwrites the derived key bytes and clears decrypted secrets.
// Used on lock or quit to reduce the window of secret exposure in memory.
func (s *vaultSession) Zero() {
	for i := range s.key {
		s.key[i] = 0
	}
	if s.vault != nil {
		for i := range s.vault.Keys {
			s.vault.Keys[i].Secret = ""
			s.vault.Keys[i].PrivateNote = ""
		}
		for i := range s.vault.ScratchPads {
			s.vault.ScratchPads[i].Body = ""
		}
	}
	s.vault = nil
}

// lockVault clears the unlocked session and all decrypted material.
// This is the security-critical teardown: wipe the derived key, drop
// decrypted secrets, and clear every form field that may have held them.
func (m *model) lockVault() {
	if m.vaultSession != nil {
		m.vaultSession.Zero()
	}
	m.vaultSession = nil
	m.unlocked = false
	m.keys = nil
	m.lastActivity = time.Time{}
	// Clear all form state that may carry secret-adjacent data.
	m.keyForm.secret = ""
	m.keyForm = keyFormState{}
	m.addInput.Reset()
	m.commandInput.Reset()
	m.passwordInput.Reset()
	m.filterInput.Reset()
	m.modal = modalNone
	m.modalTarget = ""
	m.wizard = wizardState{}
	m.modelCatalog = modelCatalogState{}
	m.scratchEditing = false
	m.scratchEditingID = ""
	m.scratchTitleInput.Reset()
	m.scratchBodyInput.Reset()
	m.scratchDirty = false
	m.statusMsg = "Vault locked."
}

// autoLockIfIdle locks the vault if the user has been inactive longer than
// autoLockAfter. Returns true if it triggered a lock.
func (m *model) autoLockIfIdle() bool {
	if !m.unlocked || m.autoLockAfter <= 0 {
		return false
	}
	if time.Since(m.lastActivity) > m.autoLockAfter {
		m.lockVault()
		m.statusMsg = "Vault auto-locked (inactivity)."
		return true
	}
	return false
}

// logAudit records a metadata-only audit event. Never logs raw secrets —
// only event type, provider slug, and profile name. Nil-safe before the logger
// is wired.
func (m *model) logAudit(event, provider, profile string) {
	if m.auditLogger == nil {
		return
	}
	m.auditLogger.Log(audit.Event{
		Event:    event,
		Provider: provider,
		Profile:  profile,
	})
}

func (m *model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if !m.unlocked && m.vaultExists {
		cmds = append(cmds, m.passwordInput.Focus())
	}
	cmds = append(cmds, tickCmd())
	return tea.Batch(cmds...)
}

var _ tea.Model = (*model)(nil)

// --- internal messages ---

type unlockResultMsg struct {
	vault    *secret.Vault
	envelope *secret.VaultEnvelope
	key      [32]byte
	keys     []secret.MaskedKeyItem
	err      error
}

type doctorResultMsg struct {
	results []security.CheckResult
}

type launchPreparedMsg struct {
	profile   string
	cmd       *exec.Cmd
	cleanup   func() error
	vault     *vaultSession
	configDir string
	keyID     string
	err       error
}

type launchFinishedMsg struct {
	err        error
	usageErr   error
	cleanupErr error
}

// wizardModelsFetchedMsg carries the result of an async dynamic model catalog
// fetch triggered when the wizard enters StepModels for a dynamic provider.
type wizardModelsFetchedMsg struct {
	providerSlug string
	models       []provider.ProviderModel
	err          error
}

// modelCatalogLoadedMsg carries the result of an async model catalog refresh
// triggered from the providers screen overlay.
type modelCatalogLoadedMsg struct {
	providerSlug string
	models       []provider.ProviderModel
	err          error
}

// selectedIndex returns the screen's current selection, clamped.
func (m *model) selectedIndex(s screen) int {
	i := m.selected[s]
	return i
}
