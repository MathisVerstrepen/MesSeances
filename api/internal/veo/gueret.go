package veo

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

var gueretAlias = regexp.MustCompile(`^/r/[1-9][0-9]{0,19}$`)
var gueretDestination = regexp.MustCompile(`^/reserver/F[1-9][0-9]{0,19}/D([0-9]{10})/(VF|VO)/[1-9][0-9]{0,19}/$`)
var gueretDecimal = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var gueretRootClock = regexp.MustCompile(`^([01][0-9]|2[0-3])(:|%3[Aa])[0-5][0-9]$`)

func canonicalCheckoutURL(u *url.URL) bool {
	return safeURL(u) && u.RawQuery == "" && (u.Host == "achat.veocinemas.fr" && checkoutDestination.MatchString(u.Path) || u.Host == "achat.cinema-senechal.com" && gueretDestination.MatchString(u.Path))
}

func gueretSourceBinding(raw string, start time.Time, tags []string) bool {
	if !schedule.ValidVeoGueretRelayURL(raw) || start.IsZero() || start.Nanosecond() != 0 || start.Second() != 0 {
		return false
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return false
	}
	local := start.In(loc)
	return q.Get("perfd") == local.Format("20060102") && q.Get("perft") == local.Format("15:04") && (tags == nil || q.Get("tags") == strings.Join(tags, ","))
}

func gueretRoot(u *url.URL) bool {
	if !safeURL(u) || u.Host != "relay.mvtx.us" || u.Path != "/" || len(u.String()) > 4096 {
		return false
	}
	// The two observed opaque decimal relay IDs have no inferred source-film
	// identity equivalence. Literal keys only; reject before URL normalization.
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 4 {
		return false
	}
	for _, key := range []string{"house_id", "movie_id", "perfd", "perft"} {
		if len(q[key]) != 1 || q.Get(key) == "" {
			return false
		}
	}
	for _, pair := range strings.Split(u.RawQuery, "&") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return false
		}
		switch key {
		case "house_id", "movie_id":
			if !gueretDecimal.MatchString(value) {
				return false
			}
		case "perfd":
			day, err := time.Parse("20060102", value)
			if err != nil || day.Year() < 1 || day.Format("20060102") != value {
				return false
			}
		case "perft":
			if !gueretRootClock.MatchString(value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func gueretFetchURL(u *url.URL) bool {
	return gueretRoot(u) || safeURL(u) && u.Host == "achat.cinema-senechal.com" && u.RawQuery == "" && (gueretAlias.MatchString(u.Path) || gueretDestination.MatchString(u.Path))
}

func boundGueretURL(u *url.URL, b checkoutBinding) bool {
	if u == nil || !gueretSourceBinding(b.source, b.start, nil) {
		return false
	}
	if u.String() == b.source {
		return true
	}
	if !gueretFetchURL(u) {
		return false
	}
	if u.Host == "relay.mvtx.us" {
		source, _ := url.Parse(b.source)
		return u.Query().Get("perfd") == source.Query().Get("perfd") && u.Query().Get("perft") == source.Query().Get("perft")
	}
	if gueretAlias.MatchString(u.Path) {
		return true
	}
	parts := gueretDestination.FindStringSubmatch(u.Path)
	return len(parts) != 0 && parts[1] == strconv.FormatInt(b.start.Unix(), 10)
}

func gueretHop(u *url.URL, b checkoutBinding) int {
	if !boundGueretURL(u, b) {
		return -1
	}
	if u.String() == b.source {
		return 0
	}
	if u.Host == "relay.mvtx.us" {
		return 1
	}
	if gueretAlias.MatchString(u.Path) {
		return 2
	}
	return 3
}

func gueretRedirect(req *http.Request, via []*http.Request, b checkoutBinding) bool {
	if req == nil || req.Method != http.MethodGet || len(via) < 1 || len(via) > 3 || gueretHop(req.URL, b) != len(via) {
		return false
	}
	for i, prior := range via {
		if prior == nil || prior.Method != http.MethodGet || gueretHop(prior.URL, b) != i {
			return false
		}
	}
	return true
}

func gueretUnavailable(u *url.URL, r *http.Response, b checkoutBinding) bool {
	if b.response == nil || gueretHop(u, b) != 1 || r.StatusCode != http.StatusFound || len(r.Header.Values("Location")) != 1 ||
		r.Request == nil || r.Request.Method != http.MethodGet || r.Request.URL == nil || r.Request.URL.String() != u.String() {
		return false
	}
	raw := r.Header.Get("Location")
	// This literal root-relative response is an unavailable outcome, NOT fetch
	// authority. Inspect before net/http resolves or normalizes its Location.
	if len(raw) > 4096 || !strings.HasPrefix(raw, "/noweb?") || strings.Contains(raw, "#") {
		return false
	}
	target, err := url.Parse(raw)
	if err != nil || target.String() != raw || target.Scheme != "" || target.Host != "" || target.Path != "/noweb" {
		return false
	}
	// Reuse the strict root query grammar, without authorizing /noweb itself.
	target.Scheme, target.Host, target.Path = "https", "relay.mvtx.us", "/"
	if !gueretRoot(target) {
		return false
	}
	targetQuery, rootQuery := target.Query(), u.Query()
	for _, key := range []string{"house_id", "movie_id", "perfd", "perft"} {
		if targetQuery.Get(key) != rootQuery.Get(key) {
			return false
		}
	}
	return true
}

func gueretResponseAllowed(u *url.URL, r *http.Response, b checkoutBinding) bool {
	hop := gueretHop(u, b)
	if hop < 0 {
		return false
	}
	if r.StatusCode >= 300 && r.StatusCode < 400 {
		if hop == 3 || len(r.Header.Values("Location")) != 1 {
			return false
		}
		expected := []int{http.StatusFound, http.StatusFound, http.StatusTemporaryRedirect}
		if r.StatusCode != expected[hop] {
			return false
		}
		raw := r.Header.Get("Location")
		// Only the observed first redirect is root-relative. Reject before
		// net/http can normalize traversal, escaping or a bare fragment marker.
		if hop == 0 && strings.HasPrefix(raw, "/?") {
			raw = "https://relay.mvtx.us" + raw
		}
		target, err := url.Parse(raw)
		return err == nil && target.String() == raw && !strings.Contains(raw, "#") && gueretHop(target, b) == hop+1
	}
	// Existing challenge/status/server/cancellation classifications stay intact.
	return r.StatusCode != http.StatusOK || hop == 3 && len(r.Header.Values("Location")) == 0
}
