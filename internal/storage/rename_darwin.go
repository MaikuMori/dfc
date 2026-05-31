//go:build darwin

package storage

import "golang.org/x/sys/unix"

// renameNoReplace renames oldpath to newpath but fails (with a syscall error
// that satisfies errors.Is(err, fs.ErrExist)) when newpath already exists,
// so a rename can never silently overwrite another task file.
func renameNoReplace(oldpath, newpath string) error {
	return unix.RenamexNp(oldpath, newpath, unix.RENAME_EXCL)
}
