package storage

import (
	"errors"
	"os"
	"path/filepath"
)

// EnvRoot lets tests (and power users) override the default ~/.dfc location.
const EnvRoot = "DFC_ROOT"

// Root returns the base dfc directory, creating it if necessary.
func Root() (string, error) {
	if r := os.Getenv(EnvRoot); r != "" {
		if err := os.MkdirAll(r, 0o755); err != nil {
			return "", err
		}
		return r, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New("cannot resolve home directory")
	}
	root := filepath.Join(home, ".dfc")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

// ProjectDir returns the project directory under the dfc root, creating it.
func ProjectDir(slug string) (string, error) {
	if slug == "" {
		return "", errors.New("missing project slug")
	}
	root, err := Root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ProjectDirPath returns the path to a project's task directory without
// creating it. Restoring from trash must not pre-create the destination,
// otherwise the restore's "already exists" guard trips against an empty
// directory we just made — use this instead of ProjectDir there.
func ProjectDirPath(slug string) (string, error) {
	if slug == "" {
		return "", errors.New("missing project slug")
	}
	root := projectsRoot()
	if root == "" {
		return "", errors.New("could not resolve dfc data directory")
	}
	return filepath.Join(root, slug), nil
}

// projectsRoot returns ~/.dfc/projects without creating it. Returns ""
// when the dfc root can't be resolved (which is rare — only on a totally
// hostile filesystem).
func projectsRoot() string {
	if r := os.Getenv(EnvRoot); r != "" {
		return filepath.Join(r, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".dfc", "projects")
}

// ProjectDirExists reports whether a project's task directory is present on
// disk. It deliberately does NOT create the directory — use this to probe
// before opening a store for a slug we're not sure is still around.
func ProjectDirExists(slug string) bool {
	if slug == "" {
		return false
	}
	root := projectsRoot()
	if root == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, slug))
	return err == nil && info.IsDir()
}

// RegistryPath returns the absolute path to ~/.dfc/projects.json. The UI
// watches this for "new project / rename" signals from out-of-process
// `dfc` invocations.
func RegistryPath() string {
	if r := os.Getenv(EnvRoot); r != "" {
		return filepath.Join(r, "projects.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".dfc", "projects.json")
}
