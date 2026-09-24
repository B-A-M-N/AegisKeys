package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aegiskeys/internal/config"
)

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
		if strings.TrimSpace(string(raw)) != "{}" {
			t.Fatalf("reset %s = %q", path, raw)
		}
	}
}
