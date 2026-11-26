//go:build !windows

package resume

import (
	"os"
	"syscall"
)

// tryLock takes an exclusive, non-blocking lock on f.
func tryLock(f *os.File) bool {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}
