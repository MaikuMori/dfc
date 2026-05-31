//go:build !linux && !darwin

package storage

import (
	"io/fs"
	"os"
)

// renameNoReplace renames oldpath to newpath, refusing to overwrite an
// existing newpath. Platforms without an atomic no-replace rename fall back to
// a stat-then-rename, whose small TOCTOU window is acceptable for a
// single-user interactive tool.
func renameNoReplace(oldpath, newpath string) error {
	if _, err := os.Lstat(newpath); err == nil {
		return fs.ErrExist
	}
	return os.Rename(oldpath, newpath)
}
