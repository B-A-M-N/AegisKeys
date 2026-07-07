// Package adapter renders a profile into a launch plan for a specific
// coding application. Each adapter knows how its target app expects
// credentials and model configuration: env vars, CLI args, config files.
package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// LaunchPlan is the fully-resolved launch configuration for a profile.
type LaunchPlan struct {
	Command string
	Args    []string
	Env     map[string]string
	Files   []FileWrite
	Preview []string // human-readable summary lines

	Warnings []string
	Hazards  []Hazard
}

// AppAdapter renders a profile into a launch strategy for one application type.
type AppAdapter interface {
	// ID returns the adapter's app identifier (e.g. "crush", "aider").
	ID() string

	// DisplayName returns the human-readable app name.
	DisplayName() string

	// SupportsProvider reports whether this adapter can render config
	// for the given provider compatibility mode.
	SupportsProvider(p provider.Provider) bool

	// CanInjectCredential reports whether AegisKeys can directly inject
	// the credential for this provider via env.
	CanInjectCredential(p provider.Provider) bool

	// CanConfigureProvider reports whether AegisKeys can configure
	// provider/model routing.
	CanConfigureProvider(p provider.Provider) bool

	// Contract returns the app support contract.
	Contract() AppSupportContract

	// Render builds the launch strategy for the given profile, provider, and key.
	Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error)

	// Validate checks the profile for app-specific issues before launch.
	// Returns warnings (non-fatal) and errors (fatal).
	Validate(p profile.Profile, prov provider.Provider) (warnings []string, err error)

	// DefaultCommand returns the default binary name for this app.
	DefaultCommand() string
}

// Registry holds all known adapters.
type Registry struct {
	adapters map[string]AppAdapter
	order    []string
}

// NewRegistry creates an adapter registry with all built-in adapters.
func NewRegistry() *Registry {
	r := &Registry{
		adapters: make(map[string]AppAdapter),
		order: []string{
			"generic", "crush", "aider", "cline", "hermes", "qwen", "claude", "vibe", "goose",
			"codex", "mimo", "openhands", "gemini", "copilot", "continue",
			"zed", "intellij",
			"roo", "kilo", "cursor",
		},
	}
	for _, a := range []AppAdapter{
		GenericOpenAIAdapter{},
		CrushAdapter{},
		AiderAdapter{},
		ClineAdapter{},
		HermesAdapter{},
		QwenCodeAdapter{},
		ClaudeCodeAdapter{},
		MistralVibeAdapter{},
		GooseAdapter{},
		CodexAdapter{},
		MiMoOpenCodeAdapter{},
		OpenHandsAdapter{},
		GeminiCLIAdapter{},
		CopilotCLIAdapter{},
		ContinueAdapter{},
		ZedAdapter{},
		IntelliJAdapter{},
		RooCodeAdapter{},
		KiloCodeAdapter{},
		CursorAdapter{},
	} {
		r.adapters[a.ID()] = a
	}
	return r
}

// Get returns the adapter by ID.
func (r *Registry) Get(id string) (AppAdapter, bool) {
	a, ok := r.adapters[id]
	return a, ok
}

// ForProvider returns the best adapter for a provider compatibility mode.
func (r *Registry) ForProvider(p provider.Provider) AppAdapter {
	// Try to find a specific adapter for the provider's compatibility.
	switch p.Compatibility {
	case provider.CompatAnthropic:
		if a, ok := r.adapters["claude"]; ok {
			return a
		}
	case provider.CompatOpenAI:
		if a, ok := r.adapters["generic"]; ok {
			return a
		}
	}
	// Fall back to generic.
	if a, ok := r.adapters["generic"]; ok {
		return a
	}
	// Last resort: first registered.
	for _, id := range r.order {
		if a, ok := r.adapters[id]; ok {
			return a
		}
	}
	return GenericOpenAIAdapter{}
}

// DemotedConfidence returns the confidence level that is actually justified
// by an adapter's verification state. If an adapter claims ConfidenceVerified
// but its Verification.Verified() is false, this returns ConfidenceExperimental
// so downstream consumers (UI, doctor, wizard) never show false confidence.
func DemotedConfidence(c AppSupportContract) SupportConfidence {
	if c.SupportConfidence == ConfidenceVerified && !c.Verification.Verified() {
		return ConfidenceExperimental
	}
	return c.SupportConfidence
}

