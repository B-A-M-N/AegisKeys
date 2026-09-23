package secret

import (
	"path/filepath"
	"sync"
	"testing"
)

func newTransactionalVaultForTest(t *testing.T) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.enc")
	password := "transaction-test-password"
	if err := InitVault(path, password); err != nil {
		t.Fatalf("InitVault: %v", err)
	}
	return path, password
}

func loadRecordIDs(t *testing.T, path, password string) map[string]SecretRecord {
	t.Helper()
	v, err := LoadVault(path, password)
	if err != nil {
		t.Fatalf("LoadVault: %v", err)
	}
	out := make(map[string]SecretRecord, len(v.Keys))
	for _, rec := range v.Keys {
		out[rec.ID] = rec
	}
	return out
}

func TestMutateVault_DeletePersists(t *testing.T) {
	path, password := newTransactionalVaultForTest(t)
	var doomed string
	if err := MutateVault(path, password, func(v *Vault) error {
		if err := v.Add(SecretRecord{ID: "delete-me", Secret: "old-secret"}); err != nil {
			return err
		}
		doomed = "delete-me"
		return nil
	}); err != nil {
		t.Fatalf("seed mutation: %v", err)
	}
	if err := MutateVault(path, password, func(v *Vault) error {
		return v.Remove(doomed)
	}); err != nil {
		t.Fatalf("delete mutation: %v", err)
	}
	records := loadRecordIDs(t, path, password)
	if _, ok := records[doomed]; ok {
		t.Fatal("deleted record was resurrected")
	}
}

func TestMutateVault_IndependentExistingRecordUpdatesBothSurvive(t *testing.T) {
	path, password := newTransactionalVaultForTest(t)
	if err := MutateVault(path, password, func(v *Vault) error {
		return v.Add(SecretRecord{ID: "A", Secret: "old-a"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := MutateVault(path, password, func(v *Vault) error {
		return v.Add(SecretRecord{ID: "B", Secret: "old-b"})
	}); err != nil {
		t.Fatal(err)
	}

	const writers = 4
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, value := "A", "rotated-A"
			if i%2 == 1 {
				id, value = "B", "rotated-B"
			}
			errs[i] = MutateVault(path, password, func(v *Vault) error {
				return v.Rotate(id, value)
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	records := loadRecordIDs(t, path, password)
	if records["A"].Secret != "rotated-A" || records["B"].Secret != "rotated-B" {
		t.Fatalf("independent rotations lost: A=%q B=%q", records["A"].Secret, records["B"].Secret)
	}
}

func TestMutateVault_ConcurrentAddAndDeleteBothSurvive(t *testing.T) {
	path, password := newTransactionalVaultForTest(t)
	if err := MutateVault(path, password, func(v *Vault) error {
		return v.Add(SecretRecord{ID: "Y", Secret: "delete-target"})
	}); err != nil {
		t.Fatal(err)
	}

	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = MutateVault(path, password, func(v *Vault) error {
			return v.Add(SecretRecord{ID: "X", Secret: "added-secret"})
		})
	}()
	go func() {
		defer wg.Done()
		errs[1] = MutateVault(path, password, func(v *Vault) error {
			return v.Remove("Y")
		})
	}()
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("operation %d: %v", i, err)
		}
	}
	records := loadRecordIDs(t, path, password)
	if _, ok := records["X"]; !ok {
		t.Error("concurrently added record was lost")
	}
	if _, ok := records["Y"]; ok {
		t.Error("concurrently deleted record survived")
	}
}

func TestMutateVault_ConcurrentIndependentScratchpadAndKeyMutations(t *testing.T) {
	path, password := newTransactionalVaultForTest(t)
	var scratchID string
	if err := MutateVault(path, password, func(v *Vault) error {
		if err := v.Add(SecretRecord{ID: "scratch-key", Secret: "key-secret"}); err != nil {
			return err
		}
		if err := v.AddScratchPad(ScratchPadRecord{ID: "old-scratch", Body: "old"}); err != nil {
			return err
		}
		scratchID = "old-scratch"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := MutateVault(path, password, func(v *Vault) error {
		if err := v.Remove("scratch-key"); err != nil {
			return err
		}
		return v.RemoveScratchPad(scratchID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := MutateVault(path, password, func(v *Vault) error {
		if err := v.AddScratchPad(ScratchPadRecord{ID: "new-scratch", Body: "new"}); err != nil {
			return err
		}
		return v.Add(SecretRecord{ID: "new-key", Secret: "new-key-secret"})
	}); err != nil {
		t.Fatal(err)
	}

	v, err := LoadVault(path, password)
	if err != nil {
		t.Fatal(err)
	}
	if v.Get(scratchID) != nil || v.GetScratchPad(scratchID) != nil || v.Get("scratch-key") != nil {
		t.Fatal("deleted records were resurrected after unrelated mutation")
	}
	if v.Get("new-key") == nil || v.GetScratchPad("new-scratch") == nil {
		t.Fatal("new records were lost after transactional mutation")
	}
}
