package capture

import (
	"crypto/rand"
	"io"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/MaikuMori/dfc/internal/storage"
)

// SplitHeading splits a captured text buffer into a heading and a body.
// The first non-blank line becomes the heading; everything after it
// (with any leading blank lines that separated the two consumed) becomes
// the body. Used by every capture entry point — CLI stdin, CLI inline
// prompt, TUI overlay — so they all read the same way.
func SplitHeading(text string) (heading, body string) {
	lines := strings.Split(text, "\n")
	idx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			heading = strings.TrimSpace(line)
			idx = i
			break
		}
	}
	if idx == -1 {
		return "", ""
	}
	body = strings.TrimSpace(strings.Join(lines[idx+1:], "\n"))
	return heading, body
}

// Options controls how a new task is built. The zero value is fine for
// production use; tests override Now and Entropy for determinism.
type Options struct {
	Now     func() time.Time
	Entropy io.Reader
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o Options) entropy() io.Reader {
	if o.Entropy != nil {
		return o.Entropy
	}
	return rand.Reader
}

// New builds a Task for the given description and returns it alongside its
// filename slug. Pure: it touches no filesystem.
func New(description string, opts Options) (storage.Task, string, error) {
	now := opts.now().UTC().Truncate(time.Second)
	id, err := ulid.New(ulid.Timestamp(now), opts.entropy())
	if err != nil {
		return storage.Task{}, "", err
	}

	desc := strings.TrimSpace(description)
	// First 10 chars of a ULID encode the timestamp; that's enough to keep
	// filenames sortable and unique-per-ms without dragging the full 26 chars
	// of entropy into every name.
	filenameSlug := storage.FilenameSlug(id.String()[:10], desc)

	t := storage.Task{
		ID:          id.String(),
		Status:      storage.StatusOpen,
		Created:     now,
		Description: desc,
	}
	return t, filenameSlug, nil
}
