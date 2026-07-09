package logo

import (
	"os"
	"path/filepath"
	"sync"
)

const defaultLogoSheet = "assets/logos/c7cd9534-6455-4993-9eed-014e18d85d8f.png"

// DefaultAssets names the logo assets used as source-of-truth for rain masks.
// Missing files are treated as unavailable rather than replaced by fake art.
var DefaultAssets = map[string]Asset{
	"aider":     {ID: "aider", Path: "assets/logos/aider.png", Width: 176, Height: 96},
	"crush":     {ID: "crush", Path: "assets/logos/Crush.png", Width: 176, Height: 96},
	"qwen":      {ID: "qwen", Path: "assets/logos/qwencode.png", Width: 176, Height: 96},
	"goose":     {ID: "goose", Path: "assets/logos/goose.png", Width: 176, Height: 96},
	"cline":     {ID: "cline", Path: "assets/logos/cline.png", Width: 176, Height: 96},
	"claude":    {ID: "claude", Path: "assets/logos/claudecode.png", Width: 176, Height: 96},
	"vibe":      {ID: "vibe", Path: "assets/logos/MistralVibe.png", Width: 176, Height: 96},
	"codex":     {ID: "codex", Path: "assets/logos/codex.png", Width: 176, Height: 96},
	"mimo":      {ID: "mimo", Path: "assets/logos/mimocode.png", Width: 176, Height: 96},
	"opencode":  {ID: "opencode", Path: "assets/logos/opencode.png", Width: 176, Height: 96},
	"openhands": {ID: "openhands", Path: "assets/logos/openhands.png", Width: 176, Height: 96},
	"gemini":    {ID: "gemini", Path: "assets/logos/geminicli.png", Width: 176, Height: 96},
	"copilot":   {ID: "copilot", Path: "assets/logos/githubcopilot.png", Width: 176, Height: 96},
	"continue":  {ID: "continue", Path: "assets/logos/continue.png", Width: 176, Height: 96},
	"zed":       {ID: "zed", Path: "assets/logos/zed.png", Width: 176, Height: 96},
	"intellij":  {ID: "intellij", Path: "assets/logos/intellij.png", Width: 176, Height: 96},
	"mistral":   {ID: "mistral", Path: "assets/logos/MistralVibe.png", Width: 176, Height: 96},
	"mistralai": {ID: "mistralai", Path: "assets/logos/MistralVibe.png", Width: 176, Height: 96},

	// Provider aliases without dedicated art in the current sheet.
	"anthropic":  {ID: "anthropic", Path: "assets/logos/claudecode.png", Width: 176, Height: 96},
	"google":     {ID: "google", Path: "assets/logos/geminicli.png", Width: 176, Height: 96},
	"moonshotai": {ID: "moonshotai", Path: "assets/logos/moonshotai.png", Width: 60, Height: 18},
	"z-ai":       {ID: "z-ai", Path: "assets/logos/z-ai.png", Width: 60, Height: 18},
}

var (
	defaultMaskCacheMu sync.Mutex
	defaultMaskCache   = map[string]Mask{}
)

// ResolveAssetPath searches from cwd upward for a relative asset path.
func ResolveAssetPath(path string) (string, bool) {
	if filepath.IsAbs(path) {
		_, err := os.Stat(path)
		return path, err == nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return path, false
	}
	for {
		candidate := filepath.Join(wd, path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return path, false
}

// LoadDefaultMask loads a registered logo asset if the file is present.
func LoadDefaultMask(id string) (Mask, bool) {
	asset, ok := DefaultAssets[id]
	if !ok {
		return Mask{}, false
	}
	path, ok := ResolveAssetPath(asset.Path)
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

	mask, err := LoadAssetMask(asset, path)
	if err != nil {
		return Mask{}, false
	}
	defaultMaskCacheMu.Lock()
	defaultMaskCache[cacheKey] = mask
	defaultMaskCacheMu.Unlock()
	return mask, true
}

// DefaultAssetAvailable reports whether a registered logo asset exists without
// decoding or sampling the image.
func DefaultAssetAvailable(id string) bool {
	asset, ok := DefaultAssets[id]
	if !ok {
		return false
	}
	_, ok = ResolveAssetPath(asset.Path)
	return ok
}
