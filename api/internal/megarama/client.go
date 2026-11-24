package megarama

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/ticketingcine"
)

const (
	ConfigURL        = "https://ws.ticketingcine.com/config.js?site_id=CHN0042"
	ProgramURL       = ticketingcine.ProgramURL
	MaxResponseBytes = 16 << 20
)

// Operation names one kind of request; its phase drives sync classification.
type Operation = syncproxy.Operation

var (
	OperationConfig  = syncproxy.Operation{Name: "cinemas", Phase: syncproxy.PhaseCinemas}
	OperationProgram = syncproxy.Operation{Name: "program", Phase: syncproxy.PhaseProgram}
	OperationPoster  = syncproxy.Operation{Name: "poster", Phase: syncproxy.PhaseMovies}
)

// sourceName labels Megarama request errors.
const sourceName = "Megarama"

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if err := syncproxy.ValidateSourceConfig("megarama", config.Proxies, config.Timeout); err != nil {
		return nil, err
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Megarama proxy transport")
	}
	return newClient(clients, nil)
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{
		Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxResponseBytes,
		Headers: http.Header{"User-Agent": {"Mozilla/5.0"}}, CancelReadOnContext: true,
		Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true},
	})
	if err != nil {
		return nil, fmt.Errorf("invalid Megarama executor")
	}
	return &Client{executor: e}, nil
}

func (c *Client) RequestCount() int { return c.executor.RequestCount() }
func (c *Client) Config(ctx context.Context) ([]byte, error) {
	return c.request(ctx, OperationConfig, syncproxy.Request{Method: http.MethodGet, URL: ConfigURL, MediaTypes: []string{"text/javascript", "application/javascript"}, NoRedirect: true})
}
func (c *Client) SiteConfig(ctx context.Context, id string) ([]byte, error) {
	if !cinemaID.MatchString(id) {
		return nil, syncproxy.InvalidRequest(sourceName, OperationConfig)
	}
	return c.request(ctx, OperationConfig, syncproxy.Request{Method: http.MethodGet, URL: "https://ws.ticketingcine.com/config.js?site_id=" + id, MediaTypes: []string{"text/javascript", "application/javascript"}, NoRedirect: true})
}
func (c *Client) Program(ctx context.Context, id, website string) ([]byte, error) {
	if !schedule.ValidMegaramaBookingURL(website, id, "") {
		return nil, syncproxy.InvalidRequest(sourceName, OperationProgram)
	}
	body := ticketingcine.ProgramRequestBody(id)
	return c.request(ctx, OperationProgram, syncproxy.Request{Method: http.MethodPost, URL: ProgramURL, Body: body, Headers: http.Header{"Content-Type": {"application/json"}, "Referer": {strings.TrimSuffix(website, "/") + "/"}}, NoRedirect: true})
}
func (c *Client) Poster(ctx context.Context, id string) ([]byte, error) {
	if !globalID.MatchString(id) {
		return nil, syncproxy.InvalidRequest(sourceName, OperationPoster)
	}
	return c.request(ctx, OperationPoster, syncproxy.Request{Method: http.MethodGet, URL: "https://www.ticketingcine.com/film/" + id + ".html", MediaTypes: []string{"text/html"}, NoRedirect: true})
}

func (c *Client) request(ctx context.Context, operation Operation, request syncproxy.Request) ([]byte, error) {
	body, failure := c.executor.Do(ctx, request, syncproxy.StandardResponsePolicy())
	if failure == nil {
		return body, nil
	}
	return nil, syncproxy.NewRequestError(sourceName, operation, request.Method, request.URL, failure)
}

func allowedURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.Fragment != "" || u.ForceQuery {
		return false
	}
	if u.String() == ConfigURL || u.String() == ProgramURL {
		return true
	}
	if u.Host == "ws.ticketingcine.com" && u.Path == "/config.js" {
		return strings.HasPrefix(u.RawQuery, "site_id=") && cinemaID.MatchString(strings.TrimPrefix(u.RawQuery, "site_id="))
	}
	return u.Host == "www.ticketingcine.com" && u.RawQuery == "" && strings.HasPrefix(u.Path, "/film/") && strings.HasSuffix(u.Path, ".html") && globalID.MatchString(strings.TrimSuffix(strings.TrimPrefix(u.Path, "/film/"), ".html"))
}
