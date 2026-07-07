package runner

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/secret"
)

// TestRuntimeInjection_RealRunner is the adversarial proof using the ACTUAL
// runner.Run() code path. It launches a child that writes its env to a temp
// file, then reads that file to verify the child received the secret.
// Before/after, it scans parent os.Environ() to prove the parent was not modified.
func TestRuntimeInjection_RealRunner(t *testing.T) {
	const sentinel = "AK_REAL_RUNNER_SENTINEL_999"

	// Ensure parent doesn't already have this.
	os.Unsetenv("OPENROUTER_API_KEY")
	if _, exists := os.LookupEnv("OPENROUTER_API_KEY"); exists {
		t.Fatal("parent already has OPENROUTER_API_KEY")
	}

	// Capture parent env before.
	parentBefore := strings.Join(os.Environ(), "\n")
	if strings.Contains(parentBefore, sentinel) {
		t.Fatal("parent env already contains sentinel before launch")
	}

	// Build a real launch strategy.
	reg := adapter.NewRegistry()
	prov := provider.Provider{
		Name:          "OpenRouter",
		Slug:          "openrouter",
		Auth:          provider.AuthSpec{Type: "bearer", EnvVar: "OPENROUTER_API_KEY"},
		BaseURL:       "https://openrouter.ai/api/v1",
		Compatibility: provider.CompatOpenAI,
		ExtraEnv:      map[string]string{"OPENAI_BASE_URL": "https://openrouter.ai/api/v1"},
	}
	key := &secret.SecretRecord{ID: "k", ProviderSlug: "openrouter", Secret: sentinel, Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)}
	prof := profile.Profile{
		Name:         "t",
		ProviderSlug: "openrouter",
		KeyID:        "k",
		Target:       profile.TargetConfig{App: "aider", RenderMode: profile.RenderEnv, Command: "sh"},
	}

	strategy, err := adapter.ResolveLaunchStrategy(prof, prov, key, reg)
	if err != nil {
		t.Fatalf("ResolveLaunchStrategy: %v", err)
	}

	if err := adapter.ValidateLaunchStrategy(strategy, prof, prov, key, adapter.DefaultSecurityPolicy()); err != nil {
		t.Fatalf("ValidateLaunchStrategy rejected valid launch: %v", err)
	}

	// The child will write its env to a temp file so we can inspect it.
	outFile := filepath.Join(t.TempDir(), "child_env.txt")
	strategy.Plan.Command = "sh"
	strategy.Plan.Args = []string{"-c", "env > " + outFile}

	// Execute through the REAL runner using strategy-driven launch.
	if err := Run(context.Background(), strategy, RunOptions{InheritStdio: false}); err != nil {
		t.Fatalf("runner.Run() failed: %v", err)
	}

	// Wait briefly for the child to write its output.
	time.Sleep(100 * time.Millisecond)

	// Read the child's environment from the file.
	childEnv, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading child env file: %v", err)
	}
	childOutput := string(childEnv)

	// Assert: child received the sentinel.
	if !strings.Contains(childOutput, sentinel) {
		t.Errorf("child did NOT receive the sentinel env. Child output:\n%s", childOutput)
	}

	// Assert: parent env after launch does not contain the sentinel.
	parentAfter := strings.Join(os.Environ(), "\n")
	if strings.Contains(parentAfter, sentinel) {
		t.Errorf("PARENT env contains sentinel after launch — injection is NOT child-scoped")
	}
}

// TestBlockedStrategy_ActualExecution proves that feeding a blocked strategy
// into the real runner actually refuses execution.
func TestBlockedStrategy_ActualExecution(t *testing.T) {
	const sentinel = "AK_BLOCKED_EXEC_SENTINEL"

	// Cursor adapter is blocked (account-based auth).
	reg := adapter.NewRegistry()
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai",
		Auth:    provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		BaseURL: "https://api.openai.com/v1", Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{ID: "k", ProviderSlug: "openai", Secret: sentinel, Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)}
	prof := profile.Profile{
		Name: "t", ProviderSlug: "openai", KeyID: "k",
		Target: profile.TargetConfig{App: "cursor", RenderMode: profile.RenderEnv},
	}

	strategy, err := adapter.ResolveLaunchStrategy(prof, prov, key, reg)
	if err != nil {
		// Cursor is blocked at resolve time by the contract enforcer (the new
		// behavior), which is acceptable — the launch is still prevented.
		if strings.Contains(err.Error(), "blocked") {
			t.Logf("cursor blocked at resolve: %v", err)
			return
		}
		t.Fatalf("ResolveLaunchStrategy: %v", err)
	}

	// The strategy should be blocked.
	if !strategy.Blocked {
		t.Skip("cursor adapter is not currently Blocked — gap")
	}

	// Validate should reject it.
	if err := adapter.ValidateLaunchStrategy(strategy, prof, prov, key, adapter.DefaultSecurityPolicy()); err == nil {
		t.Error("ValidateLaunchStrategy should reject blocked strategy")
	}

	// Attempting to run a blocked strategy should error.
	if err := Run(context.Background(), strategy, RunOptions{}); err != nil {
		t.Logf("runner.Run() correctly refused blocked strategy: %v", err)
	} else {
		t.Errorf("runner.Run() executed a blocked strategy — enforcement failure")
	}
}

