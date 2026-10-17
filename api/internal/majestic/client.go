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

type RequestError struct {
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("Majestic program request failed (category %d, status %d)", e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 || config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("majestic requires proxies and a timeout between 5s and 60s")
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
		return nil, &RequestError{Kind: syncproxy.FailureInvalidURL}
	}
	body, _ := json.Marshal(struct {
		JSONRPC string            `json:"jsonrpc"`
		Method  string            `json:"method"`
		Params  map[string]string `json:"params"`
		ID      int               `json:"id"`
	}{"2.0", "get_prog", map[string]string{"site_id": id}, 1})
	body, failure := c.executor.Do(ctx, syncproxy.Request{Method: http.MethodPost, URL: ProgramURL, Body: body, Headers: http.Header{"Content-Type": {"application/json"}, "Referer": {"https://www.ticketingcine.com/"}}, NoRedirect: true}, syncproxy.ResponsePolicy{
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
	err := &RequestError{Kind: failure.Kind, StatusCode: failure.StatusCode}
	if failure.Kind == syncproxy.FailureCanceled {
		err.cause = failure.Cause
	}
	return nil, err
}
