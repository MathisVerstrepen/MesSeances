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

	"messeances/api/internal/syncproxy"
)

const maxResponseBytes = 8 << 20
const maxRequestAttempts = 4
const maxRequestsPerProxy = 2

var (
	errRedirectHost  = errors.New("redirect host rejected")
	errRedirectLimit = errors.New("redirect limit exceeded")
	errResponseLarge = errors.New("response too large")
)

type Operation string

const (
	OperationUnknown  Operation = "unknown"
	OperationSitemap  Operation = "sitemap"
	OperationCinema   Operation = "cinema"
	OperationShowings Operation = "showings"
)

type ErrorCategory string

const (
	CategoryCanceled             ErrorCategory = "canceled"
	CategoryInvalidURL           ErrorCategory = "invalid URL"
	CategoryTransportUnavailable ErrorCategory = "transport unavailable"
	CategoryTransport            ErrorCategory = "transport"
	CategoryRedirect             ErrorCategory = "redirect"
	CategoryResponseRead         ErrorCategory = "response unreadable"
	CategoryResponseLarge        ErrorCategory = "response too large"
	CategoryChallenge            ErrorCategory = "challenge"
	CategoryHTTPStatus           ErrorCategory = "HTTP status"
	CategoryInvalidPayload       ErrorCategory = "invalid payload"
)

type RequestError struct {
	Operation    Operation
	Category     ErrorCategory
	StatusCode   int
	Attempt      int
	AttemptLimit int
	cause        error
}

func (e *RequestError) Error() string { return "UGC request failed" }
func (e *RequestError) Unwrap() error { return e.cause }

func requestError(operation Operation, category ErrorCategory, status, attempt int, cause error) *RequestError {
	if operation != OperationSitemap && operation != OperationCinema && operation != OperationShowings {
		operation = OperationUnknown
	}
	if category != CategoryHTTPStatus || status < 100 || status > 599 {
		status = 0
	}
	limit := 0
	if attempt > 0 && attempt <= maxRequestAttempts {
		limit = maxRequestAttempts
	} else {
		attempt = 0
	}
	return &RequestError{Operation: operation, Category: category, StatusCode: status, Attempt: attempt, AttemptLimit: limit, cause: cause}
}

type ClientConfig struct {
	Proxies []Proxy
	Timeout time.Duration
}
type FetchResult struct {
	Body     []byte
	FinalURL string
}

type Client struct {
	mu              sync.Mutex
	clients         []*http.Client
	unavailable     []bool
	inFlight        []int
	capacityChanged chan struct{}
	next            int
	requestCount    int
	sleep           func(context.Context, time.Duration) error
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
		clients:         clients,
		unavailable:     make([]bool, len(config.Proxies)),
		inFlight:        make([]int, len(config.Proxies)),
		capacityChanged: make(chan struct{}),
		sleep:           sleepContext,
	}
	return client, nil
}

func (c *Client) RequestCount() int { c.mu.Lock(); defer c.mu.Unlock(); return c.requestCount }

