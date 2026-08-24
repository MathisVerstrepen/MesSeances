package pathe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"messeances/api/internal/syncproxy"
)

const (
	APIBaseURL         = "https://www.pathe.fr"
	CinemasURL         = APIBaseURL + "/api/cinemas"
	ShowsURL           = APIBaseURL + "/api/shows"
	MaxResponseBytes   = 8 << 20
	maxRequestAttempts = 4

	chromeUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type Operation string

const (
	OperationCinemas       Operation = "cinemas"
	OperationShows         Operation = "shows"
	OperationCinemaProgram Operation = "cinema program"
	OperationMovieTimes    Operation = "movie showtimes"
	OperationEventTimes    Operation = "event showtimes"
)

type ErrorCategory string

const (
	CategoryCanceled      ErrorCategory = "canceled"
	CategoryInvalidURL    ErrorCategory = "invalid URL"
	CategoryNoProxy       ErrorCategory = "no proxy available"
	CategoryTransport     ErrorCategory = "transport"
	CategoryRedirect      ErrorCategory = "redirect rejected"
	CategoryResponseRead  ErrorCategory = "response unreadable"
	CategoryResponseLarge ErrorCategory = "response too large"
	CategoryChallenge     ErrorCategory = "challenge response"
	CategoryServer        ErrorCategory = "server error"
	CategoryStatus        ErrorCategory = "HTTP status"
	CategoryContentType   ErrorCategory = "non-JSON content"
	CategoryInvalidJSON   ErrorCategory = "invalid JSON"
	CategoryEmptyResponse ErrorCategory = "empty response"
)

type RequestError struct {
	Operation  Operation
	Category   ErrorCategory
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("Pathé %s request failed: HTTP %d", e.Operation, e.StatusCode)
	}
	return fmt.Sprintf("Pathé %s request failed: %s", e.Operation, e.Category)
}

func (e *RequestError) Unwrap() error { return e.cause }

var (
	errRedirectAuthority = errors.New("redirect authority rejected")
	errRedirectLimit     = errors.New("redirect limit exceeded")
	errResponseTooLarge  = errors.New("response too large")
)

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}

type Client struct {
	mu          sync.Mutex
	clients     []*http.Client
	unavailable []bool
	next        int
	requests    atomic.Int64
	sleep       func(context.Context, time.Duration) error
}

func NewClient(config ClientConfig) (*Client, error) {
	if len(config.Proxies) == 0 {
		return nil, fmt.Errorf("at least one proxy is required")
	}
	if config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("timeout must be between 5s and 60s")
	}
	clients, err := syncproxy.NewFingerprintHTTP2Clients(config.Proxies, config.Timeout, checkRedirect)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy transport")
	}
	return &Client{
		clients:     clients,
		unavailable: make([]bool, len(clients)),
		sleep:       sleepContext,
	}, nil
}

func (c *Client) RequestCount() int { return int(c.requests.Load()) }

func (c *Client) Get(ctx context.Context, operation Operation, rawURL string) ([]byte, error) {
	if !validOperation(operation) {
		return nil, requestError(operation, CategoryInvalidURL, 0, nil)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !allowedAPIURL(parsed) || !operationMatchesURL(operation, parsed) {
		return nil, requestError(operation, CategoryInvalidURL, 0, nil)
	}

	attempted := make([]bool, len(c.clients))
	start := -1
	lastCategory := CategoryNoProxy
	for attempt := 0; attempt < maxRequestAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, requestError(operation, CategoryCanceled, 0, err)
		}
		ordinal := c.acquire(start, attempted, attempt == 0)
		if ordinal < 0 {
			return nil, requestError(operation, lastCategory, 0, nil)
		}
		attempted[ordinal] = true
		start = (ordinal + 1) % len(c.clients)

		body, retry, category, status, fetchErr := c.attempt(ctx, ordinal, operation, parsed)
		if fetchErr == nil {
			return body, nil
		}
		if !retry {
			return nil, fetchErr
		}
		lastCategory = category
		c.disable(ordinal)
		if attempt == maxRequestAttempts-1 || !c.hasCandidate(attempted) {
			return nil, requestError(operation, category, status, nil)
		}
		if err := c.wait(ctx, 500*time.Millisecond<<attempt); err != nil {
			return nil, requestError(operation, CategoryCanceled, 0, err)
		}
	}
	return nil, requestError(operation, lastCategory, 0, nil)
}

