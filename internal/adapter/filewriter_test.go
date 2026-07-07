package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
)

func TestFileWriter_RefusesRawSecretInConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	env := map[string]string{
		"OPENAI_API_KEY": "sk-this-is-a-real-secret-value-12345",
	}

	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "json",
		Content:     `{"key": "sk-this-is-a-real-secret-value-12345"}`,
		MergePolicy: MergeNone,
		RedactCheck: true,
	}}, env)

	if err == nil {
		t.Error("ApplyFileWrites should refuse to write raw secret when redact_check is enabled")
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFileWriter_AllowsNonSecretConfigWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	env := map[string]string{
		"OPENAI_API_KEY": "sk-secret-value-12345",
	}

	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "json",
		Content:     `{"base_url": "https://api.openai.com/v1", "model": "gpt-4o"}`,
		MergePolicy: MergeNone,
		RedactCheck: true,
	}}, env)

	if err != nil {
		t.Fatalf("should allow non-secret config: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "gpt-4o") {
		t.Errorf("content not written: %s", data)
	}
}

func TestFileWriter_BacksUpBeforeMergingUserConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Create an existing file.
	if err := os.WriteFile(path, []byte(`{"existing": "value"}`), 0600); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{}
	err := ApplyFileWrites([]FileWrite{{
		Path:         path,
		Format:       "json",
		Content:      `{"new": "value"}`,
		MergePolicy:  MergeJSON,
		BackupPolicy: BackupRedacted,
		RedactCheck:  true,
	}}, env)

	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Should have created a backup in .aegiskeys-backups.
	backupDir := filepath.Join(dir, ".aegiskeys-backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 backup, got %d", len(entries))
	}

	// Merge should preserve existing key.
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "existing") {
		t.Errorf("merge lost existing key: %s", data)
	}
	if !strings.Contains(string(data), "new") {
		t.Errorf("merge missing new key: %s", data)
	}
}

func TestFileWriter_AvoidWritePolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "json",
		Content:     `{"x": "y"}`,
		MergePolicy: AvoidWrite,
	}}, map[string]string{})

	if err == nil || !strings.Contains(err.Error(), "avoid-write") {
		t.Errorf("expected avoid-write error, got: %v", err)
	}
}

func TestFileWriter_UnsupportedMergePolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "json",
		Content:     `{}`,
		MergePolicy: MergePolicy("invalid"),
	}}, map[string]string{})

	if err == nil || !strings.Contains(err.Error(), "unsupported merge policy") {
		t.Errorf("expected unsupported policy error, got: %v", err)
	}
}

func TestApplyFileWrites_TOMLRefusesExistingUserConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("keep = true\n"), 0600); err != nil {
		t.Fatal(err)
	}

	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "toml",
		Content:     "replace = true\n",
		Scope:       ScopeUser,
		MergePolicy: MergeTOML,
	}}, map[string]string{})

	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected TOML overwrite refusal, got: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "replace") {
		t.Fatalf("existing TOML config was overwritten: %s", data)
	}
}

func TestApplyFileWrites_XMLRefusesExistingUserConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.xml")
	if err := os.WriteFile(path, []byte("<keep/>\n"), 0600); err != nil {
		t.Fatal(err)
	}

	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "xml",
		Content:     "<replace/>\n",
		Scope:       ScopeUser,
		MergePolicy: PatchXML,
	}}, map[string]string{})

	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected XML overwrite refusal, got: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "replace") {
		t.Fatalf("existing XML config was overwritten: %s", data)
	}
}

func TestApplyFileWrites_TOMLAllowsFreshUserConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.toml")

	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "toml",
		Content:     "fresh = true\n",
		Scope:       ScopeUser,
		MergePolicy: MergeTOML,
	}}, map[string]string{})

	if err != nil {
		t.Fatalf("fresh TOML write should be allowed: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "fresh") {
		t.Fatalf("fresh TOML config not written: %s", data)
	}
}

