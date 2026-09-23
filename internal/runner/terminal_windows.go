//go:build windows

package runner

import (
	"fmt"
	"os"
)

// OpenControllingTTY has no safe Windows console fallback in this path.
func OpenControllingTTY() (*os.File, error) {
	return nil, fmt.Errorf("no controlling terminal fallback is available")
}
