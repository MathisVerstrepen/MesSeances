package schedule

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OcineSlug is the fixed source theaterID to route manifest.
func OcineSlug(id string) string {
	switch id {
	case "309":
		return "maubeuge"
	case "310":
		return "st-omer"
	case "461":
		return "dunkerque"
	default:
		return ""
	}
}

var ocineMovie = regexp.MustCompile(`^([1-9][0-9]*|[a-z][a-z0-9]*(-[a-z0-9]+)*)$`)
var ocineBooking = regexp.MustCompile(`^https://www\.ocine\.fr/(maubeuge|st-omer|dunkerque)/reserver/F([1-9][0-9]*)/D([1-9][0-9]*)/(VF|VO)/([1-9][0-9]*)/$`)
var ocinePoster = regexp.MustCompile(`^https://fr\.web\.img[0-9]*\.acsta\.net/(img|medias|pictures)/[A-Za-z0-9_./-]+\.(jpg|jpeg|png|webp)$`)

func ValidOcineIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return OcineSlug(value) != ""
	case "movie":
		return len(value) <= maxIdentityLength-len("ocine-film-") && ocineMovie.MatchString(value)
	case "showing":
		t, m, epoch, hall, ok := ocineShowingParts(value)
		return ok && ValidOcineIdentity("theater", t) && ValidOcineIdentity("movie", m) && canonicalEpoch(epoch) && (hall == "past" || validPositiveDecimal(hall))
	default:
		return false
	}
}

func ocineShowingParts(value string) (theater, movie, epoch, hall string, ok bool) {
	if len(value) > maxIdentityLength-len("ocine-showing-") {
		return
	}
	theater, rest, found := strings.Cut(value, "-")
	i := strings.LastIndex(rest, "-")
	if !found || i <= 0 {
		return "", "", "", "", false
	}
	hall, rest = rest[i+1:], rest[:i]
	i = strings.LastIndex(rest, "-")
	if i <= 0 {
		return "", "", "", "", false
	}
	return theater, rest[:i], rest[i+1:], hall, true
}

func OcineShowingMatches(showing, theater, movie string, start time.Time) bool {
	t, m, epoch, _, ok := ocineShowingParts(showing)
	return ok && ValidOcineIdentity("showing", showing) && t == theater && m == movie && start.Nanosecond() == 0 && epoch == strconv.FormatInt(start.Unix(), 10)
}

// OcineBookingParts admits only original GET routes. Acquisition binds local
// film slugs to numeric F tokens, without inventing numeric film identities.
func OcineBookingParts(raw, theater string) (film, epoch, version, hall string, ok bool) {
	m := ocineBooking.FindStringSubmatch(raw)
	if len(raw) > maxURLLength || len(m) != 6 || m[1] != OcineSlug(theater) || !canonicalEpoch(m[3]) {
		return "", "", "", "", false
	}
	return m[2], m[3], m[4], m[5], true
}

func ValidOcineBookingURL(raw, theater, showing string) bool {
	t, movie, epoch, hall, ok := ocineShowingParts(showing)
	if !ok || !ValidOcineIdentity("showing", showing) || t != theater {
		return false
	}
	if hall == "past" {
		return raw == ""
	}
	f, e, _, h, valid := OcineBookingParts(raw, theater)
	return valid && e == epoch && h == hall && (movie[0] < '0' || movie[0] > '9' || f == movie)
}

func ValidOcinePosterURL(raw string) bool {
	return len(raw) <= maxURLLength && ocinePoster.MatchString(raw) && !strings.Contains(strings.TrimPrefix(raw, "https://"), "//") && !hasPatheTraversalSegment(strings.TrimPrefix(raw, "https://"))
}

func validOcineShowing(s ShowtimeRecord, theater string) bool {
	if !OcineShowingMatches(s.ProviderShowingID, theater, s.Movie.ProviderID, s.StartTime) || !ValidOcineBookingURL(s.BookingURL, theater, s.ProviderShowingID) {
		return false
	}
	_, _, _, hall, _ := ocineShowingParts(s.ProviderShowingID)
	validVersion := s.Language == LanguageVF && s.ProviderVersion == "VF" || s.Language == LanguageVOSTFR && s.ProviderVersion == "VOSTF"
	if hall == "past" {
		return s.EndTime.Equal(s.StartTime) && (validVersion || s.Language == "" && s.ProviderVersion == "")
	}
	_, _, version, _, _ := OcineBookingParts(s.BookingURL, theater)
	return validVersion && (version == "VF") == (s.Language == LanguageVF)
}
