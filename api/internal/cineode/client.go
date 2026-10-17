package cineode

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

// IDs preserve the approved sandbox order; routes are owned by schedule's manifest.
var cinemaIDs = [...]string{"4640", "597", "4077", "593", "4626", "592", "4388", "4582", "4622", "3524", "981", "3955", "4426", "991", "600", "595", "636", "4420", "603", "601", "594", "3859", "4444", "4624", "4638", "4277", "4386", "4384", "3064", "4075", "3074", "4592", "4181", "4281", "3857", "3945", "3104"}

type RequestError struct {
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("Cineode request failed (category %d, status %d)", e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 || config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("cineode requires proxies and a timeout between 5s and 60s")
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Cineode proxy transport")
	}
	return newClient(clients, nil)
}

func allowedURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	for _, id := range cinemaIDs {
		for _, route := range []string{"seances", "infos-cine", "contact"} {
			if u.String() == "https://www.cineode.fr/"+schedule.CineodeSlug(id)+"/"+route+"/" {
				return true
			}
		}
	}
	return false
}

func newClient(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxResponseBytes, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept-Language": {"fr-FR,fr;q=0.9"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid Cineode executor")
	}
	return &Client{executor: e}, nil
}
func (c *Client) RequestCount() int { return c.executor.RequestCount() }

// Page accepts only a verified cinema and one of the three acquisition routes.
func (c *Client) Page(ctx context.Context, id, route string) ([]byte, error) {
	if !schedule.ValidCineodeIdentity("theater", id) || route != "seances" && route != "infos-cine" && route != "contact" {
		return nil, &RequestError{Kind: syncproxy.FailureInvalidURL}
	}
	raw := "https://www.cineode.fr/" + schedule.CineodeSlug(id) + "/" + route + "/"
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
	if route != "seances" && failure.Kind == syncproxy.FailureStatus && failure.StatusCode == 404 {
		return nil, nil
	}
	e := &RequestError{Kind: failure.Kind, StatusCode: failure.StatusCode}
	if failure.Kind == syncproxy.FailureCanceled {
		e.cause = failure.Cause
	}
	return nil, e
}
