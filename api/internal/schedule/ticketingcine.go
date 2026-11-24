package schedule

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Generic TicketingCiné (Monnaie Services) cinemas. Unlike Majestic and
// Megarama, their sites are configured as sync sources, so any EMSdddd site
// is a valid identity and bookings always use www.ticketingcine.com routes.

var errTicketingcineBinding = errors.New("invalid TicketingCiné booking or movie cinema")

var ticketingcineSite = regexp.MustCompile(`^EMS[0-9]{4}$`)

// ValidTicketingcineIdentity validates generic TicketingCiné theater, movie and
// showing identities. Movie IDs are national five-character films or
// site-scoped events bound to their site digits.
func ValidTicketingcineIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return ticketingcineSite.MatchString(value)
	case "movie":
		return len(value)+len(ProviderTicketingcine)+len("-film-") <= maxIdentityLength &&
			(megaramaGlobalIdentity.MatchString(value) || megaramaLocalIdentity.MatchString(value) && value[3:7] == value[12:16])
	case "showing":
		return megaramaShowingIdentity.MatchString(value)
	default:
		return false
	}
}

// TicketingcineBookingURL is the session route used when the source publishes
// no booking link.
func TicketingcineBookingURL(theater, showing string) string {
	return "https://www.ticketingcine.com?" + theater + "#showsession?id=" + showing
}

// ValidTicketingcineBookingURL binds a booking link to its site and session.
func ValidTicketingcineBookingURL(raw, theater, showing string) bool {
	return len(raw) <= maxURLLength && ValidTicketingcineIdentity("theater", theater) && ValidTicketingcineIdentity("showing", showing) &&
		megaramaShowingMatchesTheater(showing, theater) && ticketingcineSessionURL(raw, theater, showing)
}

// ticketingcineSessionURL accepts the exact www.ticketingcine.com session
// routes: a film path or a query-only site route, each with the showing
// fragment and an optional "&ps=ticketingcine" suffix.
func ticketingcineSessionURL(raw, theater, showing string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "www.ticketingcine.com" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.RawFragment != "" || u.ForceQuery {
		return false
	}
	fragment := "showsession?id=" + showing
	if u.Fragment != fragment && u.Fragment != fragment+"&ps=ticketingcine" {
		return false
	}
	if u.Path == "" && u.RawQuery == theater {
		return raw == "https://www.ticketingcine.com?"+theater+"#"+u.Fragment
	}
	prefix := "/" + theater + "/FR"
	return u.RawQuery == "" && strings.HasPrefix(u.Path, prefix) && megaramaGlobalIdentity.MatchString(strings.TrimPrefix(u.Path, prefix)) && raw == "https://www.ticketingcine.com"+u.Path+"#"+u.Fragment
}

// TicketingcineBookingMatchesMovie binds a validated session route to its
// film: film paths name the global film; site routes serve global films and
// local events alike.
func TicketingcineBookingMatchesMovie(raw, theater, movie string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Path == "" && u.RawQuery == theater {
		return true
	}
	if megaramaGlobalIdentity.MatchString(movie) {
		return u.Path == "/"+theater+"/FR"+movie && u.RawQuery == ""
	}
	return false
}

func ticketingcineShowingRule(s ShowtimeRecord, t TheaterRecord, validEnd bool) (bool, error) {
	if !megaramaGlobalIdentity.MatchString(s.Movie.ProviderID) && !strings.HasPrefix(s.Movie.ProviderID, t.ProviderID+"-") ||
		!TicketingcineBookingMatchesMovie(s.BookingURL, t.ProviderID, s.Movie.ProviderID) {
		return false, errTicketingcineBinding
	}
	return megaramaShowingRule(s, t, validEnd)
}

func validTicketingcinePosterURL(raw string) bool {
	if !ValidMegaramaPosterURL(raw) {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.String() == raw
}
