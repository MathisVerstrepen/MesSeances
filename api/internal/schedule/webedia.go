package schedule

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Webedia Movies Pro cinemas run their own Gatsby site backed by the
// Boxoffice API and are configured as sync sources (theater id + site
// origin). Boxoffice film ids are global; site-local "c" films are scoped by
// theater. Booking links point to each cinema's ticketing pages.

var (
	webediaTheater = regexp.MustCompile(`^[A-Z][A-Z0-9]{4}$`)
	webediaMovie   = regexp.MustCompile(`^([1-9][0-9]{0,18}|[A-Z][A-Z0-9]{4}-c[A-Za-z0-9_-]{1,64})$`)
	webediaShowing = regexp.MustCompile(`^([A-Z][A-Z0-9]{4})-[a-f0-9]{32}$`)
)

var errWebediaBinding = errors.New("invalid Webedia cinema binding")

// ValidWebediaIdentity validates Webedia theater, movie and showing identities.
func ValidWebediaIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return webediaTheater.MatchString(value)
	case "movie":
		return webediaMovie.MatchString(value)
	case "showing":
		return webediaShowing.MatchString(value)
	default:
		return false
	}
}

// ValidWebediaOrigin accepts a site origin: a public HTTPS root URL.
func ValidWebediaOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && ValidPublicHTTPSURL(raw) && u.Path == "/" && u.RawQuery == "" && u.Fragment == ""
}

// ValidWebediaPosterURL accepts Boxoffice (Allociné CDN) poster URLs.
func ValidWebediaPosterURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && ValidPublicHTTPSURL(raw) && (u.Host == "acsta.net" || strings.HasSuffix(u.Host, ".acsta.net")) && u.RawQuery == "" && u.Fragment == ""
}

func webediaBooking(raw, showingID, theaterID string) bool {
	return strings.HasPrefix(showingID, theaterID+"-") && (raw == "" || ValidPublicHTTPSURL(raw))
}

func webediaShowingRule(s ShowtimeRecord, t TheaterRecord, _ bool) (bool, error) {
	global := !strings.Contains(s.Movie.ProviderID, "-")
	if !strings.HasPrefix(s.ProviderShowingID, t.ProviderID+"-") || !global && !strings.HasPrefix(s.Movie.ProviderID, t.ProviderID+"-") || s.FirstPartDurationMinutes != 0 {
		return false, errWebediaBinding
	}
	return endNotBeforeStart(s), nil
}
