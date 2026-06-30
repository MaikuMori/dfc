// Package query parses dfc's search query language: whitespace-separated
// terms, `"…"` phrases, leading-`-` negation, and `#tag` / `-#tag` predicates.
// It is the single source of truth for query parsing — the FTS5 MATCH builder,
// the CLI highlighter, and the TUI's live filter all call into it so they stay
// in lockstep.
package query

import (
	"strings"

	"github.com/MaikuMori/dfc/internal/tag"
)

// Token is one element of a parsed search query.
type Token struct {
	// Value is the raw token content with surrounding double-quotes (for
	// phrases) and the leading `-` (for negations) stripped.
	Value string
	// Phrase is true when the user wrote `"…"` around the token — phrases
	// must match exactly, with no wildcard expansion.
	Phrase bool
	// Negate is true when the user prefixed the token with `-`.
	Negate bool
}

// Tokenize splits a raw user query into tokens. The grammar:
//
//   - Whitespace separates tokens.
//   - `"…"` runs are phrase tokens; the surrounding quotes are stripped.
//     An unterminated phrase consumes the rest of the input.
//   - A leading `-` (when not standalone) marks a negation.
func Tokenize(q string) []Token {
	var out []Token
	i := 0
	for i < len(q) {
		switch c := q[i]; c {
		case ' ', '\t', '\n':
			i++
		case '"':
			j := strings.IndexByte(q[i+1:], '"')
			if j < 0 {
				out = append(out, Token{Value: q[i+1:], Phrase: true})
				i = len(q)
				continue
			}
			out = append(out, Token{Value: q[i+1 : i+1+j], Phrase: true})
			i = i + 1 + j + 1
		default:
			start := i
			for i < len(q) && q[i] != ' ' && q[i] != '\t' && q[i] != '\n' {
				i++
			}
			tok := q[start:i]
			if strings.HasPrefix(tok, "-") && len(tok) > 1 {
				out = append(out, Token{Value: tok[1:], Negate: true})
			} else {
				out = append(out, Token{Value: tok})
			}
		}
	}
	return out
}

// Query is a raw query split into its inline #tag predicates and the residual
// full-text query. Include holds `#tag` tokens (AND-ed), Exclude holds `-#tag`
// tokens; both are lowercased for case-insensitive matching. Text is the query
// with those tokens removed, ready for the FTS path.
type Query struct {
	Text    string
	Include []string
	Exclude []string
}

// Split extracts `#tag` / `-#tag` predicates from a raw query and returns
// them alongside the residual text query. Tag forms recognized mirror the
// content grammar in internal/tag: bare `#word`, nested `#a/b`, single-token
// wrapped `#word#`, and multi-word wrapped `#two words#` (which spans tokens
// because whitespace separates them). A quoted `"#word"` stays text so a
// literal hash phrase can still be searched. The residual text preserves the
// phrase quoting and negation of every remaining token.
func Split(q string) Query {
	var tq Query
	var text []string
	toks := Tokenize(q)
	for i := 0; i < len(toks); i++ {
		if name, consumed, ok := tagAt(toks, i); ok {
			lname := strings.ToLower(name)
			if toks[i].Negate {
				tq.Exclude = append(tq.Exclude, lname)
			} else {
				tq.Include = append(tq.Include, lname)
			}
			i += consumed - 1
			continue
		}
		v := toks[i].Value
		switch {
		case toks[i].Phrase:
			v = `"` + v + `"`
		case toks[i].Negate:
			v = "-" + v
		}
		text = append(text, v)
	}
	tq.Text = strings.Join(text, " ")
	return tq
}

// tagAt decodes a tag predicate starting at toks[i], returning the tag name,
// how many tokens it consumed, and whether it is a tag at all. The name
// grammar is shared with task content: the token and its following plain
// tokens are re-joined with single spaces and handed to tag.Match, so bare
// `#tag`, nested `#a/b`, wrapped `#foo#`, and multi-word `#two words#` (which
// spans tokens because whitespace separates them) parse identically in a query
// and in a task body. The match must end exactly on a token boundary — a
// partial consume (`#ship.`, `#3`) is not a tag and stays text. A phrase or a
// token that opens its own tag ends a multi-word run so `#a #b#` stays two
// tags rather than one.
func tagAt(toks []Token, i int) (name string, consumed int, ok bool) {
	t := toks[i]
	if t.Phrase || len(t.Value) < 2 || t.Value[0] != '#' {
		return "", 0, false
	}
	parts := []string{t.Value}
	ends := []int{len(t.Value)} // candidate length after each appended token
	for j := i + 1; j < len(toks); j++ {
		jt := toks[j]
		if jt.Phrase || jt.Value == "" || jt.Value[0] == '#' {
			break
		}
		v := jt.Value
		if jt.Negate {
			v = "-" + v // restore the raw token text; '-' is a valid tag character
		}
		parts = append(parts, v)
		ends = append(ends, ends[len(ends)-1]+1+len(v))
	}
	if name, n, ok := tag.Match([]byte(strings.Join(parts, " "))); ok {
		for k, end := range ends {
			if n == end {
				return name, k + 1, true
			}
		}
	}
	// A multi-word attempt that closed mid-token (`#a b#c`) still leaves the
	// leading token's own bare form on the table.
	if name, n, ok := tag.Match([]byte(t.Value)); ok && n == len(t.Value) {
		return name, 1, true
	}
	return "", 0, false
}

// PositiveTerms returns the lowercased positive (non-negated) terms from
// a user query. Trailing `*` wildcards are stripped because callers use
// these for substring/highlight matching, not FTS5 prefix expansion.
func PositiveTerms(q string) []string {
	var out []string
	for _, t := range Tokenize(q) {
		if t.Negate {
			continue
		}
		v := strings.ToLower(t.Value)
		v = strings.TrimSuffix(v, "*")
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
