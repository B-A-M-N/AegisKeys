package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"aegiskeys/internal/fsutil"
	"aegiskeys/internal/redact"
)

type Event struct {
	Time     time.Time         `json:"time"`
	Event    string            `json:"event"`
	Provider string            `json:"provider,omitempty"`
	Profile  string            `json:"profile,omitempty"`
	Command  string            `json:"command,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Logger struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
}

func NewLogger(path string) *Logger {
	return &Logger{path: path, maxBytes: 8 << 20}
}

func (l *Logger) Log(e Event) error {
	e.Time = time.Now()
	e = sanitizeEvent(e)
	if err := validateEvent(e); err != nil {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := fsutil.EnsureDir(filepath.Dir(l.path)); err != nil {
		return err
	}
	if err := rejectPathChain(l.path); err != nil {
		return err
	}
	if info, statErr := os.Lstat(l.path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("audit log path is a symlink")
	}
	if err := rotateIfNeeded(l.path, l.maxBytes, int64(len(line)+1)); err != nil {
		return err
	}
	f, err := fsutil.OpenAppendFile(l.path, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if l.maxBytes > 0 {
		info, statErr := f.Stat()
		if statErr != nil {
			return statErr
		}
		if l.maxBytes > 0 && info.Size()+int64(len(line)+1) > l.maxBytes {
			return errors.New("audit log exceeds size limit")
		}
	}
	if _, err = f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func (l *Logger) Tail(n int) ([]Event, error) {
	if n <= 0 || n > 1000 {
		return nil, errors.New("audit tail count must be between 1 and 1000")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := rejectPathChain(l.path); err != nil {
		return nil, err
	}
	f, err := fsutil.OpenReadFile(l.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const maxLine = 64 << 10
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > l.maxBytes {
		return nil, errors.New("audit log exceeds size limit")
	}
	lines := make([]string, 0, n)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4<<10), maxLine)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			copy(lines, lines[1:])
			lines = lines[:n]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(lines))
	for _, line := range lines {
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("parse audit event: %w", err)
		}
		events = append(events, e)
	}
	return events, nil
}

func rejectPathChain(path string) error {
	clean := filepath.Clean(path)
	for cur := filepath.Dir(clean); ; cur = filepath.Dir(cur) {
		info, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("audit log path contains symlink")
		}
		if !info.IsDir() {
			return errors.New("audit log parent is not a directory")
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
	}
	return nil
}

func rotateIfNeeded(path string, maxBytes, incoming int64) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) || maxBytes <= 0 || info.Size()+incoming <= maxBytes {
		return nil
	}
	if err != nil {
		return err
	}
	old := path + ".1"
	if err := fsutil.RenameNoFollow(path, old); err != nil {
		return err
	}
	return fsutil.ChmodNoFollow(old, 0600)
}

func sanitizeEvent(e Event) Event {
	r := redact.NewRedactor(nil)
	e.Provider = r.RedactString(e.Provider)
	e.Profile = r.RedactString(e.Profile)
	e.Command = r.RedactString(e.Command)
	if len(e.Metadata) > 0 {
		clean := make(map[string]string, len(e.Metadata))
		for k, v := range e.Metadata {
			clean[k] = r.RedactString(v)
		}
		e.Metadata = clean
	}
	return e
}

// Report records an event and returns its write error. Callers should use
// Report where propagating the error would change the operation's control
// flow; operational commands should prefer handling it explicitly.
func Report(logger *Logger, event Event) error {
	if logger == nil {
		return nil
	}
	return logger.Log(event)
}
