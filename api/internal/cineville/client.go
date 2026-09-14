package cineville

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"messeances/api/internal/syncproxy"
)

const BootstrapURL = "https://www.cineville.fr/programmes/www"
const MaxBodySize = 32 << 20

var pathSegment = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type Operation string

const (
	OperationBootstrap Operation = "bootstrap"
	OperationCinema    Operation = "cinema"
)

// RequestError retains only bounded classifications and context cancellation.
type RequestError struct {
	Operation  Operation
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	op := "unknown"
	if e.Operation == OperationBootstrap || e.Operation == OperationCinema {
		op = string(e.Operation)
	}
	return fmt.Sprintf("cineville %s request failed (category %d, status %d)", op, e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies         []syncproxy.Proxy
	RequestInterval time.Duration
	Timeout         time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if config.RequestInterval == 0 {
		config.RequestInterval = 2 * time.Second
	}
	if len(config.Proxies) == 0 || config.RequestInterval < time.Second || config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("cineville requires proxies, interval at least 1s and timeout between 5s and 60s")
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid Cineville proxy transport")
	}
	return newClient(clients, config.RequestInterval, nil)
}

// One pacer spans every proxy, including retries and redirects.
type pacer struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
}
type pacedTransport struct {
	base http.RoundTripper
	pace *pacer
}

func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay := p.interval - time.Since(p.last); !p.last.IsZero() && delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	p.last = time.Now()
	return nil
}
func (t pacedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := t.pace.wait(r.Context()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(r)
}
func newClient(clients []*http.Client, interval time.Duration, sleep func(context.Context, time.Duration) error) (*Client, error) {
	p := &pacer{interval: interval}
	wrapped := make([]*http.Client, len(clients))
	for i, client := range clients {
		copy := *client
		base := copy.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		copy.Transport = pacedTransport{base: base, pace: p}
		copy.CheckRedirect = syncproxy.NewRedirectChecker(allowedURL)
		wrapped[i] = &copy
	}
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: wrapped, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxBodySize, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept-Language": {"fr-FR,fr;q=0.9"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid Cineville executor")
	}
	return &Client{executor: e}, nil
}
func (c *Client) RequestCount() int { return c.executor.RequestCount() }
func (c *Client) Fetch(ctx context.Context) ([]byte, error) {
	return c.request(ctx, OperationBootstrap, BootstrapURL, []string{"text/html", "application/xhtml+xml"})
}
func (c *Client) FetchCinema(ctx context.Context, build, route string) ([]byte, error) {
	if !pathSegment.MatchString(build) || !pathSegment.MatchString(route) {
		return nil, &RequestError{Operation: OperationCinema, Kind: syncproxy.FailureInvalidURL}
	}
	return c.request(ctx, OperationCinema, "https://www.cineville.fr/_next/data/"+build+"/programmes/"+route+".json", []string{"application/json"})
}
func (c *Client) request(ctx context.Context, op Operation, target string, media []string) ([]byte, error) {
	body, failure := c.executor.Do(ctx, syncproxy.Request{Method: http.MethodGet, URL: target, MediaTypes: media}, syncproxy.ResponsePolicy{
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
		if op == OperationCinema && !json.Valid(body) {
			return nil, &RequestError{Operation: op, Kind: syncproxy.FailureInvalidJSON}
		}
		return body, nil
	}
	err := &RequestError{Operation: op, Kind: failure.Kind, StatusCode: failure.StatusCode}
	if failure.Kind == syncproxy.FailureCanceled {
		err.cause = ctx.Err()
	}
	return nil, err
}
func allowedURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.Host != "www.cineville.fr" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(u.String(), "?#%\\") {
		return false
	}
	if u.String() == BootstrapURL {
		return true
	}
	parts := strings.Split(u.Path, "/")
	return len(parts) == 6 && parts[0] == "" && parts[1] == "_next" && parts[2] == "data" && pathSegment.MatchString(parts[3]) && parts[4] == "programmes" && strings.HasSuffix(parts[5], ".json") && pathSegment.MatchString(strings.TrimSuffix(parts[5], ".json"))
}
