package adapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
	"aegiskeys/internal/sensitive"
)

// --- App classification ---

// AppClass categorizes target applications by their injection surface.
type AppClass string

const (
	AppCLI          AppClass = "cli"
	AppTUI          AppClass = "tui"
	AppGUI          AppClass = "gui"
	AppIDE          AppClass = "ide"
	AppServer       AppClass = "server"
	AppLauncherOnly AppClass = "launcher_only"
)

// SupportLevel describes how strongly AegisKeys can control app auth.
type SupportLevel string

const (
	SupportFullEnv           SupportLevel = "full_env"
	SupportEnvConfig         SupportLevel = "env_config"
	SupportConfigKeychain    SupportLevel = "config_keychain"
	SupportLauncherIsolation SupportLevel = "launcher_isolation"
	SupportManualCredential  SupportLevel = "manual_credential"
	SupportOAuthManual       SupportLevel = "oauth_manual"
	SupportProxyMediated     SupportLevel = "proxy_mediated"
)

// CredentialControl describes how the credential reaches the app.
type CredentialControl string

const (
	CredentialVaultInjected CredentialControl = "vault_injected"
	CredentialEnvInjected   CredentialControl = "env_injected"
	CredentialConfigPatched CredentialControl = "config_patched"
	CredentialKeychainHint  CredentialControl = "keychain_handoff"
	CredentialManualLogin   CredentialControl = "manual_login"
	CredentialExternalStore CredentialControl = "external_store"
)

// InjectionSupport describes how well env injection works for an app.
type InjectionSupport string

const (
	InjectionFull        InjectionSupport = "full"
	InjectionPartial     InjectionSupport = "partial"
	InjectionConfigOnly  InjectionSupport = "config_only"
	InjectionUnsupported InjectionSupport = "unsupported"
)

// SupportConfidence states how well-verified an adapter's claims are.
type SupportConfidence string

const (
	ConfidenceVerified     SupportConfidence = "verified"
	ConfidenceManualProof  SupportConfidence = "manual_proof"
	ConfidenceExperimental SupportConfidence = "experimental"
	ConfidenceGuided       SupportConfidence = "guided"
	ConfidenceBlocked      SupportConfidence = "blocked"
)

// --- Contracts ---

// AppSupportContract describes how AegisKeys interacts with a target application.
type AppSupportContract struct {
	ID             string
	DisplayName    string
	DefaultCommand string

	SupportLevel      SupportLevel
	CredentialControl CredentialControl
	RenderModes       []string
	LaunchSurfaces    []string // cli, tui, ide, gui, daemon, extension

	SupportConfidence SupportConfidence // verified, manual_proof, experimental, guided, blocked

	// Verification records which test gates have passed. If
	// SupportConfidence is ConfidenceVerified but Verification.Verified()
	// is false, the confidence is overstated.
	Verification AdapterVerification

	CanLaunch                 bool
	CanLaunchArbitraryCommand bool // true = adapter may launch any user-specified command; DefaultCommand may be empty
	CanInjectSecrets          bool
	CanPatchConfig            bool
	CanManageModels           bool
	CanIsolateProfile         bool
	RequiresManualStep        bool

	RequiredEnv []EnvContract
	OptionalEnv []EnvContract

	ModelSlots       []ModelSlotContract
	ConfigFiles      []ConfigFileContract
	Hazards          []Hazard
	ValidationChecks []string

	// AcceptedCompatibility lists the compatibility modes this adapter's
	// SupportsProvider method accepts. Used by the wizard to explain why a
	// provider is incompatible ("Crush supports openai/local; provider has none").
	AcceptedCompatibility []provider.CompatibilityMode
}

// EnvContract describes a single env var requirement.
type EnvContract struct {
	Name        string
	Description string
	Required    bool
	Secret      bool
}

// ModelSlotContract describes a configurable model role.
type ModelSlotContract struct {
	Name        string
	Description string
	Default     string
	Optional    bool
}

// AdapterVerification records which test gates an adapter has passed.
// SupportConfidence may only be ConfidenceVerified when all required
// gates are true. This makes "verified" mean something auditable.
type AdapterVerification struct {
	RenderGolden    bool // golden file exists for Render output
	NoSecretLeak    bool // render/env output does not contain the raw secret
	ConfigMergeTest bool // merge policy does not destroy existing user config
	LaunchSmokeTest bool // adapter produces a launch-able strategy.
}

// Verified reports whether the adapter has passed every gate required
// to claim ConfidenceVerified status.
func (v AdapterVerification) Verified() bool {
	return v.RenderGolden && v.NoSecretLeak && v.ConfigMergeTest && v.LaunchSmokeTest
}

// ConfigFileContract describes a known config file location.
type ConfigFileContract struct {
	Path        string
	Format      string
	Description string
	Optional    bool
}

