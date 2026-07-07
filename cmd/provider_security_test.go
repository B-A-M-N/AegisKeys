package cmd

import (
	"strings"
	"testing"

	"aegiskeys/internal/provider"
)

func TestRedactProviderOutput(t *testing.T) {
	raw := "sk-abcdefghijklmnopqrstuvwxyz1234567890"
	out := redactProviderOutput("Authorization: Bearer " + raw)
	if strings.Contains(out, raw) {
		t.Fatalf("provider output leaked raw secret: %s", out)
	}
	if !strings.Contains(out, "<redacted>") {
		t.Fatalf("provider output was not redacted: %s", out)
	}
}

func TestValidateProviderRegistryForExportRefusesSecrets(t *testing.T) {
	raw := "sk-abcdefghijklmnopqrstuvwxyz1234567890"
	reg := &provider.Registry{Providers: []provider.Provider{{
		ID:            "bad",
		Name:          "Bad",
		Slug:          "bad",
		BaseURL:       "https://api.example.com/v1",
		EnvVar:        "BAD_API_KEY",
		AuthHeader:    "Authorization: Bearer " + raw,
		Compatibility: provider.CompatOpenAI,
	}}}

	err := validateProviderRegistryForExport(reg)
	if err == nil {
		t.Fatal("expected provider export validation to reject secret-bearing metadata")
	}
	if strings.Contains(err.Error(), raw) {
		t.Fatalf("export validation error leaked raw secret: %v", err)
	}
}

func TestValidateProviderRegistryForExportAllowsDefaults(t *testing.T) {
	reg := provider.NewRegistry()
	if err := validateProviderRegistryForExport(reg); err != nil {
		t.Fatalf("default providers should be exportable: %v", err)
	}
}
