package storage

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

// mdParser is shared by every splitBody call. goldmark's parser is safe for
// concurrent reuse and Parse-only work is allocation-light.
var mdParser parser.Parser = goldmark.New().Parser()

type Status string

const (
	StatusOpen Status = "open"
	StatusDone Status = "done"
)

// ULIDLen is the canonical string length of a ULID (Crockford base32
// encoding of 128 bits is always 26 characters).
const ULIDLen = 26

type Task struct {
	ID          string    `yaml:"id"`
	Status      Status    `yaml:"status"`
	Created     time.Time `yaml:"created"`
	Description string    `yaml:"-"`
	Details     string    `yaml:"-"`
	Path        string    `yaml:"-"`
	Modified    time.Time `yaml:"-"` // populated from filesystem mtime, used for sorting
	ProjectSlug string    `yaml:"-"` // populated by Store.tagOwn; never persisted
}

const frontmatterSep = "---"

// Marshal renders a task to its on-disk markdown form:
//
//	---
//	<yaml frontmatter>
//	---
//
//	# Description goes here
//
//	Optional details body.
//
// A blank line follows the closing frontmatter delimiter so the file reads
// cleanly as markdown. The description is the first `#` heading.
func Marshal(t Task) ([]byte, error) {
	fm, err := yaml.Marshal(struct {
		ID      string    `yaml:"id"`
		Status  Status    `yaml:"status"`
		Created time.Time `yaml:"created"`
	}{ID: t.ID, Status: t.Status, Created: t.Created})
	if err != nil {
		return nil, fmt.Errorf("could not marshal task frontmatter: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString(frontmatterSep)
	buf.WriteByte('\n')
	buf.Write(fm)
	buf.WriteString(frontmatterSep)
	buf.WriteString("\n\n")

	desc := strings.TrimSpace(t.Description)
	if desc != "" {
		buf.WriteString("# ")
		buf.WriteString(desc)
		buf.WriteByte('\n')
	}
	if t.Details != "" {
		buf.WriteByte('\n')
		buf.WriteString(t.Details)
		if !strings.HasSuffix(t.Details, "\n") {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

// Unmarshal parses a markdown file with YAML frontmatter into a Task.
func Unmarshal(b []byte) (Task, error) {
	var t Task
	s := string(b)
	if !strings.HasPrefix(s, frontmatterSep) {
		return t, errors.New("task file is missing frontmatter")
	}
	rest := s[len(frontmatterSep):]
	rest = strings.TrimLeft(rest, "\r\n")

	end := strings.Index(rest, "\n"+frontmatterSep)
	if end < 0 {
		return t, errors.New("task file has unterminated frontmatter")
	}
	fm := rest[:end]
	body := rest[end+len("\n"+frontmatterSep):]
	body = strings.TrimLeft(body, "\r\n")

	var meta struct {
		ID      string    `yaml:"id"`
		Status  Status    `yaml:"status"`
		Created time.Time `yaml:"created"`
	}
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return t, fmt.Errorf("could not parse task frontmatter: %w", err)
	}
	t.ID = meta.ID
	t.Status = meta.Status
	t.Created = meta.Created

	t.Description, t.Details = splitBody(body)
	return t, nil
}

// splitBody extracts the description and details from the markdown body via
// a goldmark AST. Priority for the description:
//   1. The first level-1 heading (`# ...`).
//   2. Any heading at another level (h2..h6, including setext underline form).
//   3. The first line of the first paragraph (covers files where an external
//      editor stripped the `#`).
// Details are the body content from the line after the picked node onward,
// with one leading blank line trimmed (Marshal inserts one separating blank
// line above details).
func splitBody(body string) (desc, details string) {
	src := []byte(body)
	doc := mdParser.Parse(text.NewReader(src))

	var h1, anyHeading, firstPara ast.Node
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		switch n := c.(type) {
		case *ast.Heading:
			if anyHeading == nil {
				anyHeading = n
			}
			if n.Level == 1 && h1 == nil {
				h1 = n
				// h1 always wins; stop scanning further siblings.
			}
		case *ast.Paragraph:
			if firstPara == nil {
				firstPara = n
			}
		}
		if h1 != nil {
			break
		}
	}

	var (
		picked            ast.Node
		paragraphFallback bool
	)
	switch {
	case h1 != nil:
		picked = h1
	case anyHeading != nil:
		picked = anyHeading
	case firstPara != nil:
		picked = firstPara
		paragraphFallback = true
	default:
		return "", strings.TrimRight(body, "\n")
	}

	if paragraphFallback {
		desc, details = paragraphSplit(src, picked.(*ast.Paragraph))
		return desc, details
	}

	desc, details = headingSplit(src, picked.(*ast.Heading))
	return desc, details
}

// headingSplit extracts the heading's rendered text and the details body
// that follows it. Setext headings span two source lines (text + underline)
// but goldmark's Lines() only reports the text line; we manually skip the
// underline so it doesn't leak into details.
func headingSplit(src []byte, h *ast.Heading) (desc, details string) {
	desc = inlineText(src, h)
	endLine := lastLine(src, h.Lines())
	if isSetextHeading(src, h) {
		endLine++
	}
	return desc, trimDetails(src, endLine+1)
}

// isSetextHeading reports whether h's source representation uses setext
// (underline) syntax rather than ATX (`#` prefix).
func isSetextHeading(src []byte, h *ast.Heading) bool {
	if h.Lines().Len() == 0 {
		return false
	}
	seg := h.Lines().At(0)
	line := strings.TrimSpace(string(src[seg.Start:seg.Stop]))
	return !strings.HasPrefix(line, "#")
}

// inlineText concatenates the textual content of an inline subtree, ignoring
// emphasis/strong/link wrappers but preserving their text children. This is
// goldmark's replacement for the deprecated Node.Text helper.
func inlineText(src []byte, n ast.Node) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			b.Write(v.Segment.Value(src))
		default:
			b.WriteString(inlineText(src, c))
		}
	}
	return b.String()
}

