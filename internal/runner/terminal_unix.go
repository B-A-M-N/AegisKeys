//go:build !windows

package runner

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// OpenControllingTTY returns the process's controlling terminal for the rare
// case where the parent process streams are redirected but the TUI still owns
// a real terminal. It does not alter process groups.
func OpenControllingTTY() (*os.File, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open controlling terminal: %w", err)
	}
	if !term.IsTerminal(int(tty.Fd())) {
		_ = tty.Close()
		return nil, fmt.Errorf("/dev/tty is not a terminal")
	}
	return tty, nil
}
