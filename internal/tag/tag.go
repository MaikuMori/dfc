// Package tag is the single source of truth for dfc's inline #tag grammar:
// bare #word, nested #a/b, and multi-word #two words# forms, starting only at
// a whitespace boundary (which makes a backslash-escaped \# a non-tag), with
// at least one letter required so issue/version numbers like #3 stay literal.
// Spans scans plain text for the TUI to paint; FromMarkdown extracts tags from
// a markdown body, skipping code spans and fenced code.
package tag

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Span is one inline #tag occurrence in a plain string: its display name
// (delimiters stripped) and the byte range it covers, inclusive of the leading
// '#' and any wrapping '#'. The TUI uses the range to paint the whole tag.
type Span struct {
	Name  string
	Start int
	End   int
}

// isBareTagChar reports whether r may appear in a bare (single-word) tag:
// letters, digits, '_', '-', and nesting via '/'.
func isBareTagChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '/'
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// Match decodes an inline #tag at the head of line (returns ok=false unless
// line[0] is '#') and returns its display name (delimiters stripped) plus the
// bytes consumed. Boundary and escape rules are the caller's job; Match only
// decodes the grammar:
//
//	#bare            ends at the first non-tag character
//	#a/b/c           nested via '/'
//	#multi word#     multi-word, wrapped in '#'
//	#a/b c d#        nested + multi-word
//
// A tag must contain at least one letter, so #3 and #2026 are not tags, which
// keeps version/issue numbers from becoming tags. Trailing separators are
// dropped from the name (and, for the unclosed bare form, from the consumed
// span) so a hash anchor like #thread-<id> yields "thread", not "thread-".
func Match(line []byte) (name string, n int, ok bool) {
	if len(line) < 2 || line[0] != '#' {
		return "", 0, false
	}
	i := 1
	for i < len(line) {
		r, size := utf8.DecodeRune(line[i:])
		if size == 0 || !isBareTagChar(r) {
			break
		}
		i += size
	}
	bareEnd := i

	// A bare run followed by a space may open a "#multi word#" tag; try that
	// before settling for the bare form.
	if bareEnd > 1 && bareEnd < len(line) && line[bareEnd] == ' ' {
		if mw, mn, mok := matchMultiword(line); mok {
			return mw, mn, true
		}
	}
	if bareEnd == 1 {
		return "", 0, false
	}

	name = trimTagEnd(string(line[1:bareEnd]))
	if !hasLetter(name) {
		return "", 0, false
	}
	if bareEnd < len(line) && line[bareEnd] == '#' {
		// A trailing '#' closes a spaceless "#foo#" tag: the close is explicit,
		// so it is consumed even when a trimmed separator precedes it.
		return name, bareEnd + 1, true
	}
	// Unclosed bare run: the consumed span is exactly '#' + the trimmed name,
	// so the painter and parser don't swallow trailing separators. name is a
	// byte-prefix of the run, so its byte length gives the span directly.
	return name, 1 + len(name), true
}

// trimTagEnd strips trailing separators (and any space they expose) from a
// resolved tag name. A separator never carries meaning at the end of a tag; a
// trailing one is usually punctuation that bled in from surrounding prose
// (#thread-<id>, #a/b/).
func trimTagEnd(s string) string {
	return strings.TrimRight(s, "-_/ ")
}

// matchMultiword decodes a "#words with spaces#" tag. The closing '#' must sit
// on the same line, immediately after a non-space, and the content holds only
// tag characters plus spaces — so "#a #b#" stays two bare tags, not one.
func matchMultiword(line []byte) (name string, n int, ok bool) {
	for i := 1; i < len(line); i++ {
		switch line[i] {
		case '\n', '\r':
			return "", 0, false
		case '#':
			content := line[1:i]
			if len(content) == 0 || content[0] == ' ' || content[len(content)-1] == ' ' {
				return "", 0, false
			}
			if !bytes.ContainsRune(content, ' ') {
				return "", 0, false // spaceless: let the bare form handle it
			}
			for _, r := range string(content) {
				if r != ' ' && !isBareTagChar(r) {
					return "", 0, false
				}
			}
			cs := trimTagEnd(string(content))
			if !hasLetter(cs) {
				return "", 0, false
			}
			return cs, i + 1, true
		}
	}
	return "", 0, false
}

// isTagBoundary reports whether r (the character before a '#') lets a tag
// start. Tags begin only at the start of input or after whitespace; that rule
// also makes a backslash-escaped "\#" a non-tag for free, since '\' is not
// whitespace.
func isTagBoundary(r rune) bool {
	return r == 0 || r == '\n' || unicode.IsSpace(r)
}

// Spans returns every inline #tag in a plain string with its byte range, in
// order. It is what the TUI uses to paint tags; FromMarkdown is the
// markdown-aware extractor that also skips code spans and fences.
func Spans(s string) []Span {
	if !strings.Contains(s, "#") {
		return nil
	}
	src := []byte(s)
	var spans []Span
	for i := 0; i < len(src); {
		if src[i] != '#' {
			i++
			continue
		}
		if i > 0 {
			if r, _ := utf8.DecodeLastRune(src[:i]); !isTagBoundary(r) {
				i++
				continue
			}
		}
		name, end, ok := Match(src[i:])
		if !ok {
			i++
			continue
		}
		spans = append(spans, Span{Name: name, Start: i, End: i + end})
		i += end
	}
	return spans
}

// Has reports whether tagset contains want or a tag nested under it (want
// followed by "/…"), case-insensitively. The hierarchy match means filtering
// by #journal also catches #journal/2024.
func Has(tagset []string, want string) bool {
	lw := strings.ToLower(want)
	for _, t := range tagset {
		lt := strings.ToLower(t)
		if lt == lw || strings.HasPrefix(lt, lw+"/") {
			return true
		}
	}
	return false
}

// tagNode is the goldmark inline node produced for a parsed #tag. It carries no
// children: the source span is consumed and only the name is needed.
type tagNode struct {
	ast.BaseInline
	Name string
}

var kindTag = ast.NewNodeKind("DfcTag")

func (n *tagNode) Kind() ast.NodeKind { return kindTag }
func (n *tagNode) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{"Name": n.Name}, nil)
}

type tagInlineParser struct{}

func (tagInlineParser) Trigger() []byte { return []byte{'#'} }

func (tagInlineParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	if !isTagBoundary(block.PrecendingCharacter()) {
		return nil
	}
	line, _ := block.PeekLine()
	name, n, ok := Match(line)
	if !ok {
		return nil
	}
	block.Advance(n)
	return &tagNode{Name: name}
}

// docParser is goldmark with the #tag inline parser layered onto the
// CommonMark defaults, so code spans, fenced code, and backslash escapes keep
// precedence: a #tag inside `code` or after a '\' is not a tag.
var docParser = goldmark.New(
	goldmark.WithParserOptions(parser.WithInlineParsers(
		util.Prioritized(tagInlineParser{}, 500),
	)),
).Parser()

// FromMarkdown returns the unique inline #tags in a markdown body, in document
// order, skipping any inside code spans or fenced code blocks. Matching is
// case-insensitive; the first-seen spelling is kept.
func FromMarkdown(body string) []string {
	if !strings.Contains(body, "#") {
		return nil
	}
	doc := docParser.Parse(text.NewReader([]byte(body)))
	seen := map[string]bool{}
	var out []string
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := node.(*tagNode); ok {
			key := strings.ToLower(t.Name)
			if !seen[key] {
				seen[key] = true
				out = append(out, t.Name)
			}
		}
		return ast.WalkContinue, nil
	})
	return out
}
