package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aegiskeys/internal/config"
)

func TestRecoveryCrashLeavesOriginalsAtCanonicalPaths(t *testing.T) {
	points := []string{"before-replace:providers", "before-replace:profiles", "before-replace:settings"}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			dir := t.TempDir()
			paths := []string{config.ProvidersPath(dir), config.ProfilesPath(dir), config.ConfigPath(dir)}
			for _, path := range paths {
				if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(os.Args[0], "-test.run=TestRecoveryCrashChildProcess")
			cmd.Env = append(os.Environ(), "AEGISKEYS_RECOVERY_CRASH_CHILD=1", "AEGISKEYS_RECOVERY_DIR="+dir, "AEGISKEYS_RECOVERY_POINT="+point)
			if err := cmd.Run(); err == nil {
				t.Fatal("recovery crash child exited successfully")
			}
			for _, path := range paths {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("canonical file missing after crash: %s", path)
				}
				if string(raw) != "damaged" && !json.Valid(raw) {
					t.Fatalf("partial invalid file %s: %q", path, raw)
				}
				matches, _ := filepath.Glob(path + ".damaged.*")
				if len(matches) == 0 {
					t.Fatalf("damaged backup missing for %s", path)
				}
			}
		})
	}
}

func TestRecoveryCrashChildProcess(t *testing.T) {
	if os.Getenv("AEGISKEYS_RECOVERY_CRASH_CHILD") == "" {
		t.Skip("helper")
	}
	point := os.Getenv("AEGISKEYS_RECOVERY_POINT")
	recoveryCrashPoint = func(name string) {
		if name == point {
			os.Exit(24)
		}
	}
	old := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = old; r.Close() }()
	_, _ = w.WriteString("RESET\n")
	w.Close()
	if err := recoverMalformedConfig(os.Getenv("AEGISKEYS_RECOVERY_DIR")); err != nil {
		t.Fatal(err)
	}
}
