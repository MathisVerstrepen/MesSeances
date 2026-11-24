package majestic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const ProgramURL = "https://ws.ticketingcine.com/site"
const MaxResponseBytes = 16 << 20

// sourceName labels Majestic request errors.
const sourceName = "Majestic"

// operationProgram is the only Majestic request: one ticketingcine programme.
var operationProgram = syncproxy.Operation{Name: "program", Phase: syncproxy.PhaseProgram}

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if err := syncproxy.ValidateSourceConfig("majestic", config.Proxies, config.Timeout); err != nil {
		return nil, err
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Majestic proxy transport")
	}
	return newClient(clients, nil)
}
func allowedURL(u *url.URL) bool { return u != nil && u.String() == ProgramURL }
func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxResponseBytes, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid Majestic executor")
	}
	return &Client{executor: e}, nil
}
func (c *Client) RequestCount() int { return c.executor.RequestCount() }
func (c *Client) Program(ctx context.Context, id string) ([]byte, error) {
	if !schedule.ValidMajesticIdentity("theater", id) {
		return nil, syncproxy.InvalidRequest(sourceName, operationProgram)
	}
	body, _ := json.Marshal(struct {
		JSONRPC string            `json:"jsonrpc"`
		Method  string            `json:"method"`
		Params  map[string]string `json:"params"`
		ID      int               `json:"id"`
	}{"2.0", "get_prog", map[string]string{"site_id": id}, 1})
	body, failure := c.executor.Do(ctx, syncproxy.Request{Method: http.MethodPost, URL: ProgramURL, Body: body, Headers: http.Header{"Content-Type": {"application/json"}, "Referer": {"https://www.ticketingcine.com/"}}, NoRedirect: true}, syncproxy.StandardResponsePolicy())
	if failure == nil {
		return body, nil
	}
	return nil, syncproxy.NewRequestError(sourceName, operationProgram, http.MethodPost, ProgramURL, failure)
}
