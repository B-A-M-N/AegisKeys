package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"aegiskeys/internal/fsutil"
)

// ModelCache is a non-secret cache of API-discovered model candidates for a
// provider. It stores only model names/IDs and metadata — never secrets.
// The cache backs the provider model catalog UI without bloating providers.json.
type ModelCache struct {
	ProviderSlug string          `json:"provider_slug"`
	FetchedAt    time.Time       `json:"fetched_at"`
	RefreshURL   string          `json:"refresh_url"`
	Models       []ProviderModel `json:"models"`
}

// ModelCacheDir returns the path to the model-cache directory. It lives
// alongside the other config files and is created with 0700 perms.
func ModelCacheDir(configDir string) string {
	return filepath.Join(configDir, "model-cache")
}

// ModelCachePath returns the path to a provider's model cache file.
func ModelCachePath(configDir, slug string) string {
	return filepath.Join(ModelCacheDir(configDir), slug+".json")
}

// EnsureModelCacheDir creates the model-cache directory with 0700 perms.
func EnsureModelCacheDir(configDir string) error {
	return os.MkdirAll(ModelCacheDir(configDir), 0700)
}

// LoadModelCache reads a provider's model cache. Returns a zero-value cache
// (nil Models) on missing file — callers should treat nil Models as "not cached".
func LoadModelCache(configDir, slug string) (ModelCache, error) {
	path := ModelCachePath(configDir, slug)
	var cache ModelCache
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cache, nil
		}
		return cache, err
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		// Corrupt cache: remove it rather than fail the whole workflow.
		_ = os.Remove(path)
		return ModelCache{}, nil
	}
	return cache, nil
}

// SaveModelCache writes the cache atomically. Never stores secrets.
func SaveModelCache(configDir string, cache ModelCache) error {
	if err := EnsureModelCacheDir(configDir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal model cache: %w", err)
	}
	// Enforce 0600 on the cache even if atomicWrite created it looser.
	if err := fsutil.AtomicWriteFile(ModelCachePath(configDir, cache.ProviderSlug), data); err != nil {
		return err
	}
	_ = os.Chmod(ModelCachePath(configDir, cache.ProviderSlug), 0600)
	return nil
}

// IsCacheExpired reports whether the cache is older than ttlMinutes. A zero
// or negative ttlMinutes means "never expire" (treat as fresh).
func (c ModelCache) IsCacheExpired(ttlMinutes int) bool {
	if ttlMinutes <= 0 {
		return false
	}
	return time.Since(c.FetchedAt) > time.Duration(ttlMinutes)*time.Minute
}

// NewModelCache builds a cache from a successful refresh. The caller provides
// the fetched candidates and the config dir; the provider slug and timestamp
// are filled in automatically.
func NewModelCache(p Provider, models []ProviderModel) ModelCache {
	return ModelCache{
		ProviderSlug: p.Slug,
		FetchedAt:    time.Now(),
		RefreshURL:   p.ModelRefreshURL(),
		Models:       models,
	}
}
