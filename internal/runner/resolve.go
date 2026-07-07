package runner

import (
	"fmt"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// ResolvedEnv is the fully-resolved environment for a profile.
// EnvVars holds the literal KEY=secret pairs to inject into the child.
// Masked holds the same keys with values masked for safe display.
type ResolvedEnv struct {
	Profile  *profile.Profile
	Provider *provider.Provider
	EnvVars  map[string]string
}

// ResolveEnv builds the environment map that should be injected when
// running a command under the given profile.
//
// Resolution order:
//  1. The provider's ExtraEnv (e.g. OPENAI_BASE_URL for routers).
//  2. The provider's primary EnvVar set to the secret value (if any).
//  3. The profile's per-profile Env overrides (highest precedence — but
//     CANNOT override the provider credential env var).
//
// A provider with an empty EnvVar (e.g. Ollama) contributes no primary
// var; only its ExtraEnv applies.
func ResolveEnv(p *profile.Profile, prov *provider.Provider, secretValue string) (*ResolvedEnv, error) {
	if p == nil {
		return nil, fmt.Errorf("profile is nil")
	}
	if prov == nil {
		return nil, fmt.Errorf("provider is nil")
	}

	env := make(map[string]string)

	secretName := prov.CanonicalEnvVar()

	for k, v := range prov.ExtraEnv {
		env[k] = v
	}
	if secretName != "" {
		env[secretName] = secretValue
	}
	for k, v := range p.Env {
		if k == secretName {
			return nil, fmt.Errorf("profile env may not override provider credential env %q (vault is authoritative)", k)
		}
		if looksSecretName(k) {
			return nil, fmt.Errorf("profile env %q looks credential-like; store it as a key", k)
		}
		env[k] = v
	}
	return &ResolvedEnv{
		Profile:  p,
		Provider: prov,
		EnvVars:  env,
	}, nil
}

// ResolveLaunchPlan builds a full launch plan using the adapter system.
// It combines the base env resolution with app-specific rendering.
func ResolveLaunchPlan(
	p *profile.Profile,
	prov *provider.Provider,
	key *secret.SecretRecord,
	registry *adapter.Registry,
) (*adapter.LaunchPlan, error) {
	return adapter.ResolveLaunchPlan(*p, *prov, key, registry)
}

// ResolveRunConfig builds a RunConfig from a profile, provider, and key.
// This is the high-level entry point for launching a profile.
// It uses the full LaunchStrategy (not just LaunchPlan) so that contract
// metadata like Blocked flows through to the runner.
func ResolveRunConfig(
	p *profile.Profile,
	prov *provider.Provider,
	key *secret.SecretRecord,
	registry *adapter.Registry,
) (*RunConfig, []adapter.FileWrite, error) {
	strategy, err := adapter.ResolveLaunchStrategy(*p, *prov, key, registry)
	if err != nil {
		return nil, nil, err
	}
	// Validate through the central gate.
	if err := adapter.ValidateLaunchStrategy(strategy, *p, *prov, key, adapter.DefaultSecurityPolicy()); err != nil {
		return nil, nil, err
	}
	return &RunConfig{
		ProfileName: p.Name,
		Command:     strategy.Plan.Command,
		Args:        strategy.Plan.Args,
		ExtraEnv:    strategy.Plan.Env,
		Blocked:     strategy.Blocked,
		BlockReason: strategy.BlockReason,
	}, strategy.Plan.Files, nil
}

// Non-secret values (URLs, base URLs) are left intact; only the injected
// primary API key and any profile env values are masked.
func (r *ResolvedEnv) Masked() map[string]string {
	secretKeys := map[string]bool{}
	if r.Provider != nil && r.Provider.CanonicalEnvVar() != "" {
		secretKeys[r.Provider.CanonicalEnvVar()] = true
	}
	for k := range r.Profile.Env {
		secretKeys[k] = true
	}

	out := make(map[string]string, len(r.EnvVars))
	for k, v := range r.EnvVars {
		if secretKeys[k] {
			out[k] = maskForDisplay(v)
		} else {
			out[k] = v
		}
	}
	return out
}

// maskForDisplay returns a masked preview of a secret for safe display.
// Reuses secret.MaskSecret semantics without importing the secret package
// (avoids a cycle when runner is used by packages that also import secret).
func maskForDisplay(s string) string {
	if len(s) == 0 || len(s) <= 8 {
		return "<hidden>"
	}
	return fmt.Sprintf("%s...%s", s[:4], s[len(s)-4:])
}
