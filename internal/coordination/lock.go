package coordination

import (
	"fmt"
	"path/filepath"

	"aegiskeys/internal/fsutil"
)

// WithConfigLock serializes operations that span encrypted vault records and
// non-secret provider/profile metadata. Callers must load fresh state only
// after acquiring the lock.
func WithConfigLock(configDir string, fn func() error) error {
	if fn == nil {
		return fmt.Errorf("nil coordinated operation")
	}
	if err := fsutil.EnsureDir(configDir); err != nil {
		return err
	}
	f, err := fsutil.OpenLockFile(filepath.Join(configDir, "coordination.lock"))
	if err != nil {
		return err
	}
	defer f.Close()
	if err := fsutil.LockFile(f); err != nil {
		return fmt.Errorf("acquire configuration coordination lock: %w", err)
	}
	defer fsutil.UnlockFile(f)
	return fn()
}
