package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"aegiskeys/internal/config"
	tea "charm.land/bubbletea/v2"
)

var externalEditorCrashPoint func(string)

type externalEditorPreparedMsg struct {
	path       string
	command    []string
	remove     bool
	sessionGen uint64
	scratchID  string
	err        error
}

type externalEditorFinishedMsg struct {
	body       string
	sessionGen uint64
	scratchID  string
	remove     bool
	err        error
}

// prepareExternalEditor writes plaintext only into a newly created directory
// under the actual configured private runtime tree. The directory is verified
// as current-user owned, non-symlink, and mode 0700 before data is written.
func prepareExternalEditor(configDir, body string, sessionGen uint64, scratchID string) tea.Cmd {
	return func() tea.Msg {
		runtimeRoot := config.RuntimePath(configDir)
		_, statErr := os.Lstat(runtimeRoot)
		existed := statErr == nil
		if err := os.MkdirAll(runtimeRoot, 0700); err != nil {
			return externalEditorPreparedMsg{err: err}
		}
		if !existed {
			_ = os.Chmod(runtimeRoot, 0700)
		}
		if err := validatePrivateDir(runtimeRoot); err != nil {
			return externalEditorPreparedMsg{err: err}
		}
		dir, err := os.MkdirTemp(runtimeRoot, "scratch-editor-")
		if err != nil {
			return externalEditorPreparedMsg{err: err}
		}
		if err := validatePrivateDir(dir); err != nil {
			_ = os.RemoveAll(dir)
			return externalEditorPreparedMsg{err: err}
		}
		remove := true
		defer func() {
			if remove {
				_ = os.RemoveAll(dir)
			}
		}()
		editor, err := editorCommand()
		if err != nil {
			return externalEditorPreparedMsg{err: err}
		}
		path := filepath.Join(dir, "scratchpad.md")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			return externalEditorPreparedMsg{err: err}
		}
		if externalEditorCrashPoint != nil {
			externalEditorCrashPoint("after-write")
		}
		remove = false
		return externalEditorPreparedMsg{path: path, command: append(append([]string(nil), editor...), path), remove: true, sessionGen: sessionGen, scratchID: scratchID}
	}
}

func (m externalEditorPreparedMsg) cleanup() {
	if m.path == "" {
		return
	}
	_ = os.Remove(m.path)
	_ = os.RemoveAll(filepath.Dir(m.path))
}

func executeExternalEditor(msg externalEditorPreparedMsg) tea.Cmd {
	return tea.ExecProcess(exec.Command(msg.command[0], msg.command[1:]...), func(err error) tea.Msg {
		return finishExternalEditor(msg, msg.sessionGen, msg.scratchID, err)
	})
}

func finishExternalEditor(msg externalEditorPreparedMsg, sessionGen uint64, scratchID string, err error) externalEditorFinishedMsg {
	if externalEditorCrashPoint != nil {
		externalEditorCrashPoint("after-editor")
	}
	var body string
	if err == nil {
		data, readErr := os.ReadFile(msg.path)
		if readErr != nil {
			err = readErr
		} else {
			body = string(data)
		}
	}
	// Remove AegisKeys' original even on failure. We cannot remove arbitrary
	// editor backups/swaps safely, so the warning is explicit and opt-in.
	_ = os.Remove(msg.path)
	_ = os.RemoveAll(filepath.Dir(msg.path))
	return externalEditorFinishedMsg{body: body, sessionGen: sessionGen, scratchID: scratchID, remove: msg.remove, err: err}
}

func editorCommand() ([]string, error) {
	raw := strings.TrimSpace(os.Getenv("EDITOR"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("VISUAL"))
	}
	if raw == "" {
		for _, name := range []string{"micro", "nano", "vim", "vi"} {
			if path, err := exec.LookPath(name); err == nil {
				return []string{path}, nil
			}
		}
		return nil, errors.New("no editor found (set $EDITOR)")
	}
	fields, err := splitCommandLine(raw)
	if err != nil || len(fields) == 0 {
		return nil, errors.New("invalid $EDITOR command")
	}
	path, err := exec.LookPath(fields[0])
	if err != nil {
		return nil, fmt.Errorf("resolve editor: %w", err)
	}
	fields[0] = path
	return fields, nil
}

func validatePrivateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("editor directory is not a real directory")
	}
	if info.Mode().Perm() != 0700 {
		return fmt.Errorf("editor directory permissions are %04o, want 0700", info.Mode().Perm())
	}
	v := reflect.ValueOf(info.Sys())
	if v.IsValid() && v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.IsValid() && v.Kind() == reflect.Struct {
		uid := v.FieldByName("Uid")
		if uid.IsValid() && int(uid.Uint()) != os.Getuid() {
			return errors.New("editor directory is not owned by the current user")
		}
	}
	return nil
}

// cleanupExternalEditor is retained for deterministic tests and startup cleanup
// of abandoned editor directories. It never follows symlinks inside the tree.
func cleanupExternalEditor(path string) error {
	if path == "" {
		return nil
	}
	return os.RemoveAll(path)
}

// cleanupAbandonedEditorDirs removes private scratchpad-editor directories
// left by a prior process crash. It runs before the TUI starts and refuses to
// traverse symlinked entries.
func cleanupAbandonedEditorDirs(configDir string) error {
	root := config.RuntimePath(configDir)
	if err := validatePrivateDir(root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var cleanupErr error
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) < len("scratch-editor-") || entry.Name()[:len("scratch-editor-")] != "scratch-editor-" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
			continue
		}
		if err := cleanupExternalEditor(path); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}
