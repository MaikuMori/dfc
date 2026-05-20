package project

import (
	"path/filepath"
	"strings"
)

// DeriveName returns a friendly display name for a resolved project,
// suitable as the initial entry in the registry.
//
//   - git-remote: `owner/repo` if available, else the last path segment.
//   - git-toplevel or cwd: basename of the directory.
//   - anything weirder: the slug itself.
func DeriveName(r Resolved) string {
	switch r.Source {
	case SourceRemote:
		// r.Raw is host/owner/repo (or possibly host/longer/path/repo).
		parts := strings.Split(r.Raw, "/")
		switch {
		case len(parts) >= 3:
			return parts[len(parts)-2] + "/" + parts[len(parts)-1]
		case len(parts) == 2:
			return parts[1]
		case len(parts) == 1 && parts[0] != "":
			return parts[0]
		}
	case SourceToplevel, SourceCwd:
		if base := filepath.Base(r.Raw); base != "" && base != "." && base != "/" {
			return base
		}
	}
	return r.Slug
}
