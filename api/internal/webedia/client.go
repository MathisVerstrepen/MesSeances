package webedia

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"messeances/api/internal/boxoffice"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const sourceName = "Webedia"

// Boxoffice API paths served by every Webedia Movies Pro site.
const (
	programPath  = "/api/gatsby-source-boxofficeapi/scheduledMovies"
	moviesPath   = "/api/gatsby-source-boxofficeapi/movies"
	schedulePath = "/api/gatsby-source-boxofficeapi/schedule"
)

var (
	OperationCinemas  = syncproxy.Operation{Name: "cinemas", Phase: syncproxy.PhaseCinemas}
	OperationProgram  = syncproxy.Operation{Name: "program", Phase: syncproxy.PhaseProgram}
	OperationMovies   = syncproxy.Operation{Name: "movies", Phase: syncproxy.PhaseMovies}
	OperationSchedule = syncproxy.Operation{Name: "schedule", Phase: syncproxy.PhaseShowings}
)

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}

// Client fetches Boxoffice data from configured sites through the proxies.
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if err := syncproxy.ValidateSourceConfig("webedia", config.Proxies, config.Timeout); err != nil {
		return nil, err
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Webedia proxy transport")
	}
	return newClient(clients, nil)
}

// allowedURL accepts the Boxoffice endpoints of any public HTTPS site; Sync
// only builds URLs from configured origins.
func allowedURL(u *url.URL) bool {
	if u == nil || u.User != nil || u.Opaque != "" || u.Fragment != "" || !schedule.ValidWebediaOrigin(u.Scheme+"://"+u.Host+"/") {
		return false
	}
	switch u.Path {
	case boxoffice.CatalogPath:
		return u.RawQuery == ""
	case programPath, moviesPath, schedulePath:
		return u.RawQuery != ""
	default:
		return false
	}
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: boxoffice.MaxResponseBytes, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept": {"application/json"}}, AdvanceNextOnRetry: true, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid Webedia executor")
	}
	return &Client{executor: e}, nil
}

func (c *Client) RequestCount() int { return c.executor.RequestCount() }

// Get fetches one Boxoffice URL of a configured site.
func (c *Client) Get(ctx context.Context, operation syncproxy.Operation, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || !allowedURL(u) || u.String() != raw {
		return nil, syncproxy.InvalidRequest(sourceName, operation)
	}
	body, failure := c.executor.Do(ctx, syncproxy.Request{Method: http.MethodGet, URL: raw, NoRedirect: true}, syncproxy.StandardResponsePolicy())
	if failure == nil {
		return body, nil
	}
	return nil, syncproxy.NewRequestError(sourceName, operation, http.MethodGet, u.Scheme+"://"+u.Host+u.Path, failure)
}
