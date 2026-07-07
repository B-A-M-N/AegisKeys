package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerCreatesParentAndLocksPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "audit.log")
	l := NewLogger(path)
	l.Log(Event{Event: "test_event", Profile: "profile"})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected audit log to be created: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("audit log mode = %o, want 0600", got)
	}

	events, err := l.Tail(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Event != "test_event" {
		t.Fatalf("unexpected audit events: %#v", events)
	}
}

func TestLoggerRepairsPermissiveAuditLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	l := NewLogger(path)
	l.Log(Event{Event: "repair_mode"})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("audit log mode = %o, want 0600", got)
	}
}

func TestLoggerRedactsSecretLookingMetadata(t *testing.T) {
	raw := "sk-abcdefghijklmnopqrstuvwxyz1234567890"
	path := filepath.Join(t.TempDir(), "audit.log")
	l := NewLogger(path)
	l.Log(Event{
		Event:   "launch",
		Command: "tool --header Authorization: Bearer " + raw,
		Metadata: map[string]string{
			"output": "token=" + raw,
		},
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), raw) {
		t.Fatalf("audit log leaked raw secret: %s", string(data))
	}
	if !strings.Contains(string(data), "redacted") {
		t.Fatalf("audit log did not redact secret-looking values: %s", string(data))
	}
}
