// Catalog-based launch resolution for multi-provider apps.

package adapter

import (
	"fmt"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// buildCatalogProviders builds the list of catalog providers from the provider
// registry and vault. It includes every provider the adapter supports, then
// separately records launch-injectable keys for providers that have one.
//
// Selection logic:
//   - Include every compatible provider in the rendered catalog
//   - Include local/no-auth providers without a key
//   - Inject env secrets only for providers that have a launch-enabled key
func buildCatalogProviders(
	adapter AppAdapter,
	registry *provider.Registry,
	vault *secret.Vault,
) ([]provider.Provider, map[string]*secret.SecretRecord, error) {
	var providers []provider.Provider
	keysByProvider := make(map[string]*secret.SecretRecord)

	for _, prov := range registry.Providers {
		// Normalize to ensure endpoints/base URL are populated.
		prov.Normalize()

		// Skip providers the adapter doesn't support.
		if !adapter.SupportsProvider(prov) {
			continue
		}

		providers = append(providers, prov)

		if !prov.NeedsKey() {
			continue
		}

		// Find a usable key for this provider by scanning the vault.
		var found *secret.SecretRecord
		for i := range vault.Keys {
			if vault.Keys[i].ProviderSlug == prov.Slug {
				// Verify the key allows launch injection.
				if err := vault.Keys[i].AllowAccess(secret.AccessInjectEnv); err == nil {
					found = &vault.Keys[i]
					break
				}
			}
		}
		if found != nil {
			keysByProvider[prov.Slug] = found
		}
	}

	return providers, keysByProvider, nil
}

// ResolveLaunchStrategyCatalog builds a launch strategy for adapters that
// support the ProviderCatalogAdapter interface. It builds a full provider
// catalog from the registry and vault, then delegates to RenderCatalog.
//
// If the adapter does NOT implement ProviderCatalogAdapter, it falls back
// to the standard single-provider ResolveLaunchStrategyForMode.
func ResolveLaunchStrategyCatalog(
	p profile.Profile,
	prov provider.Provider,
	key *secret.SecretRecord,
	registry *Registry,
	providerRegistry *provider.Registry,
	vault *secret.Vault,
	mode ResolveMode,
) (*LaunchStrategy, error) {
	adapter, ok := registry.Get(p.TargetApp())
	if !ok {
		return nil, fmt.Errorf("unknown target app %q", p.TargetApp())
	}

	// If the adapter supports catalog mode, build the catalog and use it.
	if catalogAdapter, ok := adapter.(ProviderCatalogAdapter); ok {
		catalogProviders, keysByProvider, err := buildCatalogProviders(adapter, providerRegistry, vault)
		if err != nil {
			return nil, fmt.Errorf("build catalog: %w", err)
		}

		ctx := ProviderCatalogRenderContext{
			Profile:          p,
			SelectedProvider: prov,
			SelectedKey:      key,
			Providers:        catalogProviders,
			KeysByProvider:   keysByProvider,
		}

		strategy, err := catalogAdapter.RenderCatalog(ctx)
		if err != nil {
			return nil, err
		}

		// Apply the same post-render validation and contract enforcement.
		if valWarnings, valErr := adapter.Validate(p, prov); valErr != nil {
			return nil, fmt.Errorf("profile validation failed for %s: %w", p.Name, valErr)
		} else {
			strategy.Plan.Warnings = append(strategy.Plan.Warnings, valWarnings...)
		}

		// For apps that cannot receive secrets, strip all secret env vars.
		contract := adapter.Contract()
		if !contract.CanInjectSecrets {
			secretKeys := secretEnvKeys(prov, contract)
			for k := range secretKeys {
				delete(strategy.Plan.Env, k)
			}
		}

		// Profile-level env overrides (highest precedence, non-secret only).
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

		strategy.Support = contract
		strategy.Hazards = append(strategy.Hazards, contract.Hazards...)
		strategy.Hazards = dedupeHazards(strategy.Hazards)

		if len(p.Args) > 0 {
			strategy.Plan.Args = append(strategy.Plan.Args, p.Args...)
		}

		if err := ValidateLaunchStrategyForMode(strategy, p, prov, key, DefaultSecurityPolicy(), mode); err != nil {
			return nil, err
		}

		return strategy, nil
	}

	// Fall back to single-provider mode.
	return ResolveLaunchStrategyForMode(p, prov, key, registry, mode)
}
