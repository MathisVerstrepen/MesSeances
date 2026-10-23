// Package veo acquires the two Véo Boxoffice circuits and Castelia atomically.
package veo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html/charset"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const (
	MainOrigin         = "https://veocinemas.fr"
	SeteOrigin         = "https://sete.veocinemas.fr"
	GueretOrigin       = "https://www.cinema-senechal.com"
	CatalogPath        = "/page-data/sq/d/2506275789.json"
	CasteliaURL        = "https://castelginest.miracinemas.fr/"
	ProgramURL         = "https://ws.ticketingcine.com/site"
	MaxResponseBytes   = 16 << 20
	MaxHTMLBytes       = 2 << 20
	MaxRequestURLBytes = 8 << 10
	MovieBatchSize     = 50
	WorkerCount        = 16
)

type Operation string

const (
	OperationCinemas  Operation = "cinemas"
	OperationProgram  Operation = "scheduled movies"
	OperationSchedule Operation = "schedule"
	OperationMovies   Operation = "movies"
	OperationCheckout Operation = "checkout"
	OperationCastelia Operation = "castelia"
)

// RequestError keeps request diagnostics for the admin journal separately from
// its generic Error text and context cancellation chain.
type RequestError struct {
	syncproxy.RequestDiagnostic
	Operation  Operation
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
	redirect   *checkoutRedirectDiagnostic
}

