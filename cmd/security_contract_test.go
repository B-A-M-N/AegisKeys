package cmd

import (
	"strings"
	"testing"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/provider"
	"aegiskeys/internal/resolve"
	"aegiskeys/internal/secret"
)

func TestSecretAddCommandsDoNotAcceptSecretFlags(t *testing.T) {
	if keyAddCmd.Flags().Lookup("secret") != nil {
		t.Fatal("key add must not accept --secret; argv leaks through shell history/process listings")
	}
	if vaultAddCmd.Flags().Lookup("secret") != nil {
		t.Fatal("vault add must not accept --secret; argv leaks through shell history/process listings")
	}

	for _, help := range []string{keyAddCmd.UsageString(), vaultAddCmd.UsageString()} {
		if strings.Contains(help, "--secret") {
			t.Fatalf("secret flag still appears in help: %s", help)
		}
	}
}

func TestValidateResolutionRejectsKeyProviderMismatch(t *testing.T) {
	reg := provider.NewRegistry()
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
	}
	prov.Normalize()
	if err := reg.Add(prov); err != nil {
		t.Fatal(err)
	}

	vault := &secret.Vault{Version: 1}
	if err := vault.Add(secret.SecretRecord{
		ID:           "key_mismatch",
		Kind:         secret.SecretAPIKey,
		ProviderSlug: "anthropic",
		Label:        "wrong",
		Secret:       "sk-test-secret",
		Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
	}); err != nil {
		t.Fatal(err)
	}

	prof := profile.Profile{
		Name:         "bad",
		ProviderSlug: "openai",
		KeyID:        "key_mismatch",
		Target:       profile.TargetConfig{App: "generic", Command: "echo"},
	}

	err := resolve.ValidateResolution(prof, reg, vault, adapter.NewRegistry())
	if err == nil || !strings.Contains(err.Error(), "belongs to provider") {
		t.Fatalf("expected provider/key mismatch rejection, got: %v", err)
	}
}

func TestValidateResolutionRejectsUnknownGenericTargetAtSaveTime(t *testing.T) {
	reg := provider.NewRegistry()
	prov := provider.Provider{
		Name:          "OpenAI",
		Slug:          "openai",
		EnvVar:        "OPENAI_API_KEY",
		BaseURL:       "https://api.openai.com/v1",
		Compatibility: provider.CompatOpenAI,
	}
	prov.Normalize()
	if err := reg.Add(prov); err != nil {
		t.Fatal(err)
	}

	vault := &secret.Vault{Version: 1}
	if err := vault.Add(secret.SecretRecord{
		ID:           "key_ok",
		Kind:         secret.SecretAPIKey,
		ProviderSlug: "openai",
		Label:        "ok",
		Secret:       "sk-test-secret",
		Policy:       secret.DefaultSecretPolicy(secret.SecretAPIKey),
	}); err != nil {
		t.Fatal(err)
	}

	prof := profile.Profile{
		Name:         "bad-app",
		ProviderSlug: "openai",
		KeyID:        "key_ok",
		Target:       profile.TargetConfig{App: "not-real"},
	}

	err := resolve.ValidateResolution(prof, reg, vault, adapter.NewRegistry())
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected unknown target rejection, got: %v", err)
	}
}
