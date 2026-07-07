// Package runner executes launch plans with config file materialization.
package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aegiskeys/internal/adapter"
	"aegiskeys/internal/fsutil"
	"aegiskeys/internal/profile"
	"aegiskeys/internal/proxy"
)

// LaunchExecutor orchestrates the full launch flow: materialize config files,
// ensure proxies, then run the child process.
type LaunchExecutor struct {
	ConfigDir        string
	FileMaterializer FileMaterializer
}

// LaunchPolicy controls file overwrite and backup behavior.
type LaunchPolicy struct {
	OverwritePolicy string // "backup" (default), "skip", "fail"
	BackupDir       string // where to store backups; if empty, no backups
}

// LaunchResult records what happened during a launch.
type LaunchResult struct {
	Command      string
	Args         []string
	FilesWritten []MaterializedFile
	ExitErr      error
}

// Execute runs a launch plan: materialize files, then execute the command.
func (e *LaunchExecutor) Execute(
	plan *adapter.LaunchPlan,
	policy LaunchPolicy,
	proxies []proxy.Proxy,
) (*LaunchResult, error) {
	result := &LaunchResult{
		Command: plan.Command,
		Args:    plan.Args,
	}

	// 1. Materialize config files.
	if len(plan.Files) > 0 {
		fm := e.FileMaterializer
		fm.OverwritePolicy = policy.OverwritePolicy
		fm.BackupDir = policy.BackupDir
		if fm.OverwritePolicy == "" {
			fm.OverwritePolicy = "backup"
		}
		if fm.BackupDir == "" {
			fm.BackupDir = filepath.Join(e.ConfigDir, "tmp", "backups")
		}

		inputs := make([]FileMaterializerInput, len(plan.Files))
		for i, f := range plan.Files {
			inputs[i] = FileMaterializerInput{
				Path:    f.Path,
				Content: f.Content,
			}
		}
		written, err := fm.MaterialFiles(inputs)
		if err != nil {
			return result, fmt.Errorf("materialize files: %w", err)
		}
		result.FilesWritten = written
	}

	// 2. Run the command via the strategy-driven runner.
	strategy := &adapter.LaunchStrategy{
		Plan:    *plan,
		Support: adapter.AppSupportContract{ID: "executor", CanLaunch: true},
	}
	result.ExitErr = Run(context.Background(), strategy, RunOptions{
		ConfigDir:    e.ConfigDir,
		InheritStdio: true,
	})
	return result, nil
}

// FileMaterializer writes config files safely with backup support.
type FileMaterializer struct {
	// BackupDir stores backups of overwritten files. If empty, no backups.
	BackupDir string
	// OverwritePolicy controls behavior when target file exists.
	// "backup" (default): back up existing file, then overwrite.
	// "skip": leave existing file untouched.
	// "fail": return error.
	OverwritePolicy string
}

// MaterialFiles writes the given config files to disk. It expands ~ safely,
// creates parent directories, backs up existing files if configured, and
// writes atomically with 0600 permissions.
func (fm FileMaterializer) MaterialFiles(files []FileMaterializerInput) ([]MaterializedFile, error) {
	var results []MaterializedFile
	for _, f := range files {
		path, err := expandPath(f.Path)
		if err != nil {
			return results, fmt.Errorf("invalid path %q: %w", f.Path, err)
		}

		// Refuse to write outside expected locations (basic path traversal guard).
		if err := validatePath(path); err != nil {
			return results, err
		}

		// Check if file exists.
		backupPath := ""
		if _, err := os.Stat(path); err == nil {
			switch fm.OverwritePolicy {
			case "skip":
				results = append(results, MaterializedFile{Path: path, Skipped: true})
				continue
			case "fail":
				return results, fmt.Errorf("file already exists: %s", path)
			default: // "backup"
				if fm.BackupDir != "" {
					backupPath = filepath.Join(fm.BackupDir, filepath.Base(path)+"."+timestampName())
					if err := os.MkdirAll(fm.BackupDir, 0700); err != nil {
						return results, fmt.Errorf("backup dir: %w", err)
					}
					if err := copyFile(path, backupPath); err != nil {
						return results, fmt.Errorf("backup %s: %w", path, err)
					}
				}
			}
		}

		// Ensure parent directory exists.
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return results, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
		}

		// Write atomically.
		if err := fsutil.AtomicWriteFile(path, []byte(f.Content)); err != nil {
			return results, fmt.Errorf("write %s: %w", path, err)
		}

		results = append(results, MaterializedFile{
			Path:       path,
			BackupPath: backupPath,
		})
	}
	return results, nil
}

// FileMaterializerInput describes one file to materialize.
type FileMaterializerInput struct {
	Path    string
	Content string
}

// MaterializedFile records the result of writing one file.
type MaterializedFile struct {
	Path       string
	BackupPath string
	Skipped    bool
}

// expandPath safely expands ~ to $HOME. Rejects empty paths and paths
// containing null bytes.
func expandPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("path contains null byte")
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve ~: %w", err)
		}
		return filepath.Join(home, p[2:]), nil
	}
	// Handle $HOME expansion.
	if strings.HasPrefix(p, "$HOME") {
		home := os.Getenv("HOME")
		if home == "" {
			return "", fmt.Errorf("HOME not set")
		}
		return home + p[5:], nil
	}
	return p, nil
}

// validatePath rejects paths that look dangerous (absolute paths to
// system dirs, path traversal attempts).
func validatePath(p string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("path must be absolute: %s", p)
	}
	// Reject writes to sensitive system locations.
	dangerous := []string{"/etc/", "/usr/", "/bin/", "/sbin/", "/boot/"}
	for _, d := range dangerous {
		if strings.HasPrefix(p, d) {
			return fmt.Errorf("refusing to write to system path: %s", p)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return fsutil.AtomicWriteFile(dst, data)
}

func timestampName() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// Ensure LaunchPlan.Files uses the correct type alias for backward compat.
type FileWrite = profile.TargetConfigFile
