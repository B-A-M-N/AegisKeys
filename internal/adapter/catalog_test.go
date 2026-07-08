package adapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

type catalogGoldenSnapshot struct {
	App        string              `json:"app"`
	Command    string              `json:"command"`
	Args       []string            `json:"args,omitempty"`
	EnvKeys    []string            `json:"env_keys"`
	Files      []catalogGoldenFile `json:"files,omitempty"`
	Preview    []string            `json:"preview,omitempty"`
	ConfigBody string              `json:"config_body,omitempty"`
}

type catalogGoldenFile struct {
	Path        string `json:"path"`
	Format      string `json:"format"`
	Scope       string `json:"scope"`
	MergePolicy string `json:"merge_policy"`
	Description string `json:"description,omitempty"`
}

func TestCrushCatalogRender_MultipleProviders(t *testing.T) {
	reg := provider.NewRegistry()
	openai := provider.Provider{
		ID: "openai", Name: "OpenAI", Slug: "openai",
		BaseURL:       "https://api.openai.com/v1",
		EnvVar:        "OPENAI_API_KEY",
		Compatibility: provider.CompatOpenAI,
		Models: []provider.ProviderModel{
			{ID: "gpt-4o", Name: "GPT-4o"},
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini"},
		},
	}
	openrouter := provider.Provider{
		ID: "openrouter", Name: "OpenRouter", Slug: "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		EnvVar:        "OPENROUTER_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	ollama := provider.Provider{
		ID: "ollama", Name: "Ollama", Slug: "ollama",
		BaseURL:       "http://localhost:11434/v1",
		Compatibility: provider.CompatLocal,
	}
	reg.Providers = []provider.Provider{openai, openrouter, ollama}

	vault := &secret.Vault{
		Keys: []secret.SecretRecord{
			{ID: "key_openai_1", ProviderSlug: "openai", Label: "openai-main", Secret: "sk-openai-secret-123", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
			{ID: "key_openrouter_1", ProviderSlug: "openrouter", Label: "openrouter-main", Secret: "sk-or-secret-456", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
		},
	}

	registry := NewRegistry()
	prof := profile.Profile{
		Name:         "test-catalog",
		ProviderSlug: "openrouter",
		KeyID:        "key_openrouter_1",
		Target:       profile.TargetConfig{App: "crush"},
	}
	selectedProv := reg.Find("openrouter")
	if selectedProv == nil {
		t.Fatal("openrouter provider not found")
	}
	selectedKey := vault.Get("key_openrouter_1")
	if selectedKey == nil {
		t.Fatal("openrouter key not found")
	}

	strategy, err := ResolveLaunchStrategyCatalog(
		prof, *selectedProv, selectedKey,
		registry, reg, vault, ResolvePreview,
	)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategyCatalog failed: %v", err)
	}

	// Check that the config file contains all compatible providers
	if len(strategy.Plan.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(strategy.Plan.Files))
	}
	content := strategy.Plan.Files[0].Content
	for _, slug := range []string{"openai", "openrouter"} {
		if !strings.Contains(content, slug) {
			t.Errorf("config missing provider %q:\n%s", slug, content)
		}
	}

	// Verify NO raw secrets in config
	for _, key := range vault.Keys {
		if strings.Contains(content, key.Secret) {
			t.Errorf("config contains raw secret for key %s", key.ID)
		}
	}

	// Check that env vars for ALL providers are injected
	if strategy.Plan.Env["OPENAI_API_KEY"] != "sk-openai-secret-123" {
		t.Errorf("OPENAI_API_KEY not injected correctly: %q", strategy.Plan.Env["OPENAI_API_KEY"])
	}
	if strategy.Plan.Env["OPENROUTER_API_KEY"] != "sk-or-secret-456" {
		t.Errorf("OPENROUTER_API_KEY not injected correctly: %q", strategy.Plan.Env["OPENROUTER_API_KEY"])
	}
}

func TestCrushCatalogRender_MissingSelectedProviderKey(t *testing.T) {
	reg := provider.NewRegistry()
	openrouter := provider.Provider{
		ID: "openrouter", Name: "OpenRouter", Slug: "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		EnvVar:        "OPENROUTER_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	reg.Providers = []provider.Provider{openrouter}

	// Empty vault — no keys at all
	vault := &secret.Vault{}

	registry := NewRegistry()
	prof := profile.Profile{
		Name:         "test-missing-key",
		ProviderSlug: "openrouter",
		Target:       profile.TargetConfig{App: "crush"},
	}
	selectedProv := reg.Find("openrouter")
	if selectedProv == nil {
		t.Fatal("openrouter not found")
	}

	strategy, err := ResolveLaunchStrategyCatalog(
		prof, *selectedProv, nil,
		registry, reg, vault, ResolvePreview,
	)
	if err != nil {
		t.Fatalf("catalog preview should include providers without keys: %v", err)
	}
	if len(strategy.Plan.Files) == 0 || !strings.Contains(strategy.Plan.Files[0].Content, "openrouter") {
		t.Fatalf("catalog config should include selected provider without key: %#v", strategy.Plan.Files)
	}
}

// TestCrushCatalog_LocalProviderNoCredentialField verifies that local/no-auth
// providers (Ollama) appear in catalog config WITHOUT any fake credential field
// — no env var reference, no placeholder, nothing.
func TestCrushCatalog_LocalProviderNoCredentialField(t *testing.T) {
	reg := provider.NewRegistry()
	openai := provider.Provider{
		ID: "openai", Name: "OpenAI", Slug: "openai",
		BaseURL:       "https://api.openai.com/v1",
		EnvVar:        "OPENAI_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	ollama := provider.Provider{
		ID: "ollama", Name: "Ollama", Slug: "ollama",
		BaseURL:       "http://localhost:11434/v1",
		Compatibility: provider.CompatLocal,
	}
	reg.Providers = []provider.Provider{openai, ollama}

	vault := &secret.Vault{
		Keys: []secret.SecretRecord{
			{ID: "key_1", ProviderSlug: "openai", Label: "openai", Secret: "sk-openai", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
		},
	}

	registry := NewRegistry()
	prof := profile.Profile{
		Name:         "test-local",
		ProviderSlug: "openai",
		KeyID:        "key_1",
		Target:       profile.TargetConfig{App: "crush"},
	}
	selectedProv := reg.Find("openai")
	selectedKey := vault.Get("key_1")

	strategy, err := ResolveLaunchStrategyCatalog(
		prof, *selectedProv, selectedKey,
		registry, reg, vault, ResolvePreview,
	)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategyCatalog: %v", err)
	}

	if len(strategy.Plan.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(strategy.Plan.Files))
	}
	content := strategy.Plan.Files[0].Content

	// Ollama (local) must have no credential-related field in config.
	if strings.Contains(content, `"ollama"`) {
		idx := strings.Index(content, `"ollama"`)
		block := content[idx:]
		if end := strings.Index(block, "}"); end > 0 {
			block = block[:end]
		}
		for _, field := range []string{"api_key_env", "apiKey", "api_key"} {
			if strings.Contains(block, field) {
				t.Errorf("local provider ollama has fake credential field %q in config block: %s", field, block)
			}
		}
	}

	// OPENAI (remote) should have its api_key_env set.
	if !strings.Contains(content, "OPENAI_API_KEY") {
		t.Errorf("remote provider openai missing env var reference: %s", content)
	}
}

// TestQwenCatalog_LocalProviderNoCredentialField verifies that Qwen Code's
// catalog config omits credential fields for local providers.
func TestQwenCatalog_LocalProviderNoCredentialField(t *testing.T) {
	reg := provider.NewRegistry()
	ollama := provider.Provider{
		ID: "ollama", Name: "Ollama", Slug: "ollama",
		BaseURL:       "http://localhost:11434/v1",
		Compatibility: provider.CompatLocal,
	}
	openrouter := provider.Provider{
		ID: "openrouter", Name: "OpenRouter", Slug: "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		EnvVar:        "OPENROUTER_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	reg.Providers = []provider.Provider{ollama, openrouter}

	vault := &secret.Vault{
		Keys: []secret.SecretRecord{
			{ID: "key_1", ProviderSlug: "openrouter", Label: "or", Secret: "sk-or", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
		},
	}

	registry := NewRegistry()
	prof := profile.Profile{
		Name:         "test-qwen-local",
		ProviderSlug: "openrouter",
		KeyID:        "key_1",
		Target:       profile.TargetConfig{App: "qwen"},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}},
	}
	selectedProv := reg.Find("openrouter")
	selectedKey := vault.Get("key_1")

	strategy, err := ResolveLaunchStrategyCatalog(
		prof, *selectedProv, selectedKey,
		registry, reg, vault, ResolvePreview,
	)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategyCatalog: %v", err)
	}

	if len(strategy.Plan.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(strategy.Plan.Files))
	}
	content := strategy.Plan.Files[0].Content

	// Parse the JSON to inspect the ollama entry structurally.
	var cfg map[string]any
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatalf("parse qwen config: %v", err)
	}
	providersObj, ok := cfg["modelProviders"].(map[string]any)
	if !ok {
		t.Fatalf("modelProviders not an object: %T", cfg["modelProviders"])
	}
	openaiSection, ok := providersObj["openai"].(map[string]any)
	if !ok {
		t.Fatalf("openai section not an object: %T", providersObj["openai"])
	}
	models, ok := openaiSection["models"].([]any)
	if !ok {
		t.Fatalf("models not an array: %T", openaiSection["models"])
	}

	var openrouterHasEnvKey bool
	for _, m := range models {
		entry, ok := m.(map[string]any)
		if !ok {
			continue
		}
		id, _ := entry["id"].(string)
		if id == "ollama" {
			// Local provider must not have any credential field.
			for _, field := range []string{"envKey", "apiKey", "api_key", "base_url"} {
				if _, exists := entry[field]; exists && field != "base_url" {
					t.Errorf("local provider ollama has credential field %q in config entry: %#v", field, entry)
				}
			}
		}
		if id == "openrouter" {
			if envKey, exists := entry["envKey"].(string); exists && envKey == "OPENROUTER_API_KEY" {
				openrouterHasEnvKey = true
			}
		}
	}
	if !openrouterHasEnvKey {
		t.Errorf("remote provider openrouter missing envKey=OPENROUTER_API_KEY in Qwen catalog config")
	}
}