// Hazard describes a safety concern the user should know about.
type Hazard struct {
	Severity string // info, warn, high, critical
	Title    string
	Detail   string
	Fix      string
}

// ManualStep describes a guided user action that AegisKeys cannot automate.
type ManualStep struct {
	Title       string
	Description string
	When        string // before_launch, after_first_launch, optional
}

// --- Launch strategy ---

// LaunchStrategy wraps a launch plan with app-level metadata.
type LaunchStrategy struct {
	Plan        LaunchPlan
	Support     AppSupportContract
	ManualSteps []ManualStep
	Hazards     []Hazard
	Blocked     bool
	BlockReason string
}

// --- File-write safety ---

// MergePolicy controls how config files are written.
type MergePolicy string

const (
	MergeNone  MergePolicy = "none" // exact write, only for isolated profile dirs
	MergeJSON  MergePolicy = "json_merge"
	MergeJSONC MergePolicy = "jsonc_merge"
	MergeYAML  MergePolicy = "yaml_merge"
	MergeTOML  MergePolicy = "toml_merge"
	PatchXML   MergePolicy = "xml_patch"
	AvoidWrite MergePolicy = "avoid"
)

// BackupPolicy controls how config files are backed up before overwrite.
// Redacted/Encrypted prevent plaintext secret duplication in backup files.
type BackupPolicy string

const (
	BackupNone      BackupPolicy = "none"
	BackupPlain     BackupPolicy = "plain_0600" // only for ScopeProfile/ScopeTemp
	BackupRedacted  BackupPolicy = "redacted"   // default for user-scope
	BackupEncrypted BackupPolicy = "encrypted"  // sealed with vault key
)

// ConfigScope describes where a config file lives.
type ConfigScope string

const (
	ScopeUser    ConfigScope = "user"
	ScopeProject ConfigScope = "project"
	ScopeProfile ConfigScope = "profile"
	ScopeTemp    ConfigScope = "temp"
)

// FileWrite describes a config file to write with full safety semantics.
type FileWrite struct {
	Path         string
	Format       string
	Scope        ConfigScope
	Content      string
	MergePolicy  MergePolicy
	BackupPolicy BackupPolicy
	Mode         os.FileMode
	RedactCheck  bool
	Description  string

	// ManagedBlockID identifies which profile owns a managed config block. When
	// set, the merge marker incorporates it so profiles sharing a config file
	// (e.g. two profiles both targeting ~/.config/goose/config.yaml) get distinct
	// managed blocks instead of colliding. Empty falls back to the file basename.
	ManagedBlockID string
}

// --- Helpers ---

// safeName returns a filesystem-safe version of a profile name.
func safeName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
}

// expandPath expands ~ and selected env vars in a path. Only HOME, XDG_CONFIG_HOME,
// and TMPDIR are expanded from the launch plan's environment to prevent injection
// of arbitrary parent-process environment variables.
func expandPath(path string, env map[string]string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home := env["HOME"]
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", err
			}
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}

	return os.Expand(path, func(k string) string {
		switch k {
		case "HOME", "XDG_CONFIG_HOME", "TMPDIR":
			if v := env[k]; v != "" {
				return v
			}
			return os.Getenv(k)
		default:
			return ""
		}
	}), nil
}

// --- Contract validation ---

