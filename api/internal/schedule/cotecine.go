package schedule

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Cotecine (Cinegroup) cinemas run a Cinegroup site whose "seances/" page
// lists the whole programme. Cinemas are configured as sync sources: the
// Cinegroup theater id and the site base URL, which may be a sub-path when
// one site hosts several cinemas. Cotecine film ids are global; repertory
// films listed by slug are scoped to their theater. Showings are identified by
// theater, film and start instant.

var (
	cotecineTheater = regexp.MustCompile(`^[1-9][0-9]{0,6}$`)
	cotecineMovie   = regexp.MustCompile(`^(?:[1-9][0-9]{0,9}|[1-9][0-9]{0,6}-[a-z0-9]+(?:-[a-z0-9]+)*)$`)
	cotecineShowing = regexp.MustCompile(`^([1-9][0-9]{0,6})-([1-9][0-9]{0,9}|[1-9][0-9]{0,6}-[a-z0-9]+(?:-[a-z0-9]+)*)-([1-9][0-9]{0,11})$`)
	// cotecineSitePath is a site root or a lowercase sub-path, always ending
	// with a slash.
	cotecineSitePath = regexp.MustCompile(`^/(?:[a-z0-9][a-z0-9-]*/)*$`)
)

var errCotecineBinding = errors.New("invalid Cotecine cinema binding")

// ValidCotecineIdentity validates Cotecine theater, movie and showing identities.
func ValidCotecineIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return cotecineTheater.MatchString(value)
	case "movie":
		return len(value) <= maxIdentityLength-len("cotecine-film-") && cotecineMovie.MatchString(value)
	case "showing":
		m := cotecineShowing.FindStringSubmatch(value)
		return len(value) <= maxIdentityLength-len("cotecine-showing-") && m != nil && canonicalEpoch(m[3])
	default:
		return false
	}
}

// ValidCotecineWebsite accepts a Cinegroup site base URL: a public HTTPS
// URL whose path is the root or a lowercase sub-path ending with a slash.
func ValidCotecineWebsite(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && ValidPublicHTTPSURL(raw) && cotecineSitePath.MatchString(u.Path) && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}

// CotecineShowingID is the showing identity of a film session at a theater.
func CotecineShowingID(theater, movie string, start time.Time) string {
	return theater + "-" + movie + "-" + strconv.FormatInt(start.Unix(), 10)
}

// ValidCotecinePosterURL accepts Allociné CDN and Cotecine poster URLs.
func ValidCotecinePosterURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !ValidPublicHTTPSURL(raw) || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return u.Host == "static.cotecine.fr" || strings.HasSuffix(u.Host, ".acsta.net")
}

// cotecineBooking accepts a canonical public HTTPS booking link of the
// showing's own theater, or no link.
func cotecineBooking(raw, showingID, theaterID string) bool {
	return strings.HasPrefix(showingID, theaterID+"-") && (raw == "" || ValidPublicHTTPSURL(raw))
}

// CotecineScopedMovie reports whether a movie id is scoped to a theater (a
// repertory film listed by slug) rather than a global Cotecine film id.
func CotecineScopedMovie(movie string) bool { return strings.Contains(movie, "-") }

func cotecineShowingRule(s ShowtimeRecord, t TheaterRecord, _ bool) (bool, error) {
	scoped := CotecineScopedMovie(s.Movie.ProviderID)
	if s.ProviderShowingID != CotecineShowingID(t.ProviderID, s.Movie.ProviderID, s.StartTime) || scoped && !strings.HasPrefix(s.Movie.ProviderID, t.ProviderID+"-") || s.StartTime.Nanosecond() != 0 || s.FirstPartDurationMinutes != 0 {
		return false, errCotecineBinding
	}
	return endNotBeforeStart(s), nil
}
