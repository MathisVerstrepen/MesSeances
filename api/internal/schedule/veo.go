package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
	"time"
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
	return len(raw) <= maxURLLength && (veoCheckout.MatchString(raw) || ValidVeoGueretRelayURL(raw))
}

// ValidVeoGueretRelayURL is structural only. Acquisition binds the decoded date,
// clock and tags to the actual source row; checkout binds the final epoch again.
// Keep the single ASCII decoding policy mirrored in migration 065 and web/tool.
func ValidVeoGueretRelayURL(raw string) bool {
	const prefix = "https://relay.mvtx.us/ticketing/dbz?"
	if len(raw) > maxURLLength || !strings.HasPrefix(raw, prefix) {
		return false
	}
	values := map[string]string{}
	for _, pair := range strings.Split(strings.TrimPrefix(raw, prefix), "&") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || values[key] != "" || !containsVeoRelayKey(key) {
			return false
		}
		// No plus-as-space, encoded delimiters, Unicode, double decoding or
		// parameter injection. Opaque values have no inferred ID semantics.
		decoded, err := url.QueryUnescape(value)
		if err != nil || len(value) > 3072 || len(decoded) > 1024 || !veoRelayValue.MatchString(value) || !veoRelayDecoded.MatchString(decoded) {
			return false
		}
		values[key] = decoded
	}
	date := values["perfd"]
	d, err := time.Parse("20060102", date)
	return len(values) == 5 && values["code_theater"] == "P1394" && err == nil && d.Year() >= 1 && d.Format("20060102") == date && veoRelayClock.MatchString(values["perft"])
}

var veoRelayValue = regexp.MustCompile(`^[A-Za-z0-9._~,:%\-]+$`)
var veoRelayDecoded = regexp.MustCompile(`^[A-Za-z0-9._~,:\-]+$`)
var veoRelayClock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func containsVeoRelayKey(key string) bool {
	switch key {
	case "code_theater", "gid_entity", "tags", "perfd", "perft":
		return true
	}
	return false
}

func ValidVeoBookingURL(raw, theater, showing string) bool {
	if !ValidVeoIdentity("theater", theater) || !ValidVeoIdentity("showing", showing) || !strings.HasPrefix(showing, theater+"-") {
		return false
	}
	if theater != "EMS1361" {
		return len(raw) <= maxURLLength && (veoCheckout.MatchString(raw) || theater == "G06DB" && ValidVeoGueretRelayURL(raw))
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
