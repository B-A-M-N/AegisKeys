//go:build linux

package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// AtomicWriteFile atomically writes data through a directory descriptor. The
// parent chain is opened component-by-component with O_NOFOLLOW, so a checked
// directory cannot be swapped for a symlink between validation and the write.
func AtomicWriteFile(path string, data []byte) error {
	return atomicWriteFile(path, data, 0600)
}

// AtomicWriteFileMode is the mode-aware form used by adapter overlays. The
// final rename replaces the target name atomically; it never follows a target
// symlink.
func AtomicWriteFileMode(path string, data []byte, mode os.FileMode) error {
	return atomicWriteFile(path, data, mode)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := openPrivateParent(filepath.Dir(abs))
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(abs)
	if name == "." || name == ".." || name == "" {
		return fmt.Errorf("invalid atomic write target %q", path)
	}
	for i := 0; i < 16; i++ {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		tempName := ".tmp-" + hex.EncodeToString(buf)
		fd, err := unix.Openat(int(parent.Fd()), tempName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
		if err != nil {
			if err == unix.EEXIST {
				continue
			}
			return err
		}
		f := os.NewFile(uintptr(fd), tempName)
		if f == nil {
			_ = unix.Close(fd)
			return fmt.Errorf("open temporary file")
		}
		ok := false
		defer func() {
			if !ok {
				_ = f.Close()
				_ = unix.Unlinkat(int(parent.Fd()), tempName, 0)
			}
		}()
		if _, err := f.Write(data); err != nil {
			return err
		}
		if err := f.Chmod(mode); err != nil {
			return err
		}
		if err := f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		if err := unix.Renameat(int(parent.Fd()), tempName, int(parent.Fd()), name); err != nil {
			return err
		}
		if err := parent.Sync(); err != nil {
			return err
		}
		ok = true
		return nil
	}
	return fmt.Errorf("unable to allocate temporary file")
}

func openPrivateParent(dir string) (*os.File, error) {
	root, err := os.Open("/")
	if err != nil {
		return nil, err
	}
	defer root.Close()
	cur := root
	rel, err := filepath.Rel("/", dir)
	if err != nil {
		return nil, err
	}
	for _, part := range splitPath(rel) {
		if part == "." || part == "" || part == ".." {
			return nil, fmt.Errorf("unsafe atomic write parent %q", dir)
		}
		fd, err := unix.Openat(int(cur.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		next := os.NewFile(uintptr(fd), part)
		if next == nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("open atomic write parent")
		}
		info, statErr := next.Stat()
		if statErr != nil {
			_ = next.Close()
			return nil, statErr
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			owner := stat.Uid
			if owner != 0 && owner != uint32(os.Geteuid()) {
				_ = next.Close()
				return nil, fmt.Errorf("atomic write parent %q is not owned by current user or root", part)
			}
		}
		if cur != root {
			_ = cur.Close()
		}
		cur = next
	}
	return cur, nil
}

func splitPath(path string) []string {
	var out []string
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}

// EnsureDir creates a directory and its missing components relative to the
// filesystem root, refusing symlink components. Existing directories are
// opened no-follow and must be owned by the current user or root.
func EnsureDir(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := os.Open("/")
	if err != nil {
		return err
	}
	defer root.Close()
	cur := root
	rel, err := filepath.Rel("/", abs)
	if err != nil {
		return err
	}
	for _, part := range splitPath(rel) {
		if part == "." || part == ".." {
			return fmt.Errorf("unsafe directory path %q", path)
		}
		fd, err := unix.Openat(int(cur.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err == unix.ENOENT {
			if err := unix.Mkdirat(int(cur.Fd()), part, 0700); err != nil && err != unix.EEXIST {
				return err
			}
			fd, err = unix.Openat(int(cur.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		}
		if err != nil {
			return err
		}
		next := os.NewFile(uintptr(fd), part)
		if next == nil {
			_ = unix.Close(fd)
			return fmt.Errorf("open directory")
		}
		info, err := next.Stat()
		if err != nil {
			_ = next.Close()
			return err
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			owner := stat.Uid
			if owner != 0 && owner != uint32(os.Geteuid()) {
				_ = next.Close()
				return fmt.Errorf("directory %q is not owned by current user or root", part)
			}
		}
		if cur != root {
			_ = cur.Close()
		}
		cur = next
	}
	return cur.Sync()
}

// OpenAppendFile opens an existing/new file relative to a pinned parent chain
// with O_NOFOLLOW, returning a descriptor safe for appending and syncing.
func OpenAppendFile(path string, mode os.FileMode) (*os.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent, err := openPrivateParent(filepath.Dir(abs))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	name := filepath.Base(abs)
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_WRONLY|unix.O_APPEND|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

// RenameNoFollow atomically renames a path within a pinned no-follow parent
// chain. The destination name is replaced, never followed as a symlink.
func RenameNoFollow(oldPath, newPath string) error {
	oldAbs, err := filepath.Abs(oldPath)
	if err != nil {
		return err
	}
	newAbs, err := filepath.Abs(newPath)
	if err != nil {
		return err
	}
	oldParent, err := openPrivateParent(filepath.Dir(oldAbs))
	if err != nil {
		return err
	}
	defer oldParent.Close()
	newParent, err := openPrivateParent(filepath.Dir(newAbs))
	if err != nil {
		return err
	}
	defer newParent.Close()
	if err := unix.Renameat(int(oldParent.Fd()), filepath.Base(oldAbs), int(newParent.Fd()), filepath.Base(newAbs)); err != nil {
		return err
	}
	return newParent.Sync()
}

func OpenReadFile(path string) (*os.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent, err := openPrivateParent(filepath.Dir(abs))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(abs), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Base(abs)), nil
}

func ChmodNoFollow(path string, mode os.FileMode) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := openPrivateParent(filepath.Dir(abs))
	if err != nil {
		return err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(abs), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), filepath.Base(abs))
	if f == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open file")
	}
	defer f.Close()
	return f.Chmod(mode)
}

func ReadFile(path string, maxBytes int64) ([]byte, error) {
	data, err := readSecureFile(path, maxBytes)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func readSecureFile(path string, maxBytes int64) ([]byte, error) {
	f, err := OpenReadFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func OpenLockFile(path string) (*os.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent, err := openPrivateParent(filepath.Dir(abs))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(abs), unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Base(abs)), nil
}
