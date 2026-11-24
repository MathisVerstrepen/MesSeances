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

type Fetcher interface {
	Bootstrap(context.Context) ([]byte, error)
	Catalog(context.Context, string, string) ([]byte, error)
	Program(context.Context, string) ([]byte, error)
	RequestCount() int
}

// sourceName labels Cinewest request errors.
const sourceName = "Cinewest"

// Cinewest errors never name the operation; the phase still drives sync
// classification.
var (
	operationCinemas = syncproxy.Operation{Phase: syncproxy.PhaseCinemas}
	operationProgram = syncproxy.Operation{Phase: syncproxy.PhaseProgram}
)

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if err := syncproxy.ValidateSourceConfig("cinewest", config.Proxies, config.Timeout); err != nil {
		return nil, err
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
	return c.request(ctx, operationCinemas, syncproxy.Request{Method: http.MethodGet, URL: "https://www.cine-royan.com/", MediaTypes: []string{"text/html"}, NoRedirect: true})
}
func (c *Client) Catalog(ctx context.Context, kind, token string) ([]byte, error) {
	if !validCatalog(kind) || token == "" || len(token) > 1024 || strings.ContainsAny(token, "\r\n\x00") {
		return nil, syncproxy.InvalidRequest(sourceName, syncproxy.Operation{})
	}
	return c.request(ctx, operationProgram, syncproxy.Request{Method: http.MethodGet, URL: officeURL + kind + "?" + url.Values{"api_token": {token}}.Encode(), NoRedirect: true})
}
func (c *Client) Program(ctx context.Context, id string) ([]byte, error) {
	root := schedule.CinewestWebsite("ticketingcine-" + id)
	if root == "" {
		return nil, syncproxy.InvalidRequest(sourceName, syncproxy.Operation{})
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
	return c.request(ctx, operationProgram, syncproxy.Request{Method: http.MethodPost, URL: "https://ws.ticketingcine.com/site", Body: body, Headers: http.Header{"Content-Type": {"application/json"}, "Referer": {root}}, NoRedirect: true})
}
func (c *Client) request(ctx context.Context, operation syncproxy.Operation, request syncproxy.Request) ([]byte, error) {
	body, failure := c.executor.Do(ctx, request, syncproxy.StandardResponsePolicy())
	if failure == nil {
		return body, nil
	}
	return nil, syncproxy.NewRequestError(sourceName, operation, request.Method, request.URL, failure)
}
func validCatalog(kind string) bool {
	return kind == "cinemas" || kind == "shows" || kind == "media" || kind == "screens" || kind == "mediaoptions"
}
func allowedURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.Fragment != "" || u.ForceQuery {
		return false
	}
	if u.String() == "https://www.cine-royan.com/" || u.String() == "https://ws.ticketingcine.com/site" {
		return true
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	if u.Host == "cinewest.cineoffice.fr" && strings.HasPrefix(u.Path, "/vad/") && validCatalog(strings.TrimPrefix(u.Path, "/vad/")) {
		return len(q) == 1 && len(q["api_token"]) == 1 && q.Get("api_token") != "" && len(q.Get("api_token")) <= 1024 && !strings.ContainsAny(q.Get("api_token"), "\r\n\x00")
	}
	return false
}
