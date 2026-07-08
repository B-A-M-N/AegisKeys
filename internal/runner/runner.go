package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/audit"
	"aegiskeys/internal/config"
	"aegiskeys/internal/proxy"
	"aegiskeys/internal/sensitive"
)

type RunConfig struct {
	ProfileName string
	Command     string
	Args        []string
	ExtraEnv    map[string]string
	InheritEnv  bool          // if true, inherit full parent env (less safe)
	Proxies     []proxy.Proxy // optional proxies to ensure running before launch
	AppClass    string        // cli, gui, ide — determines base env allowlist

	// Blocked indicates the launch was blocked by the adapter contract.
	// The runner MUST refuse execution when Blocked is true.
	Blocked     bool
	BlockReason string
}

// proxyManager handles on-demand proxy lifecycle for Run.
var proxyManager = proxy.NewManager(os.TempDir() + "/aegiskeys")

// safeBaseEnv is the allowlist of env vars that are safe to pass to child
// processes. Everything else from the parent shell is stripped to prevent
// unrelated secrets from leaking into the child.
// Note: SSH_AUTH_SOCK is intentionally excluded — coding agents can use it
// to access private repos, but it is credential-adjacent and should be
// opt-in via ExtraEnv if needed.
var safeBaseEnv = map[string]bool{
	"PATH":            true,
	"HOME":            true,
	"USER":            true,
	"SHELL":           true,
	"TERM":            true,
	"COLORTERM":       true,
	"LANG":            true,
	"LC_ALL":          true,
	"LC_CTYPE":        true,
	"PWD":             true,
	"XDG_RUNTIME_DIR": true,
}

// guiSafeEnv contains additional env vars needed for GUI/IDE apps launched
// from AegisKeys. GUI apps (Zed, IntelliJ, VS Code, Cursor) need display and
// session bus vars that the CLI-safe allowlist strips.
var guiSafeEnv = map[string]bool{
	"DISPLAY":                  true,
	"WAYLAND_DISPLAY":          true,
	"XAUTHORITY":               true,
	"DBUS_SESSION_BUS_ADDRESS": true,
	"XDG_CURRENT_DESKTOP":      true,
	"DESKTOP_SESSION":          true,
	"GDK_BACKEND":              true,
	"QT_QPA_PLATFORM":          true,
}

// appClassFromStrategy derives the app class from the adapter contract's
// LaunchSurfaces so the correct env allowlist is applied. If any surface is
// "gui" or "ide", the expanded GUI env allowlist is used; otherwise the
// minimal CLI allowlist applies.
func appClassFromStrategy(strategy *adapter.LaunchStrategy) string {
	for _, s := range strategy.Support.LaunchSurfaces {
		if s == "gui" || s == "ide" {
			return s
		}
	}
	return "cli"
}

// baseEnvForClass returns the safe base env allowlist for an app class.
// GUI/IDE apps need extra display/session vars; CLI apps use the minimal list.
func baseEnvForClass(class string) map[string]bool {
	if class == "gui" || class == "ide" {
		// Merge common + GUI-safe.
		merged := make(map[string]bool, len(safeBaseEnv)+len(guiSafeEnv))
		for k := range safeBaseEnv {
			merged[k] = true
		}
		for k := range guiSafeEnv {
			merged[k] = true
		}
		return merged
	}
	return safeBaseEnv
}

// ExitError is returned by Run when the child process exits with a non-zero status.
// The CLI layer decides whether to call os.Exit based on Code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	return "exited with code " + itoa(e.Code) + ": " + e.Err.Error()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		b[i] = '-'
	}
	return string(b[i:])
}

func looksSecretName(k string) bool {
	if sensitive.IsAllowedNonSecretEnv(k) {
		return false
	}
	return sensitive.IsSecretName(k)
}

// SecurityPolicy defines the final safety gate between adapter render and
// child execution. ValidateRunConfig enforces it before exec.Command.
type SecurityPolicy struct {
	Blocked         bool
	BlockReason     string
	ForbiddenEnv    map[string]string // env vars the runner must not inject
	AllowInheritEnv bool
	RequireConfirm  bool // require explicit user confirmation
}

// ValidateRunConfig enforces the security policy before exec.Command.
// It is the last safety gate: even if an adapter produced something unsafe,
// the runner refuses to execute it.
func ValidateRunConfig(cfg RunConfig, policy SecurityPolicy) error {
	if cfg.Command == "" {
		return errors.New("empty command")
	}
	if policy.Blocked {
		return fmt.Errorf("launch blocked: %s", policy.BlockReason)
	}
	for k := range cfg.ExtraEnv {
		if _, forbidden := policy.ForbiddenEnv[k]; forbidden {
			return fmt.Errorf("env var %q is forbidden by launch policy", k)
		}
	}
	return nil
}

