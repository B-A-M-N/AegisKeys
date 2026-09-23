//go:build linux

package broker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxPeerResolverHashMatchesFile(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "exe")
	if err := os.WriteFile(exe, []byte("binary"), 0700); err != nil {
		t.Fatal(err)
	}
	hash, err := HashExecutable(exe)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("binary"))
	if hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash mismatch: %s", hash)
	}
	if _, err := HashExecutable(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing executable error = %v", err)
	}
}
