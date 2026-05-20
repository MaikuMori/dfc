package project

import (
	"strings"
	"unicode"
)

// Slugify lowercases the input and collapses runs of non-alphanumeric
// characters into single hyphens, trimming hyphens from both ends.
func Slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevHyphen := true // suppresses leading hyphens
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	out := b.String()
	return strings.TrimRight(out, "-")
}
