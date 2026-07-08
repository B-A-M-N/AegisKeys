package profile

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"aegiskeys/internal/fsutil"
)

// RenderMode describes how a profile renders its launch configuration.
type RenderMode string

const (
	RenderEnv        RenderMode = "env"         // environment variables only
	RenderArgs       RenderMode = "args"        // command-line args only
	RenderEnvArgs    RenderMode = "env+args"    // both env and args
	RenderConfigFile RenderMode = "config_file" // write a config file
	RenderEnvFile    RenderMode = "envfile"     // write a .env file
	RenderEnvConfig  RenderMode = "env+config"  // inject env vars AND patch config file
)

// ModelSource describes how a model ID was selected.
type ModelSource string

const (
	ModelSourceStatic  ModelSource = "static"  // from curated provider list
	ModelSourceDynamic ModelSource = "dynamic" // resolved from provider API
	ModelSourceManual  ModelSource = "manual"  // user typed it
)

// TargetConfig describes which application this profile launches and how.
type TargetConfig struct {
	App        string     `json:"app"`               // crush, aider, cline, hermes, qwen, claude, vibe, generic
	RenderMode RenderMode `json:"render_mode"`       // how to render the launch
	Command    string     `json:"command,omitempty"` // override command (empty = use app default)
}

// ModelRef references a specific model from a provider.
type ModelRef struct {
	ID     string      `json:"id"`              // provider-specific model ID
	Alias  string      `json:"alias,omitempty"` // human-friendly name
	Source ModelSource `json:"source"`          // how this model was selected
	Locked bool        `json:"locked"`          // if true, never auto-update
}

// ModelSlots holds per-app model assignments.
// Different apps need different numbers of named model roles.
type ModelSlots struct {
	Main     *ModelRef `json:"main,omitempty"`
	Fast     *ModelRef `json:"fast,omitempty"`
	Weak     *ModelRef `json:"weak,omitempty"`
	Editor   *ModelRef `json:"editor,omitempty"`
	Planner  *ModelRef `json:"planner,omitempty"`
	Actor    *ModelRef `json:"actor,omitempty"`
	Subagent *ModelRef `json:"subagent,omitempty"`

	// Feature-specific model roles.
	InlineAssistant *ModelRef `json:"inline_assistant,omitempty"`
	CommitMessage   *ModelRef `json:"commit_message,omitempty"`
	ThreadSummary   *ModelRef `json:"thread_summary,omitempty"`

	// Hermes auxiliary roles.
	Compression *ModelRef `json:"compression,omitempty"`
	Vision      *ModelRef `json:"vision,omitempty"`
	WebExtract  *ModelRef `json:"web_extract,omitempty"`

	Alternatives []ModelRef          `json:"alternatives,omitempty"`
	Catalog      []ModelRef          `json:"catalog,omitempty"`
	Fallbacks    []ModelRef          `json:"fallbacks,omitempty"`
	Custom       map[string]ModelRef `json:"custom,omitempty"`
}

// TargetConfigFile describes a config file to write for the target app.
type TargetConfigFile struct {
	Path    string `json:"path"`    // absolute or ~/ path
	Format  string `json:"format"`  // json, yaml, toml, env
	Content string `json:"content"` // rendered template

	// Safety / merge controls.
	MergePolicy  string `json:"merge_policy,omitempty"`  // none, json_merge, yaml_merge, toml_merge, jsonc_merge, xml_patch, avoid
	BackupPolicy string `json:"backup_policy,omitempty"` // none, plain_0600, redacted, encrypted
	Scope        string `json:"scope,omitempty"`         // user, project, profile, temp
	RedactCheck  bool   `json:"redact_check,omitempty"`  // refuse to write raw secrets
	Description  string `json:"description,omitempty"`   // human-readable purpose
}

