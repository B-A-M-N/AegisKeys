package security

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCheckTempEnvfiles_Recursive verifies the stale-temp-env scan walks nested
// directories (e.g. tmp/envfiles/*.env), not just top-level entries.
func TestCheckTempEnvfiles_Recursive(t *testing.T) {
	dir := t.TempDir()
	tmpDir := filepath.Join(dir, "tmp")

	// Create the temp tree up front; the scan must walk it recursively.
	nestedDir := filepath.Join(tmpDir, "envfiles")
	if err := os.MkdirAll(nestedDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Top-level stale env file (old scan caught this).
	oldTop := filepath.Join(tmpDir, "old.env")
	if err := os.WriteFile(oldTop, []byte("KEY=1"), 0600); err != nil {
		t.Fatal(err)
	}

	// Nested stale env file (old scan MISSED this; new scan must catch it).
	oldNested := filepath.Join(nestedDir, "nested.env")
	if err := os.WriteFile(oldNested, []byte("KEY=2"), 0600); err != nil {
		t.Fatal(err)
	}

	// Make both files older than the 24h cutoff.
	oldTime := time.Now().Add(-48 * time.Hour)
	os.Chtimes(oldTop, oldTime, oldTime)
	os.Chtimes(oldNested, oldTime, oldTime)

	// A recent env file must NOT be flagged.
	recent := filepath.Join(tmpDir, "recent.env")
	if err := os.WriteFile(recent, []byte("KEY=3"), 0600); err != nil {
		t.Fatal(err)
	}

	res := checkTempEnvfiles(dir)
	if res.Severity != SeverityWarn {
		t.Fatalf("expected SeverityWarn for stale nested env files, got %d", res.Severity)
	}
	// Both stale files should be mentioned.
	if !contains(res.Message, "old.env") {
		t.Errorf("expected old.env in message, got: %s", res.Message)
	}
	if !contains(res.Message, "nested.env") {
		t.Errorf("expected nested.env in message (recursive scan), got: %s", res.Message)
	}
	// The recent file must not be flagged.
	if contains(res.Message, "recent.env") {
		t.Errorf("recent.env should not be flagged, got: %s", res.Message)
	}
}

func TestCheckAdapterProofStatus(t *testing.T) {
	res := checkAdapterProofStatus()
	if res.Severity != SeverityOK {
		t.Fatalf("expected adapter proof status OK, got severity=%d message=%q fix=%q", res.Severity, res.Message, res.Fix)
	}
	if !contains(res.Message, "0 manual-proof") {
		t.Fatalf("expected manual-proof count in message, got %q", res.Message)
	}
	if !contains(res.Message, "6 verified") {
		t.Fatalf("expected verified count in message, got %q", res.Message)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