// ValidateContract checks that an AppSupportContract is fully and honestly
// declared. An adapter that claims a capability (e.g. CanPatchConfig) must
// also declare the corresponding data (ConfigFiles). This prevents the UI
// from pretending an adapter is safer than it actually is.
func ValidateContract(c AppSupportContract) error {
	if c.ID == "" {
		return fmt.Errorf("contract %q: missing ID", c.ID)
	}
	if c.DisplayName == "" {
		return fmt.Errorf("contract %q: missing DisplayName", c.ID)
	}
	if c.CredentialControl == "" {
		return fmt.Errorf("contract %q: missing CredentialControl", c.ID)
	}
	if !validCredentialControl(c.CredentialControl) {
		return fmt.Errorf("contract %q: invalid CredentialControl %q", c.ID, c.CredentialControl)
	}
	if c.SupportLevel == "" {
		return fmt.Errorf("contract %q: missing SupportLevel", c.ID)
	}
	if !validSupportLevel(c.SupportLevel) {
		return fmt.Errorf("contract %q: invalid SupportLevel %q", c.ID, c.SupportLevel)
	}
	if c.SupportConfidence == "" {
		return fmt.Errorf("contract %q: missing SupportConfidence", c.ID)
	}
	if !validSupportConfidence(c.SupportConfidence) {
		return fmt.Errorf("contract %q: invalid SupportConfidence %q", c.ID, c.SupportConfidence)
	}
	if len(c.LaunchSurfaces) == 0 {
		return fmt.Errorf("contract %q: no LaunchSurfaces declared", c.ID)
	}
	for _, s := range c.LaunchSurfaces {
		if !validLaunchSurface(s) {
			return fmt.Errorf("contract %q: invalid LaunchSurfaces value %q", c.ID, s)
		}
	}
	for _, m := range c.RenderModes {
		if !validRenderMode(m) {
			return fmt.Errorf("contract %q: invalid RenderModes value %q", c.ID, m)
		}
	}

	if c.CanPatchConfig && len(c.ConfigFiles) == 0 {
		return fmt.Errorf("contract %q: CanPatchConfig=true but ConfigFiles is empty", c.ID)
	}

	if c.CanManageModels && len(c.ModelSlots) == 0 {
		return fmt.Errorf("contract %q: CanManageModels=true but ModelSlots is empty", c.ID)
	}

	if c.CanLaunch && c.DefaultCommand == "" && !c.CanLaunchArbitraryCommand &&
		c.SupportLevel != SupportManualCredential &&
		c.SupportLevel != SupportConfigKeychain &&
		c.SupportLevel != SupportOAuthManual {
		return fmt.Errorf("contract %q: CanLaunch=true but DefaultCommand is empty", c.ID)
	}

	if !c.CanInjectSecrets && !c.RequiresManualStep && c.CredentialControl != CredentialExternalStore {
		return fmt.Errorf("contract %q: no safe credential path declared", c.ID)
	}

	if c.SupportConfidence == ConfidenceVerified {
		// Verified is a hard claim: the adapter must both declare its checks AND
		// have actually passed all verification gates (render golden, no secret
		// leak, config merge, launch smoke). This prevents "verified" from being
		// a documentation-only label with no test backing.
		if len(c.ValidationChecks) == 0 {
			return fmt.Errorf("contract %q: verified confidence requires ValidationChecks", c.ID)
		}
		if !c.Verification.Verified() {
			return fmt.Errorf("contract %q: verified confidence requires all verification gates to pass (RenderGolden, NoSecretLeak, ConfigMergeTest, LaunchSmokeTest)", c.ID)
		}
	}

	for _, h := range c.Hazards {
		switch h.Severity {
		case "info", "warn", "high", "critical":
		default:
			return fmt.Errorf("contract %q: invalid hazard severity %q", c.ID, h.Severity)
		}
		if h.Title == "" || h.Detail == "" {
			return fmt.Errorf("contract %q: hazard missing title/detail", c.ID)
		}
		if (h.Severity == "high" || h.Severity == "critical") && h.Fix == "" {
			return fmt.Errorf("contract %q: high/critical hazard requires fix", c.ID)
		}
	}

	return nil
}

func validSupportLevel(v SupportLevel) bool {
	switch v {
	case SupportFullEnv, SupportEnvConfig, SupportConfigKeychain, SupportLauncherIsolation,
		SupportManualCredential, SupportOAuthManual, SupportProxyMediated:
		return true
	default:
		return false
	}
}

func validCredentialControl(v CredentialControl) bool {
	switch v {
	case CredentialVaultInjected, CredentialEnvInjected, CredentialConfigPatched,
		CredentialKeychainHint, CredentialManualLogin, CredentialExternalStore:
		return true
	default:
		return false
	}
}

func validSupportConfidence(v SupportConfidence) bool {
	switch v {
	case ConfidenceVerified, ConfidenceManualProof, ConfidenceExperimental,
		ConfidenceGuided, ConfidenceBlocked:
		return true
	default:
		return false
	}
}

func validLaunchSurface(v string) bool {
	switch v {
	case "cli", "tui", "ide", "gui", "daemon", "extension":
		return true
	default:
		return false
	}
}

func validRenderMode(v string) bool {
	switch v {
	case "env", "args", "config_file", "manual", "proxy", "keychain", "keychain_handoff", "launcher":
		return true
	default:
		return false
	}
}

// RenderModeForContract returns the user-visible profile render mode. Secret
// injection through the child env is an implementation detail for
// config-patched apps; they should still present and persist as config-driven.
func RenderModeForContract(c AppSupportContract) profile.RenderMode {
	switch {
	case c.CanPatchConfig:
		return profile.RenderConfigFile
	case c.CanInjectSecrets:
		return profile.RenderEnv
	default:
		return profile.RenderEnv
	}
}