// paragraphSplit takes the first source line of a paragraph as the
// description (preserving the "first text line" semantics) and treats the
// rest of the document as details, starting from the next source line.
func paragraphSplit(src []byte, p *ast.Paragraph) (desc, details string) {
	if p.Lines().Len() == 0 {
		return "", trimDetails(src, 0)
	}
	first := p.Lines().At(0)
	desc = strings.TrimRight(string(src[first.Start:first.Stop]), "\r\n")
	startLine := lineOf(src, first.Start) + 1
	return desc, trimDetails(src, startLine)
}

// lastLine returns the 0-indexed source line containing the final byte of
// the given segment slice (e.g. the underline of a setext heading or the
// single line of an ATX heading).
func lastLine(src []byte, segs *text.Segments) int {
	if segs.Len() == 0 {
		return 0
	}
	last := segs.At(segs.Len() - 1)
	end := last.Stop - 1
	if end < 0 {
		end = 0
	}
	return lineOf(src, end)
}

// lineOf maps a byte offset to its 0-indexed source line.
func lineOf(src []byte, offset int) int {
	if offset > len(src) {
		offset = len(src)
	}
	if offset < 0 {
		offset = 0
	}
	return bytes.Count(src[:offset], []byte{'\n'})
}

// trimDetails joins body lines from `startLine` onward, drops one leading
// blank line that Marshal inserts above details, and right-trims trailing
// newlines so the field is stable across reloads.
func trimDetails(src []byte, startLine int) string {
	lines := strings.SplitAfter(string(src), "\n")
	if startLine >= len(lines) {
		return ""
	}
	rest := strings.Join(lines[startLine:], "")
	rest = strings.TrimPrefix(rest, "\n")
	rest = strings.TrimPrefix(rest, "\r\n")
	return strings.TrimRight(rest, "\n")
}
