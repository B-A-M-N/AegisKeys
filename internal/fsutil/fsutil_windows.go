//go:build windows

package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Windows uses reparse-point-aware opens for every security-sensitive file.
// A final symlink/junction is opened as a reparse point and then rejected by
// file attributes, so the descriptor is never followed as a target.
func AtomicWriteFile(path string, data []byte) error { return atomicWriteFile(path, data, 0600) }
func AtomicWriteFileMode(path string, data []byte, mode os.FileMode) error {
	return atomicWriteFile(path, data, mode)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := EnsureDir(dir); err != nil {
		return err
	}
	for attempt := 0; attempt < 16; attempt++ {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		tmpPath := filepath.Join(dir, ".tmp-"+hex.EncodeToString(buf))
		f, err := openWindowsFile(tmpPath, windows.GENERIC_WRITE|windows.FILE_APPEND_DATA, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0600)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return err
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		if err := f.Chmod(mode); err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		if err := os.Rename(tmpPath, path); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		df, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer df.Close()
		return df.Sync()
	}
	return fmt.Errorf("unable to allocate temporary file")
}

func EnsureDir(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(abs)
	cur := volume + string(filepath.Separator)
	rel := strings.TrimPrefix(abs, cur)
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		ptr, e := windows.UTF16PtrFromString(cur)
		if e != nil {
			return e
		}
		attrs, attrErr := windows.GetFileAttributes(ptr)
		if attrErr == nil {
			if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				return fmt.Errorf("refusing reparse-point directory")
			}
			if attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
				return fmt.Errorf("path component is not a directory")
			}
			continue
		}
		if e := windows.CreateDirectory(ptr, nil); e != nil && !os.IsExist(e) {
			return e
		}
	}
	return nil
}
func OpenAppendFile(path string, mode os.FileMode) (*os.File, error) {
	return openWindowsFile(path, windows.GENERIC_WRITE|windows.FILE_APPEND_DATA, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, mode)
}
func OpenReadFile(path string) (*os.File, error) {
	return openWindowsFile(path, windows.GENERIC_READ, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
}
func OpenLockFile(path string) (*os.File, error) {
	return openWindowsFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0600)
}
func ChmodNoFollow(path string, mode os.FileMode) error {
	f, err := OpenReadFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Chmod(mode)
}
func RenameNoFollow(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }
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

func openWindowsFile(path string, access uint32, attrs uint32, mode os.FileMode) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_ALWAYS, attrs|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("refusing reparse point")
	}
	return os.NewFile(uintptr(h), path), nil
}
