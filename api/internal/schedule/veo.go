package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var veoTheater = regexp.MustCompile(`^([A-Z0-9]{5}|EMS1361)$`)
var veoMovie = regexp.MustCompile(`^([1-9][0-9]{0,111}|c[A-Za-z0-9_-]{1,111}|ticketingcine-[A-Z0-9]{5})$`)
var veoShowing = regexp.MustCompile(`^([A-Z0-9]{5}|EMS1361)-[a-f0-9]{64}$`)
var veoCheckout = regexp.MustCompile(`^https://achat\.veocinemas\.fr/[a-z0-9]+(-[a-z0-9]+)*/(reserver/)?r/[1-9][0-9]*$`)
var veoTicketSession = regexp.MustCompile(`^emsx1361[0-9]{8}$`)

func ValidVeoIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return veoTheater.MatchString(value)
	case "movie":
		return veoMovie.MatchString(value)
	case "showing":
		return veoShowing.MatchString(value)
	}
	return false
}

// VeoShowingID namespaces opaque session identities before hashing.
func VeoShowingID(theater, sourceSession string) string {
	sum := sha256.Sum256([]byte(theater + "\x00" + sourceSession))
	return theater + "-" + hex.EncodeToString(sum[:])
}

func ValidVeoCheckoutURL(raw string) bool {
	return len(raw) <= maxURLLength && veoCheckout.MatchString(raw)
}

func ValidVeoBookingURL(raw, theater, showing string) bool {
	if !ValidVeoIdentity("theater", theater) || !ValidVeoIdentity("showing", showing) || !strings.HasPrefix(showing, theater+"-") {
		return false
	}
	if theater != "EMS1361" {
		return ValidVeoCheckoutURL(raw)
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > maxURLLength || u.Scheme != "https" || u.Host != "castelginest.miracinemas.fr" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.RawFragment != "" || u.RawQuery != "" || u.ForceQuery || (u.Path != "" && u.Path != "/") || strings.ContainsAny(raw, "%\\") {
		return false
	}
	if u.Fragment == "" {
		return raw == "https://castelginest.miracinemas.fr" || raw == "https://castelginest.miracinemas.fr/"
	}
	id, ok := strings.CutPrefix(u.Fragment, "showsession?id=")
	return ok && veoTicketSession.MatchString(id) && showing == VeoShowingID(theater, id)
}

// VeoSourcePosterURL returns unsupported optional images as unavailable.
func VeoSourcePosterURL(raw string, ticketingcine bool) (string, bool) {
	if !ticketingcine {
		return GrandEcranSourcePosterURL(raw)
	}
	if raw == "" {
		return "", true
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > maxURLLength || strings.ContainsAny(raw, "%\\?#") || strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Host != u.Hostname() || u.Path == "" || hasPatheTraversalSegment(u.Path) {
		return "", false
	}
	if u.Host != "images.monnaie-services.com" {
		return "", true
	}
	if !megaramaGlobalPoster.MatchString(u.Path) {
		return "", false
	}
	u.Path = strings.Replace(u.Path, "/movie_poster/120/", "/movie_poster/600/", 1)
	return u.String(), true
}

func ValidVeoPosterURL(raw string) bool {
	ticket := strings.HasPrefix(raw, "https://images.monnaie-services.com/")
	value, ok := VeoSourcePosterURL(raw, ticket)
	return ok && value != "" && value == raw
}