func TestBuildCatalogEnv_DuplicateEnvVarDifferentSecrets(t *testing.T) {
	// Two providers sharing the same env var with different secret values
	prov1 := provider.Provider{Slug: "prov-a", EnvVar: "SHARED_KEY", Compatibility: provider.CompatOpenAI}
	prov2 := provider.Provider{Slug: "prov-b", EnvVar: "SHARED_KEY", Compatibility: provider.CompatOpenAI}
	keys := map[string]*secret.SecretRecord{
		"prov-a": {ID: "k1", Secret: "secret-a", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
		"prov-b": {ID: "k2", Secret: "secret-b", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
	}

	_, err := buildCatalogEnv(profile.Profile{}, []provider.Provider{prov1, prov2}, keys)
	if err == nil {
		t.Error("expected error for duplicate env var with different secrets")
	}
}

func TestBuildCatalogProviders_IncludesCompatibleProvidersWithoutKeys(t *testing.T) {
	reg := provider.NewRegistry()
	local := provider.Provider{
		ID: "ollama", Name: "Ollama", Slug: "ollama",
		BaseURL:       "http://localhost:11434/v1",
		Compatibility: provider.CompatLocal,
	}
	remote := provider.Provider{
		ID: "openai", Name: "OpenAI", Slug: "openai",
		BaseURL:       "https://api.openai.com/v1",
		EnvVar:        "OPENAI_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	reg.Providers = []provider.Provider{local, remote}

	vault := &secret.Vault{}

	registry := NewRegistry()
	adapter, ok := registry.Get("crush")
	if !ok {
		t.Fatal("crush adapter not found")
	}

	providers, keys, err := buildCatalogProviders(adapter, reg, vault)
	if err != nil {
		t.Fatalf("buildCatalogProviders failed: %v", err)
	}

	foundLocal := false
	foundRemote := false
	for _, p := range providers {
		if p.Slug == "ollama" {
			foundLocal = true
		}
		if p.Slug == "openai" {
			foundRemote = true
		}
	}
	if !foundLocal {
		t.Error("ollama should be included without a key")
	}
	if !foundRemote {
		t.Error("openai should be included even when AegisKeys has no key")
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

func TestMiMoCatalogRender_MultipleProviders(t *testing.T) {
	reg := provider.NewRegistry()
	openai := provider.Provider{
		ID: "openai", Name: "OpenAI", Slug: "openai",
		BaseURL:       "https://api.openai.com/v1",
		EnvVar:        "OPENAI_API_KEY",
		Compatibility: provider.CompatOpenAI,
		Models: []provider.ProviderModel{
			{ID: "gpt-4o", Name: "GPT-4o"},
		},
	}
	ollama := provider.Provider{
		ID: "ollama", Name: "Ollama", Slug: "ollama",
		BaseURL:       "http://localhost:11434/v1",
		Compatibility: provider.CompatLocal,
	}
	reg.Providers = []provider.Provider{openai, ollama}

	vault := &secret.Vault{
		Keys: []secret.SecretRecord{
			{ID: "key_openai_1", ProviderSlug: "openai", Label: "openai-main", Secret: "sk-openai-secret-xyz", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
		},
	}

	registry := NewRegistry()
	prof := profile.Profile{
		Name:         "test-mimo-catalog",
		ProviderSlug: "openai",
		KeyID:        "key_openai_1",
		Target:       profile.TargetConfig{App: "mimo"},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}},
	}
	selectedProv := reg.Find("openai")
	if selectedProv == nil {
		t.Fatal("openai provider not found")
	}
	selectedKey := vault.Get("key_openai_1")
	if selectedKey == nil {
		t.Fatal("openai key not found")
	}

	strategy, err := ResolveLaunchStrategyCatalog(
		prof, *selectedProv, selectedKey,
		registry, reg, vault, ResolvePreview,
	)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategyCatalog failed: %v", err)
	}

	if len(strategy.Plan.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(strategy.Plan.Files))
	}
	content := strategy.Plan.Files[0].Content

	// Config should contain both providers
	if !strings.Contains(content, "openai") {
		t.Errorf("config missing openai provider:\n%s", content)
	}
	if !strings.Contains(content, "ollama") {
		t.Errorf("config missing ollama provider:\n%s", content)
	}

	// Config should declare env var names, not raw secrets.
	if !strings.Contains(content, `"env":`) || !strings.Contains(content, `"OPENAI_API_KEY"`) {
		t.Errorf("config should declare OPENAI_API_KEY via env list:\n%s", content)
	}
	if strings.Contains(content, `"apiKey"`) {
		t.Errorf("config should not write apiKey option:\n%s", content)
	}
	if strings.Contains(content, "sk-openai-secret-xyz") {
		t.Errorf("config contains raw secret:\n%s", content)
	}

	// Env should contain the actual secret
	if strategy.Plan.Env["OPENAI_API_KEY"] != "sk-openai-secret-xyz" {
		t.Errorf("OPENAI_API_KEY not injected correctly: %q", strategy.Plan.Env["OPENAI_API_KEY"])
	}
}

func TestQwenCodeCatalogRender_MultipleProviders(t *testing.T) {
	reg := provider.NewRegistry()
	openai := provider.Provider{
		ID: "openai", Name: "OpenAI", Slug: "openai",
		BaseURL:       "https://api.openai.com/v1",
		EnvVar:        "OPENAI_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	openrouter := provider.Provider{
		ID: "openrouter", Name: "OpenRouter", Slug: "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		EnvVar:        "OPENROUTER_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	ollama := provider.Provider{
		ID: "ollama", Name: "Ollama", Slug: "ollama",
		BaseURL:       "http://localhost:11434/v1",
		Compatibility: provider.CompatLocal,
	}
	reg.Providers = []provider.Provider{openai, openrouter, ollama}

	vault := &secret.Vault{
		Keys: []secret.SecretRecord{
			{ID: "key_openai_1", ProviderSlug: "openai", Label: "openai-main", Secret: "sk-openai-secret", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
			{ID: "key_openrouter_1", ProviderSlug: "openrouter", Label: "or-main", Secret: "sk-or-secret", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
		},
	}

	registry := NewRegistry()
	prof := profile.Profile{
		Name:         "test-qwen-catalog",
		ProviderSlug: "openai",
		KeyID:        "key_openai_1",
		Target:       profile.TargetConfig{App: "qwen"},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}},
	}
	selectedProv := reg.Find("openai")
	if selectedProv == nil {
		t.Fatal("openai provider not found")
	}
	selectedKey := vault.Get("key_openai_1")
	if selectedKey == nil {
		t.Fatal("openai key not found")
	}

	strategy, err := ResolveLaunchStrategyCatalog(
		prof, *selectedProv, selectedKey,
		registry, reg, vault, ResolvePreview,
	)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategyCatalog failed: %v", err)
	}

	if len(strategy.Plan.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(strategy.Plan.Files))
	}
	content := strategy.Plan.Files[0].Content

	// Config should contain providers grouped under "openai" auth type
	if !strings.Contains(content, `"openai"`) {
		t.Errorf("config missing openai auth type:\n%s", content)
	}

	// Config should reference env keys, not raw secrets
	if !strings.Contains(content, "OPENAI_API_KEY") {
		t.Errorf("config missing OPENAI_API_KEY envKey:\n%s", content)
	}
	if !strings.Contains(content, "OPENROUTER_API_KEY") {
		t.Errorf("config missing OPENROUTER_API_KEY envKey:\n%s", content)
	}
	for _, key := range vault.Keys {
		if strings.Contains(content, key.Secret) {
			t.Errorf("config contains raw secret for key %s", key.ID)
		}
	}

	// Env should contain the actual secrets
	if strategy.Plan.Env["OPENAI_API_KEY"] != "sk-openai-secret" {
		t.Errorf("OPENAI_API_KEY not injected: %q", strategy.Plan.Env["OPENAI_API_KEY"])
	}
	if strategy.Plan.Env["OPENROUTER_API_KEY"] != "sk-or-secret" {
		t.Errorf("OPENROUTER_API_KEY not injected: %q", strategy.Plan.Env["OPENROUTER_API_KEY"])
	}
}

func TestCodexCatalogRender_MultipleProviders(t *testing.T) {
	reg := provider.NewRegistry()
	openai := provider.Provider{
		ID: "openai", Name: "OpenAI", Slug: "openai",
		BaseURL:       "https://api.openai.com/v1",
		EnvVar:        "OPENAI_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	openrouter := provider.Provider{
		ID: "openrouter", Name: "OpenRouter", Slug: "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		EnvVar:        "OPENROUTER_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	reg.Providers = []provider.Provider{openai, openrouter}

	vault := &secret.Vault{
		Keys: []secret.SecretRecord{
			{ID: "key_openai_1", ProviderSlug: "openai", Label: "openai-main", Secret: "sk-openai-codex", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
			{ID: "key_openrouter_1", ProviderSlug: "openrouter", Label: "or-main", Secret: "sk-or-codex", Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)},
		},
	}

	registry := NewRegistry()
	prof := profile.Profile{
		Name:         "test-codex-catalog",
		ProviderSlug: "openai",
		KeyID:        "key_openai_1",
		Target:       profile.TargetConfig{App: "codex"},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}},
	}
	selectedProv := reg.Find("openai")
	if selectedProv == nil {
		t.Fatal("openai provider not found")
	}
	selectedKey := vault.Get("key_openai_1")
	if selectedKey == nil {
		t.Fatal("openai key not found")
	}

	strategy, err := ResolveLaunchStrategyCatalog(
		prof, *selectedProv, selectedKey,
		registry, reg, vault, ResolvePreview,
	)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategyCatalog failed: %v", err)
	}

	// Codex uses CODEX_HOME profile approach (not Files).
	// Verify CODEX_HOME is set to a temp dir containing the profile.
	if strategy.Plan.Env["CODEX_HOME"] == "" {
		t.Fatal("CODEX_HOME not set in env")
	}

	// Read the generated config from the profile path.
	profilePath := filepath.Join(strategy.Plan.Env["CODEX_HOME"], "aegiskeys.config.toml")
	contentBytes, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read codex profile: %v", err)
	}
	content := string(contentBytes)

	// Config should contain model_providers section with both providers.
	// TOML quotes keys containing special chars, so match with quotes.
	if !strings.Contains(content, `[model_providers."openai"]`) {
		t.Errorf("config missing [model_providers.openai]:\n%s", content)
	}
	if !strings.Contains(content, `[model_providers."openrouter"]`) {
		t.Errorf("config missing [model_providers.openrouter]:\n%s", content)
	}

	// wire_api MUST be "responses"
	if !strings.Contains(content, `wire_api = "responses"`) {
		t.Errorf("config missing wire_api = responses:\n%s", content)
	}

	// Config should NOT contain raw secrets
	for _, key := range vault.Keys {
		if strings.Contains(content, key.Secret) {
			t.Errorf("config contains raw secret for key %s:\n%s", key.ID, content)
		}
	}

	// Env should contain the actual secrets
	if strategy.Plan.Env["OPENAI_API_KEY"] != "sk-openai-codex" {
		t.Errorf("OPENAI_API_KEY not injected: %q", strategy.Plan.Env["OPENAI_API_KEY"])
	}
	if strategy.Plan.Env["OPENROUTER_API_KEY"] != "sk-or-codex" {
		t.Errorf("OPENROUTER_API_KEY not injected: %q", strategy.Plan.Env["OPENROUTER_API_KEY"])
	}
}