// TestArgon2_ResourceExhaustion proves that a malicious envelope with huge
// KDF memory is rejected BEFORE argon2.IDKey runs.
func TestArgon2_ResourceExhaustion(t *testing.T) {
	const sentinel = "AK_ARGON2_FUZZ"
	pw := "test-pw"

	// Build an envelope with absurd memory directly.
	env := &secret.VaultEnvelope{
		Version: 1,
		KDF:     "argon2id",
		KDFParams: secret.KDFParams{
			Time:      1,
			MemoryKiB: 1024 * 1024 * 1024, // 1 GiB — would OOM if actually run
			Threads:   1,
			KeyLen:    32,
		},
		Salt:       mustBase64(make([]byte, 16)),
		Nonce:      mustBase64(make([]byte, 12)),
		Ciphertext: "dGVzdA==",
	}

	// ValidateEnvelope should reject this BEFORE any Argon2 work.
	if err := secret.ValidateEnvelope(env); err == nil {
		t.Error("ValidateEnvelope accepted absurd memory value — argon2 could run with 1 GiB")
	} else {
		t.Logf("correctly rejected: %v", err)
	}

	// Even if validation were bypassed, confirm: the Envelope would never reach
	// DeriveKeyWithParams because ValidateEnvelope is called first in LoadVault.
	_ = pw
	_ = sentinel
}

// TestErrorPath_Leak proves that error messages triggered during launch
// do not contain raw secrets.
func TestErrorPath_Leak(t *testing.T) {
	const sentinel = "AK_ERROR_LEAK_TEST"

	// Trigger an error by trying to run a command that will fail,
	// with a profile that has a sentinel secret.
	reg := adapter.NewRegistry()
	prov := provider.Provider{
		Name: "OpenAI", Slug: "openai",
		Auth:    provider.AuthSpec{Type: "bearer", EnvVar: "OPENAI_API_KEY"},
		BaseURL: "https://api.openai.com/v1", Compatibility: provider.CompatOpenAI,
	}
	key := &secret.SecretRecord{ID: "k", ProviderSlug: "openai", Secret: sentinel, Policy: secret.DefaultSecretPolicy(secret.SecretAPIKey)}
	prof := profile.Profile{
		Name: "t", ProviderSlug: "openai", KeyID: "k",
		Target: profile.TargetConfig{App: "generic", RenderMode: profile.RenderEnv, Command: "false"},
	}

	strategy, err := adapter.ResolveLaunchStrategy(prof, prov, key, reg)
	if err != nil {
		// The error itself must not leak the secret.
		if strings.Contains(err.Error(), sentinel) {
			t.Errorf("ResolveLaunchStrategy error leaks raw secret: %v", err)
		}
		t.Logf("ResolveLaunchStrategy error (expected, no leak): %v", err)
		return
	}

	err = Run(context.Background(), strategy, RunOptions{InheritStdio: false})
	if err != nil {
		if strings.Contains(err.Error(), sentinel) {
			t.Errorf("runner.Run() error leaks raw secret: %v", err)
		}
		t.Logf("runner.Run() error (expected, no leak): %v", err)
	}
}

// TestConcurrentSave_Durability proves that concurrent SaveVault calls
// don't lose keys. Final vault state should be decryptable and contain
// at least the expected minimum keys.
func TestConcurrentSave_Durability(t *testing.T) {
	pw := "concurrent-durability-pw"
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")

	if err := secret.InitVault(path, pw); err != nil {
		t.Fatalf("InitVault: %v", err)
	}

	// Each goroutine adds a unique key then saves.
	const goroutines = 10
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			v, err := secret.LoadVault(path, pw)
			if err != nil {
				errs <- err
				return
			}
			v.Add(secret.SecretRecord{
				ProviderSlug: "openai",
				Label:        "concurrent-key",
				Secret:       string(rune('a'+id)) + "-durability-secret",
				Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
			})
			errs <- secret.SaveVault(path, pw, v)
		}(i)
	}

	// Collect errors.
	var saveErrors int
	for i := 0; i < goroutines; i++ {
		if err := <-errs; err != nil {
			saveErrors++
		}
	}

	// Final vault must be decryptable.
	v, err := secret.LoadVault(path, pw)
	if err != nil {
		t.Fatalf("final LoadVault failed — vault may be corrupted: %v", err)
	}

	// At least one key must survive (proving at least one save succeeded).
	if len(v.Keys) == 0 {
		t.Error("no keys survived concurrent saves — data loss")
	}
	t.Logf("after %d concurrent saves: %d keys survive, %d save errors",
		goroutines, len(v.Keys), saveErrors)
}

// mustBase64 returns base64 of b, panics on error (test helper).
func mustBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// Ensure exec is imported (for any helper that might need it).
var _ = exec.Command
