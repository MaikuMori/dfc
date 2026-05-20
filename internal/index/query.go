package index

import "strings"

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
//
// This is the single source of truth for query parsing; both the FTS5
// MATCH builder and the CLI highlighter call into it so the two stay in
// lockstep.
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
