package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var cinemovidaHosts = map[string]string{
	"P0176": "achat.cinema-castillet.com",
	"P0218": "achat.cinema-megacastillet.com",
	"P7951": "achat.cinemaleselect-granville.com",
	"P0876": "achat.concordecinema.com",
}

var cinemovidaMovie = regexp.MustCompile(`^([1-9][0-9]{0,111}|c[A-Za-z0-9_-]{1,111})$`)
var cinemovidaShowing = regexp.MustCompile(`^(P0176|P0218|P7951|P0876)-[a-f0-9]{64}$`)
var cinemovidaSession = regexp.MustCompile(`^[1-9][0-9]*$`)

func ValidCinemovidaIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return cinemovidaHosts[value] != ""
	case "movie":
		return cinemovidaMovie.MatchString(value)
	case "showing":
		return cinemovidaShowing.MatchString(value)
	}
	return false
}

func CinemovidaBookingHost(theater string) string { return cinemovidaHosts[theater] }

func ValidCinemovidaBookingURL(raw string, theater string) bool {
	host := CinemovidaBookingHost(theater)
	if host == "" || len(raw) > maxURLLength {
		return false
	}
	id, ok := strings.CutPrefix(raw, "https://"+host+"/reserver/r/")
	if !ok && theater == "P0876" {
		id, ok = strings.CutPrefix(raw, "https://"+host+"/r/")
	}
	return ok && cinemovidaSession.MatchString(id)
}

func CinemovidaShowingID(theater, session string) string {
	sum := sha256.Sum256([]byte(theater + "\x00" + session))
	return theater + "-" + hex.EncodeToString(sum[:])
}

func CinemovidaSourcePosterURL(raw string) (string, bool) { return GrandEcranSourcePosterURL(raw) }

func ValidCinemovidaPosterURL(raw string) bool {
	value, valid := CinemovidaSourcePosterURL(raw)
	return valid && value != "" && value == raw
}
