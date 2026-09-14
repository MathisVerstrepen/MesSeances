package schedule

import (
	"regexp"
	"strings"
)

var noeCinemasTheater = regexp.MustCompile(`^[A-Z0-9]{5}$`)
var noeCinemasMovie = regexp.MustCompile(`^([1-9][0-9]{0,111}|c[A-Za-z0-9_-]{1,111})$`)
var noeCinemasShowing = regexp.MustCompile(`^[A-Z0-9]{5}-[a-f0-9]{64}$`)
var noeCinemasSession = regexp.MustCompile(`^[1-9][0-9]*$`)

// Booking entries were verified against the complete advertised source on
// 2026-09-14. Empty-program W8391 has no inferred booking permission.
var noeCinemasBookingPrefixes = map[string]string{
	"B0158": "https://achat.noecinemas.com/domont-ermitage/r/",
	"B0181": "https://achat.cinepal.fr/reserver/r/",
	"P0089": "https://achat.noecinemas.com/pithiviers/reserver/r/",
	"P0101": "https://achat.omnia-cinemas.com/reserver/r/",
	"P0276": "https://achat.cinemamorny.fr/reserver/r/",
	"P0290": "https://achat.les-arts-cinema.com/reserver/r/",
	"P0297": "https://achat.cinema-nogent-le-rotrou.fr/reserver/r/",
	"P0542": "https://achat.noecinemas.com/houlgate/reserver/r/",
	"P0613": "https://achat.noecinemas.com/elbeuf/reserver/r/",
	"P0713": "https://achat.noecinemas.com/fecamp/reserver/r/",
	"P0714": "https://achat.3colombiers-gravenchon.fr/r/",
	"P0733": "https://achat.noecinemas.com/caudebec-en-caux/reserver/r/",
	"P0975": "https://achat.cinemas-vernon.fr/reserver/r/",
	"P0997": "https://achat.noecinemas.com/les-andelys/reserver/r/",
	"P2132": "https://achat.noecinemas.com/gisors/r/",
	"P2425": "https://achat.cinema-senonches.com/reserver/r/",
	"P2478": "https://achat.noecinemas.com/carentan/reserver/r/",
	"P7898": "https://achat.cinema-altkirch.com/reserver/r/",
	"P8088": "https://achat.cinema-laigle.com/reserver/r/",
	"P9554": "https://achat.cinemas-bernay.fr/reserver/r/",
	"W2750": "https://achat.noecinemas.com/pont-audemer/reserver/r/",
	"W5200": "https://achat.chaumont-cinemas.com/reserver/r/",
	"W7619": "https://achat.noecinemas.com/yvetot-arches-lumiere/reserver/r/",
	"W8390": "https://achat.cinemasdulavandou.fr/grand-bleu/reserver/r/",
}

func ValidNoeCinemasIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return noeCinemasTheater.MatchString(value)
	case "movie":
		return noeCinemasMovie.MatchString(value)
	case "showing":
		return noeCinemasShowing.MatchString(value)
	default:
		return false
	}
}

// ValidNoeCinemasBookingURL accepts only canonical public entry URLs. Theater
// context, when supplied, must match the exact source venue permission.
func ValidNoeCinemasBookingURL(raw string, theaterID ...string) bool {
	if len(raw) > maxURLLength || len(theaterID) > 1 {
		return false
	}
	for id, prefix := range noeCinemasBookingPrefixes {
		if len(theaterID) == 1 && theaterID[0] != id {
			continue
		}
		if session, ok := strings.CutPrefix(raw, prefix); ok && noeCinemasSession.MatchString(session) {
			return true
		}
	}
	return false
}

// Noé uses the existing acsta.net source-image policy without extending it.
func NoeCinemasSourcePosterURL(raw string) (string, bool) {
	return GrandEcranSourcePosterURL(raw)
}

func ValidNoeCinemasPosterURL(raw string) bool {
	value, valid := NoeCinemasSourcePosterURL(raw)
	return valid && value != "" && value == raw
}
