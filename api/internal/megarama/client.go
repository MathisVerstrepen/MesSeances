package megarama

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

const (
	ConfigURL        = "https://ws.ticketingcine.com/config.js?site_id=CHN0042"
	ProgramURL       = "https://ws.ticketingcine.com/site"
	MaxResponseBytes = 16 << 20
)

type Operation string

const (
	OperationConfig  Operation = "cinemas"
	OperationProgram Operation = "program"
	OperationPoster  Operation = "poster"
)

// RequestError never retains transport errors or provider bodies. Only context
// errors unwrap, allowing cancellation without exposing proxy credentials.
type RequestError struct {
	Operation  Operation
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("Megarama %s request failed (category %d, status %d)", e.Operation, e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 || config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("megarama requires proxies and a timeout between 5s and 60s")
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
func (c *Client) Program(ctx context.Context, id, website string) ([]byte, error) {
	if !schedule.ValidMegaramaBookingURL(website, id, "") {
		return nil, &RequestError{Operation: OperationProgram, Kind: syncproxy.FailureInvalidURL}
	}
	body, _ := json.Marshal(struct {
		JSONRPC string            `json:"jsonrpc"`
		Method  string            `json:"method"`
		Params  map[string]string `json:"params"`
		ID      int               `json:"id"`
	}{"2.0", "get_prog", map[string]string{"site_id": id}, 1})
	return c.request(ctx, OperationProgram, syncproxy.Request{Method: http.MethodPost, URL: ProgramURL, Body: body, Headers: http.Header{"Content-Type": {"application/json"}, "Referer": {strings.TrimSuffix(website, "/") + "/"}}, NoRedirect: true})
}
func (c *Client) Poster(ctx context.Context, id string) ([]byte, error) {
	if !globalID.MatchString(id) {
		return nil, &RequestError{Operation: OperationPoster, Kind: syncproxy.FailureInvalidURL}
	}
	return c.request(ctx, OperationPoster, syncproxy.Request{Method: http.MethodGet, URL: "https://www.ticketingcine.com/film/" + id + ".html", MediaTypes: []string{"text/html"}, NoRedirect: true})
}

func (c *Client) request(ctx context.Context, operation Operation, request syncproxy.Request) ([]byte, error) {
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

func allowedURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.Fragment != "" || u.ForceQuery {
		return false
	}
	if u.String() == ConfigURL || u.String() == ProgramURL {
		return true
	}
	return u.Host == "www.ticketingcine.com" && u.RawQuery == "" && strings.HasPrefix(u.Path, "/film/") && strings.HasSuffix(u.Path, ".html") && globalID.MatchString(strings.TrimSuffix(strings.TrimPrefix(u.Path, "/film/"), ".html"))
}
