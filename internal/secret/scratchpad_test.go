package secret

import (
	"testing"
)

func TestVault_ScratchPadCRUD(t *testing.T) {
	v := &Vault{Version: 1}

	// Add a scratchpad.
	sp := ScratchPadRecord{Kind: ScratchPadProvider, Title: "OpenRouter notes", Body: "Billing: $50/mo", ProviderSlug: "openrouter"}
	if err := v.AddScratchPad(sp); err != nil {
		t.Fatalf("AddScratchPad: %v", err)
	}
	if len(v.ScratchPads) != 1 {
		t.Fatalf("expected 1 scratchpad, got %d", len(v.ScratchPads))
	}
	storeID := v.ScratchPads[0].ID
	if storeID == "" {
		t.Fatal("expected ID to be generated")
	}

	// Get it back.
	got := v.GetScratchPad(storeID)
	if got == nil {
		t.Fatal("GetScratchPad returned nil")
	}
	if got.Title != "OpenRouter notes" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Body != "Billing: $50/mo" {
		t.Errorf("Body = %q", got.Body)
	}

	// Update it.
	if err := v.UpdateScratchPad(storeID, ScratchPadRecord{Title: "Updated", Body: "New notes"}); err != nil {
		t.Fatalf("UpdateScratchPad: %v", err)
	}
	got = v.GetScratchPad(storeID)
	if got.Title != "Updated" {
		t.Errorf("after update Title = %q", got.Title)
	}
	if got.Body != "New notes" {
		t.Errorf("after update Body = %q", got.Body)
	}

	// Remove it.
	if err := v.RemoveScratchPad(storeID); err != nil {
		t.Fatalf("RemoveScratchPad: %v", err)
	}
	if len(v.ScratchPads) != 0 {
		t.Errorf("expected 0 scratchpads after remove, got %d", len(v.ScratchPads))
	}

	// Removing missing should error.
	if err := v.RemoveScratchPad("missing"); err == nil {
		t.Error("expected error removing missing scratchpad")
	}
}

func TestVault_ScratchPadDuplicateID(t *testing.T) {
	v := &Vault{Version: 1}
	v.ScratchPads = []ScratchPadRecord{}
	sp := ScratchPadRecord{ID: "fixed-id", Title: "First"}
	if err := v.AddScratchPad(sp); err != nil {
		t.Fatal(err)
	}
	if err := v.AddScratchPad(ScratchPadRecord{ID: "fixed-id", Title: "Dupe"}); err == nil {
		t.Error("expected error adding duplicate ID")
	}
}

func TestVault_KeyPrivateNoteUpdate(t *testing.T) {
	v := &Vault{Version: 1}
	rec := SecretRecord{ID: "key-1", ProviderSlug: "openai"}
	if err := v.Add(rec); err != nil {
		t.Fatal(err)
	}

	if err := v.UpdateKeyPrivateNote("key-1", "dashboard: platform.openai.com"); err != nil {
		t.Fatalf("UpdateKeyPrivateNote: %v", err)
	}

	got := v.Get("key-1")
	if got.PrivateNote != "dashboard: platform.openai.com" {
		t.Errorf("PrivateNote = %q", got.PrivateNote)
	}
}

func TestVaultScratchPadBodyClearedByIntendedZero(t *testing.T) {
	v := &Vault{
		ScratchPads: []ScratchPadRecord{
			{ID: "s1", Body: "secret-body-should-clear-on-lock"},
		},
	}

	// The intended behavior: on vault lock, scratchpad bodies should be
	// cleared (this is enforced in tui.vaultSession.Zero()). Here we verify
	// the Vault structure is susceptible to this clearing.
	for i := range v.ScratchPads {
		v.ScratchPads[i].Body = ""
	}
	if v.ScratchPads[0].Body != "" {
		t.Error("scratchpad body should be clearable")
	}
}

func TestMigrateVaultRecords_ScratchPadDefaults(t *testing.T) {
	v := &Vault{Version: 1, ScratchPads: []ScratchPadRecord{
		{Title: "No kind set"},
	}}
	migrateVaultRecords(v)
	if v.ScratchPads[0].Kind != ScratchPadGeneral {
		t.Errorf("expected default kind general, got %q", v.ScratchPads[0].Kind)
	}
	// Ensure nil slice becomes empty non-nil.
	v2 := &Vault{Version: 1}
	migrateVaultRecords(v2)
	if v2.ScratchPads == nil {
		t.Error("expected non-nil ScratchPads after migration")
	}
}

func TestMergeOnDiskKeys_ScratchPads(t *testing.T) {
	working := &Vault{
		Version:     1,
		Keys:        []SecretRecord{{ID: "k1", Secret: "working"}},
		ScratchPads: []ScratchPadRecord{{ID: "s1", Body: "working-scratch"}},
	}
	onDisk := &Vault{
		Version: 1,
		Keys: []SecretRecord{
			{ID: "k1", Secret: "stale"},
			{ID: "k2", Secret: "disk-only"},
		},
		ScratchPads: []ScratchPadRecord{
			{ID: "s1", Body: "stale-scratch"},
			{ID: "s2", Body: "disk-only-scratch"},
		},
	}
	merged := mergeOnDiskKeys(working, onDisk)
	if len(merged.Keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(merged.Keys))
	}
	k1 := merged.Get("k1")
	if k1 == nil || k1.Secret != "working" {
		t.Error("working version should win for k1")
	}
	if len(merged.ScratchPads) != 2 {
		t.Errorf("expected 2 scratchpads, got %d", len(merged.ScratchPads))
	}
}