func (c *Client) attempt(ctx context.Context, ordinal int, operation Operation, parsed *url.URL) ([]byte, bool, ErrorCategory, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, false, CategoryInvalidURL, 0, requestError(operation, CategoryInvalidURL, 0, nil)
	}
	setBrowserHeaders(req)
	c.requests.Add(1)
	response, err := c.clients[ordinal].Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, CategoryCanceled, 0, requestError(operation, CategoryCanceled, 0, ctx.Err())
		}
		if errors.Is(err, errRedirectAuthority) || errors.Is(err, errRedirectLimit) {
			return nil, false, CategoryRedirect, 0, requestError(operation, CategoryRedirect, 0, nil)
		}
		return nil, true, CategoryTransport, 0, requestError(operation, CategoryTransport, 0, nil)
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, false, CategoryStatus, response.StatusCode, requestError(operation, CategoryStatus, response.StatusCode, nil)
	}
	if response.Body == nil {
		return nil, true, CategoryResponseRead, 0, requestError(operation, CategoryResponseRead, 0, nil)
	}
	body, readErr := readResponse(response.Body, response.ContentLength)
	_ = response.Body.Close()
	if readErr != nil {
		if errors.Is(readErr, errResponseTooLarge) {
			return nil, false, CategoryResponseLarge, 0, requestError(operation, CategoryResponseLarge, 0, nil)
		}
		if ctx.Err() != nil {
			return nil, false, CategoryCanceled, 0, requestError(operation, CategoryCanceled, 0, ctx.Err())
		}
		return nil, true, CategoryResponseRead, 0, requestError(operation, CategoryResponseRead, 0, nil)
	}
	if syncproxy.IsChallenge(body) {
		return nil, false, CategoryChallenge, 0, requestError(operation, CategoryChallenge, 0, nil)
	}
	if response.StatusCode >= 500 {
		return nil, true, CategoryServer, response.StatusCode, requestError(operation, CategoryServer, response.StatusCode, nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, CategoryStatus, response.StatusCode, requestError(operation, CategoryStatus, response.StatusCode, nil)
	}
	if !sameFinalURL(response.Request, parsed) {
		return nil, false, CategoryRedirect, 0, requestError(operation, CategoryRedirect, 0, nil)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !isJSONMediaType(mediaType) {
		return nil, false, CategoryContentType, 0, requestError(operation, CategoryContentType, 0, nil)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, false, CategoryEmptyResponse, 0, requestError(operation, CategoryEmptyResponse, 0, nil)
	}
	if !json.Valid(body) {
		return nil, false, CategoryInvalidJSON, 0, requestError(operation, CategoryInvalidJSON, 0, nil)
	}
	return body, false, "", 0, nil
}

func requestError(operation Operation, category ErrorCategory, status int, cause error) error {
	if !validOperation(operation) {
		operation = "unknown"
	}
	return &RequestError{Operation: operation, Category: category, StatusCode: status, cause: cause}
}

func validOperation(operation Operation) bool {
	switch operation {
	case OperationCinemas, OperationShows, OperationCinemaProgram, OperationMovieTimes, OperationEventTimes:
		return true
	default:
		return false
	}
}

func setBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", chromeUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Sec-CH-UA", `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"Linux"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
}

func allowedAPIURL(parsed *url.URL) bool {
	if parsed == nil || parsed.Scheme != "https" || parsed.Host != "www.pathe.fr" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.ForceQuery || strings.Contains(parsed.Path, `\`) || hasTraversal(parsed.Path) {
		return false
	}
	parts := strings.Split(parsed.Path, "/")
	if len(parts) == 3 && parts[0] == "" && parts[1] == "api" {
		return parts[2] == "cinemas" || parts[2] == "shows"
	}
	if len(parts) == 5 && parts[0] == "" && parts[1] == "api" && parts[2] == "cinema" {
		return validSourceSlug(parts[3], maxCinemaSlugLength) && parts[4] == "shows"
	}
	if len(parts) != 6 && len(parts) != 7 || parts[0] != "" || parts[1] != "api" || parts[2] != "show" || !validSourceSlug(parts[3], maxMovieSlugLength) || parts[4] != "showtimes" || !validSourceSlug(parts[5], maxCinemaSlugLength) {
		return false
	}
	if len(parts) == 6 {
		return true
	}
	date, err := time.Parse("2006-01-02", parts[6])
	return err == nil && date.Format("2006-01-02") == parts[6]
}

func operationMatchesURL(operation Operation, parsed *url.URL) bool {
	parts := strings.Split(parsed.Path, "/")
	switch operation {
	case OperationCinemas:
		return parsed.Path == "/api/cinemas"
	case OperationShows:
		return parsed.Path == "/api/shows"
	case OperationCinemaProgram:
		return len(parts) == 5 && parts[2] == "cinema"
	case OperationMovieTimes:
		return len(parts) == 6
	case OperationEventTimes:
		return len(parts) == 7
	default:
		return false
	}
}

func sameFinalURL(request *http.Request, expected *url.URL) bool {
	return request != nil && allowedAPIURL(request.URL) && request.URL.String() == expected.String()
}

func checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) > 3 {
		return errRedirectLimit
	}
	if request == nil || !allowedAPIURL(request.URL) {
		return errRedirectAuthority
	}
	return nil
}

func hasTraversal(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func isJSONMediaType(value string) bool {
	value = strings.ToLower(value)
	return value == "application/json" || strings.HasPrefix(value, "application/") && strings.HasSuffix(value, "+json")
}

func readResponse(reader io.Reader, contentLength int64) ([]byte, error) {
	if contentLength > MaxResponseBytes {
		return nil, errResponseTooLarge
	}
	limited := io.LimitReader(reader, MaxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if len(body) > MaxResponseBytes {
		return nil, errResponseTooLarge
	}
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) acquire(start int, attempted []bool, initial bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if initial {
		start = c.next
	}
	if start < 0 {
		start = 0
	}
	for offset := 0; offset < len(c.clients); offset++ {
		index := (start + offset) % len(c.clients)
		if c.unavailable[index] || attempted[index] {
			continue
		}
		if initial {
			c.next = (index + 1) % len(c.clients)
		}
		return index
	}
	return -1
}

func (c *Client) disable(ordinal int) {
	c.mu.Lock()
	c.unavailable[ordinal] = true
	c.mu.Unlock()
}

func (c *Client) hasCandidate(attempted []bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index := range c.clients {
		if !c.unavailable[index] && !attempted[index] {
			return true
		}
	}
	return false
}

func (c *Client) wait(ctx context.Context, delay time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, delay)
	}
	return sleepContext(ctx, delay)
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
