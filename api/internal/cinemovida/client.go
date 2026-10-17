package cinemovida

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const (
	APIBaseURL         = "https://www.cine-movida.com"
	CinemasURL         = APIBaseURL + "/page-data/sq/d/2506275789.json"
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
)

// RequestError never retains source URLs, proxy credentials, bodies or transport errors.
type RequestError struct {
	Operation  Operation
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	op := "unknown"
	switch e.Operation {
	case OperationCinemas, OperationProgram, OperationSchedule, OperationMovies, OperationCheckout:
		op = string(e.Operation)
	}
	return fmt.Sprintf("Ciné-Movida %s request failed (category %d, status %d)", op, e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ json, checkout *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 || config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("cinemovida requires proxies and timeout between 5s and 60s")
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Ciné-Movida proxy transport")
	}
	return newClient(clients, nil)
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	makeExecutor := func(checkout bool) (*syncproxy.Executor, error) {
		valid, limit := allowedURL, int64(MaxResponseBytes)
		if checkout {
			valid, limit = checkoutURLAllowed, MaxHTMLBytes
		}
		copies := make([]*http.Client, len(clients))
		for i, c := range clients {
			copy := *c
			copy.Jar = nil
			copy.CheckRedirect = syncproxy.NewRedirectChecker(valid)
			if checkout {
				base := copy.Transport
				if base == nil {
					base = http.DefaultTransport
				}
				copy.Transport = checkoutTransport{base: base}
				copy.CheckRedirect = checkoutRedirect
			}
			copies[i] = &copy
		}
		return syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: copies, ProxyBacked: true, ValidURL: valid, MaxResponseBytes: limit, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept": {"application/json"}}, AdvanceNextOnRetry: true, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	}
	j, err := makeExecutor(false)
	if err != nil {
		return nil, err
	}
	c, err := makeExecutor(true)
	if err != nil {
		return nil, err
	}
	return &Client{json: j, checkout: c}, nil
}
func (c *Client) RequestCount() int { return c.json.RequestCount() + c.checkout.RequestCount() }
func (c *Client) Get(ctx context.Context, op Operation, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.String() != raw || strings.Contains(raw, "#") || !allowedURL(u) || !jsonOperationMatchesURL(op, u) {
		return nil, &RequestError{Operation: op, Kind: syncproxy.FailureInvalidURL}
	}
	return request(ctx, c.json, op, syncproxy.Request{Method: http.MethodGet, URL: raw})
}
func (c *Client) Checkout(ctx context.Context, raw string, start time.Time) ([]byte, error) {
	b := checkoutBinding{source: raw, start: start}
	u, err := url.Parse(raw)
	if err != nil || !originalCheckoutURL(u) || !boundCheckoutURL(u, b) || start.IsZero() || start.Nanosecond() != 0 {
		return nil, &RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureInvalidURL}
	}
	ctx = context.WithValue(ctx, checkoutKey{}, b)
	return request(ctx, c.checkout, OperationCheckout, syncproxy.Request{Method: http.MethodGet, URL: raw, AllowRedirect: true, MediaTypes: []string{"text/html", "application/xhtml+xml"}, Headers: http.Header{"Accept": {"text/html"}}})
}
func request(ctx context.Context, e *syncproxy.Executor, op Operation, r syncproxy.Request) ([]byte, error) {
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
	err := &RequestError{Operation: op, Kind: failure.Kind, StatusCode: failure.StatusCode}
	if failure.Kind == syncproxy.FailureCanceled {
		err.cause = ctx.Err()
	}
	return nil, err
}

