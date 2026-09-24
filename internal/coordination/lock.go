package coordination

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// WithConfigLock serializes operations that span encrypted vault records and
// non-secret provider/profile metadata. Callers must load fresh state only
// after acquiring the lock.
func WithConfigLock(configDir string, fn func() error) error {
	if fn == nil {
		return fmt.Errorf("nil coordinated operation")
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(configDir, "coordination.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire configuration coordination lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