// ResolveMode selects what the resolver is allowed to return and how strict
// its checks are. The same profile resolves differently depending on intent:
//
//	ResolvePreview — TUI/launch screen. May return blocked/manual strategies so
//	                the UI can show hazards and manual steps. Never exposes raw
//	                secrets. Never errors on a blocked strategy.
//	ResolveSave    — profile create/save. Must prove provider/key/profile/app
//	                references resolve. Allows guided/manual apps (they save).
//	ResolveRun     — actual child-process launch. The hard gate: rejects blocked
//	                and manual-credential strategies, enforces secret policy.
type ResolveMode int

const (
	// ResolvePreview returns blocked/manual strategies for display. Never errors
	// on Blocked and never surfaces raw secrets.
	ResolvePreview ResolveMode = iota
	// ResolveSave validates references but allows guided/manual strategies.
	ResolveSave
	// ResolveRun is the hard launch gate: rejects blocked + manual-credential.
	ResolveRun
)

// All returns all adapters in registration order.
func (r *Registry) All() []AppAdapter {
	out := make([]AppAdapter, 0, len(r.order))
	for _, id := range r.order {
		if a, ok := r.adapters[id]; ok {
			out = append(out, a)
		}
	}
	return out
}

// AllIDs returns all registered adapter IDs in registration order.
func (r *Registry) AllIDs() []string {
	return append([]string{}, r.order...)
}

// NewRegistryWithForTest creates a registry with an additional test adapter registered.
func NewRegistryWithForTest(adapters ...AppAdapter) *Registry {
	r := NewRegistry()
	for _, a := range adapters {
		r.adapters[a.ID()] = a
		r.order = append(r.order, a.ID())
	}
	return r
}

// ResolveLaunchPlan builds the full launch plan for a profile.
// Returns the launch plan from the strategy (for backward compatibility
// with the runner), while preserving strategy metadata for callers
// that need warnings/hazards.
func ResolveLaunchPlan(
	p profile.Profile,
	prov provider.Provider,
	key *secret.SecretRecord,
	registry *Registry,
) (*LaunchPlan, error) {
	strategy, err := ResolveLaunchStrategy(p, prov, key, registry)
	if err != nil {
		return nil, err
	}
	return &strategy.Plan, nil
}

// ResolveLaunchStrategy builds the full launch strategy for a profile using
// ResolveRun mode — the hard launch gate. This is the backward-compatible
// signature for callers that intend to actually launch (CLI run, runner).
//
// Use ResolveLaunchStrategyForMode when the intent is preview or save.
func ResolveLaunchStrategy(
	p profile.Profile,
	prov provider.Provider,
	key *secret.SecretRecord,
	registry *Registry,
) (*LaunchStrategy, error) {
	return ResolveLaunchStrategyForMode(p, prov, key, registry, ResolveRun)
}