func TestCatalogVerificationGoldens(t *testing.T) {
	cases := []string{"crush", "mimo", "opencode"}
	for _, app := range cases {
		t.Run(app, func(t *testing.T) {
			providerReg, vault := catalogVerificationRegistry()
			adapterReg := NewRegistry()
			prof := profile.Profile{
				Name:         "catalog-" + app,
				ProviderSlug: "openrouter",
				KeyID:        "key_openrouter_catalog",
				Target:       profile.TargetConfig{App: app},
				Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "anthropic/claude-sonnet-4.5"}},
			}
			selectedProv := providerReg.Find("openrouter")
			if selectedProv == nil {
				t.Fatal("openrouter provider not found")
			}
			selectedKey := vault.Get("key_openrouter_catalog")
			if selectedKey == nil {
				t.Fatal("openrouter key not found")
			}

			strategy, err := ResolveLaunchStrategyCatalog(
				prof, *selectedProv, selectedKey,
				adapterReg, providerReg, vault, ResolveRun,
			)
			if err != nil {
				t.Fatalf("ResolveLaunchStrategyCatalog: %v", err)
			}
			assertNoCatalogRawSecret(t, strategy, vault)
			assertCatalogConfigWrites(t, strategy, vault)
			assertCatalogGoldenSnapshot(t, app, snapshotCatalogStrategy(app, strategy))
		})
	}
}

