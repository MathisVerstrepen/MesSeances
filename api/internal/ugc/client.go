package ugc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"movieflow/api/internal/syncproxy"
)

const maxResponseBytes = 8 << 20
const maxRequestAttempts = 4

var (
	errRedirectHost  = errors.New("redirect host rejected")
	errRedirectLimit = errors.New("redirect limit exceeded")
)

type ClientConfig struct {
	Proxies []Proxy
	Timeout time.Duration
}
type TerminalError struct{ category string }

func (e *TerminalError) Error() string { return "UGC request stopped: " + e.category }

type FetchResult struct {
	Body     []byte
	FinalURL string
}

type Client struct {
	mu           sync.Mutex
	clients      []*http.Client
	unavailable  []bool
	leased       []bool
	leaseChanged chan struct{}
	next         int
	requestCount int
	sleep        func(context.Context, time.Duration) error
}

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 {
		return nil, fmt.Errorf("at least one proxy is required")
	}
	if config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("timeout must be between 5s and 60s")
	}
	clients, err := syncproxy.NewHTTPClients(config.Proxies, config.Timeout, checkUGCRedirect)
	if err != nil {
		return nil, err
	}
	client := &Client{
		clients:      clients,
		unavailable:  make([]bool, len(config.Proxies)),
		leased:       make([]bool, len(config.Proxies)),
		leaseChanged: make(chan struct{}),
		sleep:        sleepContext,
	}
	return client, nil
}

func (c *Client) RequestCount() int { c.mu.Lock(); defer c.mu.Unlock(); return c.requestCount }

func (c *Client) Get(ctx context.Context, kind, rawURL string) (FetchResult, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !isAllowedUGCURL(parsed) {
		return FetchResult{}, fmt.Errorf("%s request rejected: invalid public URL", kind)
	}
	attempted := make([]bool, len(c.clients))
	start := -1
	lastOrdinal := -1
	for attempt := 0; attempt < maxRequestAttempts; attempt++ {
		ordinal, acquireErr := c.acquire(ctx, start, attempted)
		if acquireErr != nil {
			if ctx.Err() != nil {
				return FetchResult{}, fmt.Errorf("%s request canceled", kind)
			}
			if lastOrdinal >= 0 {
				return FetchResult{}, fmt.Errorf("%s request failed after proxy %d", kind, lastOrdinal+1)
			}
			return FetchResult{}, fmt.Errorf("%s request failed: no proxy available", kind)
		}
		attempted[ordinal] = true
		lastOrdinal = ordinal
		start = (ordinal + 1) % len(c.clients)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			c.release(ordinal, false, false)
			return FetchResult{}, fmt.Errorf("%s request rejected", kind)
		}
		req.Header.Set("User-Agent", "MovieFlow-schedule-sync/1.0")
		if kind == "sitemap" {
			req.Header.Set("Accept", "application/xml,text/xml;q=0.9")
		} else {
			req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9")
		}
		c.mu.Lock()
		c.requestCount++
		c.mu.Unlock()
		response, requestErr := c.clients[ordinal].Do(req)
		if requestErr != nil {
			if ctx.Err() != nil {
				c.release(ordinal, false, false)
				return FetchResult{}, fmt.Errorf("%s request canceled", kind)
			}
			if errors.Is(requestErr, errRedirectHost) || errors.Is(requestErr, errRedirectLimit) {
				c.release(ordinal, false, false)
				return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: redirect rejected", kind, ordinal+1)
			}
			c.release(ordinal, true, false)
			if retryErr := c.waitToRetry(ctx, attempted, attempt); retryErr == nil {
				continue
			}
			if ctx.Err() != nil {
				return FetchResult{}, fmt.Errorf("%s request canceled", kind)
			}
			return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: transport", kind, ordinal+1)
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			response.Body.Close()
			c.release(ordinal, false, false)
			return FetchResult{}, &TerminalError{category: fmt.Sprintf("HTTP %d for %s via proxy %d", response.StatusCode, kind, ordinal+1)}
		}
		body, readErr := readBounded(response.Body, response.ContentLength)
		response.Body.Close()
		if readErr != nil {
			c.release(ordinal, false, false)
			return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: response too large or unreadable", kind, ordinal+1)
		}
		if isChallenge(body) {
			c.release(ordinal, false, false)
			return FetchResult{}, &TerminalError{category: fmt.Sprintf("challenge response for %s via proxy %d", kind, ordinal+1)}
		}
		if response.StatusCode >= 500 {
			c.release(ordinal, true, false)
			if retryErr := c.waitToRetry(ctx, attempted, attempt); retryErr == nil {
				continue
			}
			if ctx.Err() != nil {
				return FetchResult{}, fmt.Errorf("%s request canceled", kind)
			}
			return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: server error", kind, ordinal+1)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			c.release(ordinal, false, false)
			return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: HTTP %d", kind, ordinal+1, response.StatusCode)
		}
		finalURL, finalURLErr := sanitizedFinalURL(response.Request)
		if finalURLErr != nil {
			c.release(ordinal, false, false)
			return FetchResult{}, fmt.Errorf("%s request failed on proxy %d: invalid final response URL", kind, ordinal+1)
		}
		c.release(ordinal, false, true)
		return FetchResult{Body: body, FinalURL: finalURL}, nil
	}
	return FetchResult{}, fmt.Errorf("%s request failed", kind)
}