func safeURL(u *url.URL) bool {
	if u == nil || len(u.String()) > MaxRequestURLBytes || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.ForceQuery || u.Host != u.Hostname() || u.RawPath != "" || strings.ContainsAny(u.String(), "\\#") || strings.ContainsFunc(u.String(), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return false
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return !strings.ContainsAny(u.Path, "%\\")
}
func allowedURL(u *url.URL) bool {
	if !safeURL(u) || u.Host != "www.cine-movida.com" {
		return false
	}
	for _, op := range []Operation{OperationCinemas, OperationProgram, OperationSchedule, OperationMovies} {
		if jsonOperationMatchesURL(op, u) {
			return true
		}
	}
	return false
}
func jsonOperationMatchesURL(op Operation, u *url.URL) bool {
	if op == OperationCinemas {
		return u.String() == CinemasURL
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || q.Encode() != u.RawQuery {
		return false
	}
	switch op {
	case OperationProgram:
		return u.Path == "/api/gatsby-source-boxofficeapi/scheduledMovies" && len(q) == 1 && len(q["theaterId"]) == 1 && schedule.ValidCinemovidaIdentity("theater", q.Get("theaterId"))
	case OperationMovies:
		if u.Path != "/api/gatsby-source-boxofficeapi/movies" || len(q) != 3 || len(q["basic"]) != 1 || q.Get("basic") != "false" || len(q["castingLimit"]) != 1 || q.Get("castingLimit") != "3" || len(q["ids"]) == 0 || len(q["ids"]) > MovieBatchSize {
			return false
		}
		seen := map[string]bool{}
		for _, id := range q["ids"] {
			if !schedule.ValidCinemovidaIdentity("movie", id) || seen[id] {
				return false
			}
			seen[id] = true
		}
		return true
	case OperationSchedule:
		var theater struct {
			ID       string `json:"id"`
			TimeZone string `json:"timeZone"`
		}
		if u.Path != "/api/gatsby-source-boxofficeapi/schedule" || len(q) != 4 || len(q["theaters"]) != 1 || len(q["includeAllMovies"]) != 1 || q.Get("includeAllMovies") != "true" || len(q["from"]) != 1 || len(q["to"]) != 1 || json.Unmarshal([]byte(q.Get("theaters")), &theater) != nil {
			return false
		}
		compact, _ := json.Marshal(theater)
		const layout = "2006-01-02T15:04:05"
		from, e1 := time.Parse(layout, q.Get("from"))
		to, e2 := time.Parse(layout, q.Get("to"))
		return string(compact) == q.Get("theaters") && schedule.ValidCinemovidaIdentity("theater", theater.ID) && theater.TimeZone == schedule.Timezone && e1 == nil && e2 == nil && from.Format(layout) == q.Get("from") && to.Format(layout) == q.Get("to") && from.Format("15:04:05") == "00:00:00" && to.Format("15:04:05") == "00:00:00" && to.After(from)
	}
	return false
}
func programURL(id string) string {
	return APIBaseURL + "/api/gatsby-source-boxofficeapi/scheduledMovies?" + url.Values{"theaterId": {id}}.Encode()
}
func moviesURL(ids []string) string {
	return APIBaseURL + "/api/gatsby-source-boxofficeapi/movies?" + url.Values{"basic": {"false"}, "castingLimit": {"3"}, "ids": ids}.Encode()
}
func scheduleURL(id, from, through string) string {
	theater, _ := json.Marshal(struct {
		ID       string `json:"id"`
		TimeZone string `json:"timeZone"`
	}{id, schedule.Timezone})
	end, _ := time.Parse(time.DateOnly, through)
	// Include the final source bucket's next-calendar-day starts without
	// excluding valid same-calendar midnight starts at the leading boundary.
	return APIBaseURL + "/api/gatsby-source-boxofficeapi/schedule?" + url.Values{"theaters": {string(theater)}, "includeAllMovies": {"true"}, "from": {from + "T00:00:00"}, "to": {end.AddDate(0, 0, 2).Format(time.DateOnly) + "T00:00:00"}}.Encode()
}

var checkoutDestination = regexp.MustCompile(`^/reserver/F[1-9][0-9]*/D([1-9][0-9]*)/(VF|VO)/[1-9][0-9]*/$`)

type checkoutKey struct{}
type checkoutBinding struct {
	source string
	start  time.Time
}

func originalCheckoutURL(u *url.URL) bool {
	if !safeURL(u) {
		return false
	}
	for _, id := range []string{"P0176", "P0218", "P7951", "P0876"} {
		if schedule.ValidCinemovidaBookingURL(u.String(), id) {
			return true
		}
	}
	return false
}
func checkoutURLAllowed(u *url.URL) bool {
	if originalCheckoutURL(u) {
		return true
	}
	if !safeURL(u) || len(u.String()) > 4096 || u.RawQuery != "" || !checkoutDestination.MatchString(u.Path) {
		return false
	}
	for _, id := range []string{"P0176", "P0218", "P7951", "P0876"} {
		if u.Host == schedule.CinemovidaBookingHost(id) {
			return true
		}
	}
	return false
}
func boundCheckoutURL(u *url.URL, b checkoutBinding) bool {
	if !checkoutURLAllowed(u) {
		return false
	}
	source, err := url.Parse(b.source)
	if err != nil || !originalCheckoutURL(source) || source.Host != u.Host {
		return false
	}
	if u.String() == b.source {
		return true
	}
	parts := checkoutDestination.FindStringSubmatch(u.Path)
	return len(parts) != 0 && parts[1] == strconv.FormatInt(b.start.Unix(), 10)
}
func checkoutRedirect(r *http.Request, via []*http.Request) error {
	valid := false
	if r != nil && len(via) == 1 && via[0] != nil && via[0].Method == http.MethodGet && r.Method == http.MethodGet {
		b, ok := via[0].Context().Value(checkoutKey{}).(checkoutBinding)
		valid = ok && via[0].URL.String() == b.source && r.URL.String() != b.source && boundCheckoutURL(r.URL, b)
	}
	return syncproxy.NewRedirectChecker(func(*url.URL) bool { return valid })(r, via)
}

type checkoutTransport struct{ base http.RoundTripper }

func (t checkoutTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	b, ok := req.Context().Value(checkoutKey{}).(checkoutBinding)
	reject := func() (*http.Response, error) {
		return nil, syncproxy.NewRedirectChecker(func(*url.URL) bool { return false })(req, nil)
	}
	if !ok || !boundCheckoutURL(req.URL, b) {
		return reject()
	}
	r, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if r.StatusCode >= 300 && r.StatusCode <= 399 {
		raw := r.Header.Get("Location")
		u, err := url.Parse(raw)
		valid := err == nil && len(r.Header.Values("Location")) == 1 && u.String() == raw && !strings.Contains(raw, "#")
		if valid && u.Host == "" && u.Scheme == "" && strings.HasPrefix(raw, "/reserver/") {
			u.Scheme, u.Host = "https", req.URL.Host
		}
		if !valid || !checkoutDestination.MatchString(u.Path) || !boundCheckoutURL(u, b) {
			if r.Body != nil {
				_ = r.Body.Close()
			}
			return reject()
		}
	}
	return r, nil
}