func catalogVerificationRegistry() (*provider.Registry, *secret.Vault) {
	reg := provider.NewRegistry()
	openai := provider.Provider{
		ID:            "openai",
		Name:          "OpenAI",
		Slug:          "openai",
		BaseURL:       "https://api.openai.com/v1",
		EnvVar:        "OPENAI_API_KEY",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		Compatibility: provider.CompatOpenAI,
		Models:        []provider.ProviderModel{{ID: "gpt-4o"}},
	}
	openrouter := provider.Provider{
		ID:            "openrouter",
		Name:          "OpenRouter",
		Slug:          "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		EnvVar:        "OPENROUTER_API_KEY",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENROUTER_API_KEY"},
		Compatibility: provider.CompatOpenAI,
		Models:        []provider.ProviderModel{{ID: "anthropic/claude-sonnet-4.5"}},
	}
	ollama := provider.Provider{
		ID:            "ollama",
		Name:          "Ollama",
		Slug:          "ollama",
		BaseURL:       "http://localhost:11434/v1",
		Compatibility: provider.CompatLocal,
		Models:        []provider.ProviderModel{{ID: "qwen2.5-coder"}},
	}
	openai.Normalize()
	openrouter.Normalize()
	ollama.Normalize()
	reg.Providers = []provider.Provider{openai, openrouter, ollama}

	vault := &secret.Vault{
		Keys: []secret.SecretRecord{
			{
				ID:           "key_openai_catalog",
				ProviderSlug: "openai",
				Label:        "openai-catalog",
				Secret:       "AK_CATALOG_OPENAI_SECRET_1234567890",
				Kind:         secret.SecretAPIKey,
				Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
			},
			{
				ID:           "key_openrouter_catalog",
				ProviderSlug: "openrouter",
				Label:        "openrouter-catalog",
				Secret:       "AK_CATALOG_OPENROUTER_SECRET_1234567890",
				Kind:         secret.SecretAPIKey,
				Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
			},
		},
	}
	return reg, vault
}

