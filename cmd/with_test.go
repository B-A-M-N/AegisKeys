package cmd

import (
	"testing"

	"aegiskeys/internal/secret"
)

func TestFindVaultRecordByLabelOrIDRejectsAmbiguousLabels(t *testing.T) {
	v := &secret.Vault{Keys: []secret.SecretRecord{
		{ID: "key_a", Label: "shared", Secret: "a"},
		{ID: "key_b", Label: "shared", Secret: "b"},
	}}
	if got := findVaultRecordByLabelOrID(v, "shared"); got != nil {
		t.Fatalf("ambiguous label selected %q", got.ID)
	}
}

func TestFindVaultRecordByLabelOrIDResolvesUniqueID(t *testing.T) {
	want := &secret.SecretRecord{ID: "key_a", Label: "shared", Secret: "a"}
	v := &secret.Vault{Keys: []secret.SecretRecord{*want}}
	if got := findVaultRecordByLabelOrID(v, want.ID); got == nil || got.ID != want.ID {
		t.Fatalf("ID lookup returned %#v, want ID %q", got, want.ID)
	}
}

func TestValidEnvName(t *testing.T) {
	for _, name := range []string{"OPENROUTER_API_KEY", "A1", "_PRIVATE"} {
		if !validEnvName(name) {
			t.Errorf("validEnvName(%q) = false", name)
		}
	}
	for _, name := range []string{"", "1BAD", "OPENROUTER-API-KEY", "OPENROUTER API KEY"} {
		if validEnvName(name) {
			t.Errorf("validEnvName(%q) = true", name)
		}
	}
}
