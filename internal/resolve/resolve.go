// Package resolve provides a single invariant for launch-profile validity:
// a saved active profile must either resolve into a safe launch strategy or
// explicitly declare itself manual/guidéd. Every path that creates or edits a
// profile must call ValidateResolution so broken adapters or missing keys never
// surface only at launch time.
package resolve

import (
	"fmt"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// ValidateResolution checks that a profile's target app adapter exists, its
// provider is registered, its key (if any) exists in the decrypted vault, the
// key's provider matches, and — for app-targeted profiles — the adapter can
// produce a launch strategy. Guided/manual profiles may save if the adapter can
// render an honest manual strategy; blocked strategies are rejected.
//
// This is the spine invariant from paste_2's "highest-impact additional fix":
// > A saved active profile must either resolve into a safe launch strategy or
// > explicitly declare itself manual/guided.
func ValidateResolution(
	p profile.Profile,
	providers *provider.Registry,
	vault *secret.Vault,
	adapters *adapter.Registry,
) error {
	// 1. Provider must exist.
	prov := providers.Find(p.ProviderSlug)
	if prov == nil {
		return fmt.Errorf("profile %q: provider %q not registered", p.Name, p.ProviderSlug)
	}

	// 2. Key must exist if one is referenced.
	if p.KeyID != "" {
		if vault == nil {
			return fmt.Errorf("profile %q: vault locked — cannot verify key %s", p.Name, p.KeyID)
		}
		key := vault.Get(p.KeyID)
		if key == nil {
			return fmt.Errorf("profile %q: key %q not found in vault", p.Name, p.KeyID)
		}
		if key.ProviderSlug != "" && key.ProviderSlug != prov.Slug {
			return fmt.Errorf("profile %q: key %q belongs to provider %q, profile uses %q", p.Name, p.KeyID, key.ProviderSlug, prov.Slug)
		}
	}

	// 3. The target adapter must resolve.
	appID := p.TargetApp()
	ad, ok := adapters.Get(appID)
	if !ok {
		return fmt.Errorf("profile %q: adapter for app %q not registered", p.Name, appID)
	}

	// 4. Strategy resolution (best-effort key for the call).
	var key *secret.SecretRecord
	if vault != nil && p.KeyID != "" {
		key = vault.Get(p.KeyID)
	}
	_, err := adapter.ResolveLaunchStrategyForMode(p, *prov, key, adapters, adapter.ResolveSave)
	if err != nil {
		return fmt.Errorf("profile %q: cannot resolve launch strategy: %w", p.Name, err)
	}
	_ = ad

	return nil
}
