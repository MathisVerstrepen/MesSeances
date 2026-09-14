package grandecran

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const (
	APIBaseURL         = "https://www.grandecran.fr"
	queryPath          = "/page-data/sq/d/2506275789.json"
	CinemasURL         = APIBaseURL + queryPath
	MaxResponseBytes   = 16 << 20
	MaxHTMLBytes       = 2 << 20
	MaxRequestURLBytes = 8 << 10
	MovieBatchSize     = 50
	WorkerCount        = 16
)

type Operation string

const (
	OperationCinemas   Operation = "cinemas"
	OperationDiscovery Operation = "discovery"
	OperationProgram   Operation = "scheduled movies"
	OperationSchedule  Operation = "schedule"
	OperationMovies    Operation = "movies"
	OperationRoom      Operation = "room"
)

// RequestError never retains transport URLs, proxy credentials, or response bodies.
type RequestError struct {
	Operation  Operation
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	op := "unknown"
	switch e.Operation {
	case OperationCinemas, OperationDiscovery, OperationProgram, OperationSchedule, OperationMovies, OperationRoom:
		op = string(e.Operation)
	}
	return fmt.Sprintf("Grand Ecran %s request failed (category %d, status %d)", op, e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ json, html, room *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 || config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("grandecran requires proxies and timeout between 5s and 60s")
	}
	clients, err := syncproxy.NewFingerprintHTTP2Clients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Grand Ecran proxy transport")
	}
	return newClient(clients, nil)
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	makeExecutor := func(limit int64, valid func(*url.URL) bool) (*syncproxy.Executor, error) {
		copies := make([]*http.Client, len(clients))
		for i, c := range clients {
			copy := *c
			copy.Jar = nil
			copy.CheckRedirect = syncproxy.NewRedirectChecker(valid)
			copies[i] = &copy
		}
		return syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: copies, ProxyBacked: true, ValidURL: valid, MaxResponseBytes: limit, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept": {"application/json"}}, AdvanceNextOnRetry: true, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	}
	j, err := makeExecutor(MaxResponseBytes, allowedURL)
	if err != nil {
		return nil, err
	}
	h, err := makeExecutor(MaxHTMLBytes, allowedURL)
	if err != nil {
		return nil, err
	}
	r, err := makeExecutor(MaxHTMLBytes, roomURLAllowed)
	if err != nil {
		return nil, err
	}
	return &Client{json: j, html: h, room: r}, nil
}
func (c *Client) RequestCount() int {
	return c.json.RequestCount() + c.html.RequestCount() + c.room.RequestCount()
}
func (c *Client) Get(ctx context.Context, op Operation, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || !operationMatchesURL(op, u) {
		return nil, &RequestError{Operation: op, Kind: syncproxy.FailureInvalidURL}
	}
	e := c.json
	r := syncproxy.Request{Method: http.MethodGet, URL: raw}
	if op == OperationDiscovery || op == OperationRoom {
		e = c.html
		r.MediaTypes = []string{"text/html", "application/xhtml+xml"}
		r.Headers = http.Header{"Accept": {"text/html"}}
		if op == OperationRoom {
			e = c.room
			r.AllowRedirect = true
		}
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
	result := &RequestError{Operation: op, Kind: failure.Kind, StatusCode: failure.StatusCode}
	if failure.Kind == syncproxy.FailureCanceled {
		result.cause = ctx.Err()
	}
	return nil, result
}

var assetPath = regexp.MustCompile(`^/prod/grandecran/[A-Za-z0-9][A-Za-z0-9._-]*/public/page-data/sq/d/2506275789\.json$`)
var assetRoot = regexp.MustCompile(`https://cms-assets\.webediamovies\.pro/prod/grandecran/[A-Za-z0-9][A-Za-z0-9._-]*/public/`)

func safeURL(u *url.URL) bool {
	if u == nil || len(u.String()) > MaxRequestURLBytes || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.ForceQuery || u.Host != u.Hostname() || strings.ContainsAny(u.String(), "\\#") || strings.ContainsFunc(u.String(), func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return false
	}
	for _, s := range strings.Split(u.Path, "/") {
		if s == "." || s == ".." {
			return false
		}
	}
	return !strings.ContainsAny(u.Path, "%\\") && !strings.ContainsFunc(u.Path, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) })
}
func allowedURL(u *url.URL) bool {
	return safeURL(u) && u.RawPath == "" && (u.Host == "www.grandecran.fr" || u.Host == "cms-assets.webediamovies.pro" && u.RawQuery == "" && assetPath.MatchString(u.Path))
}
func roomURLAllowed(u *url.URL) bool {
	if !safeURL(u) || u.Host != "achat.grandecran.fr" {
		return false
	}
	// Reject malformed or nested escapes and traversal in decoded checkout queries too.
	query, err := url.QueryUnescape(u.RawQuery)
	if err != nil || strings.ContainsAny(query, "%\\") || strings.ContainsFunc(query, unicode.IsControl) {
		return false
	}
	return !strings.Contains(query, "../") && !strings.Contains(query, "/./")
}
func operationMatchesURL(op Operation, u *url.URL) bool {
	if op == OperationRoom {
		return u != nil && schedule.ValidGrandEcranBookingURL(u.String())
	}
	if !allowedURL(u) {
		return false
	}
	if op == OperationCinemas {
		return u.String() == CinemasURL || u.Host == "cms-assets.webediamovies.pro"
	}
	if op == OperationDiscovery {
		return u.String() == APIBaseURL+"/"
	}
	if u.Host != "www.grandecran.fr" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	switch op {
	case OperationProgram:
		return u.Path == "/api/gatsby-source-boxofficeapi/scheduledMovies" && len(q) == 1 && len(q["theaterId"]) == 1 && schedule.ValidGrandEcranIdentity("theater", q.Get("theaterId"))
	case OperationSchedule:
		var theater struct {
			ID       string `json:"id"`
			TimeZone string `json:"timeZone"`
		}
		if u.Path != "/api/gatsby-source-boxofficeapi/schedule" || len(q) != 4 || len(q["theaters"]) != 1 || len(q["includeAllMovies"]) != 1 || q.Get("includeAllMovies") != "true" || len(q["from"]) != 1 || len(q["to"]) != 1 || json.Unmarshal([]byte(q.Get("theaters")), &theater) != nil {
			return false
		}
		compact, _ := json.Marshal(theater)
		from, e1 := time.Parse("2006-01-02T15:04:05", q.Get("from"))
		to, e2 := time.Parse("2006-01-02T15:04:05", q.Get("to"))
		return string(compact) == q.Get("theaters") && schedule.ValidGrandEcranIdentity("theater", theater.ID) && theater.TimeZone == schedule.Timezone && e1 == nil && e2 == nil && from.Format("15:04:05") == "03:00:00" && to.Format("15:04:05") == "03:00:00" && to.After(from)
	case OperationMovies:
		if u.Path != "/api/gatsby-source-boxofficeapi/movies" || len(q) != 3 || len(q["basic"]) != 1 || q.Get("basic") != "false" || len(q["castingLimit"]) != 1 || q.Get("castingLimit") != "3" || len(q["ids"]) == 0 || len(q["ids"]) > MovieBatchSize {
			return false
		}
		seen := map[string]bool{}
		for _, id := range q["ids"] {
			if !schedule.ValidGrandEcranIdentity("movie", id) || seen[id] {
				return false
			}
			seen[id] = true
		}
		return true
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
	return APIBaseURL + "/api/gatsby-source-boxofficeapi/schedule?" + url.Values{"theaters": {string(theater)}, "includeAllMovies": {"true"}, "from": {from + "T03:00:00"}, "to": {end.AddDate(0, 0, 1).Format(time.DateOnly) + "T03:00:00"}}.Encode()
}
