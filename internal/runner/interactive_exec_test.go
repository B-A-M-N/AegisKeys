package runner

import (
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aegiskeys/internal/audit"
)

func TestInteractiveExecCapturesImmediateExitAndAuditsLifecycle(t *testing.T) {
	cmd := exec.Command("sh", "-c", "printf 'final diagnostic\\n' >&2; exit 3")
	run := NewInteractiveExec(cmd)
	run.SetStdin(strings.NewReader(""))
	run.SetStdout(io.Discard)
	run.SetStderr(io.Discard)
	run.Profile = "test-profile"
	run.Provider = "test-provider"
	run.AuditLogger = audit.NewLogger(filepath.Join(t.TempDir(), "audit.log"))

	err := run.Run()
	if err == nil {
		t.Fatal("expected non-zero child exit")
	}
	result := run.Result()
	if !result.Started {
		t.Fatal("child was not recorded as started")
	}
	if result.PID <= 0 {
		t.Fatalf("invalid child pid: %d", result.PID)
	}
	if result.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", result.ExitCode)
	}
	if !strings.Contains(result.OutputTail, "final diagnostic") {
		t.Fatalf("output tail = %q", result.OutputTail)
	}

	events, err := run.AuditLogger.Tail(10)
	if err != nil {
		t.Fatalf("read lifecycle events: %v", err)
	}
	if len(events) != 2 || events[0].Event != "child_started" || events[1].Event != "child_exited" {
		t.Fatalf("unexpected lifecycle events: %#v", events)
	}
	if events[1].Metadata["exit_code"] != "3" {
		t.Fatalf("exit audit metadata = %#v", events[1].Metadata)
	}
}

func TestInteractiveExecStartFailureIsObservable(t *testing.T) {
	run := NewInteractiveExec(exec.Command(filepath.Join(t.TempDir(), "missing-child")))
	run.SetStdin(strings.NewReader(""))
	run.SetStdout(io.Discard)
	run.SetStderr(io.Discard)

	if err := run.Run(); err == nil {
		t.Fatal("expected start failure")
	}
	result := run.Result()
	if result.Started {
		t.Fatal("missing executable was incorrectly recorded as started")
	}
	if result.Duration < 0 {
		t.Fatalf("negative launch duration: %s", result.Duration)
	}
}

func TestInteractiveExecTTYPreflightBlocksNonTerminalChild(t *testing.T) {
	run := NewInteractiveExec(exec.Command("sh", "-c", "exit 0"))
	run.RequireTTY = true
	run.SetStdin(strings.NewReader(""))
	run.SetStdout(io.Discard)
	run.SetStderr(io.Discard)

	err := run.Run()
	if err == nil || !strings.Contains(err.Error(), "interactive launch requires a terminal") {
		t.Fatalf("TTY preflight error = %v", err)
	}
	if run.Result().Started {
		t.Fatal("TTY preflight started a child")
	}
}
