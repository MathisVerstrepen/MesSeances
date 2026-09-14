package noecinemas

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
	APIBaseURL         = "https://www.noecinemas.com"
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
	OperationRoom     Operation = "room"
)

// RequestError never retains URLs, credentials, transport errors, or bodies.
type RequestError struct {
	Operation  Operation
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	op := "unknown"
	switch e.Operation {
	case OperationCinemas, OperationProgram, OperationSchedule, OperationMovies, OperationRoom:
		op = string(e.Operation)
	}
	return fmt.Sprintf("Noé Cinémas %s request failed (category %d, status %d)", op, e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ json, room *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 || config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("noecinemas requires proxies and timeout between 5s and 60s")
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Noé Cinémas proxy transport")
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
	r, err := makeExecutor(MaxHTMLBytes, roomURLAllowed)
	if err != nil {
		return nil, err
	}
	return &Client{json: j, room: r}, nil
}
func (c *Client) RequestCount() int { return c.json.RequestCount() + c.room.RequestCount() }
func (c *Client) Get(ctx context.Context, op Operation, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || !operationMatchesURL(op, u) {
		return nil, &RequestError{Operation: op, Kind: syncproxy.FailureInvalidURL}
	}
	e := c.json
	r := syncproxy.Request{Method: http.MethodGet, URL: raw}
	if op == OperationRoom {
		e = c.room
		r.MediaTypes = []string{"text/html", "application/xhtml+xml"}
		r.Headers = http.Header{"Accept": {"text/html"}}
		r.AllowRedirect = true
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

func safeURL(u *url.URL) bool {
	if u == nil || len(u.String()) > MaxRequestURLBytes || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.ForceQuery || u.Host != u.Hostname() || u.RawPath != "" || strings.ContainsAny(u.String(), "\\#") || strings.ContainsFunc(u.String(), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return false
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return !strings.ContainsAny(u.Path, "%\\") && !strings.ContainsFunc(u.Path, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}
func allowedURL(u *url.URL) bool {
	if !safeURL(u) || u.Host != "www.noecinemas.com" {
		return false
	}
	for _, op := range []Operation{OperationCinemas, OperationProgram, OperationSchedule, OperationMovies} {
		if jsonOperationMatchesURL(op, u) {
			return true
		}
	}
	return false
}

var roomDestination = regexp.MustCompile(`^/reserver/F[0-9]+/D[0-9]{10}/(VF|VO)/[1-9][0-9]*/$`)

func roomURLAllowed(u *url.URL) bool {
	return safeURL(u) && len(u.String()) <= 4096 && u.Host == "achat.cinema-laigle.com" && u.RawQuery == "" && (schedule.ValidNoeCinemasBookingURL(u.String(), "P8088") || roomDestination.MatchString(u.Path))
}
func operationMatchesURL(op Operation, u *url.URL) bool {
	if op == OperationRoom {
		return u != nil && schedule.ValidNoeCinemasBookingURL(u.String(), "P8088")
	}
	return safeURL(u) && u.Host == "www.noecinemas.com" && jsonOperationMatchesURL(op, u)
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
		return u.Path == "/api/gatsby-source-boxofficeapi/scheduledMovies" && len(q) == 1 && len(q["theaterId"]) == 1 && schedule.ValidNoeCinemasIdentity("theater", q.Get("theaterId"))
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
		return string(compact) == q.Get("theaters") && schedule.ValidNoeCinemasIdentity("theater", theater.ID) && theater.TimeZone == schedule.Timezone && e1 == nil && e2 == nil && from.Format(layout) == q.Get("from") && to.Format(layout) == q.Get("to") && from.Format("15:04:05") == "03:00:00" && to.Format("15:04:05") == "03:00:00" && to.After(from)
	case OperationMovies:
		if u.Path != "/api/gatsby-source-boxofficeapi/movies" || len(q) != 3 || len(q["basic"]) != 1 || q.Get("basic") != "false" || len(q["castingLimit"]) != 1 || q.Get("castingLimit") != "3" || len(q["ids"]) == 0 || len(q["ids"]) > MovieBatchSize {
			return false
		}
		seen := map[string]bool{}
		for _, id := range q["ids"] {
			if !schedule.ValidNoeCinemasIdentity("movie", id) || seen[id] {
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
