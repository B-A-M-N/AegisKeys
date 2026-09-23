//go:build windows

package runner

import "os"

func processSignal(_ *os.ProcessState) string { return "" }
