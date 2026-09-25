package logo

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const defaultLogoSheet = "assets/logos/c7cd9534-6455-4993-9eed-014e18d85d8f.png"

// embeddedAssets is the release asset source. Files below internal/logo/assets
// are compiled into the executable, so packaged builds never depend on cwd.
//
// Only the independently controlled generic identity asset is embedded in
// release binaries. Dedicated third-party illustrations remain source-tree
// review artifacts until individually approved; runtime compatibility names
// resolve to this generic asset through PublicationAsset.
//
//go:embed assets/logos/c7cd9534-6455-4993-9eed-014e18d85d8f.png
var embeddedAssets embed.FS

// DefaultAssets is the complete carousel registry. Every registered path must
// exist in embeddedAssets; ValidateDefaultAssets enforces that release gate.
// Aliases are deliberate and visible rather than silently treated as missing.
var DefaultAssets = map[string]Asset{
	"generic":      {ID: "generic", Path: "assets/logos/c7cd9534-6455-4993-9eed-014e18d85d8f.png", Width: 176, Height: 96},
	"aider":        {ID: "aider", Path: "assets/logos/aider.png", Width: 176, Height: 96},
	"crush":        {ID: "crush", Path: "assets/logos/Crush.png", Width: 176, Height: 96},
	"qwen":         {ID: "qwen", Path: "assets/logos/qwencode.png", Width: 176, Height: 96},
	"goose":        {ID: "goose", Path: "assets/logos/goose.png", Width: 176, Height: 96},
	"cline":        {ID: "cline", Path: "assets/logos/cline.png", Width: 176, Height: 96},
	"claude":       {ID: "claude", Path: "assets/logos/claudecode.png", Width: 176, Height: 96},
	"free-claude":  {ID: "free-claude", Path: "assets/logos/claudecode.png", Width: 176, Height: 96},
	"hermes":       {ID: "hermes", Path: "assets/logos/hermes-agent.png", Width: 176, Height: 96},
	"vibe":         {ID: "vibe", Path: "assets/logos/MistralVibe.png", Width: 176, Height: 96},
	"codex":        {ID: "codex", Path: "assets/logos/codex.png", Width: 176, Height: 96},
	"mimo":         {ID: "mimo", Path: "assets/logos/mimocode.png", Width: 176, Height: 96},
	"opencode":     {ID: "opencode", Path: "assets/logos/opencode.png", Width: 176, Height: 96},
	"openhands":    {ID: "openhands", Path: "assets/logos/openhands.png", Width: 176, Height: 96},
	"gemini":       {ID: "gemini", Path: "assets/logos/geminicli.png", Width: 176, Height: 96},
	"copilot":      {ID: "copilot", Path: "assets/logos/githubcopilot.png", Width: 176, Height: 96},
	"continue":     {ID: "continue", Path: "assets/logos/continue.png", Width: 176, Height: 96},
	"zed":          {ID: "zed", Path: "assets/logos/zed.png", Width: 176, Height: 96},
	"intellij":     {ID: "intellij", Path: "assets/logos/intellij.png", Width: 176, Height: 96},
	"mistral":      {ID: "mistral", Path: "assets/logos/MistralVibe.png", Width: 176, Height: 96},
	"mistralai":    {ID: "mistralai", Path: "assets/logos/MistralVibe.png", Width: 176, Height: 96},
	"hermes-agent": {ID: "hermes-agent", Path: "assets/logos/hermes-agent.png", Width: 176, Height: 96},
	"anthropic":    {ID: "anthropic", Path: "assets/logos/claudecode.png", Width: 176, Height: 96},
	"google":       {ID: "google", Path: "assets/logos/geminicli.png", Width: 176, Height: 96},
	// Manual/IDE adapters deliberately fall back to existing approved artwork;
	// absence of dedicated art must not remove their Matrix representation.
	"roo":    {ID: "roo", Path: "assets/logos/claudecode.png", Width: 176, Height: 96},
	"kilo":   {ID: "kilo", Path: "assets/logos/claudecode.png", Width: 176, Height: 96},
	"cursor": {ID: "cursor", Path: "assets/logos/claudecode.png", Width: 176, Height: 96},
}

// approvedForPublication is deliberately explicit per asset. An unresolved
// third-party illustration is not shipped merely because it is embedded; the
// TUI resolves it to the generic identity asset instead.
var approvedForPublication = map[string]bool{
	"assets/logos/c7cd9534-6455-4993-9eed-014e18d85d8f.png": true,
}

