package schedule

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var grandEcranTheater = regexp.MustCompile(`^[A-Z0-9]{5}$`)
var grandEcranMovie = regexp.MustCompile(`^([1-9][0-9]{0,111}|c[A-Za-z0-9_-]{1,111})$`)
var grandEcranShowing = regexp.MustCompile(`^[A-Z0-9]{5}-[a-f0-9]{64}$`)
var grandEcranBooking = regexp.MustCompile(`^https://achat\.grandecran\.fr/[a-z0-9]+(-[a-z0-9]+)*/(?:reserver/)?r/[1-9][0-9]*$`)

// ValidGrandEcranIdentity preserves opaque event IDs without numeric coercion.
func ValidGrandEcranIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return grandEcranTheater.MatchString(value)
	case "movie":
		return grandEcranMovie.MatchString(value)
	case "showing":
		return grandEcranShowing.MatchString(value)
	default:
		return false
	}
}

func ValidGrandEcranBookingURL(raw string) bool {
	return len(raw) <= maxURLLength && grandEcranBooking.MatchString(raw)
}

func ValidGrandEcranPosterURL(raw string) bool {
	value, valid := GrandEcranSourcePosterURL(raw)
	return valid && value != "" && value == raw
}

// GrandEcranSourcePosterURL preserves safe supported images. Well-formed images
// on unsupported hosts are unavailable metadata, not acquisition failures.
func GrandEcranSourcePosterURL(raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > maxURLLength || strings.ContainsAny(raw, "%\\?#") || strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Host == "" || u.Host != u.Hostname() || u.Path == "" || u.Path == "/" || hasPatheTraversalSegment(u.Path) {
		return "", false
	}
	if u.Host == "acsta.net" || strings.HasSuffix(u.Host, ".acsta.net") {
		return raw, true
	}
	return "", true
}
