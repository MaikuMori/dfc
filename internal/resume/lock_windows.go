package resume

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryLock takes an exclusive, non-blocking lock on f.
func tryLock(f *os.File) bool {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol) == nil
}

// tryLockShared takes a shared, non-blocking lock on f.
func tryLockShared(f *os.File) bool {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol) == nil
}
