package schedule

import "strings"

const MK2PosterPrefix = "https://srv-web-vista.mk2.com/CDN/media/entity/get/FilmPosterGraphic/"
const MK2BookingPrefix = "https://www.mk2.com/panier/seance/tickets?cinemaId="

// ValidMK2Identity preserves opaque cinema codes, including leading zeros.
// Bounds include the derived public ID or source slug.
func ValidMK2Identity(kind, value string) bool {
	if value == "" {
		return false
	}
	switch kind {
	case "theater":
		return len(value)+len("mk2-") <= maxIdentityLength && digits(value) && strings.Trim(value, "0") != ""
	case "movie":
		return len(value)+len("mk2-film-") <= maxIdentityLength && strings.HasPrefix(value, "HO") && digits(value[2:])
	case "showing":
		cinema, session, ok := strings.Cut(value, "-")
		return ok && len(value)+len("mk2-showing-") <= maxIdentityLength && ValidMK2Identity("theater", cinema) && validPositiveDecimal(session)
	default:
		return false
	}
}

func digits(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func ValidMK2PosterURL(raw string) bool {
	id, ok := strings.CutPrefix(raw, MK2PosterPrefix)
	return ok && len(raw) <= 2048 && ValidMK2Identity("movie", id)
}

func ValidMK2BookingURL(raw, theaterID, showingID string) bool {
	cinema, session, ok := strings.Cut(showingID, "-")
	return ok && cinema == theaterID && ValidMK2Identity("showing", showingID) && raw == MK2BookingPrefix+cinema+"&sessionId="+session
}