// ResolveLaunchStrategyForMode builds the launch strategy with intent-aware
// strictness. See ResolveMode for the semantics of each mode.
func ResolveLaunchStrategyForMode(
	p profile.Profile,
	prov provider.Provider,
	key *secret.SecretRecord,
	registry *Registry,
	mode ResolveMode,
) (*LaunchStrategy, error) {
	// Validate key/provider binding when a key is present.
	if key != nil && key.ProviderSlug != "" && key.ProviderSlug != prov.Slug {
		return nil, fmt.Errorf("key %s belongs to provider %s, not %s", key.ID, key.ProviderSlug, prov.Slug)
	}
	// Require key only when the provider needs one.
	if prov.NeedsKey() && key == nil {
		return nil, fmt.Errorf("provider %s requires a key for profile %s", prov.Slug, p.Name)
	}
	// A provider that needs auth but has no credential env var defined is
	// malformed: it can never inject a secret. Fail closed rather than letting
	// it slide through as if it were keyless.
	if prov.NeedsKey() && prov.CanonicalEnvVar() == "" {
		return nil, fmt.Errorf("provider %s needs auth but has no credential env var", prov.Slug)
	}

	adapter, ok := registry.Get(p.TargetApp())
	if !ok {
		// Fail closed: unknown target app is rejected, not silently
		// fallback to a provider-derived adapter. A typo like "qwen-code"
		// instead of "qwen" must not silently change the injection surface.
		return nil, fmt.Errorf("unknown target app %q", p.TargetApp())
	}

	if !adapter.SupportsProvider(prov) {
		return nil, fmt.Errorf("adapter %s does not support provider %s", adapter.ID(), prov.Slug)
	}

	// C2-plus: do not pass raw secret to adapters that cannot inject.
	// Manual/keychain-adjacent adapters must never see the key material.
	contract := adapter.Contract()
	renderKey := key
	if !contract.CanInjectSecrets {
		renderKey = nil
	}

	strategy, err := adapter.Render(p, prov, renderKey)
	if err != nil {
		return nil, err
	}

	// Enforce the adapter contract after render.
	// Call Validate before trusting the render output.
	if valWarnings, valErr := adapter.Validate(p, prov); valErr != nil {
		return nil, fmt.Errorf("profile validation failed for %s: %w", p.Name, valErr)
	} else {
		strategy.Plan.Warnings = append(strategy.Plan.Warnings, valWarnings...)
	}

	// For apps that cannot receive secrets, strip all secret env vars.
	if !contract.CanInjectSecrets {
		secretKeys := secretEnvKeys(prov, contract)
		for k := range secretKeys {
			delete(strategy.Plan.Env, k)
		}
	}

	// Apply profile-level env overrides (highest precedence — but NEVER for credential vars).
	secretName := prov.CanonicalEnvVar()
	for k, v := range p.Env {
		if k == secretName {
			return nil, fmt.Errorf("profile env may not override provider credential env %q", k)
		}
		if looksSecretName(k) {
			return nil, fmt.Errorf("profile env %q looks credential-like; store it as a key", k)
		}
		strategy.Plan.Env[k] = v
	}

	// Merge support contract metadata (always, not conditionally).
	strategy.Support = contract
	strategy.Hazards = append(strategy.Hazards, contract.Hazards...)
	strategy.Hazards = dedupeHazards(strategy.Hazards)

	// Apply profile-level args once (adapters must NOT append p.Args themselves).
	if len(p.Args) > 0 {
		strategy.Plan.Args = append(strategy.Plan.Args, p.Args...)
	}

	// Hard boundary: the single contract gate. Anything that launches, previews,
	// writes config, or produces a run config MUST go through this validator.
	// Preview/Save modes pass through so they too get raw-secret-leak and
	// contract checks; only the blocked-strategy rejection is mode-dependent.
	if err := ValidateLaunchStrategyForMode(strategy, p, prov, key, DefaultSecurityPolicy(), mode); err != nil {
		return nil, err
	}

	return strategy, nil
}

func dedupeHazards(hazards []Hazard) []Hazard {
	if len(hazards) < 2 {
		return hazards
	}
	seen := make(map[string]bool, len(hazards))
	out := make([]Hazard, 0, len(hazards))
	for _, h := range hazards {
		key := h.Severity + "\x00" + h.Title
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, h)
	}
	return out
}

// secretEnvKeys returns the set of env var names that carry secrets for
// a given provider + contract combination.
func secretEnvKeys(prov provider.Provider, contract AppSupportContract) map[string]bool {
	keys := map[string]bool{}
	if prov.CanonicalEnvVar() != "" {
		keys[prov.CanonicalEnvVar()] = true
	}
	for _, e := range contract.RequiredEnv {
		if e.Secret {
			keys[e.Name] = true
		}
	}
	return keys
}

// modelEnvVar returns the conventional model env var name for a provider.
func modelEnvVar(p provider.Provider) string {
	switch p.Compatibility {
	case provider.CompatAnthropic:
		return "ANTHROPIC_MODEL"
	case provider.CompatGoogle:
		return "GOOGLE_MODEL"
	default:
		return "OPENAI_MODEL"
	}
}

// modelArgFlag returns the CLI flag for specifying a model.
func modelArgFlag(adapterID string) []string {
	switch adapterID {
	case "aider":
		return []string{"--model"}
	case "cline":
		return []string{"--model"}
	case "hermes":
		return []string{"--model"}
	case "qwen":
		return []string{"--model"}
	default:
		return nil
	}
}

// writeEnvFile writes a temporary env file and returns its path.
// The file is written under configDir/tmp/envfiles so that the doctor's
// stale-temp-env scan (which watches configDir/tmp) can detect it, and
// so permissions/cleanup stay predictable under the app-controlled tree.
func writeEnvFile(env map[string]string, profileName, configDir string) (*FileWrite, error) {
	var sb strings.Builder
	for k, v := range env {
		fmt.Fprintf(&sb, "%s=%s\n", k, v)
	}
	dir := filepath.Join(configDir, "tmp", "envfiles")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, profileName+".env")
	return &FileWrite{
		Path:        path,
		Format:      "env",
		Content:     sb.String(),
		Scope:       ScopeTemp,
		MergePolicy: MergeNone,
		Description: "Temporary env file for child-process injection",
	}, nil
}
