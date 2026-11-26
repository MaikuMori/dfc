//go:build !windows

package resume

import (
	"errors"
	"syscall"
)

// processAlive reports whether pid names a running process. EPERM means it
// exists under another user.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
