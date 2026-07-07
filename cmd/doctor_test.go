package cmd

import (
	"encoding/json"
	"testing"

	"aegiskeys/internal/security"
)

func TestBuildDoctorOutputJSONShape(t *testing.T) {
	out := buildDoctorOutput("/tmp/aegiskeys-test", []security.CheckResult{
		{Severity: security.SeverityOK, Message: "ok"},
		{Severity: security.SeverityWarn, Message: "warn", Fix: "fix it"},
	})
	if out.ConfigDir != "/tmp/aegiskeys-test" {
		t.Fatalf("config dir = %q", out.ConfigDir)
	}
	if out.Overall != "WARN" {
		t.Fatalf("overall = %q, want WARN", out.Overall)
	}
	if len(out.Checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(out.Checks))
	}
	if out.Checks[1].Severity != "WARN" || out.Checks[1].Fix != "fix it" {
		t.Fatalf("unexpected warning check: %+v", out.Checks[1])
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected JSON output")
	}
}