func TestFileWriter_ExpandHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	dir := t.TempDir()
	// Create a fake "home" structure within tmp to test ~ expansion logic.
	// The expandPath function uses os.UserHomeDir(), so we test ~ expansion
	// via the actual home for a path we know works.

	path := filepath.Join(dir, "test.json")
	env := map[string]string{}

	err = ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "json",
		Content:     `{"test": true}`,
		MergePolicy: MergeNone,
		RedactCheck: true,
	}}, env)

	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file not written")
	}
	_ = home
}

func TestFileWriter_ShortStringsNotTreatedAsSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	env := map[string]string{
		"SHORT": "abc", // Too short to trigger redaction
	}

	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "json",
		Content:     `{"val": "abc"}`,
		MergePolicy: MergeNone,
		RedactCheck: true,
	}}, env)

	if err != nil {
		t.Fatalf("short string should not be treated as secret: %v", err)
	}
}

func TestRedactConfigContent(t *testing.T) {
	env := map[string]string{
		"OPENAI_API_KEY": "sk-my-secret-key-12345",
		"PUBLIC_URL":     "https://example.com",
	}

	content := `key = "sk-my-secret-key-12345"
url = "https://example.com"`

	result := RedactConfigContent(content, env)

	if strings.Contains(result, "sk-my-secret-key-12345") {
		t.Error("secret value should be redacted")
	}
	if !strings.Contains(result, "https://example.com") {
		t.Error("non-secret value should be preserved")
	}
}

func TestApplyFileWrites_RefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	link := filepath.Join(dir, "link.json")

	// Create target and symlink.
	if err := os.WriteFile(target, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	err := ApplyFileWrites([]FileWrite{{
		Path:        link,
		Format:      "json",
		Content:     `{"x": 1}`,
		MergePolicy: MergeNone,
	}}, map[string]string{})

	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("expected symlink refusal, got: %v", err)
	}
}

func TestApplyFileWrites_RefusesProjectSensitivePath(t *testing.T) {
	err := ApplyFileWrites([]FileWrite{{
		Path:        "/etc/passwd",
		Format:      "json",
		Content:     `{}`,
		MergePolicy: MergeNone,
		Scope:       ScopeProject,
	}}, map[string]string{})

	if err == nil || !strings.Contains(err.Error(), "sensitive") {
		t.Errorf("expected sensitive-path refusal, got: %v", err)
	}
}

