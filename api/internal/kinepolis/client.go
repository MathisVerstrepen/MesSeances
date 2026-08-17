package kinepolis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"movieflow/api/internal/syncproxy"
)

const (
	ScheduleURL = "https://kinepolis.fr/?main_section=tous+les+films"
	MaxBodySize = 16 << 20
	userAgent   = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	accept      = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"
	acceptLang  = "fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7"
)

var (
	errRedirectHost  = errors.New("redirect host rejected")
	errRedirectLimit = errors.New("redirect limit exceeded")
)

type ClientConfig struct {
	Proxies         []syncproxy.Proxy
	RequestInterval time.Duration
	Timeout         time.Duration
}

type TerminalError struct{ category string }

func (e *TerminalError) Error() string { return "Kinepolis request stopped: " + e.category }

type Client struct {
	mu           sync.Mutex
	config       ClientConfig
	clients      []*http.Client
	unavailable  []bool
	next         int
	lastStart    time.Time
	requestCount int
}

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 {
		return nil, fmt.Errorf("at least one proxy is required")
	}
	if config.RequestInterval < time.Second {
		return nil, fmt.Errorf("request interval must be at least 1s")
	}
	if config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("timeout must be between 5s and 60s")
	}
	clients, err := syncproxy.NewFingerprintHTTP2Clients(config.Proxies, config.Timeout, checkRedirect)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy transport")
	}
	return &Client{config: config, clients: clients, unavailable: make([]bool, len(clients))}, nil
}

func (c *Client) RequestCount() int { c.mu.Lock(); defer c.mu.Unlock(); return c.requestCount }

func (c *Client) Fetch(ctx context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	start := c.next
	if c.availableFrom(start) < 0 {
		return nil, fmt.Errorf("fetch Kinepolis schedule failed: no proxy available")
	}
	for {
		ordinal := c.availableFrom(start)
		if ordinal < 0 {
			return nil, fmt.Errorf("fetch Kinepolis schedule failed: no proxy available")
		}
		start = (ordinal + 1) % len(c.clients)
		if err := c.pace(ctx); err != nil {
			return nil, fmt.Errorf("fetch Kinepolis schedule canceled")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ScheduleURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create Kinepolis request failed")
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", accept)
		req.Header.Set("Accept-Language", acceptLang)
		c.lastStart = time.Now()
		c.requestCount++
		response, requestErr := c.clients[ordinal].Do(req)
		if requestErr != nil {
			if errors.Is(requestErr, errRedirectHost) || errors.Is(requestErr, errRedirectLimit) {
				return nil, fmt.Errorf("fetch Kinepolis schedule failed on proxy %d: redirect rejected", ordinal+1)
			}
			c.unavailable[ordinal] = true
			if c.availableFrom(start) >= 0 {
				continue
			}
			return nil, fmt.Errorf("fetch Kinepolis schedule failed on proxy %d: transport", ordinal+1)
		}
		body, readErr := readBounded(response.Body)
		response.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("fetch Kinepolis schedule failed on proxy %d: response too large or unreadable", ordinal+1)
		}
		if syncproxy.IsChallenge(body) {
			return nil, &TerminalError{category: fmt.Sprintf("challenge response via proxy %d", ordinal+1)}
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			c.unavailable[ordinal] = true
			if c.availableFrom(start) >= 0 {
				continue
			}
			return nil, &TerminalError{category: fmt.Sprintf("HTTP %d: no proxy available", response.StatusCode)}
		}
		if response.StatusCode >= 500 {
			c.unavailable[ordinal] = true
			if c.availableFrom(start) >= 0 {
				continue
			}
			return nil, fmt.Errorf("fetch Kinepolis schedule failed on proxy %d: server error", ordinal+1)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("fetch Kinepolis schedule failed on proxy %d: HTTP %d", ordinal+1, response.StatusCode)
		}
		if !validFinalRequest(response.Request) {
			return nil, fmt.Errorf("fetch Kinepolis schedule failed on proxy %d: invalid final response URL", ordinal+1)
		}
		contentType, _, contentTypeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		contentType = strings.ToLower(contentType)
		if contentTypeErr != nil || contentType != "text/html" && contentType != "application/xhtml+xml" {
			return nil, fmt.Errorf("fetch Kinepolis schedule failed on proxy %d: unexpected content type", ordinal+1)
		}
		if len(body) == 0 {
			return nil, fmt.Errorf("fetch Kinepolis schedule failed on proxy %d: empty response", ordinal+1)
		}
		c.next = (ordinal + 1) % len(c.clients)
		return body, nil
	}
	return nil, fmt.Errorf("fetch Kinepolis schedule failed")
}

func readBounded(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, MaxBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil || len(body) > MaxBodySize {
		return nil, fmt.Errorf("invalid response body")
	}
	return body, nil
}
func validFinalRequest(request *http.Request) bool {
	if request == nil {
		return false
	}
	want, _ := url.Parse(ScheduleURL)
	actual := request.URL
	return actual != nil && actual.Scheme == "https" && actual.Host == want.Host && actual.User == nil && actual.Path == want.Path && actual.RawQuery == want.RawQuery && actual.Fragment == "" && actual.Opaque == ""
}
func allowedURL(value *url.URL) bool {
	return value != nil && value.Scheme == "https" && value.Host == "kinepolis.fr" && value.User == nil
}
func checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) > 3 {
		return errRedirectLimit
	}
	if request == nil || !allowedURL(request.URL) {
		return errRedirectHost
	}
	return nil
}
func (c *Client) availableFrom(start int) int {
	for offset := 0; offset < len(c.clients); offset++ {
		index := (start + offset) % len(c.clients)
		if !c.unavailable[index] {
			return index
		}
	}
	return -1
}
func (c *Client) pace(ctx context.Context) error {
	if c.lastStart.IsZero() {
		return nil
	}
	wait := c.config.RequestInterval - time.Since(c.lastStart)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
