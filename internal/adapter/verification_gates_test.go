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

type adapterGoldenSnapshot struct {
	App       string              `json:"app"`
	Command   string              `json:"command"`
	Args      []string            `json:"args,omitempty"`
	EnvKeys   []string            `json:"env_keys"`
	Files     []adapterGoldenFile `json:"files,omitempty"`
	Preview   []string            `json:"preview,omitempty"`
	Warnings  []string            `json:"warnings,omitempty"`
	HazardIDs []string            `json:"hazards,omitempty"`
}

type adapterGoldenFile struct {
	Path        string `json:"path"`
	Format      string `json:"format"`
	Scope       string `json:"scope"`
	MergePolicy string `json:"merge_policy"`
}

func TestAdapterVerificationGates(t *testing.T) {
	const rawSecret = "AK_VERIFICATION_GATE_SECRET_1234567890"
	cases := []struct {
		app     string
		command string
		envKeys []string
	}{
		{app: "generic", command: "generic-smoke", envKeys: []string{"OPENAI_BASE_URL", "OPENAI_MODEL", "OPENROUTER_API_KEY"}},
		{app: "crush", command: "crush", envKeys: []string{"OPENAI_BASE_URL", "OPENROUTER_API_KEY"}},
		{app: "aider", command: "aider", envKeys: []string{"AIDER_OPENAI_API_BASE", "OPENAI_BASE_URL", "OPENROUTER_API_KEY"}},
		{app: "qwen", command: "qwen", envKeys: []string{"OPENAI_BASE_URL", "OPENROUTER_API_KEY"}},
		{app: "claude", command: "claude", envKeys: []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "OPENAI_BASE_URL"}},
		{app: "goose", command: "goose", envKeys: []string{"GOOSE_MODEL", "GOOSE_PROVIDER", "GOOSE_PROVIDER__HOST", "OPENAI_BASE_URL", "OPENROUTER_API_KEY"}},
	}

	for _, tc := range cases {
		t.Run(tc.app, func(t *testing.T) {
			prov := verificationProvider()
			key := &secret.SecretRecord{
				ID:           "key_verify",
				ProviderSlug: prov.Slug,
				Secret:       rawSecret,
				Kind:         secret.SecretAPIKey,
				Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
			}
			prof := verificationProfile(tc.app, tc.command, prov)
			strategy, err := ResolveLaunchStrategy(prof, prov, key, NewRegistry())
			if err != nil {
				t.Fatalf("ResolveLaunchStrategy: %v", err)
			}
			if strategy.Plan.Command != tc.command {
				t.Fatalf("command = %q, want %q", strategy.Plan.Command, tc.command)
			}
			for _, key := range tc.envKeys {
				if _, ok := strategy.Plan.Env[key]; !ok {
					t.Fatalf("missing env key %s in %v", key, sortedKeys(strategy.Plan.Env))
				}
			}
			assertNoRawSecretInStrategy(t, strategy, rawSecret)
			assertConfigWritesMergeAndDoNotLeak(t, strategy, rawSecret)
			assertGoldenSnapshot(t, tc.app, snapshotStrategy(tc.app, strategy))
		})
	}
}

func verificationProvider() provider.Provider {
	p := provider.Provider{
		ID:            "openrouter",
		Name:          "OpenRouter",
		Slug:          "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		EnvVar:        "OPENROUTER_API_KEY",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENROUTER_API_KEY"},
		Compatibility: provider.CompatOpenAI,
		ExtraEnv:      map[string]string{"OPENAI_BASE_URL": "https://openrouter.ai/api/v1"},
		Models:        []provider.ProviderModel{{ID: "anthropic/claude-sonnet-4.5"}},
	}
	p.Normalize()
	return p
}

func verificationProfile(app, command string, prov provider.Provider) profile.Profile {
	p := profile.Profile{
		Name:         "verify-" + app,
		ProviderSlug: prov.Slug,
		KeyID:        "key_verify",
		Target:       profile.TargetConfig{App: app, Command: command},
		Models: profile.ModelSlots{
			Main: &profile.ModelRef{ID: "anthropic/claude-sonnet-4.5", Source: profile.ModelSourceStatic},
		},
	}
	return p
}

func assertNoRawSecretInStrategy(t *testing.T, strategy *LaunchStrategy, raw string) {
	t.Helper()
	if containsRawSecretInArgs(strategy.Plan.Args, raw) {
		t.Fatal("raw secret leaked into args")
	}
	if containsRawSecretInPreview(strategy.Plan.Preview, raw) {
		t.Fatal("raw secret leaked into preview")
	}
	if containsRawSecretInFiles(strategy.Plan.Files, raw) {
		t.Fatal("raw secret leaked into files")
	}
	for k, v := range strategy.Plan.Env {
		if strings.Contains(v, raw) && !isDeclaredSecretEnv(strategy.Support, k) {
			t.Fatalf("raw secret leaked into undeclared env %s", k)
		}
	}
}

func isDeclaredSecretEnv(c AppSupportContract, name string) bool {
	for _, e := range c.RequiredEnv {
		if e.Name == name && e.Secret {
			return true
		}
	}
	if strings.Contains(name, "KEY") || strings.Contains(name, "TOKEN") || strings.Contains(name, "AUTH") {
		return true
	}
	return false
}

func assertConfigWritesMergeAndDoNotLeak(t *testing.T, strategy *LaunchStrategy, raw string) {
	t.Helper()
	if len(strategy.Plan.Files) == 0 {
		return
	}
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
		t.Fatalf("fresh config write failed: %v", err)
	}
	if err := ApplyFileWrites(strategy.Plan.Files, env); err != nil {
		t.Fatalf("second config merge failed: %v", err)
	}
	if leak := findRawSecretInTree(t, tmp, raw); leak != "" {
		t.Fatalf("raw secret leaked to config tree: %s", leak)
	}
}

func snapshotStrategy(app string, strategy *LaunchStrategy) adapterGoldenSnapshot {
	s := adapterGoldenSnapshot{
		App:      app,
		Command:  strategy.Plan.Command,
		Args:     append([]string{}, strategy.Plan.Args...),
		EnvKeys:  sortedKeys(strategy.Plan.Env),
		Preview:  append([]string{}, strategy.Plan.Preview...),
		Warnings: append([]string{}, strategy.Plan.Warnings...),
	}
	for _, f := range strategy.Plan.Files {
		s.Files = append(s.Files, adapterGoldenFile{
			Path:        f.Path,
			Format:      f.Format,
			Scope:       string(f.Scope),
			MergePolicy: string(f.MergePolicy),
		})
	}
	for _, h := range strategy.Hazards {
		s.HazardIDs = append(s.HazardIDs, h.Severity+":"+h.Title)
	}
	sort.Strings(s.HazardIDs)
	return s
}

func assertGoldenSnapshot(t *testing.T, app string, got adapterGoldenSnapshot) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "adapter_golden", app+".openrouter.golden.json")
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if os.Getenv("UPDATE_GOLDEN") == "1" {
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
		t.Fatalf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", app, want, data)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func copyEnv(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func findRawSecretInTree(t *testing.T, root, raw string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), raw) {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}
