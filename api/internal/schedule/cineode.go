package schedule

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CineodeSlug is the verified dataLayer theaterID to programme-route manifest.
// The closed chantonnay-lumiere menu entry is deliberately excluded.
func CineodeSlug(id string) string {
	switch id {
	case "4640":
		return "allevard-bel-donne"
	case "597":
		return "arletty"
	case "4077":
		return "benodet-cinemarine"
	case "593":
		return "casino"
	case "4626":
		return "chatillon-chalaronne-etoile"
	case "592":
		return "cine-lumieres"
	case "4388":
		return "cine7"
	case "4582":
		return "cinema-casino"
	case "4622":
		return "doullens-tivoli"
	case "3524":
		return "joigny"
	case "981":
		return "l-arc-en-ciel"
	case "3955":
		return "la-grange"
	case "4426":
		return "le-7eme-art"
	case "991":
		return "le-don-camillo"
	case "600":
		return "le-moderne"
	case "595":
		return "le-palace"
	case "636":
		return "le-paris"
	case "4420":
		return "le-raimu"
	case "603":
		return "le-riounet"
	case "601":
		return "le-roc"
	case "594":
		return "les-clubs"
	case "3859":
		return "le-vigan-le-palace"
	case "4444":
		return "le-zoetrope"
	case "4624":
		return "marly-fontenelle"
	case "4638":
		return "mediatheque-de-nay"
	case "4277":
		return "metabief"
	case "4386":
		return "novelty"
	case "4384":
		return "pirou-le-cotentin"
	case "3064":
		return "poligny"
	case "4075":
		return "reville-richelieu"
	case "3074":
		return "saint-cyr-sur-mer"
	case "4592":
		return "saint-marcellin"
	case "4181":
		return "salle-michel-legrand"
	case "4281":
		return "salles-le-7eme-art"
	case "3857":
		return "sunset"
	case "3945":
		return "tour-du-pin-equinoxe"
	case "3104":
		return "villars-les-dombes"
	default:
		return ""
	}
}

func CineodeWebsite(id string) string {
	if slug := CineodeSlug(id); slug != "" {
		return "https://www.cineode.fr/" + slug + "/seances/"
	}
	return ""
}

var cineodeMovie = regexp.MustCompile(`^([1-9][0-9]*|[a-z][a-z0-9]*(-[a-z0-9]+)*)$`)
var cineodePoster = regexp.MustCompile(`^https://fr\.web\.img[0-9]*\.acsta\.net/(img|medias|pictures)/[A-Za-z0-9_./-]+\.(jpg|jpeg|png|webp)$`)
var cineodeCotecinePoster = regexp.MustCompile(`^https://static\.cotecine\.fr/tb/Affiches/180x240/[A-Za-z0-9_+-]+\.JPG$`)

func ValidCineodeIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return CineodeSlug(value) != ""
	case "movie":
		return len(value) <= maxIdentityLength-len("cineode-film-") && cineodeMovie.MatchString(value)
	case "showing":
		theater, movie, epoch, ok := cineodeShowingParts(value)
		return ok && ValidCineodeIdentity("theater", theater) && ValidCineodeIdentity("movie", movie) && canonicalEpoch(epoch)
	default:
		return false
	}
}

func canonicalEpoch(value string) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == value
}

func cineodeShowingParts(value string) (theater, movie, epoch string, ok bool) {
	if len(value) > maxIdentityLength-len("cineode-showing-") {
		return
	}
	theater, rest, found := strings.Cut(value, "-")
	i := strings.LastIndex(rest, "-")
	if !found || i <= 0 {
		return "", "", "", false
	}
	return theater, rest[:i], rest[i+1:], true
}

func CineodeShowingMatches(showing, theater, movie string, start time.Time) bool {
	return start.Nanosecond() == 0 && ValidCineodeIdentity("showing", showing) && showing == theater+"-"+movie+"-"+strconv.FormatInt(start.Unix(), 10)
}

func ValidCineodeBookingURL(raw, theater, showing string) bool {
	t, _, _, ok := cineodeShowingParts(showing)
	return ok && t == theater && ValidCineodeIdentity("showing", showing) && raw != "" && raw == CineodeWebsite(theater)
}

func ValidCineodePosterURL(raw string) bool {
	return len(raw) <= maxURLLength && (cineodeCotecinePoster.MatchString(raw) || (cineodePoster.MatchString(raw) && !strings.Contains(strings.TrimPrefix(raw, "https://"), "//") && !hasPatheTraversalSegment(strings.TrimPrefix(raw, "https://"))))
}
