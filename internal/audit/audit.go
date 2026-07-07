package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	mu   sync.Mutex
	path string
}

func NewLogger(path string) *Logger {
	return &Logger{path: path}
}

func (l *Logger) Log(e Event) {
	e.Time = time.Now()
	e = sanitizeEvent(e)
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0700); err != nil {
		return
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_ = f.Chmod(0600)
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
}

func (l *Logger) Tail(n int) ([]Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []Event
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}
	for i := start; i < len(lines); i++ {
		var e Event
		if err := json.Unmarshal([]byte(lines[i]), &e); err == nil {
			events = append(events, e)
		}
	}
	return events, nil
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