func assertNoCatalogRawSecret(t *testing.T, strategy *LaunchStrategy, vault *secret.Vault) {
	t.Helper()
	for _, rec := range vault.Keys {
		if containsRawSecretInArgs(strategy.Plan.Args, rec.Secret) {
			t.Fatalf("raw secret for %s leaked into args", rec.ID)
		}
		if containsRawSecretInPreview(strategy.Plan.Preview, rec.Secret) {
			t.Fatalf("raw secret for %s leaked into preview", rec.ID)
		}
		if containsRawSecretInFiles(strategy.Plan.Files, rec.Secret) {
			t.Fatalf("raw secret for %s leaked into files", rec.ID)
		}
	}
}

func assertCatalogConfigWrites(t *testing.T, strategy *LaunchStrategy, vault *secret.Vault) {
	t.Helper()
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	xdg := filepath.Join(tmp, "xdg")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(xdg, 0700); err != nil {
		t.Fatal(err)
	}
	env := copyEnv(strategy.Plan.Env)
	env["HOME"] = home
	env["XDG_CONFIG_HOME"] = xdg
	env["TMPDIR"] = tmp
	if err := ApplyFileWrites(strategy.Plan.Files, env); err != nil {
		t.Fatalf("fresh catalog config write failed: %v", err)
	}
	if err := ApplyFileWrites(strategy.Plan.Files, env); err != nil {
		t.Fatalf("second catalog config merge failed: %v", err)
	}
	for _, rec := range vault.Keys {
		if leak := findRawSecretInTree(t, tmp, rec.Secret); leak != "" {
			t.Fatalf("raw secret for %s leaked to config tree: %s", rec.ID, leak)
		}
	}
}

func snapshotCatalogStrategy(app string, strategy *LaunchStrategy) catalogGoldenSnapshot {
	s := catalogGoldenSnapshot{
		App:     app,
		Command: strategy.Plan.Command,
		Args:    append([]string{}, strategy.Plan.Args...),
		EnvKeys: sortedKeys(strategy.Plan.Env),
		Preview: append([]string{}, strategy.Plan.Preview...),
	}
	for _, f := range strategy.Plan.Files {
		s.Files = append(s.Files, catalogGoldenFile{
			Path:        f.Path,
			Format:      f.Format,
			Scope:       string(f.Scope),
			MergePolicy: string(f.MergePolicy),
			Description: f.Description,
		})
		if s.ConfigBody == "" {
			s.ConfigBody = f.Content
		}
	}
	sort.Slice(s.Files, func(i, j int) bool { return s.Files[i].Path < s.Files[j].Path })
	return s
}

func assertCatalogGoldenSnapshot(t *testing.T, app string, got catalogGoldenSnapshot) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "adapter_golden", app+".catalog.golden.json")
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if os.Getenv("UPDATE_CATALOG_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(data) {
		t.Fatalf("catalog golden mismatch for %s\nwant:\n%s\ngot:\n%s", app, want, data)
	}
}