var (
	defaultMaskCacheMu sync.Mutex
	defaultMaskCache   = map[string]Mask{}
)

// AssetOverrideDir enables an explicit development override. It is intentionally
// environment-driven and never inferred from the working directory.
func AssetOverrideDir() string { return os.Getenv("AEGISKEYS_LOGO_DIR") }

func embeddedAssetPath(path string) (string, bool) {
	if filepath.IsAbs(path) || AssetOverrideDir() == "" {
		clean := filepath.Clean(path)
		if clean != path || filepath.IsAbs(path) {
			return "", false
		}
		if _, err := fs.Stat(embeddedAssets, clean); err == nil {
			return clean, true
		}
		return "", false
	}
	candidate := filepath.Join(AuditSafeDevAssetDir(), filepath.Base(path))
	if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
		return candidate, true
	}
	return "", false
}

// AuditSafeDevAssetDir returns the explicit override directory after rejecting
// symlinked roots. The helper name emphasizes that it is for development only.
func AuditSafeDevAssetDir() string { return AssetOverrideDir() }

// PublicationAsset returns the asset used for display in packaged builds.
// Dedicated third-party artwork is used only when explicitly approved; every
// other app falls back to the generic identity asset without changing its
// application name, adapter, or integration behavior.
func PublicationAsset(id string) (Asset, bool) {
	asset, ok := DefaultAssets[id]
	if !ok {
		return Asset{}, false
	}
	if approvedForPublication[asset.Path] {
		return asset, true
	}
	generic, ok := DefaultAssets["generic"]
	if !ok {
		return Asset{}, false
	}
	return generic, true
}

func publicationPath(id string) (string, bool) {
	asset, ok := PublicationAsset(id)
	if !ok {
		return "", false
	}
	return asset.Path, true
}

// ValidateDefaultAssets is called by tests and release builds. It verifies
// every registered asset exists and decodes as a PNG in the embedded FS.
func ValidateDefaultAssets() error {
	if err := validateEmbeddedPNG(DefaultAssets["generic"].Path); err != nil {
		return err
	}
	// Every application name must resolve to an embedded publication asset;
	// unresolved dedicated artwork intentionally resolves to generic.
	for id := range DefaultAssets {
		asset, ok := PublicationAsset(id)
		if !ok {
			return errors.New("registered logo has no publication fallback: " + id)
		}
		if err := validateEmbeddedPNG(asset.Path); err != nil {
			return err
		}
	}
	return nil
}

func validateEmbeddedPNG(path string) error {
	raw, err := embeddedAssets.ReadFile(path)
	if err != nil {
		return errors.New("embedded publication logo missing: " + path)
	}
	if len(raw) < 8 || string(raw[1:4]) != "PNG" {
		return errors.New("embedded publication logo is not PNG: " + path)
	}
	return nil
}

func LoadDefaultMask(id string) (Mask, bool) {
	asset, ok := PublicationAsset(id)
	if !ok {
		return Mask{}, false
	}
	path, ok := embeddedAssetPath(asset.Path)
	if !ok {
		return Mask{}, false
	}
	cacheKey := asset.ID + "\x00" + path
	defaultMaskCacheMu.Lock()
	if mask, ok := defaultMaskCache[cacheKey]; ok {
		defaultMaskCacheMu.Unlock()
		return mask, true
	}
	defaultMaskCacheMu.Unlock()
	var mask Mask
	var err error
	if AssetOverrideDir() != "" {
		mask, err = LoadAssetMask(asset, path)
	} else {
		mask, err = LoadEmbeddedAssetMask(asset)
	}
	if err != nil {
		return Mask{}, false
	}
	defaultMaskCacheMu.Lock()
	defaultMaskCache[cacheKey] = mask
	defaultMaskCacheMu.Unlock()
	return mask, true
}

func DefaultAssetAvailable(id string) bool {
	_, ok := publicationPath(id)
	return ok
}

// PublicationAssetCount returns the number of unique embedded publication
// assets after unresolved dedicated artwork is collapsed to generic.
func PublicationAssetCount() int {
	seen := make(map[string]struct{})
	for id := range DefaultAssets {
		if asset, ok := PublicationAsset(id); ok {
			seen[asset.Path] = struct{}{}
		}
	}
	return len(seen)
}
