package storage

import (
	"strings"

	"github.com/MaikuMori/dfc/internal/project"
)

// MaxDescSlugLen caps the description portion of a filename so paths stay
// reasonable even for chatty descriptions.
const MaxDescSlugLen = 50

// FilenameSlug builds the stem of a task file (no .md extension) from a
// timestamp prefix and a description. The result is "<tsPrefix>-<descSlug>"
// where descSlug is project.Slugify(desc) truncated on a hyphen boundary.
func FilenameSlug(tsPrefix, description string) string {
	desc := project.Slugify(description)
	desc = truncateOnHyphen(desc, MaxDescSlugLen)
	if desc == "" {
		desc = "task"
	}
	return strings.ToLower(tsPrefix) + "-" + desc
}

// FilenameTimestamp returns the timestamp portion of a task filename stem,
// i.e. everything before the first hyphen. It returns "" if the stem has no
// hyphen, which should be impossible for files we created.
func FilenameTimestamp(stem string) string {
	if i := strings.IndexByte(stem, '-'); i > 0 {
		return stem[:i]
	}
	return ""
}

func truncateOnHyphen(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndex(cut, "-"); i > 0 {
		cut = cut[:i]
	}
	return strings.Trim(cut, "-")
}
