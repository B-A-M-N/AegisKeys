package provider

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"aegiskeys/internal/fsutil"
)

func (r *Registry) Save(path string) error {
	data, err := r.Serialize()
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteFile(path, data)
}

// Serialize returns the JSON representation of the registry (providers are
// non-secret metadata, so this is safe to print/export).
func (r *Registry) Serialize() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return NewRegistry(), err
	}
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	// Normalize every provider so flat-only entries (e.g. hand-edited JSON
	// or older TUI-created providers) get their structured fields and
	// compatibility derived before any adapter filter sees them.
	r.NormalizeAll()
	return &r, nil
}

func (r *Registry) Add(p Provider) error {
	p.Normalize()
	if err := p.ValidateStrict(); err != nil {
		return err
	}
	for _, existing := range r.Providers {
		if existing.Slug == p.Slug {
			return errors.New("provider slug must be unique")
		}
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	r.Providers = append(r.Providers, p)
	return nil
}

// Update replaces an existing provider, enforcing uniqueness and strict validation.
func (r *Registry) Update(slug string, p Provider) error {
	p.Normalize()
	if err := p.ValidateStrict(); err != nil {
		return err
	}
	for i, existing := range r.Providers {
		if existing.Slug == slug {
			// If the slug is changing, ensure the new slug is unique.
			if p.Slug != slug {
				for _, other := range r.Providers {
					if other.Slug == p.Slug {
						return errors.New("provider slug must be unique")
					}
				}
			}
			p.CreatedAt = existing.CreatedAt
			p.UpdatedAt = time.Now()
			r.Providers[i] = p
			return nil
		}
	}
	return errors.New("provider not found: " + slug)
}

// Import adds a provider from an external source (e.g. provider export).
// It enforces all validation including strict checks for secrets in metadata.
func (r *Registry) Import(p Provider) error {
	return r.Add(p)
}

// Find returns the provider with the given slug, or nil if none matches.
func (r *Registry) Find(slug string) *Provider {
	for i := range r.Providers {
		if r.Providers[i].Slug == slug {
			return &r.Providers[i]
		}
	}
	return nil
}

// SaveRegistry persists the registry to disk. Returns an error if the write fails.
func SaveRegistry(path string, r *Registry) error {
	return r.Save(path)
}

// MergeDefaults merges the curated default providers into the registry.
// For existing providers it backfills missing structural fields (compat,
// protocol, auth, endpoints, models, catalog, model-policy) while preserving
// user-editable values. Missing defaults are appended. Returns true if the
// registry was modified. Callers should Save() when changed is true.
func (r *Registry) MergeDefaults(defaults []Provider) bool {
	bySlug := make(map[string]int, len(r.Providers))
	for i := range r.Providers {
		bySlug[r.Providers[i].Slug] = i
	}

	now := time.Now()
	changed := false

	for i := range defaults {
		def := defaults[i]
		def.Normalize()

		idx, exists := bySlug[def.Slug]
		if exists {
			// Backfill missing structural fields on the existing provider.
			cur := &r.Providers[idx]
			cur.Normalize()

			if cur.Compatibility == "" && def.Compatibility != "" {
				cur.Compatibility = def.Compatibility
				changed = true
			}
			if cur.Protocol == "" && def.Protocol != "" {
				cur.Protocol = def.Protocol
				changed = true
			}
			if cur.Auth.Type == "" && def.Auth.Type != "" {
				cur.Auth = def.Auth
				changed = true
			}
			if cur.Endpoints.BaseURL == "" && def.Endpoints.BaseURL != "" {
				cur.Endpoints = def.Endpoints
				changed = true
			}
			if cur.BaseURL == "" && def.BaseURL != "" {
				cur.BaseURL = def.BaseURL
				changed = true
			}
			if len(cur.Models) == 0 && len(def.Models) > 0 {
				cur.Models = def.Models
				changed = true
			}
			if cur.Catalog.Source == "" && def.Catalog.Source != "" {
				cur.Catalog = def.Catalog
				changed = true
			}
			if cur.ModelPolicy.Source == "" && def.ModelPolicy.Source != "" {
				cur.ModelPolicy = def.ModelPolicy
				changed = true
			}
			continue
		}

		// Default not present — append a copy with timestamps set.
		if def.CreatedAt.IsZero() {
			def.CreatedAt = now
		}
		def.UpdatedAt = now
		r.Providers = append(r.Providers, def)
		bySlug[def.Slug] = len(r.Providers) - 1
		changed = true
	}

	return changed
}

// SetStaticModels replaces the curated model allowlist for a provider. It
// dedupes by ID, marks each model Static=true, sets ModelPolicy.Source to
// ModelSourceStatic, and refreshes Catalog + timestamps. It does NOT mutate the
// provider on disk — callers must Save().
func (r *Registry) SetStaticModels(slug string, models []ProviderModel) error {
	p := r.Find(slug)
	if p == nil {
		return errors.New("provider not found: " + slug)
	}

	seen := map[string]bool{}
	out := make([]ProviderModel, 0, len(models))
	for _, m := range models {
		m.ID = strings.TrimSpace(m.ID)
		if m.ID == "" || seen[m.ID] {
			continue
		}
		m.Static = true
		out = append(out, m)
		seen[m.ID] = true
	}
	if len(out) == 0 {
		return errors.New("static model catalog requires at least one model")
	}

	p.Models = out
	p.ModelPolicy.Source = ModelSourceStatic
	p.Catalog.Source = string(ModelSourceStatic)
	if p.ModelPolicy.RefreshURL == "" {
		p.ModelPolicy.RefreshURL = p.Catalog.RefreshURL
	}
	p.UpdatedAt = time.Now()
	p.Normalize()

	return p.ValidateStrict()
}

// SetDynamicModels replaces the provider's cached dynamic model catalog. Models
// are deduped by ID and marked Static=false. It keeps the refresh URL metadata
// so future refreshes can continue using the same endpoint.
func (r *Registry) SetDynamicModels(slug string, models []ProviderModel) error {
	p := r.Find(slug)
	if p == nil {
		return errors.New("provider not found: " + slug)
	}

	seen := map[string]bool{}
	out := make([]ProviderModel, 0, len(models))
	for _, m := range models {
		m.ID = strings.TrimSpace(m.ID)
		if m.ID == "" || seen[m.ID] {
			continue
		}
		m.Static = false
		out = append(out, m)
		seen[m.ID] = true
	}

	p.Models = out
	p.ModelPolicy.Source = ModelSourceDynamic
	p.Catalog.Source = string(ModelSourceDynamic)
	refreshURL := p.ModelRefreshURL()
	if p.ModelPolicy.RefreshURL == "" {
		p.ModelPolicy.RefreshURL = refreshURL
	}
	if p.Catalog.RefreshURL == "" {
		p.Catalog.RefreshURL = refreshURL
	}
	p.UpdatedAt = time.Now()
	p.Normalize()

	return p.ValidateStrict()
}

// Remove deletes the provider with the given slug. Returns an error if absent.
func (r *Registry) Remove(slug string) error {
	for i := range r.Providers {
		if r.Providers[i].Slug == slug {
			r.Providers = append(r.Providers[:i], r.Providers[i+1:]...)
			return nil
		}
	}
	return errors.New("provider not found: " + slug)
}
