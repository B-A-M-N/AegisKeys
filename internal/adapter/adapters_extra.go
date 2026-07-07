package adapter

import (
	"encoding/json"
	"strings"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// ---------------------------------------------------------------------------
// Additional first-class CLI agents
// ---------------------------------------------------------------------------

// MiMoOpenCodeAdapter renders config for MiMo/OpenCode (Nous Research CLI).
// OpenCode selects its model/provider from opencode.json (or --model) and reads
// provider API keys from environment variables, so we patch the user's
// opencode.json with the chosen model and provider base URL while keeping the
// secret in the env only.
type MiMoOpenCodeAdapter struct{}

func (MiMoOpenCodeAdapter) ID() string             { return "mimo" }
func (MiMoOpenCodeAdapter) DisplayName() string    { return "MiMo / OpenCode" }
func (MiMoOpenCodeAdapter) DefaultCommand() string { return "opencode" }

func (MiMoOpenCodeAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI ||
		p.Compatibility == provider.CompatAnthropic ||
		p.Compatibility == provider.CompatLocal
}

func (MiMoOpenCodeAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (MiMoOpenCodeAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (MiMoOpenCodeAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID: "mimo", DisplayName: "MiMo / OpenCode", DefaultCommand: "opencode",
		SupportLevel: SupportEnvConfig, RenderModes: []string{"env", "config_file"},
		CredentialControl: CredentialConfigPatched,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true, CanInjectSecrets: true, CanPatchConfig: true,
		ConfigFiles: []ConfigFileContract{
			{Path: "$HOME/.config/opencode/opencode.json", Format: "json", Description: "OpenCode model/provider config"},
		},
		CanManageModels: true, CanIsolateProfile: false, RequiresManualStep: false,
		ValidationChecks: []string{"config_no_raw_secret", "model_slots_validated", "backup_before_merge"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic, provider.CompatLocal},
	}
}

func (MiMoOpenCodeAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return nil, nil
}

func (MiMoOpenCodeAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	files := buildOpenCodeConfig(p, prov)
	return &LaunchStrategy{
		Plan:    LaunchPlan{Command: "opencode", Env: env, Files: files, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{ID: "mimo", DisplayName: "MiMo / OpenCode", SupportLevel: SupportEnvConfig},
	}, nil
}

// buildOpenCodeConfig writes the chosen model and provider base URL into the
// user's opencode.json. The API key stays env-only (referenced via {env:...}),
// so no raw secret is written to disk. The merge is recursive (deepMerge), so
// existing provider blocks and other settings are preserved.
func buildOpenCodeConfig(p profile.Profile, prov provider.Provider) []FileWrite {
	modelID := ""
	if p.Models.Main != nil {
		modelID = p.Models.Main.ID
	}
	cfg := map[string]any{}
	if modelID != "" {
		if strings.Contains(modelID, "/") {
			cfg["model"] = modelID
		} else {
			cfg["model"] = prov.Slug + "/" + modelID
		}
	}
	cfg["provider"] = map[string]any{
		prov.Slug: map[string]any{
			"options": map[string]any{
				"apiKey":  "{env:" + prov.CanonicalEnvVar() + "}",
				"baseURL": prov.CanonicalBaseURL(),
			},
		},
	}
	content, _ := json.MarshalIndent(cfg, "", "  ")
	return []FileWrite{
		{
			Path:         "$HOME/.config/opencode/opencode.json",
			Format:       "json",
			Content:      string(content),
			Scope:        ScopeUser,
			MergePolicy:  MergeJSON,
			BackupPolicy: BackupRedacted,
			RedactCheck:  true,
			Description:  "OpenCode model/provider config; secret stays env-only",
		},
	}
}

// OpenHandsAdapter renders config for OpenHands CLI.
type OpenHandsAdapter struct{}

func (OpenHandsAdapter) ID() string             { return "openhands" }
func (OpenHandsAdapter) DisplayName() string    { return "OpenHands" }
func (OpenHandsAdapter) DefaultCommand() string { return "openhands" }

func (OpenHandsAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI ||
		p.Compatibility == provider.CompatAnthropic ||
		p.Compatibility == provider.CompatLocal
}

func (OpenHandsAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (OpenHandsAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (OpenHandsAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID: "openhands", DisplayName: "OpenHands", DefaultCommand: "openhands",
		SupportLevel: SupportFullEnv, RenderModes: []string{"env"},
		CredentialControl: CredentialEnvInjected,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true, CanInjectSecrets: true, CanPatchConfig: false,
		CanManageModels: true, CanIsolateProfile: false, RequiresManualStep: false,
		ValidationChecks: []string{"env_injection_only", "model_slots_validated"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic, provider.CompatLocal},
	}
}

func (OpenHandsAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return nil, nil
}

func (OpenHandsAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	if p.ModelID() != "" {
		env["OPENHANDS_MODEL"] = p.ModelID()
	}
	env["OPENHANDS_PROVIDER"] = prov.Slug
	return &LaunchStrategy{
		Plan:    LaunchPlan{Command: "openhands", Env: env, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{ID: "openhands", DisplayName: "OpenHands", SupportLevel: SupportEnvConfig},
	}, nil
}

// GeminiCLIAdapter renders config for Google Gemini CLI.
type GeminiCLIAdapter struct{}

func (GeminiCLIAdapter) ID() string             { return "gemini" }
func (GeminiCLIAdapter) DisplayName() string    { return "Gemini CLI" }
func (GeminiCLIAdapter) DefaultCommand() string { return "gemini" }

func (GeminiCLIAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatGoogle ||
		p.Compatibility == provider.CompatOpenAI
}

func (GeminiCLIAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (GeminiCLIAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (GeminiCLIAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID: "gemini", DisplayName: "Gemini CLI", DefaultCommand: "gemini",
		SupportLevel: SupportFullEnv, RenderModes: []string{"env"},
		CredentialControl: CredentialEnvInjected,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true, CanInjectSecrets: true, CanPatchConfig: false,
		CanManageModels: true, CanIsolateProfile: false, RequiresManualStep: false,
		ValidationChecks: []string{"env_injection_only", "model_slots_validated", "args_no_raw_secret"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatGoogle, provider.CompatOpenAI},
	}
}

func (GeminiCLIAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return nil, nil
}

func (GeminiCLIAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	if prov.Compatibility == provider.CompatGoogle {
		if p.ModelID() != "" {
			env["GOOGLE_MODEL"] = p.ModelID()
		}
	} else {
		if p.ModelID() != "" {
			env["OPENAI_MODEL"] = p.ModelID()
		}
	}
	return &LaunchStrategy{
		Plan:    LaunchPlan{Command: "gemini", Env: env, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{ID: "gemini", DisplayName: "Gemini CLI", SupportLevel: SupportFullEnv},
	}, nil
}

// CopilotCLIAdapter renders config for GitHub Copilot CLI.
type CopilotCLIAdapter struct{}

func (CopilotCLIAdapter) ID() string             { return "copilot" }
func (CopilotCLIAdapter) DisplayName() string    { return "GitHub Copilot CLI" }
func (CopilotCLIAdapter) DefaultCommand() string { return "copilot" }

func (CopilotCLIAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI ||
		p.Compatibility == provider.CompatAnthropic
}

func (CopilotCLIAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (CopilotCLIAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (CopilotCLIAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID: "copilot", DisplayName: "GitHub Copilot CLI", DefaultCommand: "copilot",
		SupportLevel: SupportFullEnv, RenderModes: []string{"env"},
		CredentialControl: CredentialEnvInjected,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true, CanInjectSecrets: true, CanPatchConfig: false,
		CanManageModels: true, CanIsolateProfile: false, RequiresManualStep: false,
		ValidationChecks: []string{"env_injection_only", "model_slots_validated"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic},
	}
}

func (CopilotCLIAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return nil, nil
}

func (CopilotCLIAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	if p.ModelID() != "" {
		env["COPILOT_MODEL"] = p.ModelID()
	}
	return &LaunchStrategy{
		Plan:    LaunchPlan{Command: "copilot", Env: env, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{ID: "copilot", DisplayName: "GitHub Copilot CLI", SupportLevel: SupportFullEnv},
	}, nil
}

// ContinueAdapter renders config for Continue.dev (CLI + extension).
type ContinueAdapter struct{}

func (ContinueAdapter) ID() string             { return "continue" }
func (ContinueAdapter) DisplayName() string    { return "Continue" }
func (ContinueAdapter) DefaultCommand() string { return "continue" }

func (ContinueAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI ||
		p.Compatibility == provider.CompatAnthropic ||
		p.Compatibility == provider.CompatLocal
}

func (ContinueAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (ContinueAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (ContinueAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID: "continue", DisplayName: "Continue", DefaultCommand: "continue",
		SupportLevel: SupportFullEnv, RenderModes: []string{"env"},
		CredentialControl: CredentialEnvInjected,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"cli", "extension"},
		CanLaunch:         true, CanInjectSecrets: true, CanPatchConfig: false,
		CanManageModels: true, CanIsolateProfile: false, RequiresManualStep: false,
		ValidationChecks: []string{"env_injection_only", "model_slots_validated"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic, provider.CompatLocal},
	}
}

func (ContinueAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return nil, nil
}

func (ContinueAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	if p.ModelID() != "" {
		env["CONTINUE_MODEL"] = p.ModelID()
	}
	return &LaunchStrategy{
		Plan:    LaunchPlan{Command: "continue", Env: env, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{ID: "continue", DisplayName: "Continue", SupportLevel: SupportFullEnv},
	}, nil
}

// CodexAdapter renders config for Codex CLI.
type CodexAdapter struct{}

func (CodexAdapter) ID() string             { return "codex" }
func (CodexAdapter) DisplayName() string    { return "Codex CLI" }
func (CodexAdapter) DefaultCommand() string { return "codex" }

func (CodexAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI ||
		p.Compatibility == provider.CompatAnthropic ||
		p.Compatibility == provider.CompatLocal ||
		p.Compatibility == provider.CompatGoogle
}

func (CodexAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (CodexAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (CodexAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID: "codex", DisplayName: "Codex CLI", DefaultCommand: "codex",
		SupportLevel: SupportFullEnv, RenderModes: []string{"env"},
		CredentialControl: CredentialEnvInjected,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true, CanInjectSecrets: true, CanPatchConfig: false,
		CanManageModels: true, CanIsolateProfile: false, RequiresManualStep: false,
		ValidationChecks: []string{"env_injection_only", "model_slots_validated"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model (gpt54 equivalent)"},
			{Name: "gpt54mini", Description: "GPT-5.4 Mini", Optional: true},
			{Name: "gpt53codex", Description: "GPT-5.3 Codex", Optional: true},
			{Name: "gpt52codex", Description: "GPT-5.2 Codex", Optional: true},
			{Name: "gpt52", Description: "GPT-5.2", Optional: true},
			{Name: "gpt51codexmax", Description: "GPT-5.1 Codex Max", Optional: true},
			{Name: "gpt51codexmini", Description: "GPT-5.1 Codex Mini", Optional: true},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic, provider.CompatLocal, provider.CompatGoogle},
	}
}

func (CodexAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return nil, nil
}

func (CodexAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	var args []string
	if p.ModelID() != "" {
		args = append(args, "--model", p.ModelID())
	}
	// Inject auxiliary codex slots as env vars for wrappers/scripts
	if m := p.Models.Get("gpt54mini"); m != nil && m.ID != "" {
		env["CODEX_MODEL_GPT54MINI"] = m.ID
	}
	if m := p.Models.Get("gpt53codex"); m != nil && m.ID != "" {
		env["CODEX_MODEL_GPT53CODEX"] = m.ID
	}
	if m := p.Models.Get("gpt52codex"); m != nil && m.ID != "" {
		env["CODEX_MODEL_GPT52CODEX"] = m.ID
	}
	if m := p.Models.Get("gpt52"); m != nil && m.ID != "" {
		env["CODEX_MODEL_GPT52"] = m.ID
	}
	if m := p.Models.Get("gpt51codexmax"); m != nil && m.ID != "" {
		env["CODEX_MODEL_GPT51CODEXMAX"] = m.ID
	}
	if m := p.Models.Get("gpt51codexmini"); m != nil && m.ID != "" {
		env["CODEX_MODEL_GPT51CODEXMINI"] = m.ID
	}
	return &LaunchStrategy{
		Plan:    LaunchPlan{Command: "codex", Args: args, Env: env, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{ID: "codex", DisplayName: "Codex CLI", SupportLevel: SupportFullEnv},
	}, nil
}

// ---------------------------------------------------------------------------
// Extension / guided adapters (partial support)
// ---------------------------------------------------------------------------

// RooCodeAdapter renders config for Roo Code (VS Code extension).
type RooCodeAdapter struct{}

func (RooCodeAdapter) ID() string             { return "roo" }
func (RooCodeAdapter) DisplayName() string    { return "Roo Code" }
func (RooCodeAdapter) DefaultCommand() string { return "" }

func (RooCodeAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI ||
		p.Compatibility == provider.CompatAnthropic ||
		p.Compatibility == provider.CompatLocal
}

func (RooCodeAdapter) CanInjectCredential(p provider.Provider) bool  { return false }
func (RooCodeAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (RooCodeAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID: "roo", DisplayName: "Roo Code", DefaultCommand: "",
		SupportLevel: SupportManualCredential, RenderModes: []string{"config_file"},
		CredentialControl: CredentialManualLogin,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"extension"},
		CanLaunch:         false, CanInjectSecrets: false, CanPatchConfig: false,
		CanManageModels: true, CanIsolateProfile: false, RequiresManualStep: true,
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
		},
		Hazards: []Hazard{
			{
				Severity: "high",
				Title:    "Roo Code is a VS Code extension, not a CLI",
				Detail:   "AegisKeys can prepare provider/model settings but cannot inject credentials into the extension host.",
				Fix:      "Enter the API key through the Roo Code panel in VS Code.",
			},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic, provider.CompatLocal},
	}
}

func (RooCodeAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return []string{"Roo Code requires manual credential setup in VS Code"}, nil
}

func (RooCodeAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	return &LaunchStrategy{
		Plan:    LaunchPlan{Env: env, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{ID: "roo", DisplayName: "Roo Code", SupportLevel: SupportManualCredential},
		ManualSteps: []ManualStep{
			{
				Title:       "Enter API key in Roo Code",
				Description: "Open the Roo Code panel in VS Code and enter the API key in the provider settings.",
				When:        "after_first_launch",
			},
		},
		Hazards: []Hazard{
			{
				Severity: "high",
				Title:    "Roo Code is a VS Code extension, not a CLI",
				Detail:   "AegisKeys can prepare provider/model settings but cannot inject credentials into the extension host.",
				Fix:      "Enter the API key through the Roo Code panel in VS Code.",
			},
		},
	}, nil
}

// KiloCodeAdapter renders config for Kilo Code (VS Code extension).
type KiloCodeAdapter struct{}

func (KiloCodeAdapter) ID() string             { return "kilo" }
func (KiloCodeAdapter) DisplayName() string    { return "Kilo Code" }
func (KiloCodeAdapter) DefaultCommand() string { return "" }

func (KiloCodeAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI ||
		p.Compatibility == provider.CompatAnthropic ||
		p.Compatibility == provider.CompatLocal
}

func (KiloCodeAdapter) CanInjectCredential(p provider.Provider) bool  { return false }
func (KiloCodeAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (KiloCodeAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID: "kilo", DisplayName: "Kilo Code", DefaultCommand: "",
		SupportLevel: SupportManualCredential, RenderModes: []string{"config_file"},
		CredentialControl: CredentialManualLogin,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"extension"},
		CanLaunch:         false, CanInjectSecrets: false, CanPatchConfig: false,
		CanManageModels: true, CanIsolateProfile: false, RequiresManualStep: true,
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic, provider.CompatLocal},
	}
}

func (KiloCodeAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return []string{"Kilo Code requires manual credential setup in VS Code"}, nil
}

func (KiloCodeAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	return &LaunchStrategy{
		Plan:    LaunchPlan{Env: env, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{ID: "kilo", DisplayName: "Kilo Code", SupportLevel: SupportManualCredential},
		ManualSteps: []ManualStep{
			{
				Title:       "Enter API key in Kilo Code",
				Description: "Open the Kilo Code panel in VS Code and enter the API key in the provider settings.",
				When:        "after_first_launch",
			},
		},
	}, nil
}

// CursorAdapter renders config for Cursor agent (guided/manual).
type CursorAdapter struct{}

func (CursorAdapter) ID() string             { return "cursor" }
func (CursorAdapter) DisplayName() string    { return "Cursor" }
func (CursorAdapter) DefaultCommand() string { return "" }

func (CursorAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI ||
		p.Compatibility == provider.CompatAnthropic ||
		p.Compatibility == provider.CompatLocal
}

func (CursorAdapter) CanInjectCredential(p provider.Provider) bool  { return false }
func (CursorAdapter) CanConfigureProvider(p provider.Provider) bool { return false }

func (CursorAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID: "cursor", DisplayName: "Cursor", DefaultCommand: "",
		SupportLevel: SupportManualCredential, RenderModes: []string{},
		CredentialControl: CredentialManualLogin,
		SupportConfidence: ConfidenceExperimental,
		LaunchSurfaces:    []string{"gui", "ide"},
		CanLaunch:         false, CanInjectSecrets: false, CanPatchConfig: false,
		CanManageModels: false, CanIsolateProfile: false, RequiresManualStep: true,
		Hazards: []Hazard{
			{
				Severity: "critical",
				Title:    "Cursor uses account-based auth, not env vars",
				Detail:   "AegisKeys cannot inject credentials into Cursor. Use Cursor's built-in provider settings.",
				Fix:      "Configure API keys in Cursor Settings → AI → API Keys.",
			},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic, provider.CompatLocal},
	}
}

func (CursorAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return []string{"Cursor uses account-based auth; AegisKeys cannot inject credentials"}, nil
}

func (CursorAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	return &LaunchStrategy{
		Plan:        LaunchPlan{Preview: buildPreview(p.Name, prov)},
		Support:     AppSupportContract{ID: "cursor", DisplayName: "Cursor", SupportLevel: SupportManualCredential},
		Blocked:     true,
		BlockReason: "Cursor uses account-based auth; AegisKeys cannot inject credentials",
		ManualSteps: []ManualStep{
			{
				Title:       "Configure API key in Cursor",
				Description: "Open Cursor Settings → AI → API Keys and enter the API key manually.",
				When:        "before_launch",
			},
		},
		Hazards: []Hazard{
			{
				Severity: "critical",
				Title:    "Cursor uses account-based auth, not env vars",
				Detail:   "AegisKeys cannot inject credentials into Cursor.",
				Fix:      "Configure API keys in Cursor Settings → AI → API Keys.",
			},
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Guided actions (for GUI/IDE apps)
// ---------------------------------------------------------------------------

// GuidedActionKind identifies a user action AegisKeys can prepare but not automate.
type GuidedActionKind string

const (
	ActionLaunch             GuidedActionKind = "launch"
	ActionPatchConfig        GuidedActionKind = "patch_config"
	ActionOpenSettings       GuidedActionKind = "open_settings"
	ActionCopyEnvSummary     GuidedActionKind = "copy_env_summary"
	ActionWriteProfileDir    GuidedActionKind = "write_profile_dir"
	ActionVerifyCredential   GuidedActionKind = "verify_credential"
	ActionClearPersistedAuth GuidedActionKind = "clear_persisted_auth"
)

// GuidedAction describes a user action that AegisKeys can prepare.
type GuidedAction struct {
	Kind            GuidedActionKind
	Label           string
	Description     string
	Command         []string
	FileWrites      []FileWrite
	ManualText      string
	RequiresConfirm bool
}

// GuidedActions returns the guided actions for a given app.
func GuidedActions(appID string) []GuidedAction {
	switch appID {
	case "zed":
		return []GuidedAction{
			{
				Kind:            ActionPatchConfig,
				Label:           "Patch Zed agent model settings",
				Description:     "Writes agent.default_model, agent.inline_assistant_model, and custom provider metadata to settings.json.",
				RequiresConfirm: true,
			},
			{
				Kind:        ActionOpenSettings,
				Label:       "Open Zed Agent Settings",
				Description: "User may need to enter provider API key into Zed keychain if env injection is not viable.",
				ManualText:  "Open Zed → Agent Panel → Settings → LLM Providers.",
			},
		}
	case "intellij":
		return []GuidedAction{
			{
				Kind:        ActionWriteProfileDir,
				Label:       "Create isolated IntelliJ config profile",
				Description: "Generates IDEA_VM_OPTIONS with idea.config.path and idea.system.path.",
			},
			{
				Kind:        ActionOpenSettings,
				Label:       "Open AI provider settings",
				Description: "Enter API key manually into IntelliJ PasswordSafe.",
				ManualText:  "Settings → Tools → AI Assistant → Third-party AI providers.",
			},
		}
	case "cursor":
		return []GuidedAction{
			{
				Kind:        ActionOpenSettings,
				Label:       "Open Cursor AI settings",
				Description: "Cursor uses account-based auth. Configure API keys in the IDE.",
				ManualText:  "Cursor Settings → AI → API Keys.",
			},
		}
	case "roo":
		return []GuidedAction{
			{
				Kind:        ActionOpenSettings,
				Label:       "Open Roo Code settings",
				Description: "Enter API key in the Roo Code panel.",
				ManualText:  "VS Code → Roo Code → Settings → Provider API Key.",
			},
		}
	default:
		return nil
	}
}
