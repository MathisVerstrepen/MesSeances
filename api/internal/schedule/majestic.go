package schedule

import (
	"net/url"
	"strings"
)

// MajesticWebsite fixes the seven source identities and their official roots.
func MajesticWebsite(id string) string {
	switch id {
	case "EMS0700":
		return "https://majestic-compiegne.fr/"
	case "EMS0654":
		return "https://majestic-douai.com/"
	case "EMS1333":
		return "https://dole.majestic-cinemas.com/"
	case "EMS1212":
		return "https://vesoul.majestic-cinemas.com/"
	case "EMS1299":
		return "https://cinelaon.fr/"
	case "EMS1211":
		return "https://remiremont.majestic-cinemas.com/"
	case "EMS0957":
		return "https://cinevendome.fr/"
	default:
		return ""
	}
}

func ValidMajesticIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return MajesticWebsite(value) != ""
	case "movie":
		return megaramaGlobalIdentity.MatchString(value) || validMegaramaIdentity(kind, value) && MajesticWebsite(value[:7]) != ""
	case "showing":
		return megaramaShowingIdentity.MatchString(value) && MajesticWebsite("EMS"+value[4:8]) != ""
	default:
		return false
	}
}

// ValidMajesticBookingURL accepts only exact source spellings. Event binding is
// checked during acquisition and by ValidateDataset, not inferred from host.
func ValidMajesticBookingURL(raw, theater, showing string) bool {
	root := MajesticWebsite(theater)
	if root == "" || len(raw) > maxURLLength {
		return false
	}
	if raw == root {
		return true
	}
	if !ValidMajesticIdentity("showing", showing) || !megaramaShowingMatchesTheater(showing, theater) {
		return false
	}
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

func MajesticBookingMatchesMovie(raw, theater, showing, movie string) bool {
	if !ValidMajesticBookingURL(raw, theater, showing) || !ValidMajesticIdentity("movie", movie) {
		return false
	}
	if !megaramaGlobalIdentity.MatchString(movie) && !strings.HasPrefix(movie, theater+"-") {
		return false
	}
	if raw == MajesticWebsite(theater) {
		return true
	}
	u, _ := url.Parse(raw)
	if megaramaGlobalIdentity.MatchString(movie) {
		return u.Path == "/"+theater+"/FR"+movie && u.RawQuery == ""
	}
	return strings.HasPrefix(movie, theater+"-") && u.Path == "" && u.RawQuery == theater
}

func ValidMajesticPosterURL(raw string) bool {
	if !ValidMegaramaPosterURL(raw) {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.String() == raw
}
