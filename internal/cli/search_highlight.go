package cli

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Marker tokens we use inside strings before rendering. The choice is
// arbitrary as long as the pair never appears in real task content.
const (
	markStart = "\x00MS\x00"
	markEnd   = "\x00ME\x00"
)

// containsAnyFold reports whether s contains any term (case-insensitive
// substring match). Used to decide whether the match is in description
// (skip body paragraph) or only in body (show body paragraph).
func containsAnyFold(s string, terms []string) bool {
	if s == "" || len(terms) == 0 {
		return false
	}
	lc := strings.ToLower(s)
	for _, t := range terms {
		if t != "" && strings.Contains(lc, t) {
			return true
		}
	}
	return false
}

// highlightTerms wraps every occurrence (case-insensitive,
// prefix-aware) of any term in `terms` with our internal mark
// sentinels. The wrap extends to the end of the surrounding word so a
// prefix query like `mac` highlights `macOS` whole, not just `mac`.
//
// The output still carries the sentinel pair; renderMarks turns those
// into either ANSI escapes or `<mark>…</mark>` depending on the
// caller's needs.
func highlightTerms(s string, terms []string) string {
	if s == "" || len(terms) == 0 {
		return s
	}
	lc, offs := lowerWithOffsets(s)
	var spans []byteSpan
	for _, t := range terms {
		if t == "" {
			continue
		}
		from := 0
		for {
			idx := strings.Index(lc[from:], t)
			if idx < 0 {
				break
			}
			lpos := from + idx
			lend := lpos + len(t)
			start, end := offs[lpos], offs[lend]
			// Extend through the rest of the current word so the
			// whole token is highlighted, not just the typed prefix.
			for end < len(s) {
				r, sz := utf8.DecodeRuneInString(s[end:])
				if !isWordRune(r) {
					break
				}
				end += sz
			}
			spans = append(spans, byteSpan{start, end})
			from = lend
		}
	}
	if len(spans) == 0 {
		return s
	}
	// Merge overlapping spans so we don't double-wrap.
	merged := mergeSpans(spans)
	var b strings.Builder
	prev := 0
	for _, sp := range merged {
		b.WriteString(s[prev:sp.start])
		b.WriteString(markStart)
		b.WriteString(s[sp.start:sp.end])
		b.WriteString(markEnd)
		prev = sp.end
	}
	b.WriteString(s[prev:])
	return b.String()
}

// renderMarks converts the internal mark sentinels into terminal
// styling (ANSI bold + yellow) for TTY output, or `<mark>…</mark>` for
// non-TTY (so piping into another consumer still carries the
// information). Returns input unchanged when no marks are present.
func renderMarks(s string, ansi bool) string {
	if !strings.Contains(s, markStart) {
		return s
	}
	if ansi {
		s = strings.ReplaceAll(s, markStart, "\x1b[1;33m")
		s = strings.ReplaceAll(s, markEnd, "\x1b[0m")
		return s
	}
	s = strings.ReplaceAll(s, markStart, "<mark>")
	s = strings.ReplaceAll(s, markEnd, "</mark>")
	return s
}

// matchedParagraph returns the markdown block (paragraph or heading)
// in body that contains the first occurrence of any term. Blocks are
// delimited by blank lines; headings (lines starting with `#`) count
// as standalone blocks. Returns "" if no term is found.
func matchedParagraph(body string, terms []string) string {
	if body == "" {
		return ""
	}
	lc := strings.ToLower(body)
	pos := -1
	for _, t := range terms {
		if t == "" {
			continue
		}
		if i := strings.Index(lc, t); i >= 0 && (pos == -1 || i < pos) {
			pos = i
		}
	}
	if pos == -1 {
		return ""
	}
	start := paragraphStart(body, pos)
	end := paragraphEnd(body, pos)
	return strings.TrimSpace(body[start:end])
}

// paragraphStart walks back from pos to find the start of the
// containing markdown block. Stops at a blank line or the start of a
// heading line.
func paragraphStart(body string, pos int) int {
	// Walk back line by line.
	lineStart := pos
	for lineStart > 0 && body[lineStart-1] != '\n' {
		lineStart--
	}
	for lineStart > 0 {
		// Look at the previous line.
		prevEnd := lineStart - 1 // the '\n' itself
		prevStart := prevEnd
		for prevStart > 0 && body[prevStart-1] != '\n' {
			prevStart--
		}
		prevLine := body[prevStart:prevEnd]
		if strings.TrimSpace(prevLine) == "" {
			break // blank line: paragraph boundary
		}
		if strings.HasPrefix(strings.TrimLeft(prevLine, " \t"), "#") {
			// The previous line is a heading — paragraph boundary.
			// The heading itself anchors the block above; we keep
			// lineStart on the current paragraph below it.
			break
		}
		lineStart = prevStart
	}
	return lineStart
}

// paragraphEnd walks forward from pos to find the end of the
// containing markdown block. Stops before a blank line or the start of
// a new heading.
func paragraphEnd(body string, pos int) int {
	// Determine current line's nature.
	lineStart := pos
	for lineStart > 0 && body[lineStart-1] != '\n' {
		lineStart--
	}
	lineEnd := pos
	for lineEnd < len(body) && body[lineEnd] != '\n' {
		lineEnd++
	}
	currLine := body[lineStart:lineEnd]
	isHeading := strings.HasPrefix(strings.TrimLeft(currLine, " \t"), "#")
	if isHeading {
		// Headings are standalone blocks.
		return lineEnd
	}

	end := lineEnd
	for end < len(body) {
		// Skip the trailing newline of the line we just consumed.
		next := end + 1
		if next > len(body) {
			break
		}
		// Find the next line's bounds.
		nextEnd := next
		for nextEnd < len(body) && body[nextEnd] != '\n' {
			nextEnd++
		}
		nextLine := body[next:nextEnd]
		if strings.TrimSpace(nextLine) == "" {
			break
		}
		if strings.HasPrefix(strings.TrimLeft(nextLine, " \t"), "#") {
			break
		}
		end = nextEnd
	}
	return end
}

// lowerWithOffsets returns strings.ToLower(s) alongside a map from each byte
// index in the lowercased result back to the byte index in s that produced
// it, plus one trailing entry equal to len(s). Lowercasing is not
// byte-length-preserving (e.g. 'İ' U+0130 folds to a shorter 'i'), so spans
// found in the lowercased text must be translated through this map before
// they can index s — otherwise offsets drift and can slice mid-rune.
func lowerWithOffsets(s string) (string, []int) {
	var b strings.Builder
	b.Grow(len(s))
	offs := make([]int, 0, len(s)+1)
	for i, r := range s {
		lr := unicode.ToLower(r)
		for k := 0; k < utf8.RuneLen(lr); k++ {
			offs = append(offs, i)
		}
		b.WriteRune(lr)
	}
	offs = append(offs, len(s))
	return b.String(), offs
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

type byteSpan struct{ start, end int }

func mergeSpans(spans []byteSpan) []byteSpan {
	if len(spans) <= 1 {
		return spans
	}
	// Sort by start using a small insertion sort; spans count is tiny.
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j].start < spans[j-1].start; j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
	out := spans[:1]
	for _, s := range spans[1:] {
		last := &out[len(out)-1]
		if s.start <= last.end {
			if s.end > last.end {
				last.end = s.end
			}
			continue
		}
		out = append(out, s)
	}
	return out
}