// Profile binds a provider + key + models + target app into a launch contract.
type Profile struct {
	Name         string `json:"name"`
	ProviderSlug string `json:"provider_slug"`
	KeyID        string `json:"key_id"`

	Target TargetConfig       `json:"target"`
	Models ModelSlots         `json:"models"`
	Env    map[string]string  `json:"env,omitempty"`
	Args   []string           `json:"args,omitempty"`
	Files  []TargetConfigFile `json:"files,omitempty"`

	Aliases   []string  `json:"aliases,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TargetApp returns the target app ID, defaulting to "generic".
func (p Profile) TargetApp() string {
	if p.Target.App != "" {
		return p.Target.App
	}
	return "generic"
}

// Command returns the launch command, deriving from Target.App if Command is empty.
func (p Profile) Command() string {
	if p.Target.Command != "" {
		return p.Target.Command
	}
	switch p.Target.App {
	case "crush":
		return "crush"
	case "aider":
		return "aider"
	case "cline":
		return "cline"
	case "hermes":
		return "hermes"
	case "qwen":
		return "qwen"
	case "claude":
		return "claude"
	case "vibe":
		return "vibe"
	case "goose":
		return "goose"
	case "mimo":
		return "opencode"
	case "openhands":
		return "openhands"
	case "gemini":
		return "gemini"
	case "copilot":
		return "copilot"
	case "continue":
		return "continue"
	case "zed":
		return "zed"
	case "intellij":
		return "idea"
	case "roo", "kilo", "cursor":
		return "" // not directly launchable
	default:
		return ""
	}
}

// ModelID returns the main model ID (backward compat).
func (p Profile) ModelID() string {
	if p.Models.Main != nil {
		return p.Models.Main.ID
	}
	return ""
}

// ActiveModelSlots returns the list of slot names this app supports.
func ActiveModelSlots(app string) []string {
	switch app {
	case "aider":
		return []string{"main", "weak", "editor"}
	case "cline":
		return []string{"planner", "actor"}
	case "goose":
		return []string{"main", "fast"}
	case "hermes":
		return []string{"main", "compression", "vision", "web_extract"}
	case "zed":
		return []string{"main", "inline_assistant", "subagent", "commit_message", "thread_summary", "alternatives"}
	case "claude":
		return []string{"main"}
	case "qwen":
		return []string{"main", "catalog", "fallbacks"}
	case "crush":
		return []string{"main", "catalog"}
	case "vibe":
		return []string{"main"}
	case "intellij":
		return []string{"main"}
	case "generic":
		return []string{"main"}
	case "codex":
		return []string{"main", "gpt54mini", "gpt53codex", "gpt52codex", "gpt52", "gpt51codexmax", "gpt51codexmini"}
	default:
		return []string{"main"}
	}
}

// Get returns the ModelRef for the named slot, or nil if not found/empty.
func (s *ModelSlots) Get(name string) *ModelRef {
	if s == nil {
		return nil
	}
	switch name {
	case "main":
		return s.Main
	case "fast":
		return s.Fast
	case "weak":
		return s.Weak
	case "editor":
		return s.Editor
	case "planner":
		return s.Planner
	case "actor":
		return s.Actor
	case "subagent":
		return s.Subagent
	case "inline_assistant":
		return s.InlineAssistant
	case "commit_message":
		return s.CommitMessage
	case "thread_summary":
		return s.ThreadSummary
	case "compression":
		return s.Compression
	case "vision":
		return s.Vision
	case "web_extract":
		return s.WebExtract
	default:
		if ref, ok := s.Custom[name]; ok {
			return &ref
		}
		return nil
	}
}

// SupportsConfigFile reports whether the app needs config file output.
func SupportsConfigFile(app string) bool {
	switch app {
	case "qwen", "cline", "goose", "hermes", "vibe", "crush":
		return true
	default:
		return false
	}
}

// NeedsMultiModel reports whether the app needs more than one model slot.
func NeedsMultiModel(app string) bool {
	switch app {
	case "aider", "cline", "goose", "hermes":
		return true
	default:
		return false
	}
}

// Store holds all profiles.
const StoreVersion = 2

type Store struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

func NewStore() *Store {
	return &Store{Version: StoreVersion, Profiles: []Profile{}}
}

func (s *Store) Find(name string) *Profile {
	for i := range s.Profiles {
		if s.Profiles[i].Name == name {
			return &s.Profiles[i]
		}
		if slices.Contains(s.Profiles[i].Aliases, name) {
			return &s.Profiles[i]
		}
	}
	return nil
}

func (s *Store) Add(p Profile) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("profile name is required")
	}
	if s.Find(p.Name) != nil {
		return errors.New("profile name must be unique")
	}
	for _, a := range p.Aliases {
		if s.Find(a) != nil {
			return errors.New("alias " + a + " collides with an existing profile or alias")
		}
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	s.Profiles = append(s.Profiles, p)
	return nil
}

func (s *Store) Remove(name string) error {
	for i, p := range s.Profiles {
		if p.Name == name {
			s.Profiles = append(s.Profiles[:i], s.Profiles[i+1:]...)
			return nil
		}
	}
	return errors.New("profile not found: " + name)
}

func LoadStore(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return NewStore(), err
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	migrateStore(&s)
	return &s, nil
}

// migrateStore migrates an in-place loaded store to the current version.
func migrateStore(s *Store) {
	if s.Version == 0 {
		// v0 → v1: add default Target.App and RenderMode to profiles.
		for i := range s.Profiles {
			if s.Profiles[i].Target.App == "" {
				s.Profiles[i].Target.App = "generic"
			}
			if s.Profiles[i].Target.RenderMode == "" {
				s.Profiles[i].Target.RenderMode = RenderEnv
			}
		}
		s.Version = 1
	}
	if s.Version == 1 {
		// v1 → v2: nothing structural yet; placeholder for future migrations.
		s.Version = StoreVersion
	}
}

func SaveStore(path string, s *Store) error {
	now := time.Now()
	for i := range s.Profiles {
		s.Profiles[i].UpdatedAt = now
	}
	s.Version = StoreVersion
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteFile(path, data)
}