func (e *RequestError) Error() string {
	op := "unknown"
	switch e.Operation {
	case OperationCinemas, OperationProgram, OperationSchedule, OperationMovies, OperationCheckout, OperationCastelia:
		op = string(e.Operation)
	}
	return fmt.Sprintf("Véo %s request failed (category %d, status %d)", op, e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ json, html *syncproxy.Executor }

func NewClient(c ClientConfig) (*Client, error) {
	if len(c.Proxies) == 0 || c.Timeout < 5*time.Second || c.Timeout > 60*time.Second {
		return nil, fmt.Errorf("veo requires proxies and timeout between 5s and 60s")
	}
	clients, err := syncproxy.NewHTTPClients(c.Proxies, c.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Véo proxy transport")
	}
	return newClient(clients, nil)
}

// Decode HTML at the provider-local transport boundary to honor HTTP charset as
// well as meta declarations. Executor still bounds the decoded response body.
type htmlTransport struct {
	base http.RoundTripper
	pool clientPoolPosition
}

type clientPoolPosition struct{ index, count int }
type decodedBody struct {
	io.Reader
	io.Closer
}

type checkoutBindingKey struct{}
type checkoutBinding struct {
	source   string
	start    time.Time
	response *checkoutResponse
}

// CheckoutResult keeps trusted response identity separate from untrusted HTML.
// Only this provider's transport can supply the canonical URL.
type CheckoutResult struct {
	Body                       []byte
	canonicalURL               string
	unavailableEnrichment      bool
	regularFinal404Unavailable bool
	pool                       clientPoolPosition
}

type checkoutResponse struct {
	canonicalURL               string
	unavailableEnrichment      bool
	regularFinal404Unavailable bool
	pool                       clientPoolPosition
	transportPool              clientPoolPosition
	redirect                   *checkoutRedirectDiagnostic
}

// Only this observed canonicalization is internal fetch authority. It is never
// accepted as a source/public booking URL.
var checkoutDestination = regexp.MustCompile(`^/([a-z0-9]+(?:-[a-z0-9]+)*)/reserver/F[1-9][0-9]*/D([0-9]{10})/(VF|VO)/[1-9][0-9]*/$`)

func checkoutFetchURLAllowed(u *url.URL) bool {
	return checkoutURLAllowed(u) || canonicalCheckoutURL(u) || gueretFetchURL(u)
}

func boundCheckoutURL(u *url.URL, b checkoutBinding) bool {
	if schedule.ValidVeoGueretRelayURL(b.source) {
		return boundGueretURL(u, b)
	}
	if !checkoutFetchURLAllowed(u) {
		return false
	}
	if u.String() == b.source {
		return true
	}
	source, err := url.Parse(b.source)
	if err != nil || !checkoutURLAllowed(source) || b.start.IsZero() || b.start.Nanosecond() != 0 {
		return false
	}
	parts := checkoutDestination.FindStringSubmatch(u.Path)
	return len(parts) != 0 && parts[1] == strings.Split(source.Path, "/")[1] && parts[2] == strconv.FormatInt(b.start.Unix(), 10)
}

func checkoutRedirect(request *http.Request, via []*http.Request) error {
	valid := false
	if len(via) > 0 && via[0] != nil {
		if b, ok := via[0].Context().Value(checkoutBindingKey{}).(checkoutBinding); ok && schedule.ValidVeoGueretRelayURL(b.source) {
			valid = gueretRedirect(request, via, b)
			if !valid {
				recordRedirectCallback(request, via, b)
			}
			return syncproxy.NewRedirectChecker(func(*url.URL) bool { return valid })(request, via)
		}
	}
	if request != nil && request.URL != nil && len(via) == 1 && via[0] != nil && via[0].URL != nil && via[0].Method == http.MethodGet && request.Method == http.MethodGet {
		b, ok := via[0].Context().Value(checkoutBindingKey{}).(checkoutBinding)
		valid = ok && via[0].URL.String() == b.source && request.URL.String() != b.source && boundCheckoutURL(request.URL, b)
	}
	// Use the shared error classification, but enforce the narrower one-hop,
	// cinema- and session-bound provider policy before any destination is sent.
	if !valid && len(via) > 0 && via[0] != nil {
		if b, ok := via[0].Context().Value(checkoutBindingKey{}).(checkoutBinding); ok {
			recordRedirectCallback(request, via, b)
		}
	}
	return syncproxy.NewRedirectChecker(func(*url.URL) bool { return valid })(request, via)
}

func regularFinal404Unavailable(req *http.Request, r *http.Response, b checkoutBinding) bool {
	if b.response == nil || b.start.IsZero() || b.start.Nanosecond() != 0 || req == nil || req.Method != http.MethodGet ||
		r == nil || r.StatusCode != http.StatusNotFound || len(r.Header.Values("Location")) != 0 ||
		r.Request == nil || r.Request.Method != http.MethodGet || r.Request.URL == nil ||
		!canonicalCheckoutURL(req.URL) || req.URL.Host != "achat.veocinemas.fr" || !boundCheckoutURL(req.URL, b) ||
		r.Request.URL.String() != req.URL.String() {
		return false
	}
	source, err := url.Parse(b.source)
	if err != nil || !checkoutURLAllowed(source) || source.Host != "achat.veocinemas.fr" {
		return false
	}
	// Attest the actual one-hop redirect, not just a plausible final URL. The
	// literal absolute Location must be this exact validated destination.
	previous := req.Response
	return previous != nil && previous.StatusCode == http.StatusTemporaryRedirect &&
		previous.Request != nil && previous.Request.Method == http.MethodGet && previous.Request.URL != nil &&
		previous.Request.URL.String() == b.source && previous.Request.Response == nil &&
		len(previous.Header.Values("Location")) == 1 && previous.Header.Get("Location") == req.URL.String()
}

func (t htmlTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	binding, bound := req.Context().Value(checkoutBindingKey{}).(checkoutBinding)
	if bound && binding.response != nil {
		// Each Checkout owns this cell. Redirects and retries are sequential;
		// never reuse metadata from an earlier response/attempt.
		binding.response.canonicalURL = ""
		binding.response.unavailableEnrichment = false
		binding.response.regularFinal404Unavailable = false
		binding.response.pool = clientPoolPosition{}
		binding.response.transportPool = clientPoolPosition{}
		binding.response.redirect = nil
	}
	if bound && !boundCheckoutURL(req.URL, binding) {
		reason := "request_binding"
		if schedule.ValidVeoGueretRelayURL(binding.source) && !gueretSourceBinding(binding.source, binding.start, nil) {
			reason = "source_binding"
		}
		recordCheckoutRedirect(binding, req.URL, nil, t.pool, "request", reason)
		return nil, syncproxy.NewRedirectChecker(func(*url.URL) bool { return false })(req, nil)
	}
	r, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	regularUnavailable := bound && regularFinal404Unavailable(req, r, binding)
	gueretBound := bound && schedule.ValidVeoGueretRelayURL(binding.source)
	if bound && req.Method == http.MethodGet && (regularUnavailable || gueretBound && gueretUnavailable(req.URL, r, binding)) {
		// Exact bound unavailable responses carry no trusted page facts. Close
		// unread and stop through the existing non-retryable redirect sentinel.
		if r.Body != nil {
			_ = r.Body.Close()
		}
		binding.response.unavailableEnrichment = true
		binding.response.regularFinal404Unavailable = regularUnavailable
		return nil, syncproxy.NewRedirectChecker(func(*url.URL) bool { return false })(req, nil)
	}
	if gueretBound {
		if !gueretResponseAllowed(req.URL, r, binding) {
			recordCheckoutRedirect(binding, req.URL, r, t.pool, "response", gueretResponseReason(req.URL, r, binding))
			if r.Body != nil {
				_ = r.Body.Close()
			}
			return nil, syncproxy.NewRedirectChecker(func(*url.URL) bool { return false })(req, nil)
		}
	}
	if bound && binding.response != nil && req.Method == http.MethodGet && req.URL.String() == binding.source &&
		r.StatusCode == http.StatusTemporaryRedirect && len(r.Header.Values("Location")) == 1 &&
		r.Header.Get("Location") == "https://achat.veocinemas.fr/"+strings.Split(req.URL.Path, "/")[1]+"/reserver/" {
		// This exact first-hop response means enrichment is unavailable, not that
		// the cinema entry is a session page. Never follow or read its body. Stop
		// through the existing non-retryable redirect classification; Checkout
		// alone consumes this per-request transport fact as an explicit outcome.
		if r.Body != nil {
			_ = r.Body.Close()
		}
		binding.response.unavailableEnrichment = true
		return nil, syncproxy.NewRedirectChecker(func(*url.URL) bool { return false })(req, nil)
	}
	if bound && !schedule.ValidVeoGueretRelayURL(binding.source) && (r.StatusCode == 301 || r.StatusCode == 302 || r.StatusCode == 303 || r.StatusCode == 307 || r.StatusCode == 308) && r.Header.Get("Location") != "" {
		// Inspect the raw header before net/http resolves dot segments or loses
		// a bare fragment marker. Relative/normalized aliases are not approved.
		raw := r.Header.Get("Location")
		target, err := url.Parse(raw)
		if err != nil || target.String() != raw || strings.Contains(raw, "#") || !checkoutDestination.MatchString(target.Path) || !boundCheckoutURL(target, binding) {
			reason := "raw_location_parse"
			if err == nil {
				reason = redirectTargetReason(target, binding, 1)
				if target.String() != raw || strings.Contains(raw, "#") {
					reason = "representation"
				}
			}
			recordCheckoutRedirect(binding, req.URL, r, t.pool, "response", reason)
			if r.Body != nil {
				_ = r.Body.Close()
			}
			return nil, syncproxy.NewRedirectChecker(func(*url.URL) bool { return false })(req, nil)
		}
	}
	if bound && r.Request != nil && !boundCheckoutURL(r.Request.URL, binding) {
		recordCheckoutRedirect(binding, req.URL, r, t.pool, "response", "response_binding")
		if r.Body != nil {
			_ = r.Body.Close()
		}
		return nil, syncproxy.NewRedirectChecker(func(*url.URL) bool { return false })(req, nil)
	}
	if r.StatusCode == http.StatusOK && (req.URL.Host == "achat.veocinemas.fr" || req.URL.Host == "achat.cinema-senechal.com") && strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "html") {
		reader, err := charset.NewReader(r.Body, r.Header.Get("Content-Type"))
		if err != nil {
			_ = r.Body.Close()
			return nil, fmt.Errorf("invalid checkout encoding")
		}
		r.Body = decodedBody{reader, r.Body}
		r.ContentLength = -1
	}
	if bound && binding.response != nil && r.StatusCode == http.StatusOK && canonicalCheckoutURL(req.URL) {
		binding.response.canonicalURL = req.URL.String()
	}
	if bound && binding.response != nil && r.StatusCode == http.StatusOK {
		binding.response.pool = t.pool
	}
	if bound && binding.response != nil && (r.StatusCode == 301 || r.StatusCode == 302 || r.StatusCode == 303 || r.StatusCode == 307 || r.StatusCode == 308) && r.Header.Get("Location") != "" {
		binding.response.transportPool = t.pool
	}
	if bound && r.StatusCode == http.StatusOK && r.Request == nil {
		// Executor's existing final-response check rejects missing identity. Keep
		// this exact response's bounded facts only if that check returns redirect.
		recordCheckoutRedirect(binding, req.URL, r, t.pool, "response", "response_identity_unknown")
	}
	return r, nil
}
func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	makeExecutor := func(limit int64, checkout bool) (*syncproxy.Executor, error) {
		copies := make([]*http.Client, len(clients))
		for i, c := range clients {
			copy := *c
			copy.Jar = nil
			valid := allowedURL
			if checkout {
				valid = checkoutFetchURLAllowed
				base := copy.Transport
				if base == nil {
					base = http.DefaultTransport
				}
				copy.Transport = htmlTransport{base: base, pool: clientPoolPosition{index: i + 1, count: len(clients)}}
			}
			copy.CheckRedirect = syncproxy.NewRedirectChecker(valid)
			if checkout {
				copy.CheckRedirect = checkoutRedirect
			}
			copies[i] = &copy
		}
		valid := allowedURL
		if checkout {
			valid = checkoutFetchURLAllowed
		}
		// Catalogs and thousands of independent checkout pages share these pools.
		// A timeout or server error must not drain them for every remaining request.
		return syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: copies, ProxyBacked: true, ValidURL: valid, MaxResponseBytes: limit, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept": {"application/json"}}, AdvanceNextOnRetry: true, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true, RequestScopedFailures: true}})
	}
	j, err := makeExecutor(MaxResponseBytes, false)
	if err != nil {
		return nil, err
	}
	h, err := makeExecutor(MaxHTMLBytes, true)
	if err != nil {
		return nil, err
	}
	return &Client{j, h}, nil
}
func (c *Client) RequestCount() int { return c.json.RequestCount() + c.html.RequestCount() }