// ValidateAllContracts checks every registered adapter's contract.
// Returns a list of validation errors (one per broken contract).
func (r *Registry) ValidateAllContracts() []error {
	var errs []error
	for _, a := range r.All() {
		if err := ValidateContract(a.Contract()); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// --- Launch strategy enforcement ---

// SecurityPolicy controls what ValidateLaunchStrategy enforces.
type SecurityPolicy struct {
	ForbiddenProfileEnvPatterns []string
}

// DefaultSecurityPolicy returns the standard enforcement policy.
func DefaultSecurityPolicy() SecurityPolicy {
	return SecurityPolicy{
		ForbiddenProfileEnvPatterns: []string{
			"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL", "AUTH",
		},
	}
}

// ValidateLaunchStrategy is the hard boundary between adapter output and
// execution (ResolveRun mode). It enforces the adapter contract: blocked
// strategies cannot run, manual apps cannot receive raw secrets, and profile
// env cannot override provider secret vars. CLI and runner pass through this
// gate — no exceptions.
//
// Use ValidateLaunchStrategyForMode for preview/save intents.
func ValidateLaunchStrategy(
	strategy *LaunchStrategy,
	prof profile.Profile,
	prov provider.Provider,
	key *secret.SecretRecord,
	policy SecurityPolicy,
) error {
	return ValidateLaunchStrategyForMode(strategy, prof, prov, key, policy, ResolveRun)
}

// ValidateLaunchStrategyForMode enforces the adapter contract with
// intent-aware strictness for blocked strategies. Raw-secret-leak checks and
// contract honestedness apply in every mode; only the blocked-strategy
// rejection is deferred to Preview (which displays instead of rejecting).
func ValidateLaunchStrategyForMode(
	strategy *LaunchStrategy,
	prof profile.Profile,
	prov provider.Provider,
	key *secret.SecretRecord,
	policy SecurityPolicy,
	mode ResolveMode,
) error {
	if strategy == nil {
		return errors.New("nil launch strategy")
	}

	// Run and Save reject blocked strategies. Preview lets them through so the
	// UI can display hazards and manual steps instead of an error panel.
	if strategy.Blocked && mode != ResolvePreview {
		if strategy.BlockReason == "" {
			return errors.New("launch blocked by adapter contract")
		}
		return fmt.Errorf("launch blocked: %s", strategy.BlockReason)
	}

	c := strategy.Support

	if err := ValidateContract(c); err != nil {
		return fmt.Errorf("adapter %s has an invalid contract: %w", c.ID, err)
	}

	if !c.CanLaunch && strategy.Plan.Command != "" {
		return fmt.Errorf("adapter %s cannot launch but produced command %q", c.ID, strategy.Plan.Command)
	}

	if key != nil {
		if containsRawSecretInArgs(strategy.Plan.Args, key.Secret) {
			return errors.New("launch plan would expose raw secret in argv")
		}
		if containsRawSecretInPreview(strategy.Plan.Preview, key.Secret) {
			return errors.New("launch preview contains raw secret")
		}
		if containsRawSecretInFiles(strategy.Plan.Files, key.Secret) {
			return errors.New("launch plan would write raw secret to config file")
		}
		if !c.CanInjectSecrets && containsRawSecretInEnv(strategy.Plan.Env, key.Secret) {
			return fmt.Errorf("adapter %s cannot inject secrets but env contains raw secret", c.ID)
		}
	}

	if profileEnvOverridesSecret(prof, prov) {
		return fmt.Errorf("profile env cannot override provider secret env %s", prov.CanonicalEnvVar())
	}

	return nil
}

func containsRawSecret(s, rawSecret string) bool {
	if rawSecret == "" || s == "" {
		return false
	}
	return strings.Contains(s, rawSecret)
}

func containsRawSecretInArgs(args []string, rawSecret string) bool {
	for _, a := range args {
		if containsRawSecret(a, rawSecret) {
			return true
		}
	}
	return false
}

func containsRawSecretInPreview(preview []string, rawSecret string) bool {
	for _, p := range preview {
		if containsRawSecret(p, rawSecret) {
			return true
		}
	}
	return false
}

func containsRawSecretInEnv(env map[string]string, rawSecret string) bool {
	for _, v := range env {
		if containsRawSecret(v, rawSecret) {
			return true
		}
	}
	return false
}

// containsRawSecretInFiles checks if any file content would contain the raw secret.
func containsRawSecretInFiles(files []FileWrite, rawSecret string) bool {
	for _, f := range files {
		if containsRawSecret(f.Content, rawSecret) {
			return true
		}
	}
	return false
}

// profileEnvOverridesSecret checks if profile env attempts to override
// a provider secret var or set a credential-looking variable.
func profileEnvOverridesSecret(prof profile.Profile, prov provider.Provider) bool {
	secretName := prov.CanonicalEnvVar()
	for k := range prof.Env {
		if k == secretName {
			return true
		}
		if sensitive.IsAllowedNonSecretEnv(k) {
			continue
		}
		if sensitive.IsSecretName(k) {
			return true
		}
	}
	return false
}
