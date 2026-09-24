package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"aegiskeys/internal/config"
)

func TestRecoveryLeavesHealthyFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	healthy := []byte(`{"version":3,"providers":[]}`)
	if err := os.WriteFile(config.ProvidersPath(dir), healthy, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProfilesPath(dir), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = old; r.Close() }()
	_, _ = w.WriteString("RESET\n")
	w.Close()
	if err := recoverMalformedConfig(dir); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(config.ProvidersPath(dir))
	if string(after) != string(healthy) {
		t.Fatal("healthy providers file was modified")
	}
	matches, _ := filepath.Glob(config.ProvidersPath(dir) + ".damaged.*")
	if len(matches) != 0 {
		t.Fatal("healthy providers file was backed up")
	}
}

func TestRecoverMalformedConfigBacksUpAndResets(t *testing.T) {
	dir := t.TempDir()
	paths := []string{config.ProvidersPath(dir), config.ProfilesPath(dir), config.ConfigPath(dir)}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = old; r.Close() }()
	_, _ = w.WriteString("RESET\n")
	w.Close()
	if err := recoverMalformedConfig(dir); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		matches, _ := filepath.Glob(path + ".damaged.*")
		if len(matches) != 1 {
			t.Fatalf("backup for %s = %v", path, matches)
		}
		raw, _ := os.ReadFile(path)
		if !json.Valid(raw) {
			t.Fatalf("reset %s is invalid JSON", path)
		}
	}
}