// Checkout carries explicit session identity into the private transport policy.
// A bare Get has no such identity and therefore may not follow any redirect.
func (c *Client) Checkout(ctx context.Context, raw string, start time.Time) (CheckoutResult, error) {
	if start.IsZero() || start.Nanosecond() != 0 {
		return CheckoutResult{}, &RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureInvalidURL}
	}
	response := &checkoutResponse{}
	body, err := c.Get(context.WithValue(ctx, checkoutBindingKey{}, checkoutBinding{source: raw, start: start, response: response}), OperationCheckout, raw)
	if ctx.Err() != nil {
		return CheckoutResult{}, &RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureCanceled, cause: ctx.Err()}
	}
	if err != nil {
		var failure *RequestError
		if response.unavailableEnrichment && errors.As(err, &failure) && failure.Kind == syncproxy.FailureRedirect {
			return CheckoutResult{unavailableEnrichment: true, regularFinal404Unavailable: response.regularFinal404Unavailable}, nil
		}
		return CheckoutResult{}, err
	}
	return CheckoutResult{Body: body, canonicalURL: response.canonicalURL, pool: response.pool}, nil
}

func (c *Client) Get(ctx context.Context, op Operation, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || !operationMatchesURL(op, u) {
		return nil, &RequestError{Operation: op, Kind: syncproxy.FailureInvalidURL}
	}
	e := c.json
	r := syncproxy.Request{Method: http.MethodGet, URL: raw, NoRedirect: true}
	if op == OperationCheckout {
		e = c.html
		r.MediaTypes = []string{"text/html", "application/xhtml+xml"}
		r.Headers = http.Header{"Accept": {"text/html"}}
		if _, bound := ctx.Value(checkoutBindingKey{}).(checkoutBinding); bound {
			r.NoRedirect = false
			r.AllowRedirect = true
		}
	}
	if op == OperationCastelia {
		r.Method = http.MethodPost
		r.Body = []byte(`{"jsonrpc":"2.0","id":1,"method":"get_prog","params":{"site_id":"EMS1361"}}`)
		r.Headers = http.Header{"Content-Type": {"application/json"}, "Referer": {CasteliaURL}}
	}
	body, failure := e.Do(ctx, r, syncproxy.ResponsePolicy{
		BeforeRead: func(status int) (*syncproxy.Failure, bool) {
			if status == 403 || status == 429 {
				return &syncproxy.Failure{Kind: syncproxy.FailureChallenge, StatusCode: status}, false
			}
			return nil, false
		},
		AfterRead: func(status int, body []byte) (*syncproxy.Failure, bool) {
			if syncproxy.IsChallenge(body) {
				return &syncproxy.Failure{Kind: syncproxy.FailureChallenge}, false
			}
			if status >= 500 {
				return &syncproxy.Failure{Kind: syncproxy.FailureServer, StatusCode: status}, true
			}
			if status != 200 {
				return &syncproxy.Failure{Kind: syncproxy.FailureStatus, StatusCode: status}, false
			}
			return nil, false
		},
	})
	if failure == nil {
		return body, nil
	}
	rerr := &RequestError{Operation: op, Kind: failure.Kind, StatusCode: failure.StatusCode, RequestDiagnostic: syncproxy.NewRequestDiagnostic(r.Method, r.URL, failure)}
	if failure.Kind == syncproxy.FailureRedirect {
		if b, ok := ctx.Value(checkoutBindingKey{}).(checkoutBinding); ok && b.response != nil {
			rerr.redirect = b.response.redirect
		}
	}
	if failure.Kind == syncproxy.FailureCanceled {
		rerr.cause = ctx.Err()
	}
	return nil, rerr
}
func safeURL(u *url.URL) bool {
	return u != nil && len(u.String()) <= MaxRequestURLBytes && u.Scheme == "https" && u.User == nil && u.Opaque == "" && u.Fragment == "" && !u.ForceQuery && u.Host == u.Hostname() && u.RawPath == "" && !strings.ContainsAny(u.String(), "\\#") && !strings.ContainsAny(u.Path, "%\\") && !strings.Contains(u.Path, "..") && !strings.ContainsFunc(u.String(), func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) })
}
func checkoutURLAllowed(u *url.URL) bool { return u != nil && schedule.ValidVeoCheckoutURL(u.String()) }
func allowedURL(u *url.URL) bool {
	if !safeURL(u) {
		return false
	}
	if u.String() == ProgramURL {
		return true
	}
	if u.Host != "veocinemas.fr" && u.Host != "sete.veocinemas.fr" && u.Host != "www.cinema-senechal.com" {
		return false
	}
	for _, op := range []Operation{OperationCinemas, OperationProgram, OperationSchedule, OperationMovies} {
		if operationMatchesURL(op, u) {
			return true
		}
	}
	return false
}
func operationMatchesURL(op Operation, u *url.URL) bool {
	if op == OperationCheckout {
		return checkoutURLAllowed(u)
	}
	if !safeURL(u) {
		return false
	}
	if op == OperationCastelia {
		return u.String() == ProgramURL
	}
	if u.Host != "veocinemas.fr" && u.Host != "sete.veocinemas.fr" && u.Host != "www.cinema-senechal.com" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	switch op {
	case OperationCinemas:
		return u.Host != "www.cinema-senechal.com" && u.Path == CatalogPath && u.RawQuery == ""
	case OperationProgram:
		return u.Path == "/api/gatsby-source-boxofficeapi/scheduledMovies" && len(q) == 1 && len(q["theaterId"]) == 1 && boxTheater(q.Get("theaterId")) && (u.Host != "www.cinema-senechal.com" || q.Get("theaterId") == "P1394")
	case OperationMovies:
		if u.Path != "/api/gatsby-source-boxofficeapi/movies" || len(q) != 1 || len(q["ids"]) == 0 || len(q["ids"]) > MovieBatchSize {
			return false
		}
		seen := map[string]bool{}
		for _, id := range q["ids"] {
			if !boxMovie(id) || seen[id] {
				return false
			}
			seen[id] = true
		}
		return true
	case OperationSchedule:
		var t struct {
			ID       string `json:"id"`
			TimeZone string `json:"timeZone"`
		}
		if u.Path != "/api/gatsby-source-boxofficeapi/schedule" || len(q) != 4 || len(q["theaters"]) != 1 || len(q["includeAllMovies"]) != 1 || q.Get("includeAllMovies") != "true" || len(q["from"]) != 1 || len(q["to"]) != 1 || json.Unmarshal([]byte(q.Get("theaters")), &t) != nil {
			return false
		}
		compact, _ := json.Marshal(t)
		f, e1 := time.Parse("2006-01-02T15:04:05", q.Get("from"))
		to, e2 := time.Parse("2006-01-02T15:04:05", q.Get("to"))
		return string(compact) == q.Get("theaters") && boxTheater(t.ID) && (u.Host != "www.cinema-senechal.com" || t.ID == "P1394") && t.TimeZone == schedule.Timezone && e1 == nil && e2 == nil && f.Format("15:04:05") == "03:00:00" && to.Format("15:04:05") == "03:00:00" && to.After(f)
	}
	return false
}
func programURL(origin, id string) string {
	return origin + "/api/gatsby-source-boxofficeapi/scheduledMovies?" + url.Values{"theaterId": {id}}.Encode()
}
func moviesURL(origin string, ids []string) string {
	return origin + "/api/gatsby-source-boxofficeapi/movies?" + url.Values{"ids": ids}.Encode()
}
func scheduleURL(origin, id, from, through string) string {
	t, _ := json.Marshal(struct {
		ID       string `json:"id"`
		TimeZone string `json:"timeZone"`
	}{id, schedule.Timezone})
	end, _ := time.Parse(time.DateOnly, through)
	return origin + "/api/gatsby-source-boxofficeapi/schedule?" + url.Values{"theaters": {string(t)}, "includeAllMovies": {"true"}, "from": {from + "T03:00:00"}, "to": {end.AddDate(0, 0, 1).Format(time.DateOnly) + "T03:00:00"}}.Encode()
}
