package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
	"aegiskeys/internal/sensitive"
)

// GenericOpenAIAdapter renders config for any OpenAI-compatible API.
type GenericOpenAIAdapter struct{}

func (GenericOpenAIAdapter) ID() string             { return "generic" }
func (GenericOpenAIAdapter) DisplayName() string    { return "Generic OpenAI-compatible" }
func (GenericOpenAIAdapter) DefaultCommand() string { return "" }

func (GenericOpenAIAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI || p.Compatibility == provider.CompatLocal
}

func (GenericOpenAIAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (GenericOpenAIAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (GenericOpenAIAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                        "generic",
		DisplayName:               "Generic OpenAI-compatible",
		DefaultCommand:            "",
		SupportLevel:              SupportFullEnv,
		CredentialControl:         CredentialEnvInjected,
		SupportConfidence:         ConfidenceVerified,
		Verification:              AdapterVerification{RenderGolden: true, NoSecretLeak: true, ConfigMergeTest: true, LaunchSmokeTest: true},
		RenderModes:               []string{"env", "args"},
		LaunchSurfaces:            []string{"cli"},
		CanLaunch:                 true,
		CanLaunchArbitraryCommand: true,
		CanInjectSecrets:          true,
		CanPatchConfig:            false,
		CanManageModels:           true,
		CanIsolateProfile:         false,
		RequiresManualStep:        false,
		ValidationChecks:          []string{"render_golden", "no_secret_leak", "config_merge_or_not_applicable", "fake_launch_smoke"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatLocal},
	}
}

func (GenericOpenAIAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	if p.Command() == "" {
		return nil, fmt.Errorf("generic adapter requires a command")
	}
	return nil, nil
}

func (GenericOpenAIAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	if p.ModelID() != "" && prov.Auth.Type != "aws" {
		env[modelEnvVar(prov)] = p.ModelID()
	}
	cmd := p.Command()
	if cmd == "" {
		return nil, fmt.Errorf("no command for profile %s", p.Name)
	}
	return &LaunchStrategy{
		Plan: LaunchPlan{Command: cmd, Env: env, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{
			ID:                        "generic",
			DisplayName:               "Generic OpenAI-compatible",
			SupportLevel:              SupportFullEnv,
			CredentialControl:         CredentialEnvInjected,
			SupportConfidence:         ConfidenceManualProof,
			CanLaunchArbitraryCommand: true,
		},
	}, nil
}

// CrushAdapter renders config for Crush.
type CrushAdapter struct{}

func (CrushAdapter) ID() string             { return "crush" }
func (CrushAdapter) DisplayName() string    { return "Crush" }
func (CrushAdapter) DefaultCommand() string { return "crush" }

func (CrushAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI || p.Compatibility == provider.CompatLocal
}

func (CrushAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (CrushAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (CrushAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                "crush",
		DisplayName:       "Crush",
		DefaultCommand:    "crush",
		SupportLevel:      SupportEnvConfig,
		CredentialControl: CredentialConfigPatched,
		SupportConfidence: ConfidenceVerified,
		Verification:      AdapterVerification{RenderGolden: true, NoSecretLeak: true, ConfigMergeTest: true, LaunchSmokeTest: true},
		RenderModes:       []string{"env", "config_file"},
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true,
		CanInjectSecrets:  true,
		CanPatchConfig:    true,
		ConfigFiles: []ConfigFileContract{
			{Path: "$HOME/.config/crush/crush.json", Format: "json", Description: "Crush provider catalog"},
		},
		CanManageModels:    true,
		CanIsolateProfile:  false,
		RequiresManualStep: false,
		ValidationChecks:   []string{"config_no_raw_secret", "model_slots_validated", "backup_before_merge"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
			{Name: "catalog", Description: "Provider catalog", Optional: true},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatLocal},
	}
}

func (CrushAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return nil, nil
}

func (CrushAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	var files []FileWrite
	// Optionally write crush.json for custom providers.
	if prov.Slug != "openai" && prov.Slug != "anthropic" {
		files = append(files, buildCrushConfig(p, prov))
	}
	return &LaunchStrategy{
		Plan: LaunchPlan{Command: "crush", Env: env, Files: files, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{
			ID: "crush", DisplayName: "Crush", SupportLevel: SupportEnvConfig,
		},
	}, nil
}

// RenderCatalog implements ProviderCatalogAdapter for Crush. It writes a
// provider catalog containing metadata for all compatible providers and
// injects env vars with the actual secrets.
func (CrushAdapter) RenderCatalog(ctx ProviderCatalogRenderContext) (*LaunchStrategy, error) {
	env, err := buildCatalogEnv(ctx.Profile, ctx.Providers, ctx.KeysByProvider)
	if err != nil {
		return nil, err
	}

	file := buildCrushCatalogConfig(ctx)

	return &LaunchStrategy{
		Plan: LaunchPlan{
			Command: "crush",
			Env:     env,
			Files:   []FileWrite{file},
			Preview: []string{
				fmt.Sprintf("Launch %s with Crush", ctx.Profile.Name),
				fmt.Sprintf("Write Crush provider catalog: %d providers", len(ctx.Providers)),
				"Secrets remain env-only",
			},
		},
		Support: CrushAdapter{}.Contract(),
	}, nil
}

func buildCrushConfig(_ profile.Profile, prov provider.Provider) FileWrite {
	providers := map[string]any{
		prov.Slug: map[string]any{
			"id":          prov.Slug,
			"name":        prov.Name,
			"base_url":    prov.CanonicalBaseURL(),
			"api_key_env": prov.CanonicalEnvVar(),
		},
	}
	content, _ := json.MarshalIndent(map[string]any{
		"providers": providers,
		"options":   map[string]any{"disable_provider_auto_update": false},
	}, "", "  ")
	return FileWrite{
		Path:         "$HOME/.config/crush/crush.json",
		Format:       "json",
		Content:      string(content),
		Scope:        ScopeUser,
		MergePolicy:  MergeJSON,
		BackupPolicy: BackupRedacted,
		RedactCheck:  true,
		Description:  "Crush provider catalog; secrets remain env-only",
	}
}

// buildCrushCatalogConfig writes a Crush provider catalog containing metadata
// for all compatible providers. Secrets are NOT written here; they are
// injected via env vars at launch time.
func buildCrushCatalogConfig(ctx ProviderCatalogRenderContext) FileWrite {
	modelProviders := map[string]any{}

	for _, prov := range ctx.Providers {
		entry := map[string]any{
			"id":       prov.Slug,
			"name":     prov.DisplayName(),
			"base_url": prov.CanonicalBaseURL(),
		}
		// Only set api_key_env for providers that need a credential.
		// Local/no-auth providers (Ollama) must not have any credential field.
		if envVar := prov.CanonicalEnvVar(); envVar != "" {
			entry["api_key_env"] = envVar
		}

		switch prov.Compatibility {
		case provider.CompatOpenAI, provider.CompatLocal:
			entry["type"] = "openai-compatible"
		case provider.CompatAnthropic:
			entry["type"] = "anthropic"
		default:
			entry["type"] = string(prov.Compatibility)
		}

		if len(prov.Models) > 0 {
			models := make([]string, 0, len(prov.Models))
			for _, m := range prov.Models {
				models = append(models, m.ID)
			}
			entry["models"] = models
		}

		modelProviders[prov.Slug] = entry
	}

	cfg := map[string]any{
		"model_providers": modelProviders,
		"options":         map[string]any{"disable_provider_auto_update": false},
	}

	if ctx.SelectedProvider.Slug != "" {
		cfg["default_provider"] = ctx.SelectedProvider.Slug
	}
	if ctx.Profile.ModelID() != "" {
		cfg["default_model"] = ctx.Profile.ModelID()
	}

	content, _ := json.MarshalIndent(cfg, "", "  ")

	return FileWrite{
		Path:         "$HOME/.config/crush/crush.json",
		Format:       "json",
		Content:      string(content),
		Scope:        ScopeUser,
		MergePolicy:  MergeJSON,
		BackupPolicy: BackupRedacted,
		RedactCheck:  true,
		Description:  "Crush provider catalog; secrets remain env-only",
	}
}

// AiderAdapter renders config for Aider with main/weak/editor model slots.
type AiderAdapter struct{}

func (AiderAdapter) ID() string             { return "aider" }
func (AiderAdapter) DisplayName() string    { return "Aider" }
func (AiderAdapter) DefaultCommand() string { return "aider" }

func (AiderAdapter) SupportsProvider(p provider.Provider) bool { return true }

func (AiderAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (AiderAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (AiderAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                 "aider",
		DisplayName:        "Aider",
		DefaultCommand:     "aider",
		SupportLevel:       SupportFullEnv,
		CredentialControl:  CredentialEnvInjected,
		SupportConfidence:  ConfidenceVerified,
		Verification:       AdapterVerification{RenderGolden: true, NoSecretLeak: true, ConfigMergeTest: true, LaunchSmokeTest: true},
		RenderModes:        []string{"env", "args"},
		LaunchSurfaces:     []string{"cli"},
		CanLaunch:          true,
		CanInjectSecrets:   true,
		CanPatchConfig:     false,
		CanManageModels:    true,
		CanIsolateProfile:  false,
		RequiresManualStep: false,
		ValidationChecks:   []string{"env_injection_only", "model_slots_validated", "hazard_dotenv_shadowed"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary coding model"},
			{Name: "weak", Description: "Cheap/fast model for simple tasks", Optional: true},
			{Name: "editor", Description: "Model for edit operations", Optional: true},
		},
		Hazards: []Hazard{
			{
				Severity: "high",
				Title:    "Aider loads .env files with override behavior",
				Detail:   "Project .env can shadow AegisKeys-injected secrets. Use --env-file /dev/null to prevent shadowing.",
				Fix:      "Launch with --env-file /dev/null or remove conflicting keys from .env",
			},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic, provider.CompatGoogle, provider.CompatLocal},
	}
}

func (AiderAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	var warnings []string
	if p.ModelID() == "" {
		warnings = append(warnings, "no model selected; aider will use its default")
	}
	return warnings, nil
}

func (AiderAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}

	// Aider recognizes AIDER_OPENAI_API_BASE in .env/options docs.
	if prov.Compatibility == provider.CompatOpenAI || prov.Compatibility == provider.CompatLocal {
		env["AIDER_OPENAI_API_BASE"] = prov.CanonicalBaseURL()
	}

	args := []string{}

	// Avoid project .env overriding AegisKeys unless user opted into normal dotenv behavior.
	if p.Env["aider_disable_env_file"] != "false" {
		args = append(args, "--env-file", "/dev/null")
	}

	if m := p.Models.Main; m != nil {
		args = append(args, "--model", formatModelForAider(m.ID, prov))
	}
	if m := p.Models.Weak; m != nil {
		args = append(args, "--weak-model", formatModelForAider(m.ID, prov))
	}
	if m := p.Models.Editor; m != nil {
		args = append(args, "--editor-model", formatModelForAider(m.ID, prov))
	}

	return &LaunchStrategy{
		Plan: LaunchPlan{Command: "aider", Args: args, Env: env, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{
			ID: "aider", DisplayName: "Aider", SupportLevel: SupportFullEnv,
		},
		Hazards: []Hazard{
			{
				Severity: "high",
				Title:    "Aider loads .env files with override behavior",
				Detail:   "Project .env can shadow AegisKeys-injected secrets. Using --env-file /dev/null by default.",
				Fix:      "Set profile env aider_disable_env_file=false to allow normal dotenv behavior",
			},
		},
	}, nil
}

func formatModelForAider(modelID string, prov provider.Provider) string {
	// Only OpenRouter needs the "openrouter/" prefix for model IDs without a slash.
	if prov.Slug == "openrouter" && !strings.Contains(modelID, "/") {
		return "openrouter/" + modelID
	}
	return modelID
}

// ClineAdapter renders config for Cline with planner/actor model slots.
type ClineAdapter struct{}

func (ClineAdapter) ID() string             { return "cline" }
func (ClineAdapter) DisplayName() string    { return "Cline" }
func (ClineAdapter) DefaultCommand() string { return "cline" }

func (ClineAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI || p.Compatibility == provider.CompatAnthropic
}

func (ClineAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (ClineAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (ClineAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                "cline",
		DisplayName:       "Cline",
		DefaultCommand:    "cline",
		SupportLevel:      SupportEnvConfig,
		CredentialControl: CredentialConfigPatched,
		SupportConfidence: ConfidenceExperimental,
		RenderModes:       []string{"env", "config_file"},
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true,
		CanInjectSecrets:  true,
		CanPatchConfig:    true,
		ConfigFiles: []ConfigFileContract{
			{Path: "$HOME/.cline/data/settings/providers.json", Format: "json", Description: "Cline provider settings"},
		},
		CanManageModels:    true,
		CanIsolateProfile:  true,
		RequiresManualStep: false,
		ValidationChecks:   []string{"config_no_raw_secret", "model_slots_validated", "backup_before_merge"},
		ModelSlots: []ModelSlotContract{
			{Name: "planner", Description: "Planning model", Optional: true},
			{Name: "actor", Description: "Execution model", Optional: true},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic},
	}
}

func (ClineAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return nil, nil
}

func (ClineAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	var files []FileWrite

	// Cline needs a providers.json config file.
	if prov.Compatibility == provider.CompatOpenAI {
		files = append(files, buildClineConfig(p, prov))
	}

	return &LaunchStrategy{
		Plan: LaunchPlan{Command: "cline", Env: env, Files: files, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{
			ID: "cline", DisplayName: "Cline", SupportLevel: SupportEnvConfig,
		},
	}, nil
}

func buildClineConfig(p profile.Profile, prov provider.Provider) FileWrite {
	modelID := ""
	if p.Models.Main != nil {
		modelID = p.Models.Main.ID
	} else if p.Models.Planner != nil {
		modelID = p.Models.Planner.ID
	}
	content, _ := json.MarshalIndent(map[string]any{
		"apiProvider":     "openai-compatible",
		"openaiBaseUrl":   prov.CanonicalBaseURL(),
		"modelApiBaseUrl": prov.CanonicalBaseURL(),
		"openAiModelId":   modelID,
		"validateApiKey":  true,
	}, "", "  ")
	return FileWrite{
		Path:         "$HOME/.cline/data/settings/providers.json",
		Format:       "json",
		Content:      string(content),
		Scope:        ScopeUser,
		MergePolicy:  MergeJSON,
		BackupPolicy: BackupRedacted,
		RedactCheck:  true,
		Description:  "Cline provider settings; secrets remain env-only",
	}
}

// HermesAdapter renders config for Hermes.
type HermesAdapter struct{}

func (HermesAdapter) ID() string             { return "hermes" }
func (HermesAdapter) DisplayName() string    { return "Hermes" }
func (HermesAdapter) DefaultCommand() string { return "hermes" }

func (HermesAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI || p.Compatibility == provider.CompatLocal
}

func (HermesAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (HermesAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (HermesAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                "hermes",
		DisplayName:       "Hermes",
		DefaultCommand:    "hermes",
		SupportLevel:      SupportEnvConfig,
		CredentialControl: CredentialConfigPatched,
		SupportConfidence: ConfidenceExperimental,
		RenderModes:       []string{"env", "config_file"},
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true,
		CanInjectSecrets:  true,
		CanPatchConfig:    true,
		ConfigFiles: []ConfigFileContract{
			{Path: "$HOME/.hermes/config.yaml", Format: "yaml", Description: "Hermes model/provider config"},
		},
		CanManageModels:    true,
		CanIsolateProfile:  true,
		RequiresManualStep: false,
		ValidationChecks:   []string{"config_no_raw_secret", "model_slots_validated", "backup_before_merge", "hazard_dotenv_shadowed"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary inference model"},
			{Name: "compression", Description: "Context compression model", Optional: true},
			{Name: "vision", Description: "Vision model", Optional: true},
			{Name: "web_extract", Description: "Web page summarization model", Optional: true},
		},
		Hazards: []Hazard{
			{
				Severity: "high",
				Title:    "Hermes ~/.hermes/.env may shadow AegisKeys env",
				Detail:   "Hermes prefers ~/.hermes/.env over shell env. Use HERMES_HOME isolation to avoid conflicts.",
				Fix:      "Use isolated HERMES_HOME profile so ~/.hermes/.env cannot override AegisKeys.",
			},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatLocal},
	}
}

func (HermesAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return nil, nil
}

func (HermesAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}

	home := p.Env["HERMES_HOME"]
	if home == "" {
		home = filepath.Join(os.TempDir(), "aegiskeys-hermes", safeName(p.Name))
		env["HERMES_HOME"] = home
	}

	// Model and provider routing live exclusively in config.yaml — they are
	// NOT mirrored to env vars. Hermes reads config.yaml as the source of
	// truth for model/provider selection, and duplicating them as env vars
	// would only carry the main model (losing auxiliary slots like
	// compression/vision/web_extract) while creating a shadowing risk.
	// The only env-side concern is the API key credential (handled by
	// buildBaseEnv) and HERMES_HOME isolation.

	files := []FileWrite{
		buildHermesConfig(p, prov, home),
	}

	// Note: do NOT append p.Args here. The resolver appends profile args once.
	// Adapters may add app-required args (like "chat") but must not duplicate p.Args.
	return &LaunchStrategy{
		Plan: LaunchPlan{Command: "hermes", Env: env, Files: files, Preview: buildPreview(p.Name, prov),
			Args: []string{"chat"}},
		Support: AppSupportContract{
			ID: "hermes", DisplayName: "Hermes", SupportLevel: SupportEnvConfig,
		},
		Hazards: []Hazard{
			{
				Severity: "high",
				Title:    "Hermes ~/.hermes/.env may shadow AegisKeys env",
				Detail:   "Using HERMES_HOME isolation to prevent .env shadowing.",
				Fix:      "HERMES_HOME is set to an isolated profile directory.",
			},
		},
	}, nil
}

func hermesProviderID(prov provider.Provider) string {
	switch prov.Compatibility {
	case provider.CompatAnthropic:
		return "anthropic"
	case provider.CompatGoogle:
		return "google"
	case provider.CompatLocal:
		return "local"
	default:
		return prov.Slug
	}
}

func buildHermesConfig(p profile.Profile, prov provider.Provider, hermesHome string) FileWrite {
	cfg := map[string]any{
		"model": map[string]any{
			"provider": hermesProviderID(prov),
			"default":  modelID(p.Models.Main),
			"base_url": prov.CanonicalBaseURL(),
		},
	}

	aux := map[string]any{}
	if p.Models.Compression != nil {
		aux["compression"] = map[string]any{
			"provider": hermesProviderID(prov),
			"model":    p.Models.Compression.ID,
		}
	}
	if p.Models.Vision != nil {
		aux["vision"] = map[string]any{
			"provider": hermesProviderID(prov),
			"model":    p.Models.Vision.ID,
		}
	}
	if p.Models.WebExtract != nil {
		aux["web_extract"] = map[string]any{
			"provider": hermesProviderID(prov),
			"model":    p.Models.WebExtract.ID,
		}
	}
	if len(aux) > 0 {
		cfg["auxiliary"] = aux
	}

	content := mustYAML(cfg)

	return FileWrite{
		Path:         filepath.Join(hermesHome, "config.yaml"),
		Format:       "yaml",
		Content:      content,
		Scope:        ScopeProfile,
		MergePolicy:  MergeYAML,
		BackupPolicy: BackupRedacted,
		RedactCheck:  true,
		Description:  "Hermes model/provider config; secrets remain env-only",
	}
}

func modelID(m *profile.ModelRef) string {
	if m == nil {
		return ""
	}
	return m.ID
}

// mustYAML serializes to YAML. Falls back to simple format on error.
func mustYAML(v any) string {
	// Simple YAML serialization for our known structures.
	return yamlEncode(v)
}

func yamlEncode(v any) string {
	switch val := v.(type) {
	case map[string]any:
		var sb strings.Builder
		for k, v := range val {
			fmt.Fprintf(&sb, "%s:", k)
			switch inner := v.(type) {
			case string:
				fmt.Fprintf(&sb, " %q\n", inner)
			case map[string]any:
				sb.WriteString("\n")
				for ik, iv := range inner {
					switch siv := iv.(type) {
					case string:
						fmt.Fprintf(&sb, "  %s: %q\n", ik, siv)
					default:
						fmt.Fprintf(&sb, "  %s: %v\n", ik, iv)
					}
				}
			default:
				fmt.Fprintf(&sb, " %v\n", v)
			}
		}
		return sb.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// QwenCodeAdapter renders config for Qwen Code.
type QwenCodeAdapter struct{}

func (QwenCodeAdapter) ID() string             { return "qwen" }
func (QwenCodeAdapter) DisplayName() string    { return "Qwen Code" }
func (QwenCodeAdapter) DefaultCommand() string { return "qwen" }

func (QwenCodeAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI || p.Compatibility == provider.CompatLocal
}

func (QwenCodeAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (QwenCodeAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (QwenCodeAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                "qwen",
		DisplayName:       "Qwen Code",
		DefaultCommand:    "qwen",
		SupportLevel:      SupportEnvConfig,
		CredentialControl: CredentialConfigPatched,
		SupportConfidence: ConfidenceVerified,
		Verification:      AdapterVerification{RenderGolden: true, NoSecretLeak: true, ConfigMergeTest: true, LaunchSmokeTest: true},
		RenderModes:       []string{"env", "config_file"},
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true,
		CanInjectSecrets:  true,
		CanPatchConfig:    true,
		ConfigFiles: []ConfigFileContract{
			{Path: "$HOME/.qwen/settings.json", Format: "json", Description: "Qwen Code modelProviders catalog"},
		},
		CanManageModels:    true,
		CanIsolateProfile:  false,
		RequiresManualStep: false,
		ValidationChecks:   []string{"config_no_raw_secret", "model_slots_validated", "backup_before_merge"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
			{Name: "catalog", Description: "Model providers catalog", Optional: true},
			{Name: "fallbacks", Description: "Fallback models", Optional: true},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatLocal},
	}
}

func (QwenCodeAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	if p.Models.Main == nil || strings.TrimSpace(p.Models.Main.ID) == "" {
		return nil, fmt.Errorf("Qwen Code requires a main model")
	}
	return nil, nil
}

func (QwenCodeAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	var files []FileWrite
	files = append(files, buildQwenConfig(p, prov))
	return &LaunchStrategy{
		Plan: LaunchPlan{Command: "qwen", Env: env, Files: files, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{
			ID: "qwen", DisplayName: "Qwen Code", SupportLevel: SupportEnvConfig,
		},
	}, nil
}

// RenderCatalog implements ProviderCatalogAdapter for Qwen Code. It writes
// all compatible providers into the modelProviders section of settings.json,
// grouped by auth type. Each provider references its API key via envKey; the
// actual secrets are injected via env vars at launch time.
func (QwenCodeAdapter) RenderCatalog(ctx ProviderCatalogRenderContext) (*LaunchStrategy, error) {
	env, err := buildCatalogEnv(ctx.Profile, ctx.Providers, ctx.KeysByProvider)
	if err != nil {
		return nil, err
	}

	files := []FileWrite{
		buildQwenCatalogConfig(ctx),
	}

	return &LaunchStrategy{
		Plan: LaunchPlan{
			Command: "qwen",
			Env:     env,
			Files:   files,
			Preview: []string{
				fmt.Sprintf("Launch %s with Qwen Code", ctx.Profile.Name),
				fmt.Sprintf("Write Qwen Code modelProviders: %d providers", len(ctx.Providers)),
				"Secrets remain env-only via envKey references",
			},
		},
		Support: QwenCodeAdapter{}.Contract(),
	}, nil
}

func buildQwenConfig(p profile.Profile, prov provider.Provider) FileWrite {
	modelID := ""
	if p.Models.Main != nil {
		modelID = p.Models.Main.ID
	}
	models := []map[string]any{
		{
			"id":      modelID,
			"name":    modelID,
			"envKey":  prov.CanonicalEnvVar(),
			"baseUrl": prov.CanonicalBaseURL(),
			"generationConfig": map[string]any{
				"timeout":           120000,
				"maxRetries":        3,
				"contextWindowSize": 128000,
				"samplingParams": map[string]any{
					"temperature": 0.2,
					"max_tokens":  8192,
				},
			},
		},
	}
	content, _ := json.MarshalIndent(map[string]any{
		"env": map[string]string{
			prov.CanonicalEnvVar(): "$" + prov.CanonicalEnvVar(),
		},
		"modelProviders": map[string]any{
			"openai": map[string]any{
				"protocol": "openai",
				"models":   models,
			},
		},
	}, "", "  ")
	return FileWrite{
		Path:         "$HOME/.qwen/settings.json",
		Format:       "json",
		Content:      string(content),
		Scope:        ScopeUser,
		MergePolicy:  MergeJSON,
		BackupPolicy: BackupRedacted,
		RedactCheck:  true,
		Description:  "Qwen Code modelProviders catalog; secrets remain env-only",
	}
}

// qwenAuthType maps a provider's compatibility mode to Qwen Code's auth type key.
// Keys must be valid Qwen Code auth types; unknown types silently fail.
func qwenAuthType(compat provider.CompatibilityMode) string {
	switch compat {
	case provider.CompatOpenAI, provider.CompatLocal:
		return "openai"
	case provider.CompatAnthropic:
		return "anthropic"
	case provider.CompatGoogle:
		return "gemini"
	default:
		return "openai"
	}
}

// buildQwenCatalogConfig writes ALL compatible providers into Qwen Code's
// modelProviders section, grouped by auth type. Each provider becomes a model
// entry referencing its API key via envKey. Secrets are NOT written — they are
// injected via env vars at launch time.
//
// Gotchas handled:
//   - Duplicate (id + baseUrl) are deduped within each auth type (first wins).
//   - generationConfig is a sealed package per provider — all fields included.
//   - Auth type keys must be valid Qwen Code values (openai, anthropic, gemini).
func buildQwenCatalogConfig(ctx ProviderCatalogRenderContext) FileWrite {
	// Group providers by auth type.
	byAuthType := map[string][]provider.Provider{}
	for _, prov := range ctx.Providers {
		authType := qwenAuthType(prov.Compatibility)
		byAuthType[authType] = append(byAuthType[authType], prov)
	}

	modelProviders := map[string]any{}
	for authType, providers := range byAuthType {
		// Dedup by id + baseUrl within same auth type (first wins).
		seen := map[string]bool{}
		models := []map[string]any{}
		for _, prov := range providers {
			// Use slug as the model id (unique per provider in our registry).
			// Append baseUrl to create uniqueness when multiple providers
			// share the same slug (unlikely but safe).
			key := prov.Slug + "|" + prov.CanonicalBaseURL()
			if seen[key] {
				continue
			}
			seen[key] = true

			entry := map[string]any{
				"id":      prov.Slug,
				"name":    prov.DisplayName(),
				"baseUrl": prov.CanonicalBaseURL(),
			}
			// Only set envKey for providers that need a credential.
			// Local/no-auth providers must not carry a fake credential field.
			if envVar := prov.CanonicalEnvVar(); envVar != "" {
				entry["envKey"] = envVar
			}
			entry["generationConfig"] = map[string]any{
				"timeout":           120000,
				"maxRetries":        3,
				"contextWindowSize": 128000,
				"samplingParams": map[string]any{
					"temperature": 0.2,
					"max_tokens":  8192,
				},
			}
			models = append(models, entry)
		}
		if len(models) > 0 {
			modelProviders[authType] = map[string]any{
				"protocol": authType,
				"models":   models,
			}
		}
	}

	content, _ := json.MarshalIndent(map[string]any{
		"modelProviders": modelProviders,
	}, "", "  ")
	return FileWrite{
		Path:         "$HOME/.qwen/settings.json",
		Format:       "json",
		Content:      string(content),
		Scope:        ScopeUser,
		MergePolicy:  MergeJSON,
		BackupPolicy: BackupRedacted,
		RedactCheck:  true,
		Description:  "Qwen Code modelProviders catalog; secrets remain env-only",
	}
}

// ClaudeCodeAdapter renders config for Claude Code.
type ClaudeCodeAdapter struct{}

func (ClaudeCodeAdapter) ID() string             { return "claude" }
func (ClaudeCodeAdapter) DisplayName() string    { return "Claude Code" }
func (ClaudeCodeAdapter) DefaultCommand() string { return "claude" }

func (ClaudeCodeAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatAnthropic || p.Compatibility == provider.CompatOpenAI
}

func (ClaudeCodeAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (ClaudeCodeAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (ClaudeCodeAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                 "claude",
		DisplayName:        "Claude Code",
		DefaultCommand:     "claude",
		SupportLevel:       SupportFullEnv,
		CredentialControl:  CredentialEnvInjected,
		SupportConfidence:  ConfidenceVerified,
		Verification:       AdapterVerification{RenderGolden: true, NoSecretLeak: true, ConfigMergeTest: true, LaunchSmokeTest: true},
		RenderModes:        []string{"env"},
		LaunchSurfaces:     []string{"cli"},
		CanLaunch:          true,
		CanInjectSecrets:   true,
		CanPatchConfig:     false,
		CanManageModels:    true,
		CanIsolateProfile:  false,
		RequiresManualStep: false,
		ValidationChecks:   []string{"env_injection_only", "model_slots_validated"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model (Sonnet equivalent)"},
			{Name: "fast", Description: "Fast model (Haiku equivalent)", Optional: true},
			{Name: "planner", Description: "Complex model (Opus equivalent)", Optional: true},
			{Name: "subagent", Description: "Subagent model", Optional: true},
		},
		Hazards: []Hazard{
			{
				Severity: "warn",
				Title:    "Claude Code OAuth/persisted auth may shadow AegisKeys",
				Detail:   "If Claude Code has existing OAuth tokens, they may take precedence over injected env vars.",
				Fix:      "Use API key mode or clear existing auth if injection does not work.",
			},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatAnthropic, provider.CompatOpenAI},
	}
}

func (ClaudeCodeAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	var warnings []string
	if p.ModelID() == "" {
		warnings = append(warnings, "no main model selected; Claude Code will use its default (claude-sonnet-4-5)")
	}
	if prov.Compatibility == provider.CompatOpenAI && prov.Slug != "openrouter" {
		warnings = append(warnings, "using OpenAI-compatible provider with Claude Code may require gateway support")
	}
	return warnings, nil
}

func (ClaudeCodeAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}

	if p.Models.Main != nil && p.Models.Main.ID != "" {
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = p.Models.Main.ID
	}
	if p.Models.Fast != nil && p.Models.Fast.ID != "" {
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = p.Models.Fast.ID
	}
	if p.Models.Planner != nil && p.Models.Planner.ID != "" {
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = p.Models.Planner.ID
	}
	if p.Models.Subagent != nil && p.Models.Subagent.ID != "" {
		env["CLAUDE_CODE_SUBAGENT_MODEL"] = p.Models.Subagent.ID
	}

	baseURL := prov.CanonicalBaseURL()
	// The Anthropic SDK natively appends "/v1/messages" to the base URL.
	// If the provider's canonical URL already ends in "/v1", we must trim it
	// to prevent requests going to "/v1/v1/messages" (which 404s on OpenRouter).
	baseURL, _ = strings.CutSuffix(baseURL, "/v1")

	switch prov.Compatibility {
	case provider.CompatAnthropic:
		if prov.CanonicalEnvVar() != "" && key != nil {
			env["ANTHROPIC_API_KEY"] = key.Secret
		}
		env["ANTHROPIC_BASE_URL"] = baseURL
	case provider.CompatOpenAI:
		if prov.Auth.Type == "aws" {
			// Bedrock: the AWS SDK reads AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
			// / AWS_REGION injected by buildBaseEnv. Do not overwrite them with
			// Anthropic-specific variables.
			break
		}
		// Claude Code has strict validation for ANTHROPIC_API_KEY (must be sk-ant-*).
		// For non-Anthropic providers, using ANTHROPIC_AUTH_TOKEN bypasses this format check.
		delete(env, prov.CanonicalEnvVar())
		if key != nil {
			env["ANTHROPIC_AUTH_TOKEN"] = key.Secret
			env["ANTHROPIC_API_KEY"] = ""
		}
		env["ANTHROPIC_BASE_URL"] = baseURL
	}

	return &LaunchStrategy{
		Plan: LaunchPlan{Command: "claude", Env: env, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{
			ID: "claude", DisplayName: "Claude Code", SupportLevel: SupportFullEnv,
		},
		Hazards: []Hazard{
			{
				Severity: "warn",
				Title:    "Claude Code OAuth/persisted auth may shadow AegisKeys",
				Detail:   "If Claude Code has existing OAuth tokens, they may take precedence over injected env vars.",
				Fix:      "Use API key mode or clear existing auth if injection does not work.",
			},
		},
	}, nil
}

// MistralVibeAdapter renders config for Mistral Vibe.
type MistralVibeAdapter struct{}

func (MistralVibeAdapter) ID() string             { return "vibe" }
func (MistralVibeAdapter) DisplayName() string    { return "Mistral Vibe" }
func (MistralVibeAdapter) DefaultCommand() string { return "vibe" }

func (MistralVibeAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI || p.Compatibility == provider.CompatLocal
}

func (MistralVibeAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (MistralVibeAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (MistralVibeAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                "vibe",
		DisplayName:       "Mistral Vibe",
		DefaultCommand:    "vibe",
		SupportLevel:      SupportEnvConfig,
		CredentialControl: CredentialConfigPatched,
		SupportConfidence: ConfidenceExperimental,
		RenderModes:       []string{"env", "config_file"},
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true,
		CanInjectSecrets:  true,
		CanPatchConfig:    true,
		ConfigFiles: []ConfigFileContract{
			{Path: "$HOME/.vibe/config.toml", Format: "toml", Description: "Mistral Vibe provider/model config"},
		},
		CanManageModels:    true,
		CanIsolateProfile:  false,
		RequiresManualStep: false,
		ValidationChecks:   []string{"config_no_raw_secret", "model_slots_validated", "backup_before_merge"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatLocal},
	}
}

func (MistralVibeAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	return nil, nil
}

func (MistralVibeAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	var files []FileWrite
	files = append(files, buildVibeConfig(p, prov))
	return &LaunchStrategy{
		Plan: LaunchPlan{Command: "vibe", Env: env, Files: files, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{
			ID: "vibe", DisplayName: "Mistral Vibe", SupportLevel: SupportEnvConfig,
		},
	}, nil
}

func buildVibeConfig(p profile.Profile, prov provider.Provider) FileWrite {
	modelID := "mistral-large-latest"
	if p.Models.Main != nil {
		modelID = p.Models.Main.ID
	}
	content := fmt.Sprintf(`[[providers]]
name = %q
api_base = %q
api_key_env_var = %q
api_style = "openai"
backend = "generic"

[[models]]
name = %q
provider = %q
alias = "aegiskeys-model"
temperature = 0.2
input_price = 0.0
output_price = 0.0

active_model = "aegiskeys-model"
`, prov.Slug, prov.CanonicalBaseURL(), prov.CanonicalEnvVar(), modelID, prov.Slug)
	return FileWrite{
		Path:         "$HOME/.vibe/config.toml",
		Format:       "toml",
		Content:      content,
		Scope:        ScopeUser,
		MergePolicy:  MergeTOML,
		BackupPolicy: BackupRedacted,
		RedactCheck:  true,
		Description:  "Mistral Vibe provider/model config; secrets remain env-only",
	}
}

// GooseAdapter renders config for Goose.
type GooseAdapter struct{}

func (GooseAdapter) ID() string             { return "goose" }
func (GooseAdapter) DisplayName() string    { return "Goose" }
func (GooseAdapter) DefaultCommand() string { return "goose" }

func (GooseAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI || p.Compatibility == provider.CompatLocal
}

func (GooseAdapter) CanInjectCredential(p provider.Provider) bool  { return true }
func (GooseAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (GooseAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                "goose",
		DisplayName:       "Goose",
		DefaultCommand:    "goose",
		SupportLevel:      SupportEnvConfig,
		CredentialControl: CredentialConfigPatched,
		SupportConfidence: ConfidenceVerified,
		Verification:      AdapterVerification{RenderGolden: true, NoSecretLeak: true, ConfigMergeTest: true, LaunchSmokeTest: true},
		RenderModes:       []string{"env", "config_file"},
		LaunchSurfaces:    []string{"cli"},
		CanLaunch:         true,
		CanInjectSecrets:  true,
		CanPatchConfig:    true,
		ConfigFiles: []ConfigFileContract{
			{Path: "$HOME/.config/goose/config.yaml", Format: "yaml", Description: "Goose provider/model config"},
		},
		CanManageModels:    true,
		CanIsolateProfile:  false,
		RequiresManualStep: false,
		ValidationChecks:   []string{"config_no_raw_secret", "model_slots_validated", "backup_before_merge"},
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Primary model"},
			{Name: "fast", Description: "Fast/cheap model", Optional: true},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatLocal},
	}
}

func (GooseAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	if p.Models.Main == nil || strings.TrimSpace(p.Models.Main.ID) == "" {
		return nil, fmt.Errorf("Goose requires a main model")
	}
	return nil, nil
}

func (GooseAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}
	env["GOOSE_PROVIDER"] = "openai"
	if p.Models.Main != nil {
		env["GOOSE_MODEL"] = p.Models.Main.ID
	}
	if p.Models.Fast != nil {
		env["GOOSE_FAST_MODEL"] = p.Models.Fast.ID
	}
	env["GOOSE_PROVIDER__HOST"] = prov.CanonicalBaseURL()

	var files []FileWrite
	files = append(files, buildGooseConfig(p, prov))

	return &LaunchStrategy{
		Plan: LaunchPlan{Command: "goose", Env: env, Files: files, Preview: buildPreview(p.Name, prov)},
		Support: AppSupportContract{
			ID: "goose", DisplayName: "Goose", SupportLevel: SupportEnvConfig,
		},
	}, nil
}

func buildGooseConfig(p profile.Profile, prov provider.Provider) FileWrite {
	mainModel := ""
	if p.Models.Main != nil {
		mainModel = p.Models.Main.ID
	}
	fastModel := ""
	if p.Models.Fast != nil {
		fastModel = p.Models.Fast.ID
	}
	content := fmt.Sprintf(`GOOSE_PROVIDER: openai
GOOSE_MODEL: %s
GOOSE_FAST_MODEL: %s
GOOSE_PROVIDER__HOST: %s
`, mainModel, fastModel, prov.CanonicalBaseURL())
	return FileWrite{
		Path:           "$HOME/.config/goose/config.yaml",
		Format:         "yaml",
		Content:        content,
		Scope:          ScopeUser,
		MergePolicy:    MergeYAML,
		BackupPolicy:   BackupRedacted,
		RedactCheck:    true,
		Description:    "Goose provider/model config; secrets remain env-only",
		ManagedBlockID: safeName(p.Name),
	}
}

// --- Advanced / GUI adapters ---

// ZedAdapter renders config for Zed IDE (advanced GUI support).
type ZedAdapter struct{}

func (ZedAdapter) ID() string             { return "zed" }
func (ZedAdapter) DisplayName() string    { return "Zed IDE" }
func (ZedAdapter) DefaultCommand() string { return "zed" }

func (ZedAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI ||
		p.Compatibility == provider.CompatAnthropic ||
		p.Compatibility == provider.CompatGoogle ||
		p.Compatibility == provider.CompatLocal
}

func (ZedAdapter) CanInjectCredential(p provider.Provider) bool {
	// Zed stores keys in system keychain, not env.
	return false
}

func (ZedAdapter) CanConfigureProvider(p provider.Provider) bool { return true }

func (ZedAdapter) Contract() AppSupportContract {
	hazards := []Hazard{
		{
			Severity: "critical",
			Title:    "macOS app bundle may not inherit env vars",
			Detail:   "Zed on macOS launched from Finder/Dock does not reliably inherit CLI env vars.",
			Fix:      "Launch Zed from the terminal or use keychain-based credential setup.",
		},
		{
			Severity: "high",
			Title:    "Running Zed process will not pick up new env vars",
			Detail:   "Env vars are read at process start. A running Zed instance will not see new AegisKeys env.",
			Fix:      "Restart Zed after launching with AegisKeys.",
		},
	}
	if runtime.GOOS == "darwin" {
		hazards = append(hazards, Hazard{
			Severity: "critical",
			Title:    "macOS detected: env injection is unreliable for GUI app bundles",
			Detail:   "Use keychain setup or launch Zed from terminal with `open -a Zed`.",
			Fix:      "Launch from terminal or configure credentials in Zed Agent Settings.",
		})
	}
	return AppSupportContract{
		ID:                "zed",
		DisplayName:       "Zed IDE",
		DefaultCommand:    "zed",
		SupportLevel:      SupportConfigKeychain,
		CredentialControl: CredentialKeychainHint,
		SupportConfidence: ConfidenceGuided,
		RenderModes:       []string{"env", "config_file", "keychain_handoff"},
		LaunchSurfaces:    []string{"gui", "ide"},
		CanLaunch:         true,
		CanInjectSecrets:  false,
		CanPatchConfig:    true,
		ConfigFiles: []ConfigFileContract{
			{Path: "$HOME/.config/zed/settings.json", Format: "jsonc", Description: "Zed agent model settings"},
		},
		CanManageModels:    true,
		CanIsolateProfile:  false,
		RequiresManualStep: true,
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Default agent model"},
			{Name: "inline_assistant", Description: "Inline assistant model", Optional: true},
			{Name: "subagent", Description: "Subagent model", Optional: true},
			{Name: "commit_message", Description: "Commit message model", Optional: true},
			{Name: "thread_summary", Description: "Thread summary model", Optional: true},
			{Name: "alternatives", Description: "Inline alternatives", Optional: true},
		},
		Hazards:               hazards,
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic, provider.CompatGoogle, provider.CompatLocal},
	}
}

func (ZedAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	var warnings []string
	if runtime.GOOS == "darwin" {
		warnings = append(warnings,
			"Zed macOS app bundle may not inherit AegisKeys env vars; use keychain/API setup or a verified isolated config strategy")
	}
	if p.Models.Main == nil {
		warnings = append(warnings, "no agent.default_model configured")
	}
	return warnings, nil
}

func (ZedAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		return nil, err
	}

	// Build a settings.json patch for model slots.
	patch := buildZedSettingsPatch(p, prov)

	return &LaunchStrategy{
		Plan: LaunchPlan{
			Command: "zed",
			Env:     env,
			Files:   []FileWrite{patch},
			Preview: buildPreview(p.Name, prov),
		},
		Support: AppSupportContract{
			ID: "zed", DisplayName: "Zed IDE", SupportLevel: SupportConfigKeychain,
		},
		ManualSteps: []ManualStep{
			{
				Title:       "Confirm Zed provider credentials",
				Description: "Zed may use system keychain credentials or env vars depending on OS and launch path. On macOS app-bundle launches, env injection may not reach the GUI process.",
				When:        "before_launch",
			},
		},
		Hazards: []Hazard{
			{
				Severity: "critical",
				Title:    "macOS app bundle may not inherit env vars",
				Detail:   "Zed on macOS launched from Finder/Dock does not reliably inherit CLI env vars.",
				Fix:      "Launch Zed from the terminal or use keychain-based credential setup.",
			},
			{
				Severity: "high",
				Title:    "Running Zed process will not pick up new env vars",
				Detail:   "Env vars are read at process start. A running Zed instance will not see new AegisKeys env.",
				Fix:      "Restart Zed after launching with AegisKeys.",
			},
		},
	}, nil
}

func buildZedSettingsPatch(p profile.Profile, prov provider.Provider) FileWrite {
	providerKey := "aegiskeys-router"
	languageModels := map[string]any{}
	agent := map[string]any{}

	if p.Models.Main != nil {
		agent["default_model"] = map[string]any{
			"provider": providerKey,
			"model":    p.Models.Main.ID,
		}
		languageModels["openai_compatible"] = map[string]any{
			providerKey: map[string]any{
				"api_url": prov.CanonicalBaseURL(),
				"available_models": []map[string]any{
					{"name": p.Models.Main.ID, "max_tokens": 200000},
				},
			},
		}
	}

	if p.Models.InlineAssistant != nil {
		agent["inline_assistant_model"] = map[string]any{
			"provider": providerKey,
			"model":    p.Models.InlineAssistant.ID,
		}
	}
	if p.Models.Subagent != nil {
		agent["subagent_model"] = map[string]any{
			"provider": providerKey,
			"model":    p.Models.Subagent.ID,
		}
	}
	if p.Models.CommitMessage != nil {
		agent["commit_message_model"] = map[string]any{
			"provider": providerKey,
			"model":    p.Models.CommitMessage.ID,
		}
	}
	if p.Models.ThreadSummary != nil {
		agent["thread_summary_model"] = map[string]any{
			"provider": providerKey,
			"model":    p.Models.ThreadSummary.ID,
		}
	}

	if len(p.Models.Alternatives) > 0 {
		alts := make([]map[string]any, 0, len(p.Models.Alternatives))
		for _, alt := range p.Models.Alternatives {
			alts = append(alts, map[string]any{
				"provider": providerKey,
				"model":    alt.ID,
			})
		}
		agent["inline_alternatives"] = alts
	}

	settings := map[string]any{}
	if len(languageModels) > 0 {
		settings["language_models"] = languageModels
	}
	if len(agent) > 0 {
		settings["agent"] = agent
	}

	content, _ := json.MarshalIndent(settings, "", "  ")

	return FileWrite{
		Path:         "$HOME/.config/zed/settings.json",
		Format:       "jsonc",
		Content:      string(content),
		Scope:        ScopeUser,
		MergePolicy:  MergeJSONC,
		BackupPolicy: BackupRedacted,
		RedactCheck:  true,
		Description:  "Zed agent model settings; no raw API keys written",
	}
}

// IntelliJAdapter renders config for IntelliJ IDEA (launcher/config isolation only).
type IntelliJAdapter struct{}

func (IntelliJAdapter) ID() string             { return "intellij" }
func (IntelliJAdapter) DisplayName() string    { return "IntelliJ IDEA" }
func (IntelliJAdapter) DefaultCommand() string { return "idea" }

func (IntelliJAdapter) SupportsProvider(p provider.Provider) bool {
	return p.Compatibility == provider.CompatOpenAI ||
		p.Compatibility == provider.CompatAnthropic ||
		p.Compatibility == provider.CompatGoogle ||
		p.Compatibility == provider.CompatLocal
}

func (IntelliJAdapter) CanInjectCredential(p provider.Provider) bool {
	// IntelliJ AI credentials are PasswordSafe/keychain driven, not env-var driven.
	return false
}

func (IntelliJAdapter) CanConfigureProvider(p provider.Provider) bool {
	// Partial: can guide/patch settings where schema is known.
	return true
}

func (IntelliJAdapter) Contract() AppSupportContract {
	return AppSupportContract{
		ID:                "intellij",
		DisplayName:       "IntelliJ IDEA",
		DefaultCommand:    "idea",
		SupportLevel:      SupportLauncherIsolation,
		CredentialControl: CredentialManualLogin,
		SupportConfidence: ConfidenceGuided,
		RenderModes:       []string{"env", "config_file"},
		LaunchSurfaces:    []string{"ide"},
		CanLaunch:         true,
		CanInjectSecrets:  false,
		CanPatchConfig:    true,
		ConfigFiles: []ConfigFileContract{
			{Path: "$HOME/.config/JetBrains/idea.vmoptions", Format: "text", Description: "Isolated IntelliJ launcher profile"},
		},
		CanManageModels:    true,
		CanIsolateProfile:  true,
		RequiresManualStep: true,
		ModelSlots: []ModelSlotContract{
			{Name: "main", Description: "Preferred chat model"},
		},
		Hazards: []Hazard{
			{
				Severity: "critical",
				Title:    "AI provider API keys are not env-var-driven",
				Detail:   "Use IntelliJ UI/PasswordSafe; AegisKeys can only isolate config/JVM launcher state.",
				Fix:      "Enter API key through IntelliJ AI Assistant provider settings after launch.",
			},
		},
		AcceptedCompatibility: []provider.CompatibilityMode{provider.CompatOpenAI, provider.CompatAnthropic, provider.CompatGoogle, provider.CompatLocal},
	}
}

func (IntelliJAdapter) Validate(p profile.Profile, prov provider.Provider) ([]string, error) {
	var warnings []string
	warnings = append(warnings, "IntelliJ AI credentials must be entered through the IDE UI or PasswordSafe")
	return warnings, nil
}

func (IntelliJAdapter) Render(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (*LaunchStrategy, error) {
	env := map[string]string{}

	profileDir := p.Env["AEGISKEYS_IDEA_PROFILE_DIR"]
	if profileDir == "" {
		profileDir = p.Env["KNOXKEYS_IDEA_PROFILE_DIR"] // legacy alias
	}
	if profileDir == "" {
		profileDir = filepath.Join("$HOME", ".local/share/aegiskeys/apps/intellij", safeName(p.Name))
	}

	vmopts := filepath.Join(profileDir, "idea.vmoptions")
	env["IDEA_VM_OPTIONS"] = vmopts

	files := []FileWrite{
		{
			Path:   vmopts,
			Format: "text",
			Content: strings.Join([]string{
				"-Didea.config.path=" + filepath.Join(profileDir, "config"),
				"-Didea.system.path=" + filepath.Join(profileDir, "system"),
				"-Didea.plugins.path=" + filepath.Join(profileDir, "plugins"),
				"-Didea.log.path=" + filepath.Join(profileDir, "log"),
			}, "\n") + "\n",
			Scope:        ScopeProfile,
			MergePolicy:  MergeNone,
			BackupPolicy: BackupRedacted,
			RedactCheck:  true,
			Description:  "Isolated IntelliJ launcher profile; no credentials written",
		},
	}

	return &LaunchStrategy{
		Plan: LaunchPlan{
			Command: "idea",
			// Do NOT pass p.Args here — the resolver appends profile args once.
			Env:     env,
			Files:   files,
			Preview: buildPreview(p.Name, prov),
		},
		Support: AppSupportContract{
			ID: "intellij", DisplayName: "IntelliJ IDEA", SupportLevel: SupportLauncherIsolation,
		},
		ManualSteps: []ManualStep{
			{
				Title:       "Enter API key in IntelliJ",
				Description: "This IDE stores AI provider credentials in PasswordSafe/keychain. AegisKeys can guide and isolate config, but cannot directly inject the provider API key into IntelliJ AI.",
				When:        "after_first_launch",
			},
		},
		Hazards: []Hazard{
			{
				Severity: "critical",
				Title:    "Manual credential handoff",
				Detail:   "Provider API key must be entered through the IDE UI or keychain-backed PasswordSafe.",
				Fix:      "Use the guided setup panel after launch.",
			},
		},
	}, nil
}

// --- Helpers ---

// buildBaseEnv creates provider-level env vars for any profile. Profile-level
// env overrides are applied once in ResolveLaunchStrategyForMode after adapter
// rendering, so the resolver remains the canonical overlay point.
func buildBaseEnv(_ profile.Profile, prov provider.Provider, key *secret.SecretRecord) (map[string]string, error) {
	env := make(map[string]string)

	secretName := prov.CanonicalEnvVar()

	// 1. Provider primary secret. Enforce the secret's access policy before
	// ever handing the raw material to child-process env: a secret that
	// forbids launch injection must not be injected even if the adapter can.
	if secretName != "" && key != nil {
		if err := key.AllowAccess(secret.AccessInjectEnv); err != nil {
			return nil, fmt.Errorf("secret %q policy blocks launch injection: %w", key.ID, err)
		}
		env[secretName] = key.Secret
	}

	// 2. provider non-secret compatibility env.
	for k, v := range prov.ExtraEnv {
		if looksSecretName(k) {
			return nil, fmt.Errorf("provider %q ExtraEnv %q looks secret; store it as a key", prov.Slug, k)
		}
		env[k] = v
	}

	// 3. provider setup params: non-secret fields and secondary secrets.
	// These carry the values a provider needs beyond the primary secret
	// (e.g. Azure resource/deployment/api-version, Bedrock secret access key
	// + region). Secondary secrets come from key.ExtraSecrets; non-secret
	// fields come from key.Fields.
	if key != nil {
		for _, sp := range prov.Setup {
			if sp.EnvVar == "" {
				continue
			}
			if sp.Secret {
				for _, ns := range key.ExtraSecrets {
					if ns.Key == sp.Key && ns.Secret != "" {
						env[sp.EnvVar] = ns.Secret
						break
					}
				}
				continue
			}
			if sp.Endpoint {
				// Inject the resolved endpoint URL rather than the raw field.
				env[sp.EnvVar] = prov.ResolveEndpoint(key.Fields)
				continue
			}
			if v, ok := key.Fields[sp.Key]; ok && v != "" {
				env[sp.EnvVar] = v
			}
		}
	}

	return env, nil
}

// buildCatalogEnv injects one env var per catalog provider that has a key.
// Config-file apps (Crush, OpenCode, MiMo) reference these env var names
// in their provider catalog; the actual secrets stay out of the config file.
func buildCatalogEnv(
	p profile.Profile,
	providers []provider.Provider,
	keys map[string]*secret.SecretRecord,
) (map[string]string, error) {
	env := make(map[string]string)

	for _, prov := range providers {
		envName := prov.CanonicalEnvVar()
		if envName == "" {
			continue // local/no-auth provider
		}

		key := keys[prov.Slug]
		if key == nil {
			continue
		}

		if err := key.AllowAccess(secret.AccessInjectEnv); err != nil {
			return nil, fmt.Errorf("key %s cannot be launch-injected: %w", key.ID, err)
		}

		// Prevent two providers from fighting over the same env var with different values.
		if existing, ok := env[envName]; ok && existing != key.Secret {
			return nil, fmt.Errorf(
				"providers share env var %s with different keys; give them unique env vars",
				envName,
			)
		}

		env[envName] = key.Secret
	}

	// Allow non-secret profile env only.
	for k, v := range p.Env {
		if looksSecretName(k) {
			return nil, fmt.Errorf("profile env %q looks credential-like; store it as a key", k)
		}
		env[k] = v
	}

	return env, nil
}

// looksSecretName reports whether an env var name looks credential-like.
func looksSecretName(k string) bool {
	if sensitive.IsAllowedNonSecretEnv(k) {
		return false
	}
	return sensitive.IsSecretName(k)
}

// buildBaseEnvChecked is like buildBaseEnv but returns an invalid-profile error
// suitable for short-circuiting a Render. If the profile env is safe, it returns
// the env map unchanged.
func buildBaseEnvChecked(p profile.Profile, prov provider.Provider, key *secret.SecretRecord) (map[string]string, error) {
	return buildBaseEnv(p, prov, key)
}

// buildPreview creates human-readable summary lines.
func buildPreview(profileName string, prov provider.Provider) []string {
	return []string{
		"Profile: " + profileName,
		"Provider: " + prov.Name + " (" + string(prov.Compatibility) + ")",
	}
}
