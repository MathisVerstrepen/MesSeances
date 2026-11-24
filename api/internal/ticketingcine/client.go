package ticketingcine

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"messeances/api/internal/syncproxy"
)

const sourceName = "TicketingCiné"

// OperationProgram is the only generic TicketingCiné request.
var OperationProgram = syncproxy.Operation{Name: "program", Phase: syncproxy.PhaseProgram}

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}

// Client fetches programmes through the configured proxies.
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if err := syncproxy.ValidateSourceConfig("ticketingcine", config.Proxies, config.Timeout); err != nil {
		return nil, err
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid TicketingCiné proxy transport")
	}
	return newClient(clients, nil)
}

func allowedURL(u *url.URL) bool { return u != nil && u.String() == ProgramURL }

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxResponseBytes, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid TicketingCiné executor")
	}
	return &Client{executor: e}, nil
}

func (c *Client) RequestCount() int { return c.executor.RequestCount() }

// Program fetches one site's programme.
func (c *Client) Program(ctx context.Context, site string) ([]byte, error) {
	if !SiteID.MatchString(site) {
		return nil, syncproxy.InvalidRequest(sourceName, OperationProgram)
	}
	request := syncproxy.Request{Method: http.MethodPost, URL: ProgramURL, Body: ProgramRequestBody(site), Headers: http.Header{"Content-Type": {"application/json"}, "Referer": {"https://www.ticketingcine.com/"}}, NoRedirect: true}
	body, failure := c.executor.Do(ctx, request, syncproxy.StandardResponsePolicy())
	if failure == nil {
		return body, nil
	}
	return nil, syncproxy.NewRequestError(sourceName, OperationProgram, http.MethodPost, ProgramURL, failure)
}
