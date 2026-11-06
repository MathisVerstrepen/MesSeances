package schedule

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const CinevillePosterPrefix = "https://storage.googleapis.com/cineville-files-prod/images/"

var cinevillePosterFilename = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)

var cinevilleRoutes = map[string]string{
	"4670": "beaupreau", "4037": "bruz", "697": "concarneau",
	"2704": "darcy", "2714": "dijon", "465": "dorlisheim",
	"709": "henin", "685": "laroche", "707": "laval", "693": "lorient",
	"691": "katorza", "4672": "morlaix", "4153": "pontlabbe",
	"1874": "lespontsdece", "639": "katorzaquimper", "695": "quimper",
	"4673": "club6", "689": "stnazaire", "687": "stsebastien",
	"4682": "savenay", "4674": "tregueux", "681": "garenne",
	"683": "parclann", "4099": "vernsurseiche",
}

// CinevilleRoute maps verified public cinema IDs to their programme routes.
// Unknown cinemas remain unlinked until their route has been verified.
func CinevilleRoute(theaterID string) string {
	return cinevilleRoutes[theaterID]
}

// ValidCinevilleIdentity preserves exact signed int64 visas and cinema-scoped sessions.
func ValidCinevilleIdentity(kind, value string) bool {
	if kind == "showing" {
		parts := strings.Split(value, "-")
		return len(parts) == 2 && ValidCinevilleIdentity("theater", parts[0]) && ValidCinevilleIdentity("theater", parts[1])
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && strconv.FormatInt(n, 10) == value && (kind == "movie" && n != 0 || kind == "theater" && n > 0)
}

func ValidCinevillePosterURL(raw string) bool {
	name, ok := strings.CutPrefix(raw, CinevillePosterPrefix)
	return ok && len(raw) <= maxURLLength && cinevillePosterFilename.MatchString(name) && !strings.Contains(name, "..")
}

func ValidCinevilleBookingURL(raw, theaterID, showingID string) bool {
	if !ValidCinevilleIdentity("theater", theaterID) || !ValidCinevilleIdentity("showing", showingID) || !strings.HasPrefix(showingID, theaterID+"-") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "www.cineville.fr" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, "?#%\\") || len(raw) > maxURLLength {
		return false
	}
	parts := strings.Split(u.Path, "/")
	return len(parts) == 5 && parts[0] == "" && parts[1] == "vad" && parts[2] == theaterID && parts[2]+"-"+parts[3] == showingID && ValidCinevilleIdentity("theater", parts[4])
}
