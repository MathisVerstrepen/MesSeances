package veo

import (
	"crypto/sha256"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// CheckoutRedirectFacts contains only closed classifications and validated public
// identities. It is private operational evidence, never a public API payload.
type CheckoutRedirectFacts struct {
	Branch, Phase, Reason       string
	Hop                         int
	HopKnown                    bool
	HTTPStatus, LocationCount   int
	LocationCountTruncated      bool
	ExpectedHost, ExpectedRoute string
	ObservedHost, ObservedRoute string
	CinemaID, ShowingID         string
	PoolPosition, PoolSize      int
}

type checkoutRedirectDiagnostic struct {
	facts      CheckoutRedirectFacts
	sourceHash [32]byte
	start      int64
}

// RedirectFacts returns a copy. Publicly constructed RequestErrors and getter
// errors cannot manufacture transport attestation or attach arbitrary strings.
func (e *RequestError) RedirectFacts() (CheckoutRedirectFacts, bool) {
	if e == nil || e.redirect == nil || e.Operation != OperationCheckout || e.Kind != syncproxy.FailureRedirect {
		return CheckoutRedirectFacts{}, false
	}
	return e.redirect.facts, true
}

func identifyCheckoutRequestError(err error, row schedule.ShowtimeRecord) error {
	// Only the concrete client result is annotated. Preserve arbitrary caller
	// wrappers (including their cancellation semantics) without trusting them.
	rejected, direct := err.(*RequestError) //nolint:errorlint // Annotate only direct client results; preserve caller wrappers unchanged.
	if !direct || rejected == nil || rejected.redirect == nil {
		return err
	}
	copy := *rejected
	d := *rejected.redirect
	copy.redirect = &d
	d.facts.CinemaID, d.facts.ShowingID = "", ""
	cinema, ok := strings.CutPrefix(row.TheaterID, "veo-")
	if ok && schedule.ValidVeoIdentity("theater", cinema) && schedule.ValidVeoIdentity("showing", row.ProviderShowingID) && strings.HasPrefix(row.ProviderShowingID, cinema+"-") && schedule.ValidVeoBookingURL(row.BookingURL, cinema, row.ProviderShowingID) &&
		d.sourceHash == sha256.Sum256([]byte(row.BookingURL)) && d.start == row.StartTime.Unix() && row.StartTime.Nanosecond() == 0 {
		d.facts.CinemaID, d.facts.ShowingID = cinema, row.ProviderShowingID
	}
	return &copy
}

// A source booking alias can point at a different start than the schedule API
// advertises. Keep the API session, but never follow that destination or use its
// room/end. Only an attested first-hop 307 with an otherwise valid canonical
// target for the same cinema qualifies; all other redirect failures stay fatal.
func checkoutStartMismatch(err error, row schedule.ShowtimeRecord) bool {
	rejected, ok := identifyCheckoutRequestError(err, row).(*RequestError) //nolint:errorlint // Require direct transport attestation, not caller wrappers.
	if !ok || rejected == nil || rejected.Operation != OperationCheckout || rejected.Kind != syncproxy.FailureRedirect || rejected.redirect == nil {
		return false
	}
	f := rejected.redirect.facts
	return f.CinemaID != "" && f.ShowingID == row.ProviderShowingID &&
		f.Branch == "regular" && f.Phase == "response" && f.Reason == "epoch_binding" &&
		f.HopKnown && f.Hop == 0 && f.HTTPStatus == http.StatusTemporaryRedirect &&
		f.LocationCount == 1 && !f.LocationCountTruncated &&
		f.ExpectedHost == "regular" && f.ExpectedRoute == "canonical" &&
		f.ObservedHost == "regular" && f.ObservedRoute == "canonical"
}

func redirectURLClasses(u *url.URL) (host, route string) {
	host, route = "unknown", "unknown"
	if u == nil {
		return host, route
	}
	switch u.Host {
	case "achat.veocinemas.fr":
		host = "regular"
	case "relay.mvtx.us":
		host = "relay"
	case "achat.cinema-senechal.com":
		host = "dedicated"
	case "":
		host = "relative"
	default:
		host = "other"
	}
	switch {
	case host == "relay" && u.Path == "/ticketing/dbz":
		route = "relay_source"
	case host == "relay" && u.Path == "/":
		route = "relay_root"
	case host == "regular" && checkoutURLAllowed(u):
		route = "source"
	case host == "regular" && checkoutDestination.MatchString(u.Path), host == "dedicated" && gueretDestination.MatchString(u.Path):
		route = "canonical"
	case host == "dedicated" && gueretAlias.MatchString(u.Path):
		route = "alias"
	default:
		route = "other"
	}
	return host, route
}

// These classifications explain an already rejected predicate. They never
// decide acceptance, normalize a destination for fetching, or retain its value.
func redirectTargetReason(target *url.URL, b checkoutBinding, expectedHop int) string {
	if target == nil {
		return "raw_location_parse"
	}
	if !safeURL(target) {
		return "unsafe_authority"
	}
	if schedule.ValidVeoGueretRelayURL(b.source) {
		switch target.Host {
		case "relay.mvtx.us":
			if target.Path != "/" {
				return "route"
			}
			if !gueretRoot(target) {
				return "query"
			}
			source, _ := url.Parse(b.source)
			if target.Query().Get("perfd") != source.Query().Get("perfd") {
				return "date_binding"
			}
			if target.Query().Get("perft") != source.Query().Get("perft") {
				return "time_binding"
			}
		case "achat.cinema-senechal.com":
			if target.RawQuery != "" {
				return "query"
			}
			if !gueretAlias.MatchString(target.Path) && !gueretDestination.MatchString(target.Path) {
				return "route"
			}
			if parts := gueretDestination.FindStringSubmatch(target.Path); len(parts) > 0 && parts[1] != strconv.FormatInt(b.start.Unix(), 10) {
				return "epoch_binding"
			}
		default:
			return "unsafe_authority"
		}
		if gueretHop(target, b) != expectedHop {
			return "hop"
		}
		return "unknown"
	}
	if target.Host != "achat.veocinemas.fr" {
		return "unsafe_authority"
	}
	if target.RawQuery != "" {
		return "query"
	}
	parts := checkoutDestination.FindStringSubmatch(target.Path)
	if len(parts) == 0 {
		return "route"
	}
	source, err := url.Parse(b.source)
	if err != nil || !checkoutURLAllowed(source) || b.start.IsZero() || b.start.Nanosecond() != 0 {
		return "source_binding"
	}
	if parts[1] != strings.Split(source.Path, "/")[1] {
		return "cinema_binding"
	}
	if parts[2] != strconv.FormatInt(b.start.Unix(), 10) {
		return "epoch_binding"
	}
	return "unknown"
}

func redirectLocation(u *url.URL, r *http.Response, b checkoutBinding) (*url.URL, string) {
	raw := r.Header.Get("Location")
	if schedule.ValidVeoGueretRelayURL(b.source) && u != nil && u.String() == b.source && strings.HasPrefix(raw, "/?") {
		raw = "https://relay.mvtx.us" + raw
	}
	target, err := url.Parse(raw)
	if err != nil {
		return nil, "raw_location_parse"
	}
	if target.String() != raw || strings.Contains(raw, "#") {
		return target, "representation"
	}
	return target, ""
}

func recordCheckoutRedirect(b checkoutBinding, u *url.URL, r *http.Response, pool clientPoolPosition, phase, reason string) {
	if b.response == nil {
		return
	}
	f := CheckoutRedirectFacts{Branch: "regular", Phase: phase, Reason: reason, ExpectedHost: "regular", ExpectedRoute: "canonical", PoolPosition: pool.index, PoolSize: pool.count}
	if schedule.ValidVeoGueretRelayURL(b.source) {
		f.Branch = "gueret"
		if hop := gueretHop(u, b); hop >= 0 && hop <= 3 {
			f.Hop, f.HopKnown = hop, true
		}
		switch f.Hop {
		case 0:
			f.ExpectedHost, f.ExpectedRoute = "relay", "relay_root"
		case 1:
			f.ExpectedHost, f.ExpectedRoute = "dedicated", "alias"
		default:
			f.ExpectedHost, f.ExpectedRoute = "dedicated", "canonical"
		}
	} else if u != nil {
		if u.String() == b.source {
			f.HopKnown = true
		} else if boundCheckoutURL(u, b) {
			f.Hop, f.HopKnown = 1, true
		}
	}
	if !f.HopKnown {
		f.ExpectedHost, f.ExpectedRoute = "unknown", "unknown"
	}
	observed := u
	if r != nil {
		if r.StatusCode >= 100 && r.StatusCode <= 599 {
			f.HTTPStatus = r.StatusCode
		}
		n := len(r.Header.Values("Location"))
		f.LocationCount, f.LocationCountTruncated = min(n, 4), n > 4
		if n > 0 {
			observed, _ = redirectLocation(u, r, b)
		}
		if reason == "response_binding" && r.Request != nil {
			observed = r.Request.URL
		}
	}
	f.ObservedHost, f.ObservedRoute = redirectURLClasses(observed)
	b.response.redirect = &checkoutRedirectDiagnostic{facts: f, sourceHash: sha256.Sum256([]byte(b.source)), start: b.start.Unix()}
}

func recordRedirectCallback(req *http.Request, via []*http.Request, b checkoutBinding) {
	if b.response == nil || len(via) == 0 {
		return
	}
	prior := via[len(via)-1]
	var u *url.URL
	if prior != nil {
		u = prior.URL
	}
	var response *http.Response
	if req != nil {
		response = req.Response
	}
	reason := "request_binding"
	limit := 1
	if schedule.ValidVeoGueretRelayURL(b.source) {
		limit = 3
	}
	switch {
	case len(via) > limit:
		reason = "hop"
	case req == nil || req.Method != http.MethodGet || prior == nil || prior.Method != http.MethodGet:
		reason = "method"
	case req.URL != nil:
		reason = redirectTargetReason(req.URL, b, len(via))
	}
	recordCheckoutRedirect(b, u, response, b.response.transportPool, "callback", reason)
}

func gueretResponseReason(u *url.URL, r *http.Response, b checkoutBinding) string {
	hop := gueretHop(u, b)
	if hop < 0 {
		return "request_binding"
	}
	if r.StatusCode >= 300 && r.StatusCode < 400 {
		if hop == 3 {
			return "hop"
		}
		if len(r.Header.Values("Location")) != 1 {
			return "location_count"
		}
		if r.StatusCode != []int{302, 302, 307}[hop] {
			return "status"
		}
		target, reason := redirectLocation(u, r, b)
		if reason != "" {
			return reason
		}
		return redirectTargetReason(target, b, hop+1)
	}
	if hop != 3 {
		return "status"
	}
	return "location_count"
}