func (c *Client) Get(ctx context.Context, operation Operation, rawURL string) (FetchResult, error) {
	if operation != OperationSitemap && operation != OperationCinema && operation != OperationShowings {
		return FetchResult{}, requestError(OperationUnknown, CategoryInvalidURL, 0, 0, nil)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !isAllowedUGCURL(parsed) {
		return FetchResult{}, requestError(operation, CategoryInvalidURL, 0, 0, err)
	}
	attempted := make([]bool, len(c.clients))
	start := -1
	for attempt := 0; attempt < maxRequestAttempts; attempt++ {
		ordinal, acquireErr := c.acquire(ctx, start, attempted)
		if acquireErr != nil {
			if ctx.Err() != nil {
				return FetchResult{}, requestError(operation, CategoryCanceled, 0, attempt, ctx.Err())
			}
			if attempt > 0 {
				return FetchResult{}, requestError(operation, CategoryTransport, 0, attempt, acquireErr)
			}
			return FetchResult{}, requestError(operation, CategoryTransportUnavailable, 0, 0, acquireErr)
		}
		attempted[ordinal] = true
		start = (ordinal + 1) % len(c.clients)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			c.release(ordinal, false)
			return FetchResult{}, requestError(operation, CategoryInvalidURL, 0, attempt+1, err)
		}
		req.Header.Set("User-Agent", "MesSeances-schedule-sync/1.0")
		if operation == OperationSitemap {
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
				c.release(ordinal, false)
				return FetchResult{}, requestError(operation, CategoryCanceled, 0, attempt+1, ctx.Err())
			}
			if errors.Is(requestErr, errRedirectHost) || errors.Is(requestErr, errRedirectLimit) {
				c.release(ordinal, false)
				return FetchResult{}, requestError(operation, CategoryRedirect, 0, attempt+1, requestErr)
			}
			c.release(ordinal, true)
			if retryErr := c.waitToRetry(ctx, attempted, attempt); retryErr == nil {
				continue
			}
			if ctx.Err() != nil {
				return FetchResult{}, requestError(operation, CategoryCanceled, 0, attempt+1, ctx.Err())
			}
			return FetchResult{}, requestError(operation, CategoryTransport, 0, attempt+1, requestErr)
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			if response.Body != nil {
				response.Body.Close()
			}
			c.release(ordinal, false)
			return FetchResult{}, requestError(operation, CategoryHTTPStatus, response.StatusCode, attempt+1, nil)
		}
		if response.Body == nil {
			c.release(ordinal, false)
			return FetchResult{}, requestError(operation, CategoryResponseRead, 0, attempt+1, nil)
		}
		body, readErr := readBounded(response.Body, response.ContentLength)
		response.Body.Close()
		if readErr != nil {
			c.release(ordinal, false)
			category := CategoryResponseRead
			if errors.Is(readErr, errResponseLarge) {
				category = CategoryResponseLarge
			}
			return FetchResult{}, requestError(operation, category, 0, attempt+1, readErr)
		}
		if isChallenge(body) {
			c.release(ordinal, false)
			return FetchResult{}, requestError(operation, CategoryChallenge, 0, attempt+1, nil)
		}
		if response.StatusCode >= 500 {
			c.release(ordinal, true)
			if retryErr := c.waitToRetry(ctx, attempted, attempt); retryErr == nil {
				continue
			}
			if ctx.Err() != nil {
				return FetchResult{}, requestError(operation, CategoryCanceled, 0, attempt+1, ctx.Err())
			}
			return FetchResult{}, requestError(operation, CategoryHTTPStatus, response.StatusCode, attempt+1, nil)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			c.release(ordinal, false)
			return FetchResult{}, requestError(operation, CategoryHTTPStatus, response.StatusCode, attempt+1, nil)
		}
		finalURL, finalURLErr := sanitizedFinalURL(response.Request)
		if finalURLErr != nil {
			c.release(ordinal, false)
			return FetchResult{}, requestError(operation, CategoryRedirect, 0, attempt+1, finalURLErr)
		}
		c.release(ordinal, false)
		return FetchResult{Body: body, FinalURL: finalURL}, nil
	}
	return FetchResult{}, requestError(operation, CategoryTransport, 0, maxRequestAttempts, nil)
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
	firstAttempt := start < 0
	for {
		c.mu.Lock()
		c.ensureCapacityStateLocked()
		if err := ctx.Err(); err != nil {
			c.mu.Unlock()
			return -1, err
		}
		scanStart := start
		if firstAttempt {
			scanStart = c.next
		}
		hasCandidate := false
		for offset := 0; offset < len(c.clients); offset++ {
			index := (scanStart + offset) % len(c.clients)
			if c.unavailable[index] || attempted[index] {
				continue
			}
			hasCandidate = true
			if c.inFlight[index] < maxRequestsPerProxy {
				c.inFlight[index]++
				if firstAttempt {
					c.next = (index + 1) % len(c.clients)
				}
				c.mu.Unlock()
				return index, nil
			}
		}
		if !hasCandidate {
			c.mu.Unlock()
			return -1, errors.New("no distinct proxy available")
		}
		changed := c.capacityChanged
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-changed:
		}
	}
}

func (c *Client) ensureCapacityStateLocked() {
	if len(c.inFlight) != len(c.clients) {
		c.inFlight = make([]int, len(c.clients))
	}
	if c.capacityChanged == nil {
		c.capacityChanged = make(chan struct{})
	}
}

func (c *Client) release(ordinal int, unavailable bool) {
	c.mu.Lock()
	c.ensureCapacityStateLocked()
	if unavailable {
		c.unavailable[ordinal] = true
	}
	c.inFlight[ordinal]--
	close(c.capacityChanged)
	c.capacityChanged = make(chan struct{})
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
			return nil, errResponseLarge
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
				return nil, errResponseLarge
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
