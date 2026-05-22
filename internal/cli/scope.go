package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/MaikuMori/dfc/internal/core"
	"github.com/MaikuMori/dfc/internal/project"
)

// resolveProjectInput maps a user-supplied -p value (slug or display
// name, case-insensitive on name) to a canonical slug. found=false
// means the registry knows neither; callers that auto-register (e.g.
// capture) treat the input as a literal new slug, while read-only
// callers (ls / search) surface an "unknown project" error.
func resolveProjectInput(input string) (slug, displayName string, found bool, err error) {
	if input == "" {
		return "", "", false, nil
	}
	reg, err := project.LoadRegistry()
	if err != nil {
		return "", "", false, err
	}
	slug, err = reg.LookupSlug(input)
	if err != nil {
		return "", "", false, err
	}
	if slug == "" {
		return input, input, false, nil
	}
	return slug, reg.Name(slug), true, nil
}

// resolveCwdSlug returns the project slug for the current directory via
// project.Resolve. Used by ID-taking commands when --all-projects is off.
func resolveCwdSlug() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	resolved, err := project.Resolve(cwd)
	if err != nil {
		return "", err
	}
	return resolved.Slug, nil
}

// scopedNotFoundHint rewrites a core.NotFoundError into a message that
// tells the user how to widen the search. Non-NotFound errors pass
// through unchanged.
func scopedNotFoundHint(err error, id, slug string) error {
	var nf *core.NotFoundError
	if !errors.As(err, &nf) {
		return err
	}
	return fmt.Errorf("task %s not found in project %s; pass --all-projects to search every project", id, slug)
}
