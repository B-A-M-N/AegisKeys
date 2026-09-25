//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/sys/unix"
)

// AtomicWriteFile is the Unix descriptor-relative implementation shared by
// supported non-Linux systems. It refuses symlink components while opening the
// parent chain and applies the requested mode before the atomic rename.
func AtomicWriteFile(path string, data []byte) error {
	return atomicWriteFile(path, data, 0600)
}

func AtomicWriteFileMode(path string, data []byte, mode os.FileMode) error {
	return atomicWriteFile(path, data, mode)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := openNoFollowParent(filepath.Dir(abs))
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(abs)
	for attempt := 0; attempt < 16; attempt++ {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		tempName := ".tmp-" + hex.EncodeToString(buf)
		fd, err := unix.Openat(int(parent.Fd()), tempName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
		if err == unix.EEXIST {
			continue
		}
		if err != nil {
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

func openNoFollowParent(dir string) (*os.File, error) {
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
		fd, err := unix.Openat(int(cur.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		next := os.NewFile(uintptr(fd), part)
		if next == nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("open parent")
		}
		if err := validateUnixParentOwner(next); err != nil {
			_ = next.Close()
			return nil, err
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
		if part != "" && part != "." && part != ".." {
			out = append(out, part)
		}
	}
	return out
}

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
		if err := validateUnixParentOwner(next); err != nil {
			_ = next.Close()
			return err
		}
		if cur != root {
			_ = cur.Close()
		}
		cur = next
	}
	return cur.Sync()
}

func OpenAppendFile(path string, mode os.FileMode) (*os.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent, err := openNoFollowParent(filepath.Dir(abs))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(abs), unix.O_WRONLY|unix.O_APPEND|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Base(abs)), nil
}

func OpenReadFile(path string) (*os.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent, err := openNoFollowParent(filepath.Dir(abs))
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
	parent, err := openNoFollowParent(filepath.Dir(abs))
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

func RenameNoFollow(oldPath, newPath string) error {
	oldAbs, err := filepath.Abs(oldPath)
	if err != nil {
		return err
	}
	newAbs, err := filepath.Abs(newPath)
	if err != nil {
		return err
	}
	oldParent, err := openNoFollowParent(filepath.Dir(oldAbs))
	if err != nil {
		return err
	}
	defer oldParent.Close()
	newParent, err := openNoFollowParent(filepath.Dir(newAbs))
	if err != nil {
		return err
	}
	defer newParent.Close()
	if err := unix.Renameat(int(oldParent.Fd()), filepath.Base(oldAbs), int(newParent.Fd()), filepath.Base(newAbs)); err != nil {
		return err
	}
	return newParent.Sync()
}

func ReadFile(path string, maxBytes int64) ([]byte, error) {
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
	parent, err := openNoFollowParent(filepath.Dir(abs))
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

func validateUnixParentOwner(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	v := reflect.ValueOf(info.Sys())
	if v.IsValid() && v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.IsValid() && v.Kind() == reflect.Struct {
		uid := v.FieldByName("Uid")
		if uid.IsValid() && uid.Uint() != 0 && uint32(uid.Uint()) != uint32(os.Geteuid()) {
			return fmt.Errorf("directory parent is not owned by current user or root")
		}
	}
	return nil
}
