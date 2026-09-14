package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
)

// CinewestWebsite returns only roots verified against the public cinema catalogs.
func CinewestWebsite(theaterID string) string {
	hosts := map[string]string{
		"cineoffice-cognaclegalaxy":        "www.cine-cognac.com",
		"cineoffice-neverscinemazarin":     "www.cinemazarin-nevers.fr",
		"cineoffice-mouanssartouxlastrada": "lastrada.cinewest06.fr",
		"cineoffice-vitreaurore":           "www.aurorecinema.fr",
		"cineoffice-mouginslesbalcons":     "lesbalcons.cinewest06.fr",
		"cineoffice-royanlelido":           "www.cine-royan.com",
		"cineoffice-ploermelcinelac":       "cinelac.fr",
		"cineoffice-saintesatlanticcine":   "www.atlantic-cine.fr",
		"cineoffice-aurillaclecristal":     "cineaurillac.fr",
		"ticketingcine-EMS1185":            "www.etoilecinemas-bethune.fr",
		"ticketingcine-EMS1317":            "www.cinema-liberte.fr",
		"ticketingcine-EMS0042":            "www.toilesdumoun.fr",
		"webediamovies-W8400":              "www.capitolestudios.com",
	}
	if host := hosts[theaterID]; host != "" {
		return "https://" + host + "/"
	}
	return ""
}

var (
	cinewestMovieID         = regexp.MustCompile(`^(cineoffice-[1-9][0-9]*|ticketingcine-[A-Z0-9]{5}|ticketingcine-EMS[0-9]{4}-emsx[0-9]{4}HC[0-9]+|webediamovies-[1-9][0-9]*)$`)
	cinewestShowingID       = regexp.MustCompile(`^(cineoffice|ticketingcine|webediamovies)-[a-f0-9]{64}$`)
	cinewestSession         = regexp.MustCompile(`^showsession\?id=emsx[0-9]{12}$`)
	cinewestCapitoleBooking = regexp.MustCompile(`^/reserver/r/[1-9][0-9]*$`)
	cinewestOfficePoster    = regexp.MustCompile(`^/medias/[0-9]+/[a-f0-9]{2}/[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\.(jpeg|jpg|png|webp)$`)
	cinewestTicketPoster    = regexp.MustCompile(`^/movie_poster/(120|600)/FR[A-Z0-9]{5}/[A-Z0-9]{8}\.webp$`)
	cinewestCapitolePoster  = regexp.MustCompile(`^/img/[a-f0-9]{2}/[a-f0-9]{2}/[a-f0-9]{24,64}\.(jpeg|jpg|png|webp)$`)
)

func ValidCinewestIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return len("cinewest-"+value) <= 128 && CinewestWebsite(value) != ""
	case "movie":
		if len("cinewest-film-"+value) > 128 || !cinewestMovieID.MatchString(value) {
			return false
		}
		if strings.HasPrefix(value, "ticketingcine-EMS") {
			parts := strings.Split(value, "-")
			return len(parts) == 3 && CinewestWebsite(parts[0]+"-"+parts[1]) != "" && strings.HasPrefix(parts[2], "emsx"+parts[1][3:])
		}
		return true
	case "showing":
		return len("cinewest-showing-"+value) <= 128 && cinewestShowingID.MatchString(value)
	}
	return false
}

// CinewestShowingID scopes source identifiers to their cinema. The raw ID is
// bounded before hashing, never truncated or persisted as a public identity.
func CinewestShowingID(theaterID, sourceID string) (string, bool) {
	if !ValidCinewestIdentity("theater", theaterID) || strings.TrimSpace(sourceID) == "" || len(sourceID) > 2048 || strings.ContainsRune(sourceID, 0) {
		return "", false
	}
	digest := sha256.Sum256([]byte(theaterID + "\x00" + sourceID))
	platform, _, _ := strings.Cut(theaterID, "-")
	return platform + "-" + hex.EncodeToString(digest[:]), true
}

func ValidCinewestBookingURL(raw, theaterID, showingID string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || u.String() != raw || !cinewestSafeURL(u) || u.RawQuery != "" {
		return false
	}
	// Admin source links do not always carry cinema context. Resolve only an
	// exact approved host, never a suffix or the shared ticketing host.
	if theaterID == "" {
		for _, id := range CinewestTheaterIDs() {
			if ValidCinewestBookingURL(raw, id, showingID) {
				return true
			}
		}
		return false
	}
	root := CinewestWebsite(theaterID)
	if root == "" {
		return false
	}
	if u.Fragment == "" && (raw == root || raw+"/" == root) {
		return true
	}
	if strings.HasPrefix(theaterID, "ticketingcine-") && u.Host == strings.TrimSuffix(strings.TrimPrefix(root, "https://"), "/") && (u.Path == "" || u.Path == "/") && cinewestSession.MatchString(u.Fragment) {
		source := strings.TrimPrefix(u.Fragment, "showsession?id=")
		if !strings.HasPrefix(source, "emsx"+strings.TrimPrefix(theaterID, "ticketingcine-EMS")) {
			return false
		}
		id, _ := CinewestShowingID(theaterID, source)
		return showingID == "" || showingID == id
	}
	return theaterID == "webediamovies-W8400" && u.Host == "www.capitolestudios-reserver.cotecine.fr" && u.Fragment == "" && cinewestCapitoleBooking.MatchString(u.Path)
}

func CinewestTheaterIDs() []string {
	return []string{"cineoffice-cognaclegalaxy", "cineoffice-neverscinemazarin", "cineoffice-mouanssartouxlastrada", "cineoffice-vitreaurore", "cineoffice-mouginslesbalcons", "cineoffice-royanlelido", "cineoffice-ploermelcinelac", "cineoffice-saintesatlanticcine", "cineoffice-aurillaclecristal", "ticketingcine-EMS1185", "ticketingcine-EMS1317", "ticketingcine-EMS0042", "webediamovies-W8400"}
}

func ValidCinewestPosterURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || u.String() != raw || !cinewestSafeURL(u) || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	switch u.Host {
	case "cinemedia.cine.digital":
		return cinewestOfficePoster.MatchString(u.Path)
	case "images.monnaie-services.com":
		return cinewestTicketPoster.MatchString(u.Path)
	case "all.web.img.acsta.net":
		return cinewestCapitolePoster.MatchString(u.Path)
	}
	return false
}

func cinewestSafeURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.User == nil && u.Opaque == "" && u.RawPath == "" && u.RawFragment == "" && !u.ForceQuery && !strings.ContainsAny(u.String(), "\\\r\n\t")
}
