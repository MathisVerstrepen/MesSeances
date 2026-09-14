package cinewest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const MaxResponseBytes = 16 << 20
const officeURL = "https://cinewest.cineoffice.fr/vad/"
const capitoleURL = "https://www.capitolestudios.com"
const theaterPath = "/page-data/sq/d/2506275789.json"
const apiPath = "/api/gatsby-source-boxofficeapi/"

type Fetcher interface {
	Bootstrap(context.Context) ([]byte, error)
	Catalog(context.Context, string, string) ([]byte, error)
	Program(context.Context, string) ([]byte, error)
	Theater(context.Context) ([]byte, error)
	Schedule(context.Context, string, string) ([]byte, error)
	Movies(context.Context, []string) ([]byte, error)
	RequestCount() int
}

// RequestError contains only bounded categories, never URLs, tokens, response
// bodies or transport errors. Only context cancellation is retained.
type RequestError struct {
	Operation  string
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("Cinewest request failed (category %d, status %d)", e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 || config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("cinewest requires proxies and a timeout between 5s and 60s")
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Cinewest proxy transport")
	}
	return newClient(clients, nil)
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxResponseBytes, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid Cinewest executor")
	}
	return &Client{executor: e}, nil
}

func (c *Client) RequestCount() int { return c.executor.RequestCount() }
func (c *Client) Bootstrap(ctx context.Context) ([]byte, error) {
	return c.request(ctx, "cinemas", syncproxy.Request{Method: http.MethodGet, URL: "https://www.cine-royan.com/", MediaTypes: []string{"text/html"}, NoRedirect: true})
}
func (c *Client) Catalog(ctx context.Context, kind, token string) ([]byte, error) {
	if !validCatalog(kind) || token == "" || len(token) > 1024 || strings.ContainsAny(token, "\r\n\x00") {
		return nil, &RequestError{Kind: syncproxy.FailureInvalidURL}
	}
	return c.request(ctx, "program", syncproxy.Request{Method: http.MethodGet, URL: officeURL + kind + "?" + url.Values{"api_token": {token}}.Encode(), NoRedirect: true})
}
func (c *Client) Program(ctx context.Context, id string) ([]byte, error) {
	root := schedule.CinewestWebsite("ticketingcine-" + id)
	if root == "" {
		return nil, &RequestError{Kind: syncproxy.FailureInvalidURL}
	}
	body, err := json.Marshal(struct {
		JSONRPC string            `json:"jsonrpc"`
		Method  string            `json:"method"`
		Params  map[string]string `json:"params"`
		ID      int               `json:"id"`
	}{"2.0", "get_prog", map[string]string{"site_id": id}, 1})
	if err != nil {
		return nil, errShape
	}
	return c.request(ctx, "program", syncproxy.Request{Method: http.MethodPost, URL: "https://ws.ticketingcine.com/site", Body: body, Headers: http.Header{"Content-Type": {"application/json"}, "Referer": {root}}, NoRedirect: true})
}
func (c *Client) Theater(ctx context.Context) ([]byte, error) {
	return c.request(ctx, "cinemas", syncproxy.Request{Method: http.MethodGet, URL: capitoleURL + theaterPath, NoRedirect: true})
}
func (c *Client) Schedule(ctx context.Context, from, to string) ([]byte, error) {
	q := url.Values{"from": {from}, "to": {to}, "theaters": {`{"id":"W8400","timeZone":"Europe/Paris"}`}}
	return c.request(ctx, "showtimes", syncproxy.Request{Method: http.MethodGet, URL: capitoleURL + apiPath + "schedule?" + q.Encode(), NoRedirect: true})
}
func (c *Client) Movies(ctx context.Context, ids []string) ([]byte, error) {
	if len(ids) == 0 || len(ids) > 50 {
		return nil, &RequestError{Kind: syncproxy.FailureInvalidURL}
	}
	q := url.Values{"basic": {"false"}, "castingLimit": {"3"}, "ids": ids}
	return c.request(ctx, "movies", syncproxy.Request{Method: http.MethodGet, URL: capitoleURL + apiPath + "movies?" + q.Encode(), NoRedirect: true})
}
func (c *Client) request(ctx context.Context, operation string, request syncproxy.Request) ([]byte, error) {
	body, failure := c.executor.Do(ctx, request, syncproxy.ResponsePolicy{
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
			if status != http.StatusOK {
				return &syncproxy.Failure{Kind: syncproxy.FailureStatus, StatusCode: status}, false
			}
			return nil, false
		},
	})
	if failure == nil {
		return body, nil
	}
	err := &RequestError{Operation: operation, Kind: failure.Kind, StatusCode: failure.StatusCode}
	if failure.Kind == syncproxy.FailureCanceled {
		err.cause = failure.Cause
	}
	return nil, err
}
func validCatalog(kind string) bool {
	return kind == "cinemas" || kind == "shows" || kind == "media" || kind == "screens" || kind == "mediaoptions"
}
func allowedURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.Fragment != "" || u.ForceQuery {
		return false
	}
	if u.String() == "https://www.cine-royan.com/" || u.String() == "https://ws.ticketingcine.com/site" || u.String() == capitoleURL+theaterPath {
		return true
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	if u.Host == "cinewest.cineoffice.fr" && strings.HasPrefix(u.Path, "/vad/") && validCatalog(strings.TrimPrefix(u.Path, "/vad/")) {
		return len(q) == 1 && len(q["api_token"]) == 1 && q.Get("api_token") != "" && len(q.Get("api_token")) <= 1024 && !strings.ContainsAny(q.Get("api_token"), "\r\n\x00")
	}
	if u.Host != "www.capitolestudios.com" {
		return false
	}
	if u.Path == apiPath+"schedule" {
		from, e1 := time.Parse("2006-01-02", q.Get("from"))
		to, e2 := time.Parse("2006-01-02", q.Get("to"))
		return len(q) == 3 && len(q["from"]) == 1 && len(q["to"]) == 1 && len(q["theaters"]) == 1 && e1 == nil && e2 == nil && to.Equal(from.AddDate(1, 0, 0)) && q.Get("theaters") == `{"id":"W8400","timeZone":"Europe/Paris"}`
	}
	if u.Path != apiPath+"movies" || len(q) != 3 || len(q["basic"]) != 1 || q.Get("basic") != "false" || len(q["castingLimit"]) != 1 || q.Get("castingLimit") != "3" || len(q["ids"]) == 0 || len(q["ids"]) > 50 {
		return false
	}
	for _, id := range q["ids"] {
		if !schedule.ValidCinewestIdentity("movie", "webediamovies-"+id) {
			return false
		}
	}
	return true
}
