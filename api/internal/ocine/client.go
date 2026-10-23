package ocine

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const MaxResponseBytes = 16 << 20

var cinemaIDs = [...]string{"309", "310", "461"}

type RequestError struct {
	syncproxy.RequestDiagnostic
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("Ocine request failed (category %d, status %d)", e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 || config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("ocine requires proxies and a timeout between 5s and 60s")
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Ocine proxy transport")
	}
	return newClient(clients, nil)
}

func allowedURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	for _, id := range cinemaIDs {
		base := "https://www.ocine.fr/" + schedule.OcineSlug(id) + "/"
		if u.String() == base+"seances/" || u.String() == base+"plan-d-acces/" {
			return true
		}
		if _, _, _, _, ok := schedule.OcineBookingParts(u.String(), id); ok {
			return true
		}
	}
	return false
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxResponseBytes, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept-Language": {"fr-FR,fr;q=0.9"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid Ocine executor")
	}
	return &Client{executor: e}, nil
}

func (c *Client) RequestCount() int { return c.executor.RequestCount() }

func (c *Client) Page(ctx context.Context, id, route string) ([]byte, error) {
	if !schedule.ValidOcineIdentity("theater", id) || route != "seances" && route != "plan-d-acces" {
		return nil, &RequestError{Kind: syncproxy.FailureInvalidURL}
	}
	return c.get(ctx, "https://www.ocine.fr/"+schedule.OcineSlug(id)+"/"+route+"/")
}

func (c *Client) Checkout(ctx context.Context, raw string) ([]byte, error) { return c.get(ctx, raw) }

func (c *Client) get(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.String() != raw || !allowedURL(u) {
		return nil, &RequestError{Kind: syncproxy.FailureInvalidURL}
	}
	body, failure := c.executor.Do(ctx, syncproxy.Request{Method: http.MethodGet, URL: raw, MediaTypes: []string{"text/html", "application/xhtml+xml"}, NoRedirect: true}, syncproxy.ResponsePolicy{
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
	e := &RequestError{Kind: failure.Kind, StatusCode: failure.StatusCode, RequestDiagnostic: syncproxy.NewRequestDiagnostic(http.MethodGet, raw, failure)}
	if failure.Kind == syncproxy.FailureCanceled {
		e.cause = failure.Cause
	}
	return nil, e
}
