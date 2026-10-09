// Package moviesearch implements search-specific movie title matching.
package moviesearch

import (
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

// Query holds normalized search words. Its zero value applies no title filter.
type Query struct {
	words  []string
	active bool
}

// Compile distinguishes blank input from nonblank input containing only separators.
func Compile(raw string) Query {
	return Query{words: strings.Fields(normalize(raw)), active: strings.TrimSpace(raw) != ""}
}

// Blank reports whether the raw query applies no title filter.
func (q Query) Blank() bool { return !q.active }

// Matches requires every word within one title, never across both titles.
func (q Query) Matches(title, originalTitle string) bool {
	if q.Blank() {
		return true
	}
	if len(q.words) == 0 {
		return false
	}
	return q.matchesTitle(title) || originalTitle != "" && q.matchesTitle(originalTitle)
}

func (q Query) matchesTitle(title string) bool {
	title = normalize(title)
	for _, word := range q.words {
		if !strings.Contains(title, word) {
			return false
		}
	}
	return true
}

func normalize(value string) string {
	decomposed := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.M, r) {
			return -1
		}
		return r
	}, norm.NFD.String(value))
	// Casers carry mutable state, so each normalization owns its caser.
	lower := cases.Lower(language.Und).String(decomposed)
	var result strings.Builder
	separator := false
	for _, r := range lower {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			separator = result.Len() > 0
			continue
		}
		if separator {
			result.WriteByte(' ')
			separator = false
		}
		result.WriteRune(r)
	}
	return result.String()
}
