// Package appcatalog is the authoritative built-in application ID catalog.
// Adapter registration and visual fallback catalogs consume this list so new
// supported apps are represented consistently across release surfaces.
package appcatalog

var IDs = []string{
	"generic", "crush", "aider", "cline", "hermes", "qwen", "claude", "free-claude", "vibe", "goose",
	"codex", "mimo", "opencode", "openhands", "gemini", "copilot", "continue", "zed", "intellij",
	"roo", "kilo", "cursor",
}

// All returns a defensive copy of the supported built-in app IDs.
func All() []string { return append([]string(nil), IDs...) }