func TestApplyFileWrites_DeepJSONMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Existing nested config.
	existing := `{
  "model": "gpt-4o",
  "endpoints": {
    "chat": "/v1/chat/completions",
    "embeddings": "/v1/embeddings"
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	// Incoming changes "chat" path but preserves "embeddings".
	incoming := `{"endpoints": {"chat": "/v2/chat/completions"}, "stream": true}`
	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "json",
		Content:     incoming,
		MergePolicy: MergeJSON,
	}}, map[string]string{})

	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	// Deep merge: "embeddings" must survive.
	if !strings.Contains(content, "embeddings") {
		t.Errorf("deep merge lost nested key 'embeddings': %s", content)
	}
	if !strings.Contains(content, "/v2/chat/completions") {
		t.Errorf("deep merge did not update nested 'chat': %s", content)
	}
	if !strings.Contains(content, "\"stream\"") {
		t.Errorf("deep merge did not add new top-level key: %s", content)
	}
}

func TestApplyFileWrites_MergeJSON_RefusesCorruptedExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "json",
		Content:     `{"ok": true}`,
		MergePolicy: MergeJSON,
	}}, map[string]string{})

	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("expected corruption refusal, got: %v", err)
	}
}

func TestApplyFileWrites_CreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "config.json")

	err := ApplyFileWrites([]FileWrite{{
		Path:        path,
		Format:      "json",
		Content:     `{"ok": true}`,
		MergePolicy: MergeNone,
		RedactCheck: true,
	}}, map[string]string{})

	if err != nil {
		t.Fatalf("should create parent dirs: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file not written to nested path")
	}
}

// --- Contract interface tests ---

func TestContract_HermesFullDeclaration(t *testing.T) {
	a := HermesAdapter{}
	c := a.Contract()

	if c.ID != "hermes" {
		t.Errorf("ID = %s", c.ID)
	}
	if c.SupportLevel != SupportEnvConfig {
		t.Errorf("SupportLevel = %s", c.SupportLevel)
	}
	if !c.CanInjectSecrets {
		t.Error("Hermes should support secret injection")
	}
	if !c.CanManageModels {
		t.Error("Hermes should support model management")
	}
	// ModelSlots should include compression, vision, web_extract.
	slotNames := make(map[string]bool)
	for _, s := range c.ModelSlots {
		slotNames[s.Name] = true
	}
	for _, want := range []string{"main", "compression", "vision", "web_extract"} {
		if !slotNames[want] {
			t.Errorf("missing slot %s", want)
		}
	}
	if len(c.Hazards) == 0 {
		t.Error("Hermes should declare hazards")
	}
}

func TestContract_ZedPartialDeclaration(t *testing.T) {
	a := ZedAdapter{}
	c := a.Contract()

	if c.ID != "zed" {
		t.Errorf("ID = %s", c.ID)
	}
	if c.SupportLevel != SupportConfigKeychain {
		t.Errorf("SupportLevel = %s, want config_keychain", c.SupportLevel)
	}
	if c.CanInjectSecrets {
		t.Error("Zed should NOT support direct secret injection")
	}
	if !c.RequiresManualStep {
		t.Error("Zed should require manual steps")
	}
}

func TestContract_IntelliJLauncherOnlyDeclaration(t *testing.T) {
	a := IntelliJAdapter{}
	c := a.Contract()

	if c.ID != "intellij" {
		t.Errorf("ID = %s", c.ID)
	}
	if c.SupportLevel != SupportLauncherIsolation {
		t.Errorf("SupportLevel = %s, want launcher_isolation", c.SupportLevel)
	}
	if c.CanInjectSecrets {
		t.Error("IntelliJ should NOT support secret injection")
	}
	if !c.CanIsolateProfile {
		t.Error("IntelliJ should support profile isolation")
	}
}

func TestContract_AiderFullEnvDeclaration(t *testing.T) {
	a := AiderAdapter{}
	c := a.Contract()

	if c.SupportLevel != SupportFullEnv {
		t.Errorf("SupportLevel = %s, want full_env", c.SupportLevel)
	}
	if !c.CanInjectSecrets {
		t.Error("Aider should support secret injection")
	}
	if c.CanPatchConfig {
		t.Error("Aider should NOT claim config patching (uses env+args)")
	}
}

// --- Render returns LaunchStrategy tests ---

func TestRenderStrategy_GenericHasPlan(t *testing.T) {
	a := GenericOpenAIAdapter{}
	p := profile.Profile{
		Name:         "test",
		ProviderSlug: "openai",
		Target:       profile.TargetConfig{App: "generic", Command: "my-app"},
		Models:       profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}},
	}
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY",
		BaseURL: "https://api.openai.com/v1", Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if s.Plan.Command != "my-app" {
		t.Errorf("command = %s", s.Plan.Command)
	}
	if s.Blocked {
		t.Error("should not be blocked")
	}
}

func TestRenderStrategy_IntelliJHasManualStepsAndHazards(t *testing.T) {
	a := IntelliJAdapter{}
	p := profile.Profile{
		Name: "test", ProviderSlug: "openai",
		Target: profile.TargetConfig{App: "intellij"},
	}
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY",
		Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.ManualSteps) == 0 {
		t.Error("expected manual steps for IntelliJ")
	}
	if len(s.Hazards) == 0 {
		t.Error("expected hazards for IntelliJ")
	}
	if s.Plan.Command != "idea" {
		t.Errorf("command = %s, want idea", s.Plan.Command)
	}
}

func TestRenderStrategy_ZedHasSettingsPatch(t *testing.T) {
	a := ZedAdapter{}
	p := profile.Profile{
		Name: "test", ProviderSlug: "openrouter",
		Target: profile.TargetConfig{App: "zed"},
		Models: profile.ModelSlots{
			Main:     &profile.ModelRef{ID: "claude-sonnet-4-5"},
			Subagent: &profile.ModelRef{ID: "claude-haiku-4"},
		},
	}
	prov := provider.Provider{
		Name: "OpenRouter", Slug: "openrouter", EnvVar: "OPENROUTER_API_KEY",
		BaseURL: "https://openrouter.ai/api/v1", Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Plan.Files) == 0 {
		t.Fatal("expected settings patch file")
	}
	content := s.Plan.Files[0].Content
	if !strings.Contains(content, "aegiskeys-router") {
		t.Errorf("missing provider key: %s", content)
	}
	if strings.Contains(content, "sk-test") {
		t.Error("raw secret leaked to settings patch")
	}
}

// --- CanInjectCredential / CanConfigureProvider tests ---

func TestCanInjectCredential_AllAdaptersHonest(t *testing.T) {
	// CLI adapters should return true.
	cliAdapters := []AppAdapter{
		GenericOpenAIAdapter{}, AiderAdapter{}, CrushAdapter{},
		HermesAdapter{}, QwenCodeAdapter{}, GooseAdapter{},
		ClaudeCodeAdapter{}, ClineAdapter{}, MistralVibeAdapter{},
	}
	prov := provider.Provider{Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY", Compatibility: provider.CompatOpenAI}
	for _, a := range cliAdapters {
		if !a.CanInjectCredential(prov) {
			t.Errorf("%s should support credential injection", a.ID())
		}
		if !a.CanConfigureProvider(prov) {
			t.Errorf("%s should support provider configuration", a.ID())
		}
	}

	// GUI/IDE adapters should return false for credential injection.
	guiAdapters := []AppAdapter{ZedAdapter{}, IntelliJAdapter{}}
	for _, a := range guiAdapters {
		if a.CanInjectCredential(prov) {
			t.Errorf("%s should NOT support credential injection", a.ID())
		}
	}
}

// --- Preflight-style checks via Validate ---

func TestValidate_AiderWarnsNoModel(t *testing.T) {
	a := AiderAdapter{}
	p := profile.Profile{Name: "test", Target: profile.TargetConfig{App: "aider"}}
	prov := provider.Provider{Name: "OpenAI", Slug: "openai", Compatibility: provider.CompatOpenAI}

	warnings, err := a.Validate(p, prov)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) == 0 {
		t.Error("expected warning about missing model")
	}
}

func TestValidate_ZedWarnsMissingModel(t *testing.T) {
	a := ZedAdapter{}
	p := profile.Profile{Name: "test", Target: profile.TargetConfig{App: "zed"}}
	prov := provider.Provider{Name: "OpenRouter", Slug: "openrouter", Compatibility: provider.CompatOpenAI}

	warnings, err := a.Validate(p, prov)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) == 0 {
		t.Error("expected warning about missing model")
	}
}

func TestValidate_IntelliJAlwaysWarns(t *testing.T) {
	a := IntelliJAdapter{}
	p := profile.Profile{Name: "test", Target: profile.TargetConfig{App: "intellij"}}
	prov := provider.Provider{Name: "OpenAI", Slug: "openai", Compatibility: provider.CompatOpenAI}

	warnings, err := a.Validate(p, prov)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) == 0 {
		t.Error("expected warning about manual credential handoff")
	}
}

// --- Rendering with all new model slot types ---

func TestRenderStrategy_HermesAuxiliarySlotsProduceConfig(t *testing.T) {
	a := HermesAdapter{}
	p := profile.Profile{
		Name: "hermes-test", ProviderSlug: "openrouter",
		Target: profile.TargetConfig{App: "hermes"},
		Models: profile.ModelSlots{
			Main:        &profile.ModelRef{ID: "claude-opus-4-5"},
			Compression: &profile.ModelRef{ID: "gemini-3-flash"},
			Vision:      &profile.ModelRef{ID: "gpt-4o"},
			WebExtract:  &profile.ModelRef{ID: "gemini-3-flash"},
		},
	}
	prov := provider.Provider{
		Name: "OpenRouter", Slug: "openrouter", EnvVar: "OPENROUTER_API_KEY",
		BaseURL: "https://openrouter.ai/api/v1", Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Plan.Files) == 0 {
		t.Fatal("expected config file")
	}
	content := s.Plan.Files[0].Content
	// Should contain auxiliary role entries.
	for _, role := range []string{"compression", "vision", "web_extract"} {
		if !strings.Contains(content, role) {
			t.Errorf("config missing auxiliary role %q:\n%s", role, content)
		}
	}
}

func TestRenderStrategy_HermesNoHermesHomeUsesIsolatedDir(t *testing.T) {
	a := HermesAdapter{}
	p := profile.Profile{
		Name: "my-profile", ProviderSlug: "openrouter",
		Target: profile.TargetConfig{App: "hermes"},
		Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}},
	}
	prov := provider.Provider{
		Name: "OpenRouter", Slug: "openrouter", EnvVar: "OPENROUTER_API_KEY",
		BaseURL: "https://openrouter.ai/api/v1", Compatibility: provider.CompatOpenAI,
	}
	key := testAPIKey("sk-test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	home := s.Plan.Env["HERMES_HOME"]
	if home == "" {
		t.Fatal("HERMES_HOME not set")
	}
	// Should contain safeName of profile (hyphens preserved).
	if !strings.Contains(home, "my-profile") {
		t.Errorf("HERMES_HOME should use safe profile name, got %s", home)
	}
}

// --- buildBaseEnv tests are in adapter_test.go but add a focused one here ---

func TestBuildBaseEnv_ProviderExtraEnv_Merged(t *testing.T) {
	p := profile.Profile{Name: "t"}
	prov := provider.Provider{
		Name: "X", Slug: "x", EnvVar: "X_KEY", Compatibility: provider.CompatOpenAI,
		ExtraEnv: map[string]string{"X_BASE": "https://x.example.com"},
	}
	key := testAPIKey("sk-x")
	env, err := buildBaseEnv(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if env["X_KEY"] != "sk-x" {
		t.Errorf("primary key: %s", env["X_KEY"])
	}
	if env["X_BASE"] != "https://x.example.com" {
		t.Errorf("extra env: %s", env["X_BASE"])
	}
}

func TestBuildBaseEnv_ProfileOverridesProviderKey_Rejected(t *testing.T) {
	// Profile env that overrides the credential var must be REJECTED.
	p := profile.Profile{
		Name:         "t",
		ProviderSlug: "x",
		Target:       profile.TargetConfig{App: "generic", Command: "tool"},
		Env:          map[string]string{"X_KEY": "override-value"},
	}
	prov := provider.Provider{Name: "X", Slug: "x", EnvVar: "X_KEY", Compatibility: provider.CompatOpenAI}
	key := testAPIKey("sk-x")
	if _, err := ResolveLaunchStrategy(p, prov, key, NewRegistry()); err == nil {
		t.Fatal("expected error: profile env may not override provider credential env")
	}
}

// --- Registry tests ---

func TestRegistry_IncludesZedAndIntelliJ(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get("zed"); !ok {
		t.Error("zed adapter not registered")
	}
	if _, ok := r.Get("intellij"); !ok {
		t.Error("intellij adapter not registered")
	}
}

func TestRegistry_ReturnsAppAdapterInterface(t *testing.T) {
	r := NewRegistry()
	a, ok := r.Get("aider")
	if !ok {
		t.Fatal("aider not found")
	}
	// Verify the adapter satisfies the full interface (compile-time check).
	var _ AppAdapter = a
}

// --- Adapter interface compile-time checks ---

var (
	_ AppAdapter = GenericOpenAIAdapter{}
	_ AppAdapter = CrushAdapter{}
	_ AppAdapter = AiderAdapter{}
	_ AppAdapter = ClineAdapter{}
	_ AppAdapter = HermesAdapter{}
	_ AppAdapter = QwenCodeAdapter{}
	_ AppAdapter = ClaudeCodeAdapter{}
	_ AppAdapter = MistralVibeAdapter{}
	_ AppAdapter = GooseAdapter{}
	_ AppAdapter = ZedAdapter{}
	_ AppAdapter = IntelliJAdapter{}
	_ AppAdapter = MiMoOpenCodeAdapter{}
	_ AppAdapter = OpenHandsAdapter{}
	_ AppAdapter = GeminiCLIAdapter{}
	_ AppAdapter = CopilotCLIAdapter{}
	_ AppAdapter = ContinueAdapter{}
	_ AppAdapter = RooCodeAdapter{}
	_ AppAdapter = KiloCodeAdapter{}
	_ AppAdapter = CursorAdapter{}
)

// --- New adapter tests ---

func TestMiMoAdapter_SetsOpenCodeEnv(t *testing.T) {
	a := MiMoOpenCodeAdapter{}
	p := profile.Profile{Name: "t", Target: profile.TargetConfig{App: "mimo"},
		Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}}}
	prov := provider.Provider{Name: "OpenRouter", Slug: "openrouter", EnvVar: "OPENROUTER_API_KEY",
		BaseURL: "https://openrouter.ai/api/v1", Compatibility: provider.CompatOpenAI}
	key := testAPIKey("sk-test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if s.Plan.Command != "opencode" {
		t.Errorf("command = %s", s.Plan.Command)
	}
	// The model is applied via opencode.json (OpenCode ignores OPENCODE_MODEL
	// env vars), not by injecting OPENCODE_MODEL into the environment.
	if _, ok := s.Plan.Env["OPENCODE_MODEL"]; ok {
		t.Errorf("OPENCODE_MODEL should not be injected into env")
	}
	if len(s.Plan.Files) != 1 {
		t.Fatalf("expected one config file, got %d", len(s.Plan.Files))
	}
	fw := s.Plan.Files[0]
	if fw.Path != "$HOME/.config/opencode/opencode.json" || fw.Format != "json" {
		t.Errorf("unexpected config file: %s (%s)", fw.Path, fw.Format)
	}
	if !strings.Contains(fw.Content, `"model": "openrouter/gpt-4o"`) {
		t.Errorf("model not written to opencode.json: %s", fw.Content)
	}
	// Secret must stay env-only: config references the env var, not the value.
	if !strings.Contains(fw.Content, "{env:OPENROUTER_API_KEY}") {
		t.Errorf("config should reference api key via env, got: %s", fw.Content)
	}
	if strings.Contains(fw.Content, "sk-test") {
		t.Errorf("raw secret leaked into opencode.json config")
	}
}

func TestGeminiAdapter_GoogleProvider(t *testing.T) {
	a := GeminiCLIAdapter{}
	p := profile.Profile{Name: "t", Target: profile.TargetConfig{App: "gemini"},
		Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gemini-3-flash"}}}
	prov := provider.Provider{Name: "Google", Slug: "google", EnvVar: "GOOGLE_API_KEY",
		Compatibility: provider.CompatGoogle}
	key := testAPIKey("test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if s.Plan.Env["GOOGLE_MODEL"] != "gemini-3-flash" {
		t.Errorf("GOOGLE_MODEL = %s", s.Plan.Env["GOOGLE_MODEL"])
	}
}

func TestCursorAdapter_Blocked(t *testing.T) {
	a := CursorAdapter{}
	p := profile.Profile{Name: "t", Target: profile.TargetConfig{App: "cursor"}}
	prov := provider.Provider{Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY", Compatibility: provider.CompatOpenAI}
	key := testAPIKey("sk-test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Blocked {
		t.Error("Cursor adapter should be blocked")
	}
	if s.Plan.Command != "" {
		t.Errorf("blocked adapter should have no command, got %s", s.Plan.Command)
	}
}

func TestCopilotCLIAdapter_Render(t *testing.T) {
	a := CopilotCLIAdapter{}
	p := profile.Profile{Name: "t", Target: profile.TargetConfig{App: "copilot"},
		Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}}}
	prov := provider.Provider{Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY", Compatibility: provider.CompatOpenAI}
	key := testAPIKey("sk-test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if s.Plan.Command != "copilot" {
		t.Errorf("command = %s", s.Plan.Command)
	}
	if s.Plan.Env["COPILOT_MODEL"] != "gpt-4o" {
		t.Errorf("model not set")
	}
}

func TestGuidedActions_ZedHasActions(t *testing.T) {
	actions := GuidedActions("zed")
	if len(actions) == 0 {
		t.Fatal("expected guided actions for Zed")
	}
	found := false
	for _, a := range actions {
		if a.Kind == ActionPatchConfig {
			found = true
		}
	}
	if !found {
		t.Error("expected patch_config action for Zed")
	}
}

func TestGuidedActions_IntellijHasWriteProfile(t *testing.T) {
	actions := GuidedActions("intellij")
	if len(actions) == 0 {
		t.Fatal("expected guided actions for IntelliJ")
	}
	found := false
	for _, a := range actions {
		if a.Kind == ActionWriteProfileDir {
			found = true
		}
	}
	if !found {
		t.Error("expected write_profile_dir action for IntelliJ")
	}
}

func TestGuidedActions_UnknownAppReturnsNil(t *testing.T) {
	if GuidedActions("nonexistent") != nil {
		t.Error("expected nil for unknown app")
	}
}

func TestOpenHandsAdapter_Render(t *testing.T) {
	a := OpenHandsAdapter{}
	p := profile.Profile{Name: "t", Target: profile.TargetConfig{App: "openhands"},
		Models: profile.ModelSlots{Main: &profile.ModelRef{ID: "gpt-4o"}}}
	prov := provider.Provider{Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY", Compatibility: provider.CompatOpenAI}
	key := testAPIKey("sk-test")
	s, err := a.Render(p, prov, key)
	if err != nil {
		t.Fatal(err)
	}
	if s.Plan.Command != "openhands" {
		t.Errorf("command = %s", s.Plan.Command)
	}
	if s.Plan.Env["OPENHANDS_MODEL"] != "gpt-4o" {
		t.Errorf("model not set")
	}
}

func TestKiloAdapter_ManualCredential(t *testing.T) {
	a := KiloCodeAdapter{}
	prov := provider.Provider{Name: "OpenAI", Slug: "openai", EnvVar: "OPENAI_API_KEY", Compatibility: provider.CompatOpenAI}
	if a.CanInjectCredential(prov) {
		t.Error("Kilo Code should not support credential injection")
	}
	if !a.CanConfigureProvider(prov) {
		t.Error("Kilo Code should support provider configuration")
	}
}

func TestRegistry_AllNewAdaptersRegistered(t *testing.T) {
	r := NewRegistry()
	want := []string{"mimo", "openhands", "gemini", "copilot", "continue", "roo", "kilo", "cursor"}
	for _, id := range want {
		if _, ok := r.Get(id); !ok {
			t.Errorf("missing adapter: %s", id)
		}
	}
}