func sanitizedFinalURL(request *http.Request) (string, error) {
	if request == nil || !isAllowedUGCURL(request.URL) || request.URL.Fragment != "" || request.URL.Opaque != "" {
		return "", fmt.Errorf("invalid final URL")
	}
	final := *request.URL
	final.Scheme = "https"
	final.Host = "www.ugc.fr"
	final.User = nil
	return final.String(), nil
}

func isAllowedUGCURL(parsed *url.URL) bool {
	return parsed != nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Host, "www.ugc.fr") && parsed.User == nil
}

func checkUGCRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > 3 {
		return errRedirectLimit
	}
	if req == nil || !isAllowedUGCURL(req.URL) {
		return errRedirectHost
	}
	return nil
}

func (c *Client) acquire(ctx context.Context, start int, attempted []bool) (int, error) {
	for {
		c.mu.Lock()
		c.ensureLeaseStateLocked()
		if start < 0 {
			start = c.next
		}
		hasCandidate := false
		for offset := 0; offset < len(c.clients); offset++ {
			index := (start + offset) % len(c.clients)
			if c.unavailable[index] || attempted[index] {
				continue
			}
			hasCandidate = true
			if !c.leased[index] {
				c.leased[index] = true
				c.mu.Unlock()
				return index, nil
			}
		}
		if !hasCandidate {
			c.mu.Unlock()
			return -1, errors.New("no distinct proxy available")
		}
		changed := c.leaseChanged
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-changed:
		}
	}
}

func (c *Client) ensureLeaseStateLocked() {
	if len(c.leased) != len(c.clients) {
		c.leased = make([]bool, len(c.clients))
	}
	if c.leaseChanged == nil {
		c.leaseChanged = make(chan struct{})
	}
}

func (c *Client) release(ordinal int, unavailable, success bool) {
	c.mu.Lock()
	c.ensureLeaseStateLocked()
	if unavailable {
		c.unavailable[ordinal] = true
	}
	if success {
		c.next = (ordinal + 1) % len(c.clients)
	}
	c.leased[ordinal] = false
	close(c.leaseChanged)
	c.leaseChanged = make(chan struct{})
	c.mu.Unlock()
}

func (c *Client) waitToRetry(ctx context.Context, attempted []bool, attempt int) error {
	if attempt >= maxRequestAttempts-1 || !c.hasDistinctCandidate(attempted) {
		return errors.New("retry exhausted")
	}
	delay := 500 * time.Millisecond << attempt
	if c.sleep == nil {
		return sleepContext(ctx, delay)
	}
	return c.sleep(ctx, delay)
}

func (c *Client) hasDistinctCandidate(attempted []bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index := range c.clients {
		if !c.unavailable[index] && !attempted[index] {
			return true
		}
	}
	return false
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func readBounded(r io.Reader, contentLength int64) ([]byte, error) {
	const readBudget = maxResponseBytes + 1
	capacity := 0
	if contentLength > 0 {
		if contentLength >= int64(readBudget) {
			capacity = readBudget
		} else {
			capacity = int(contentLength) + 1
		}
	}
	body := make([]byte, 0, capacity)
	for {
		if len(body) > maxResponseBytes {
			return nil, fmt.Errorf("too large")
		}
		if len(body) == cap(body) {
			nextCapacity := max(512, cap(body)*2)
			if nextCapacity > readBudget {
				nextCapacity = readBudget
			}
			grown := make([]byte, len(body), nextCapacity)
			copy(grown, body)
			body = grown
		}
		n, err := r.Read(body[len(body):cap(body)])
		body = body[:len(body)+n]
		if err == io.EOF {
			if len(body) > maxResponseBytes {
				return nil, fmt.Errorf("too large")
			}
			return body, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
func isChallenge(body []byte) bool {
	return syncproxy.IsChallenge(body)
}
