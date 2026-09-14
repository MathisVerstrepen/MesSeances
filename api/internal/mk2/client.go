package mk2

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"messeances/api/internal/syncproxy"
)

const APIURL = "https://prod-paris-cf.api.mk2.com"
const MaxBodySize = 32 << 20

var complexSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Operation string

const (
	OperationCinemas Operation = "cinemas"
	OperationFilms   Operation = "films"
	OperationComplex Operation = "complex"
)

// RequestError contains classifications only, never source URLs or response bodies.
type RequestError struct {
	Operation  Operation
	Kind       syncproxy.FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	op := "unknown"
	if e.Operation == OperationCinemas || e.Operation == OperationFilms || e.Operation == OperationComplex {
		op = string(e.Operation)
	}
	return fmt.Sprintf("mk2 %s request failed (category %d, status %d)", op, e.Kind, e.StatusCode)
}
func (e *RequestError) Unwrap() error { return e.cause }

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}
type Client struct{ executor *syncproxy.Executor }

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 || config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("mk2 requires proxies and timeout between 5s and 60s")
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		return nil, fmt.Errorf("invalid MK2 proxy transport")
	}
	return newClient(clients, 2*time.Second, nil)
}

// A shared transport pacer also covers retries and allowed redirects.
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
	e, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: wrapped, ProxyBacked: true, ValidURL: allowedURL, MaxResponseBytes: MaxBodySize, Headers: http.Header{"User-Agent": {"Mozilla/5.0"}, "Accept": {"application/json"}}, CancelReadOnContext: true, Retry: syncproxy.RetryPolicy{Sleep: sleep, PreserveFailureAfterWait: true}})
	if err != nil {
		return nil, fmt.Errorf("invalid MK2 executor")
	}
	return &Client{executor: e}, nil
}
func (c *Client) RequestCount() int { return c.executor.RequestCount() }
func (c *Client) FetchCinemas(ctx context.Context) ([]byte, error) {
	return c.request(ctx, OperationCinemas, APIURL+"/cinemas")
}
func (c *Client) FetchFilms(ctx context.Context) ([]byte, error) {
	return c.request(ctx, OperationFilms, APIURL+"/films")
}
func (c *Client) FetchComplex(ctx context.Context, slug string) ([]byte, error) {
	if !validComplexSlug(slug) {
		return nil, &RequestError{Operation: OperationComplex, Kind: syncproxy.FailureInvalidURL}
	}
	return c.request(ctx, OperationComplex, APIURL+"/cinema-complex/"+slug)
}
func (c *Client) request(ctx context.Context, op Operation, target string) ([]byte, error) {
	body, failure := c.executor.Get(ctx, target, syncproxy.ResponsePolicy{
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
	err := &RequestError{Operation: op, Kind: failure.Kind, StatusCode: failure.StatusCode}
	if failure.Kind == syncproxy.FailureCanceled {
		err.cause = ctx.Err()
	}
	return nil, err
}
func validComplexSlug(slug string) bool { return len(slug) <= 128 && complexSlug.MatchString(slug) }
func allowedURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.Host != "prod-paris-cf.api.mk2.com" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(u.String(), "?#%\\") {
		return false
	}
	if u.Path == "/cinemas" || u.Path == "/films" {
		return true
	}
	slug, ok := strings.CutPrefix(u.Path, "/cinema-complex/")
	return ok && validComplexSlug(slug)
}
