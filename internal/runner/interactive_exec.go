package runner

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"aegiskeys/internal/audit"
	"golang.org/x/term"
)

const launchOutputTailLimit = 64 * 1024

// InteractiveExec implements Bubble Tea's ExecCommand contract while keeping
// launch lifecycle facts available after the terminal is restored.
type InteractiveExec struct {
	Cmd *exec.Cmd

	AuditLogger *audit.Logger
	Profile     string
	Provider    string
	RequireTTY  bool

	mu         sync.Mutex
	result     InteractiveExecResult
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	outputTail *tailBuffer
}

type InteractiveExecResult struct {
	Started   bool
	PID       int
	StartedAt time.Time
	Duration  time.Duration
	ExitCode  int
	Signal    string

	StdinTTY  bool
	StdoutTTY bool
	StderrTTY bool

	OutputTail string
}

func NewInteractiveExec(cmd *exec.Cmd) *InteractiveExec {
	return &InteractiveExec{Cmd: cmd, outputTail: newTailBuffer(launchOutputTailLimit)}
}

func (e *InteractiveExec) SetStdin(r io.Reader)  { e.stdin = r }
func (e *InteractiveExec) SetStdout(w io.Writer) { e.stdout = w }
func (e *InteractiveExec) SetStderr(w io.Writer) { e.stderr = w }

func (e *InteractiveExec) Run() error {
	if e.Cmd == nil {
		return fmt.Errorf("interactive launch command is nil")
	}
	if e.outputTail == nil {
		e.outputTail = newTailBuffer(launchOutputTailLimit)
	}

	startedAt := time.Now()
	var controllingTTY *os.File
	if e.RequireTTY {
		// Bubble Tea releases the terminal before Run, but its internal output
		// writer is not guaranteed to be an *os.File. Bind the process's real
		// terminal files directly so CLI agents observe isTTY on all streams.
		// Wrapping a terminal in io.MultiWriter would create an exec pipe.
		e.Cmd.Stdin = os.Stdin
		e.Cmd.Stdout = os.Stdout
		e.Cmd.Stderr = os.Stderr
	}

	stdinTTY := isTerminal(e.Cmd.Stdin)
	stdoutTTY := isTerminal(e.Cmd.Stdout)
	stderrTTY := isTerminal(e.Cmd.Stderr)
	if e.RequireTTY && (!stdinTTY || !stdoutTTY) {
		var err error
		controllingTTY, err = OpenControllingTTY()
		if err != nil {
			e.recordInitialResult(startedAt, stdinTTY, stdoutTTY, stderrTTY)
			e.finish(startedAt)
			e.log("child_start_failed", map[string]string{"duration_ms": strconv.FormatInt(time.Since(startedAt).Milliseconds(), 10)})
			return fmt.Errorf(
				"interactive launch requires a terminal (stdin_tty=%t stdout_tty=%t stderr_tty=%t): %w",
				stdinTTY, stdoutTTY, stderrTTY, err,
			)
		}
		defer controllingTTY.Close()
		e.Cmd.Stdin = controllingTTY
		e.Cmd.Stdout = controllingTTY
		e.Cmd.Stderr = controllingTTY
		stdinTTY, stdoutTTY, stderrTTY = true, true, true
	}
	if !e.RequireTTY {
		if e.Cmd.Stdin == nil {
			e.Cmd.Stdin = e.stdin
		}
		if e.Cmd.Stdout == nil {
			e.Cmd.Stdout = withTail(e.stdout, e.outputTail)
		}
		if e.Cmd.Stderr == nil {
			e.Cmd.Stderr = withTail(e.stderr, e.outputTail)
		}
		stdinTTY = isTerminal(e.Cmd.Stdin)
		stdoutTTY = isTerminal(e.Cmd.Stdout)
		stderrTTY = isTerminal(e.Cmd.Stderr)
	}
	e.recordInitialResult(startedAt, stdinTTY, stdoutTTY, stderrTTY)

	if e.RequireTTY && (!stdinTTY || !stdoutTTY) {
		// Kept as a defensive guard for non-Unix fallbacks.
		e.finish(startedAt)
		e.log("child_start_failed", map[string]string{"duration_ms": strconv.FormatInt(time.Since(startedAt).Milliseconds(), 10)})
		return fmt.Errorf(
			"interactive launch requires a terminal (stdin_tty=%t stdout_tty=%t stderr_tty=%t)",
			stdinTTY, stdoutTTY, stderrTTY,
		)
	}

	if err := e.Cmd.Start(); err != nil {
		e.finish(startedAt)
		e.log("child_start_failed", map[string]string{"duration_ms": strconv.FormatInt(time.Since(startedAt).Milliseconds(), 10)})
		return fmt.Errorf("start child process: %w", err)
	}

	e.mu.Lock()
	e.result.Started = true
	if e.Cmd.Process != nil {
		e.result.PID = e.Cmd.Process.Pid
	}
	pid := e.result.PID
	e.mu.Unlock()
	started := e.Result()
	e.log("child_started", map[string]string{
		"pid":        strconv.Itoa(pid),
		"stdin_tty":  strconv.FormatBool(started.StdinTTY),
		"stdout_tty": strconv.FormatBool(started.StdoutTTY),
		"stderr_tty": strconv.FormatBool(started.StderrTTY),
	})

	err := e.Cmd.Wait()
	e.finish(startedAt)
	result := e.Result()
	e.log("child_exited", map[string]string{
		"pid":         strconv.Itoa(result.PID),
		"exit_code":   strconv.Itoa(result.ExitCode),
		"signal":      result.Signal,
		"duration_ms": strconv.FormatInt(result.Duration.Milliseconds(), 10),
	})
	return err
}

func (e *InteractiveExec) recordInitialResult(startedAt time.Time, stdinTTY, stdoutTTY, stderrTTY bool) {
	e.mu.Lock()
	e.result = InteractiveExecResult{
		StartedAt: startedAt,
		StdinTTY:  stdinTTY,
		StdoutTTY: stdoutTTY,
		StderrTTY: stderrTTY,
	}
	e.mu.Unlock()
}

func (e *InteractiveExec) finish(startedAt time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.result.Duration = time.Since(startedAt)
	e.result.OutputTail = e.outputTail.String()
	if state := e.Cmd.ProcessState; state != nil {
		e.result.ExitCode = state.ExitCode()
		e.result.Signal = processSignal(state)
	}
}

func (e *InteractiveExec) Result() InteractiveExecResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.result
}

func (e *InteractiveExec) log(event string, metadata map[string]string) {
	if e.AuditLogger == nil {
		return
	}
	e.AuditLogger.Log(audit.Event{Event: event, Profile: e.Profile, Provider: e.Provider, Metadata: metadata})
}

func isTerminal(value any) bool {
	file, ok := value.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func withTail(dst io.Writer, tail *tailBuffer) io.Writer {
	if dst == nil {
		return tail
	}
	return io.MultiWriter(dst, tail)
}

type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newTailBuffer(limit int) *tailBuffer { return &tailBuffer{limit: limit} }

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		excess := len(b.data) - b.limit
		copy(b.data, b.data[excess:])
		b.data = b.data[:b.limit]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.Clone(b.data))
}
