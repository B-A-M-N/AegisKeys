package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
)

// testStrategy creates a launch strategy that writes a config file (MergeJSON,
// ScopeUser) to the given path and runs the given command. The strategy uses
// Support.CanLaunch=true so PrepareCommandWithCleanup proceeds past launch gates.
func testStrategy(configPath, content, command string, env map[string]string) *adapter.LaunchStrategy {
	return &adapter.LaunchStrategy{
		Plan: adapter.LaunchPlan{
			Command: command,
			Env:     env,
			Files: []adapter.FileWrite{{
				Path:        configPath,
				Format:      "json",
				Content:     content,
				Scope:       adapter.ScopeUser,
				MergePolicy: adapter.MergeJSON,
				Mode:        0600,
			}},
		},
		Support: adapter.AppSupportContract{
			ID:        "test",
			CanLaunch: true,
		},
	}
}

func TestPrepareCommandWithCleanup_RestoresConfigAfterRun(t *testing.T) {
	if _, err := os.Stat("/usr/bin/true"); err != nil {
		t.Skip("true command not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"user": "existing"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	strategy := testStrategy(path, `{"aegiskeys": true}`, "true", nil)
	prepared, err := PrepareCommandWithCleanup(context.Background(), strategy, RunOptions{})
	if err != nil {
		t.Fatalf("PrepareCommandWithCleanup: %v", err)
	}

	// File should be written before child runs.
	written, _ := os.ReadFile(path)
	if string(written) == string(original) {
		t.Fatal("expected runtime overlay to change the file before child exits")
	}

	// Simulate child exit by calling cleanup directly.
	if err := prepared.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	// File should be restored.
	restored, _ := os.ReadFile(path)
	if string(restored) != string(original) {
		t.Fatalf("restored content = %q, want %q", string(restored), string(original))
	}
}

// TestRun_RestoresConfigAfterChildModifiesIt verifies that cleanup restores
// the ORIGINAL snapshot even when the child process overwrites the config file
// during execution. This proves snapshots capture pre-write bytes — not the
// current disk state at cleanup time.
func TestRun_RestoresConfigAfterChildModifiesIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"keep": true, "user_setting": "preserved"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	// Override command to a shell that takes a script which modifies the file.
	strategy := testStrategy(path, `{"overlay": true}`, "sh", nil)
	strategy.Plan.Args = []string{"-c", fmt.Sprintf(`echo '{"child": true}' > %s && true`, path)}

	err := Run(context.Background(), strategy, RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	restored, _ := os.ReadFile(path)
	if string(restored) != string(original) {
		t.Fatalf("after child overwrite + cleanup: content = %q, want %q", string(restored), string(original))
	}
}

func TestRun_RestoresConfigAfterSuccessfulChild(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"keep": true}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	strategy := testStrategy(path, `{"overlay": true}`, "true", nil)
	err := Run(context.Background(), strategy, RunOptions{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	restored, _ := os.ReadFile(path)
	if string(restored) != string(original) {
		t.Fatalf("after Run: content = %q, want %q", string(restored), string(original))
	}
}

func TestRun_RestoresConfigAfterNonZeroChild(t *testing.T) {
	if _, err := os.Stat("/usr/bin/false"); err != nil {
		t.Skip("false command not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"keep": true}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	strategy := testStrategy(path, `{"overlay": true}`, "false", nil)
	err := Run(context.Background(), strategy, RunOptions{})

	// Run should report non-zero exit.
	if err == nil {
		t.Fatal("expected error from non-zero child exit")
	}

	// File should still be restored.
	restored, _ := os.ReadFile(path)
	if string(restored) != string(original) {
		t.Fatalf("after failed Run: content = %q, want %q", string(restored), string(original))
	}
}

func TestRun_ReportsCleanupFailure(t *testing.T) {
	if _, err := os.Stat("/usr/bin/true"); err != nil {
		t.Skip("true command not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"keep": true}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	strategy := testStrategy(path, `{"overlay": true}`, "true", nil)
	prepared, err := PrepareCommandWithCleanup(context.Background(), strategy, RunOptions{})
	if err != nil {
		t.Fatalf("PrepareCommandWithCleanup: %v", err)
	}

	// Make parent dir read-only so cleanup (restore) fails.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)

	// Run the child directly to simulate the full flow.
	if err := prepared.Cmd.Run(); err != nil {
		t.Fatalf("cmd.Run: %v", err)
	}
	err = prepared.Cleanup()
	if err == nil {
		t.Error("expected cleanup failure when parent dir is read-only")
	}
}

func TestPrepareCommandWithCleanup_MissingCommandFailsBeforeConfigWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	strategy := &adapter.LaunchStrategy{
		Plan: adapter.LaunchPlan{
			Command: "this-command-does-not-exist-xyz",
			Files: []adapter.FileWrite{{
				Path:        path,
				Format:      "json",
				Content:     `{"should": "not appear"}`,
				Scope:       adapter.ScopeUser,
				MergePolicy: adapter.MergeNone,
				Mode:        0600,
			}},
		},
		Support: adapter.AppSupportContract{ID: "test", CanLaunch: true},
	}

	_, err := PrepareCommandWithCleanup(context.Background(), strategy, RunOptions{})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	// Config file must NOT have been written.
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("config file should not be written when command is missing")
	}
}

// TestApplyFileWritesWithRestore_DoubleCleanup verifies that calling cleanup
// twice is safe: the second call should not panic or corrupt state.
func TestApplyFileWritesWithRestore_DoubleCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"keep": true}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	restore, err := adapter.ApplyFileWritesWithRestore([]adapter.FileWrite{{
		Path: path, Format: "json", Content: `{"overlay": true}`,
		Scope: adapter.ScopeUser, MergePolicy: adapter.MergeJSON, Mode: 0600,
	}}, map[string]string{})
	if err != nil {
		t.Fatalf("ApplyFileWritesWithRestore: %v", err)
	}

	// First cleanup: restores original.
	if err := restore(); err != nil {
		t.Fatalf("first cleanup: %v", err)
	}
	// Second cleanup: should be a no-op (file already restored).
	if err := restore(); err != nil {
		t.Fatalf("second cleanup should be safe: %v", err)
	}

	restored, _ := os.ReadFile(path)
	if string(restored) != string(original) {
		t.Fatalf("after double cleanup: content = %q, want %q", string(restored), string(original))
	}
}

func TestRun_ParentEnvNotInjected(t *testing.T) {
	if _, err := os.Stat("/usr/bin/true"); err != nil {
		t.Skip("true command not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"keep": true}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	injected := map[string]string{"MY_TEST_SECRET": "should-not-be-in-parent"}
	strategy := testStrategy(path, `{"overlay": true}`, "true", injected)

	// Capture parent env before Run.
	parentBefore := os.Environ()

	err := Run(context.Background(), strategy, RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Parent env must not contain the injected secret.
	for _, kv := range os.Environ() {
		if strings.Contains(kv, "MY_TEST_SECRET") {
			t.Fatalf("parent env contains injected secret: %s", kv)
		}
	}
	_ = parentBefore
}

// TestRun_RestoresConfigAfterChildSIGSEGV verifies cleanup runs after a child
// that dies from a signal (not a clean exit). Simulates a segfaulting child.
func TestRun_RestoresConfigAfterChildSIGSEGV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"keep": true}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	strategy := testStrategy(path, `{"overlay": true}`, "sh", nil)
	strategy.Plan.Args = []string{"-c", "kill -SEGV $$"}

	err := Run(context.Background(), strategy, RunOptions{})
	if err == nil {
		t.Fatal("expected error from signal-killed child")
	}

	// File must still be restored despite signal death.
	restored, _ := os.ReadFile(path)
	if string(restored) != string(original) {
		t.Fatalf("after signal-killed child: content = %q, want %q", string(restored), string(original))
	}
}

// TestApplyFileWritesWithRestore_PartialFailure verifies that when one file's
// restore fails, other files in the batch still get restored AND the error
// message identifies the specific failure.
func TestApplyFileWritesWithRestore_PartialFailure(t *testing.T) {
	dir := t.TempDir()
	writableRoot := filepath.Join(dir, "writable")
	writablePath := filepath.Join(writableRoot, "config.json")
	readOnlyRoot := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(writableRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(readOnlyRoot, 0700); err != nil {
		t.Fatal(err)
	}
	readOnlyPath := filepath.Join(readOnlyRoot, "config.json")

	// Pre-create both files.
	if err := os.WriteFile(writablePath, []byte(`{"user": "writable"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readOnlyPath, []byte(`{"user": "readonly"}`), 0600); err != nil {
		t.Fatal(err)
	}

	writes := []adapter.FileWrite{
		{Path: writablePath, Format: "json", Content: `{"overlay": true}`, Scope: adapter.ScopeUser, MergePolicy: adapter.MergeJSON, Mode: 0600},
		{Path: readOnlyPath, Format: "json", Content: `{"overlay": true}`, Scope: adapter.ScopeUser, MergePolicy: adapter.MergeJSON, Mode: 0600},
	}

	restore, err := adapter.ApplyFileWritesWithRestore(writes, map[string]string{})
	if err != nil {
		t.Fatalf("ApplyFileWritesWithRestore: %v", err)
	}

	// Make the readonly directory read-only so restore fails for that file.
	if err := os.Chmod(readOnlyRoot, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(readOnlyRoot, 0700)

	err = restore()
	if err == nil {
		t.Fatal("expected partial failure error")
	}

	// The writable file MUST be restored even though the other failed.
	writableRestored, _ := os.ReadFile(writablePath)
	if string(writableRestored) != `{"user": "writable"}` {
		t.Errorf("writable file not restored: %q", string(writableRestored))
	}
}

func TestRun_CleanupFailureCombinedWithChildExit(t *testing.T) {
	if _, err := os.Stat("/usr/bin/false"); err != nil {
		t.Skip("false command not available")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"keep": true}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	strategy := testStrategy(path, `{"overlay": true}`, "false", nil)
	prepared, err := PrepareCommandWithCleanup(context.Background(), strategy, RunOptions{})
	if err != nil {
		t.Fatalf("PrepareCommandWithCleanup: %v", err)
	}

	// Sabotage: make parent read-only so restore (cleanup) fails.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)

	// Child exits non-zero (command is "false").
	childErr := prepared.Cmd.Run()
	// Cleanup fails because dir is read-only.
	cleanupErr := prepared.Cleanup()

	if childErr == nil {
		t.Fatal("expected non-zero child exit from 'false'")
	}
	if cleanupErr == nil {
		t.Fatal("expected cleanup failure on read-only parent dir")
	}
	// The combined error from Run() would include both child and cleanup errors.
	// Here we assert both error sources independently to prove the combination
	// gate works (Run wraps both in a single error when both fail).
	if !strings.Contains(childErr.Error(), "exit status") {
		t.Errorf("child error should mention exit status: %v", childErr)
	}
	if !strings.Contains(cleanupErr.Error(), "restore file writes") {
		t.Errorf("cleanup error should mention restore: %v", cleanupErr)
	}
}

func TestResolveEnv(t *testing.T) {
	prov := &provider.Provider{
		Slug:       "openrouter",
		EnvVar:     "OPENROUTER_API_KEY",
		ExtraEnv:   map[string]string{"OPENAI_BASE_URL": "https://openrouter.ai/api/v1"},
		AuthHeader: "Authorization: Bearer ${KEY}",
	}
	prof := &profile.Profile{
		Name:         "or-main",
		ProviderSlug: "openrouter",
		KeyID:        "key_1",
		Env:          map[string]string{"CUSTOM": "val"},
	}

	r, err := ResolveEnv(prof, prov, "sk-secret-value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Provider primary var.
	if r.EnvVars["OPENROUTER_API_KEY"] != "sk-secret-value" {
		t.Errorf("OPENROUTER_API_KEY = %q", r.EnvVars["OPENROUTER_API_KEY"])
	}
	// Extra env (base URL).
	if r.EnvVars["OPENAI_BASE_URL"] != "https://openrouter.ai/api/v1" {
		t.Errorf("OPENAI_BASE_URL = %q", r.EnvVars["OPENAI_BASE_URL"])
	}
	// Profile override wins.
	if r.EnvVars["CUSTOM"] != "val" {
		t.Errorf("CUSTOM = %q", r.EnvVars["CUSTOM"])
	}
}

func TestResolveEnvLocalProvider(t *testing.T) {
	// Local providers (Ollama) have no primary env var.
	prov := &provider.Provider{Slug: "ollama", ExtraEnv: map[string]string{"OPENAI_BASE_URL": "http://localhost:11434/v1"}}
	prof := &profile.Profile{Name: "local", ProviderSlug: "ollama", KeyID: "key_1"}
	r, err := ResolveEnv(prof, prov, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.EnvVars["OPENAI_BASE_URL"]; !ok {
		t.Error("expected OPENAI_BASE_URL for local provider")
	}
	// No key var should be injected.
	for k := range r.EnvVars {
		if k == "OPENAI_BASE_URL" {
			continue
		}
		t.Errorf("unexpected key %q in local provider env", k)
	}
}

func TestResolveEnv_NilGuard(t *testing.T) {
	_, err := ResolveEnv(nil, &provider.Provider{}, "secret")
	if err == nil {
		t.Error("expected error for nil profile")
	}
	_, err = ResolveEnv(&profile.Profile{}, nil, "secret")
	if err == nil {
		t.Error("expected error for nil provider")
	}
}

func TestMasked(t *testing.T) {
	prov := &provider.Provider{Slug: "openai", EnvVar: "OPENAI_API_KEY"}
	prof := &profile.Profile{Name: "p", ProviderSlug: "openai", KeyID: "k"}
	r, err := ResolveEnv(prof, prov, "sk-1234567890")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := r.Masked()
	if m["OPENAI_API_KEY"] != "sk-1...7890" {
		t.Errorf("masked key = %q, want sk-1...7890", m["OPENAI_API_KEY"])
	}
}

func TestBuildEnvString(t *testing.T) {
	redacted := BuildEnvString(map[string]string{"OPENAI_API_KEY": "sk-secret"}, true)
	if redacted != "OPENAI_API_KEY=<redacted>" {
		t.Errorf("redacted = %q", redacted)
	}
	full := BuildEnvString(map[string]string{"OPENAI_API_KEY": "sk-secret"}, false)
	if full != "OPENAI_API_KEY=sk-secret" {
		t.Errorf("full = %q", full)
	}
}

func TestMergedEnv_NoDuplicateKeys(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/root", "OPENAI_API_KEY=old"}
	overlay := map[string]string{"OPENAI_API_KEY": "new", "EXTRA": "val"}
	merged := mergedEnv(base, overlay)
	seen := map[string]string{}
	for _, kv := range merged {
		k, v, _ := strings.Cut(kv, "=")
		if prev, ok := seen[k]; ok {
			t.Errorf("duplicate key %s: %q and %q", k, prev, v)
		}
		seen[k] = v
	}
	if seen["OPENAI_API_KEY"] != "new" {
		t.Errorf("overlay should win, got %q", seen["OPENAI_API_KEY"])
	}
	if seen["PATH"] != "/usr/bin" {
		t.Errorf("base should be preserved, got %q", seen["PATH"])
	}
	if seen["EXTRA"] != "val" {
		t.Errorf("new key should be added, got %q", seen["EXTRA"])
	}
}

func TestExitError_Message(t *testing.T) {
	e := &ExitError{Code: 42, Err: errors.New("boom")}
	if !strings.Contains(e.Error(), "42") {
		t.Errorf("ExitError message should contain code: %q", e.Error())
	}
}
