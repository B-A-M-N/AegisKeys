package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

func TestAdapterFakeExecutableLaunchSmoke(t *testing.T) {
	const sentinel = "AK_FAKE_LAUNCH_SMOKE_SECRET_1234567890"
	os.Unsetenv("OPENROUTER_API_KEY")
	if strings.Contains(strings.Join(os.Environ(), "\n"), sentinel) {
		t.Fatal("parent environment already contains sentinel")
	}

	cases := []struct {
		app      string
		command  string
		wantEnv  []string
		mainSlot bool
	}{
		{app: "generic", command: "generic-smoke", wantEnv: []string{"OPENROUTER_API_KEY", "OPENAI_BASE_URL"}, mainSlot: true},
		{app: "crush", command: "crush", wantEnv: []string{"OPENROUTER_API_KEY", "OPENAI_BASE_URL"}, mainSlot: true},
		{app: "aider", command: "aider", wantEnv: []string{"OPENROUTER_API_KEY", "OPENAI_BASE_URL", "AIDER_OPENAI_API_BASE"}, mainSlot: true},
		{app: "qwen", command: "qwen", wantEnv: []string{"OPENROUTER_API_KEY", "OPENAI_BASE_URL"}, mainSlot: true},
		{app: "claude", command: "claude", wantEnv: []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL"}, mainSlot: true},
		{app: "goose", command: "goose", wantEnv: []string{"OPENROUTER_API_KEY", "OPENAI_BASE_URL", "GOOSE_PROVIDER", "GOOSE_MODEL"}, mainSlot: true},
	}

	for _, tc := range cases {
		t.Run(tc.app, func(t *testing.T) {
			tmp := t.TempDir()
			bin := filepath.Join(tmp, "bin")
			home := filepath.Join(tmp, "home")
			xdg := filepath.Join(tmp, "xdg")
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(home, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(xdg, 0700); err != nil {
				t.Fatal(err)
			}
			writeSmokeExecutable(t, filepath.Join(bin, tc.command), 0)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			prov := openRouterSmokeProvider()
			key := &secret.SecretRecord{
				ID:           "key_smoke",
				ProviderSlug: prov.Slug,
				Secret:       sentinel,
				Kind:         secret.SecretAPIKey,
				Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
			}
			prof := profile.Profile{
				Name:         "smoke-" + tc.app,
				ProviderSlug: prov.Slug,
				KeyID:        key.ID,
				Target:       profile.TargetConfig{App: tc.app, Command: tc.command},
			}
			if tc.mainSlot {
				prof.Models.Main = &profile.ModelRef{ID: "anthropic/claude-sonnet-4.5", Source: profile.ModelSourceStatic}
			}

			reg := adapter.NewRegistry()
			strategy, err := adapter.ResolveLaunchStrategy(prof, prov, key, reg)
			if err != nil {
				t.Fatalf("ResolveLaunchStrategy: %v", err)
			}
			outFile := filepath.Join(tmp, "smoke.out")
			strategy.Plan.Env["HOME"] = home
			strategy.Plan.Env["XDG_CONFIG_HOME"] = xdg
			strategy.Plan.Env["TMPDIR"] = tmp
			strategy.Plan.Env["AEGIS_SMOKE_OUT"] = outFile
			strategy.Plan.Env["AEGIS_EXPECTED_KEYS"] = strings.Join(tc.wantEnv, " ")
			if err := adapter.ValidateLaunchStrategy(strategy, prof, prov, key, adapter.DefaultSecurityPolicy()); err != nil {
				t.Fatalf("ValidateLaunchStrategy: %v", err)
			}

			if err := Run(context.Background(), strategy, RunOptions{ProfileName: prof.Name, ConfigDir: tmp}); err != nil {
				t.Fatalf("Run fake executable: %v", err)
			}
			out, err := os.ReadFile(outFile)
			if err != nil {
				t.Fatalf("read smoke output: %v", err)
			}
			if strings.Contains(string(out), sentinel) {
				t.Fatal("fake executable output leaked raw secret")
			}
			for _, key := range tc.wantEnv {
				if !strings.Contains(string(out), key+"=present") {
					t.Fatalf("fake executable did not report expected env %s; output:\n%s", key, out)
				}
			}
			if leakPath := findSecretInTree(t, tmp, sentinel); leakPath != "" {
				t.Fatalf("raw secret leaked to temp tree: %s", leakPath)
			}
			if strings.Contains(strings.Join(os.Environ(), "\n"), sentinel) {
				t.Fatal("parent environment contains sentinel after launch")
			}
		})
	}
}

func TestAdapterFakeExecutableLaunchSmokeExitCodePreserved(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	writeSmokeExecutable(t, filepath.Join(bin, "exit-smoke"), 42)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	prov := openRouterSmokeProvider()
	key := &secret.SecretRecord{
		ID:           "key_smoke",
		ProviderSlug: prov.Slug,
		Secret:       "AK_EXIT_CODE_SECRET_1234567890",
		Kind:         secret.SecretAPIKey,
		Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
	}
	prof := profile.Profile{
		Name:         "exit-smoke",
		ProviderSlug: prov.Slug,
		KeyID:        key.ID,
		Target:       profile.TargetConfig{App: "generic", Command: "exit-smoke"},
	}
	strategy, err := adapter.ResolveLaunchStrategy(prof, prov, key, adapter.NewRegistry())
	if err != nil {
		t.Fatalf("ResolveLaunchStrategy: %v", err)
	}
	strategy.Plan.Env["AEGIS_EXPECTED_KEYS"] = "OPENROUTER_API_KEY"
	strategy.Plan.Env["AEGIS_SMOKE_OUT"] = filepath.Join(tmp, "exit-smoke.out")
	err = Run(context.Background(), strategy, RunOptions{ProfileName: prof.Name, ConfigDir: tmp})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != 42 {
		t.Fatalf("exit code = %d, want 42", exitErr.Code)
	}
}

func openRouterSmokeProvider() provider.Provider {
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

func writeSmokeExecutable(t *testing.T, path string, exitCode int) {
	t.Helper()
	script := `#!/bin/sh
set -eu
for key in $AEGIS_EXPECTED_KEYS; do
	eval "val=\${$key:-}"
	if [ -z "$val" ]; then
		echo "missing $key" >&2
		exit 42
	fi
done
{
	for key in $AEGIS_EXPECTED_KEYS; do
		echo "$key=present"
	done
} > "$AEGIS_SMOKE_OUT"
exit ` + itoa(exitCode) + `
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
}

func findSecretInTree(t *testing.T, root, raw string) string {
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