// cleanBaseEnv filters the parent env to only safe, non-secret vars.
func cleanBaseEnv(base []string) []string {
	return cleanBaseEnvWithAllowlist(base, safeBaseEnv)
}

// cleanBaseEnvWithAllowlist filters the parent env to only vars in the given
// allowlist, excluding anything that looks secret. The allowlist can be the
// minimal CLI list (safeBaseEnv) or the expanded GUI list (baseEnvForClass("gui")).
func cleanBaseEnvWithAllowlist(base []string, allowlist map[string]bool) []string {
	out := []string{}
	for _, kv := range base {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if !allowlist[k] {
			continue
		}
		if looksSecretName(k) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// RunLegacy is the legacy entry point using a RunConfig struct.
//
// Deprecated: use Run(ctx, strategy, opts) instead. This exists so existing
// callers have a migration path.
func RunLegacy(cfg RunConfig) error {
	if err := ValidateRunConfig(cfg, SecurityPolicy{
		AllowInheritEnv: cfg.InheritEnv,
	}); err != nil {
		return err
	}

	if cfg.Blocked {
		if cfg.BlockReason != "" {
			return fmt.Errorf("launch blocked: %s", cfg.BlockReason)
		}
		return errors.New("launch blocked by adapter contract")
	}

	for i := range cfg.Proxies {
		if _, err := proxyManager.EnsureRunning(cfg.Proxies[i]); err != nil {
			return fmt.Errorf("proxy %s: %w", cfg.Proxies[i].Name, err)
		}
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	allowlist := baseEnvForClass(cfg.AppClass)
	if cfg.InheritEnv {
		cmd.Env = mergedEnv(cleanInheritedEnv(os.Environ()), cfg.ExtraEnv)
	} else {
		cmd.Env = mergedEnv(cleanBaseEnvWithAllowlist(os.Environ(), allowlist), cfg.ExtraEnv)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return &ExitError{Code: status.ExitStatus(), Err: err}
			}
		}
		return err
	}
	return nil
}

// cleanInheritedEnv filters parent env to only safe, non-secret vars.
// Like cleanBaseEnv but checks ALL vars, not just safeBaseEnv allowlist.
func cleanInheritedEnv(base []string) []string {
	out := []string{}
	for _, kv := range base {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if looksSecretName(k) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// EnsureRunning checks and starts a proxy if needed. Exported for TUI use.
func EnsureRunning(p proxy.Proxy) (string, error) {
	return proxyManager.EnsureRunning(p)
}

// mergedEnv overlays extra on base without producing duplicate keys.
func mergedEnv(base []string, overlay map[string]string) []string {
	env := make(map[string]string, len(base)+len(overlay))
	for _, kv := range base {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[k] = v
	}
	for k, v := range overlay {
		env[k] = v
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// CredentialVar is a set of well-known env var names that carry secrets.
// These are blocked from the parent shell to prevent accidental shadowing
// of AegisKeys-injected credentials.
var CredentialVar = map[string]bool{
	"OPENAI_API_KEY":     true,
	"ANTHROPIC_API_KEY":  true,
	"GOOGLE_API_KEY":     true,
	"GEMINI_API_KEY":     true,
	"OPENROUTER_API_KEY": true,
	"MISTRAL_API_KEY":    true,
	"COHERE_API_KEY":     true,
	"GROQ_API_KEY":       true,
	"TOGETHER_API_KEY":   true,
	"FIREWORKS_API_KEY":  true,
	"DEEPSEEK_API_KEY":   true,
	"XAI_API_KEY":        true,
	"MOONSHOT_API_KEY":   true,
	"QWEN_API_KEY":       true,
	"GITHUB_TOKEN":       true,
	"GH_TOKEN":           true,
	"HF_TOKEN":           true,
	"HUGGINGFACE_TOKEN":  true,
}

// RunOptions configures a strategy-driven launch.
type RunOptions struct {
	ProfileName    string
	ConfigDir      string
	WorkingDir     string
	ExtraArgs      []string
	DryRun         bool
	ExactSecrets   []string
	InheritStdio   bool
	CleanupEnvFile bool
}

// PreparedCommand is a launch command plus cleanup work that must run after
// the child process exits.
type PreparedCommand struct {
	Cmd     *exec.Cmd
	Cleanup func() error
}

// PrepareCommand validates and materializes a launch strategy, then returns the
// exec.Cmd that should run in the caller's terminal context. CLI launches call
// Run, while TUI launches use this with tea.ExecProcess so Bubble Tea can
// release and restore the terminal around the child process.
func PrepareCommand(ctx context.Context, strategy *adapter.LaunchStrategy, opts RunOptions) (*exec.Cmd, error) {
	prepared, err := PrepareCommandWithCleanup(ctx, strategy, opts)
	if err != nil {
		return nil, err
	}
	return prepared.Cmd, nil
}

// PrepareCommandWithCleanup validates and materializes a launch strategy, then
// returns the command plus a restore hook for runtime config file overlays.
func PrepareCommandWithCleanup(ctx context.Context, strategy *adapter.LaunchStrategy, opts RunOptions) (*PreparedCommand, error) {
	if strategy == nil {
		return nil, errors.New("nil launch strategy")
	}
	if strategy.Blocked {
		if strategy.BlockReason != "" {
			return nil, fmt.Errorf("launch blocked: %s", strategy.BlockReason)
		}
		return nil, errors.New("launch blocked by adapter contract")
	}
	if strategy.Plan.Command == "" {
		return nil, errors.New("launch command is empty")
	}
	if !strategy.Support.CanLaunch && !strategy.Support.CanLaunchArbitraryCommand {
		return nil, fmt.Errorf("adapter %s cannot launch directly", strategy.Support.ID)
	}
	if _, err := exec.LookPath(strategy.Plan.Command); err != nil {
		return nil, fmt.Errorf("launch command %q not found on PATH", strategy.Plan.Command)
	}

	// Apply config file writes before launching the child.
	cleanup := func() error { return nil }
	if len(strategy.Plan.Files) > 0 {
		restore, err := adapter.ApplyFileWritesWithRestore(strategy.Plan.Files, strategy.Plan.Env)
		if err != nil {
			return nil, fmt.Errorf("apply file writes: %w", err)
		}
		cleanup = restore
	}

	// Audit: launch_start (metadata only).
	if opts.ConfigDir != "" {
		audit.NewLogger(config.AuditPath(opts.ConfigDir)).Log(audit.Event{
			Event:    "child_command_launched",
			Profile:  opts.ProfileName,
			Provider: strategy.Support.ID,
			Command:  strategy.Plan.Command,
		})
	}

	cmd := exec.CommandContext(ctx, strategy.Plan.Command, append(strategy.Plan.Args, opts.ExtraArgs...)...)
	allowlist := baseEnvForClass(appClassFromStrategy(strategy))
	cmd.Env = BuildChildEnv(cleanBaseEnvWithAllowlist(os.Environ(), allowlist), strategy.Plan.Env)
	if opts.WorkingDir != "" {
		cmd.Dir = opts.WorkingDir
	}
	if opts.InheritStdio {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return &PreparedCommand{Cmd: cmd, Cleanup: cleanup}, nil
}

// RunWithStrategy is the single entry point for strategy-driven CLI launches.
// TUI launch flows call PrepareCommandWithCleanup and pass the command to
// tea.ExecProcess so the terminal is released/restored correctly while still
// running the same runtime config cleanup hook.
//
// Invariants:
//   - Refuses execution if strategy.Blocked, Plan.Command is empty, or
//     Support.CanLaunch is false.
//   - Parent-shell credential vars are stripped so they cannot shadow the
//     profile's injected secrets.
//   - File writes are applied before the child starts.
//   - All audit events carry metadata only — never raw secrets.
func Run(ctx context.Context, strategy *adapter.LaunchStrategy, opts RunOptions) error {
	prepared, err := PrepareCommandWithCleanup(ctx, strategy, opts)
	if err != nil {
		return err
	}

	if opts.DryRun {
		return prepared.Cleanup()
	}

	if err := prepared.Cmd.Run(); err != nil {
		cleanupErr := prepared.Cleanup()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if cleanupErr != nil {
					return fmt.Errorf("%w; cleanup failed: %v", &ExitError{Code: status.ExitStatus(), Err: err}, cleanupErr)
				}
				return &ExitError{Code: status.ExitStatus(), Err: err}
			}
		}
		if cleanupErr != nil {
			return fmt.Errorf("%w; cleanup failed: %v", err, cleanupErr)
		}
		return err
	}
	return prepared.Cleanup()
}

// BuildChildEnv constructs the child process environment by starting from
// the parent env, removing any credential-carrying vars that would shadow
// AegisKeys-injected secrets, then overlaying the launch plan's env.
//
// This guarantees profile env wins and parent shell secrets do not leak in.
func BuildChildEnv(parent []string, injected map[string]string) []string {
	blocked := make(map[string]bool, len(CredentialVar)+len(injected))
	for k := range CredentialVar {
		blocked[k] = true
	}
	for k := range injected {
		blocked[k] = true
	}

	out := make([]string, 0, len(parent)+len(injected))
	for _, kv := range parent {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if blocked[k] {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range injected {
		if v == "" {
			// Empty value means "unset" — already blocked from parent above
			continue
		}
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
