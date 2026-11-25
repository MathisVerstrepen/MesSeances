package schedule

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// SCARE open programme cinemas are configured as sync sources. Identities are
// scoped by the dataset's numeric cinema id; booking links point to each
// cinema's own ticketing platform, so they are only required to be canonical
// public HTTPS URLs.

var (
	scareTheater = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	scareMovie   = regexp.MustCompile(`^([1-9][0-9]{0,18})-([A-Za-z0-9]{1,40}|t[a-f0-9]{16})$`)
	scareShowing = regexp.MustCompile(`^([1-9][0-9]{0,18})-[A-Za-z0-9]{1,64}$`)
	scareHost    = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
)

var errScareBinding = errors.New("invalid SCARE cinema binding")

// ValidScareIdentity validates SCARE theater, movie and showing identities.
func ValidScareIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return scareTheater.MatchString(value)
	case "movie":
		return len(value)+len(ProviderScare)+len("-film-") <= maxIdentityLength && scareMovie.MatchString(value)
	case "showing":
		return len(value)+len(ProviderScare)+len("-showing-") <= maxIdentityLength && scareShowing.MatchString(value)
	default:
		return false
	}
}

// ValidScareBookingURL accepts canonical public HTTPS URLs: a DNS host, no
// credentials, port or backslash, spelled exactly as Go re-encodes it.
func ValidScareBookingURL(raw string) bool {
	if len(raw) > maxURLLength || strings.ContainsAny(raw, "\\ \t\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Port() != "" || u.Host != u.Hostname() || net.ParseIP(u.Hostname()) != nil || !scareHost.MatchString(u.Host) {
		return false
	}
	return u.String() == raw
}

func scareBooking(raw, showingID, theaterID string) bool {
	return strings.HasPrefix(showingID, theaterID+"-") && (raw == "" || ValidScareBookingURL(raw))
}

func scareShowingRule(s ShowtimeRecord, t TheaterRecord, _ bool) (bool, error) {
	if !strings.HasPrefix(s.ProviderShowingID, t.ProviderID+"-") || !strings.HasPrefix(s.Movie.ProviderID, t.ProviderID+"-") || s.Room != "" || s.FirstPartDurationMinutes != 0 {
		return false, errScareBinding
	}
	return endNotBeforeStart(s), nil
}

// scareVersionRule: silent films carry VERSION_MUET without language; rows
// without version carry neither.
func scareVersionRule(s ShowtimeRecord) (bool, bool, error) {
	silent := s.Language == "" && s.ProviderVersion == "VERSION_MUET"
	unknown := s.Language == "" && s.ProviderVersion == ""
	if s.Language == "" && !silent && !unknown {
		return false, false, errors.New("invalid SCARE version")
	}
	return silent, unknown, nil
}
