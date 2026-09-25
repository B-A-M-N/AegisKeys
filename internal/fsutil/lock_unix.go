//go:build !windows

package fsutil

import (
	"os"
	"syscall"
)

func LockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }
func UnlockFile(f *os.File)     { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
