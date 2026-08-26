package shortlink

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxTargetBytes    = 2048
	maxTheaterIDBytes = 128
	sharedTheatersKey = "shared_theaters"
)

var queryKeys = map[string]map[string]struct{}{
	"/":                    {},
	"/planning":            keys("date", "language", "format", "mode", "zoom", sharedTheatersKey),
	"/recherche":           keys("theaters", "date", "start_after", "finish_before", "language", "format", "include_ads", "buffer_ads", "grouping", "layout", sharedTheatersKey),
	"/films":               keys("q", "sort", "page", sharedTheatersKey),
	"/credits":             keys(sharedTheatersKey),
	"/film/:slug":          keys("date", "language", "format", "sort", sharedTheatersKey),
	"/cinema/:slug":        keys("date", "grouping", "layout", "view", sharedTheatersKey),
	"/ville/:slug/cinemas": keys(sharedTheatersKey),
}

func keys(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func ValidCode(code string) bool {
	if len(code) != 22 {
		return false
	}
	for _, character := range code {
		if !isASCIIAlphaNumeric(character) && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func ValidTarget(target string) bool {
	if target == "" || len(target) > maxTargetBytes || !utf8.ValidString(target) || target[0] != '/' || strings.HasPrefix(target, "//") || strings.ContainsAny(target, "\\#") || hasControl(target) {
		return false
	}
	path, rawQuery, hasQuery := strings.Cut(target, "?")
	if strings.Contains(path, "%") || !validPathShape(path) {
		return false
	}
	pattern, ok := routePattern(path)
	if !ok {
		return false
	}
	if !hasQuery {
		return true
	}
	return validQuery(rawQuery, queryKeys[pattern])
}

func validPathShape(path string) bool {
	if path == "/" {
		return true
	}
	if strings.HasSuffix(path, "/") || strings.Contains(path, "//") {
		return false
	}
	for _, segment := range strings.Split(path[1:], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func routePattern(path string) (string, bool) {
	if _, ok := queryKeys[path]; ok {
		return path, true
	}
	segments := strings.Split(path[1:], "/")
	switch {
	case len(segments) == 2 && segments[0] == "film" && validSlug(segments[1]):
		return "/film/:slug", true
	case len(segments) == 2 && segments[0] == "cinema" && validSlug(segments[1]):
		return "/cinema/:slug", true
	case len(segments) == 3 && segments[0] == "ville" && validSlug(segments[1]) && segments[2] == "cinemas":
		return "/ville/:slug/cinemas", true
	default:
		return "", false
	}
}

func validSlug(slug string) bool {
	for index, character := range slug {
		if !isASCIIAlphaNumeric(character) && (index == 0 || character != '_' && character != '-') {
			return false
		}
	}
	return slug != ""
}

func isASCIIAlphaNumeric(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
}

func validQuery(raw string, allowed map[string]struct{}) bool {
	if raw == "" {
		return false
	}
	seen := make(map[string]struct{})
	for _, field := range strings.Split(raw, "&") {
		if field == "" {
			return false
		}
		rawKey, rawValue, _ := strings.Cut(field, "=")
		key, err := url.QueryUnescape(rawKey)
		if err != nil || !utf8.ValidString(key) || hasControl(key) {
			return false
		}
		value, err := url.QueryUnescape(rawValue)
		if err != nil || !utf8.ValidString(value) || hasControl(value) {
			return false
		}
		if _, ok := allowed[key]; !ok {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		if key == sharedTheatersKey && !validSharedTheaters(value) {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func validSharedTheaters(value string) bool {
	if value == "" {
		return false
	}
	seen := make(map[string]struct{})
	for _, id := range strings.Split(value, ",") {
		if !validTheaterID(id) {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func validTheaterID(id string) bool {
	if id == "" || len(id) > maxTheaterIDBytes {
		return false
	}
	for index, character := range id {
		if !isASCIIAlphaNumeric(character) && (index == 0 || character != '_' && character != '-') {
			return false
		}
	}
	return true
}

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
