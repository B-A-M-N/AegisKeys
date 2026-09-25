//go:build windows

package fsutil

import (
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

var processLocks sync.Map

type windowsLock struct {
	mu   sync.Mutex
	held bool
}

func lockState(f *os.File) *windowsLock {
	v, _ := processLocks.LoadOrStore(f.Name(), &windowsLock{})
	return v.(*windowsLock)
}

func LockFile(f *os.File) error {
	st := lockState(f)
	st.mu.Lock()
	// LockFileEx serializes across processes; the mutex only prevents a
	// same-process goroutine from attempting a conflicting lock on this file.
	if st.held {
		st.mu.Unlock()
		return windows.ERROR_LOCK_VIOLATION
	}
	ov := &windows.Overlapped{Offset: 0, OffsetHigh: 0}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ov); err != nil {
		st.mu.Unlock()
		return err
	}
	st.held = true
	return nil
}

func UnlockFile(f *os.File) {
	st := lockState(f)
	if st.held {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
		st.held = false
	}
	st.mu.Unlock()
	processLocks.Delete(f.Name())
}
