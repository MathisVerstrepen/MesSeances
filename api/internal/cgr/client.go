package cgr

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
	APIBaseURL         = "https://www.cgrcinemas.fr"
	CinemasURL         = APIBaseURL + "/page-data/sq/d/2506275789.json"
	MaxResponseBytes   = 16 << 20
	MaxRequestURLBytes = 8 << 10
	maxRequestAttempts = 4

	chromeUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type Operation string

const (
	OperationCinemas  Operation = "cinemas"
	OperationProgram  Operation = "scheduled movies"
	OperationSchedule Operation = "schedule"
	OperationMovies   Operation = "movies"
)

type ErrorCategory string

const (
	CategoryCanceled      ErrorCategory = "canceled"
	CategoryInvalidURL    ErrorCategory = "invalid URL"
	CategoryNoClient      ErrorCategory = "no HTTP client available"
	CategoryTransport     ErrorCategory = "transport"
	CategoryRedirect      ErrorCategory = "redirect rejected"
	CategoryResponseRead  ErrorCategory = "response unreadable"
	CategoryResponseLarge ErrorCategory = "response too large"
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
		return fmt.Sprintf("CGR %s request failed: HTTP %d", e.Operation, e.StatusCode)
	}
	return fmt.Sprintf("CGR %s request failed: %s", e.Operation, e.Category)
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
	proxyBacked bool
	next        int
	requests    atomic.Int64
	sleep       func(context.Context, time.Duration) error
}

func NewClient(config ClientConfig) (*Client, error) {
	if config.Timeout < 5*time.Second || config.Timeout > 60*time.Second {
		return nil, fmt.Errorf("timeout must be between 5s and 60s")
	}
	var clients []*http.Client
	if len(config.Proxies) == 0 {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		clients = []*http.Client{{Transport: transport, Timeout: config.Timeout, CheckRedirect: checkRedirect}}
	} else {
		var err error
		clients, err = syncproxy.NewFingerprintHTTP2Clients(config.Proxies, config.Timeout, checkRedirect)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy transport")
		}
	}
	return &Client{clients: clients, unavailable: make([]bool, len(clients)), proxyBacked: len(config.Proxies) > 0, sleep: sleepContext}, nil
}

func (c *Client) RequestCount() int { return int(c.requests.Load()) }

func (c *Client) Get(ctx context.Context, operation Operation, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !operationMatchesURL(operation, parsed) {
		return nil, requestError(operation, CategoryInvalidURL, 0, nil)
	}
	if len(c.clients) == 0 {
		return nil, requestError(operation, CategoryNoClient, 0, nil)
	}
	attempted := make([]bool, len(c.clients))
	var last *RequestError
	for attempt := 0; attempt < maxRequestAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, requestError(operation, CategoryCanceled, 0, err)
		}
		ordinal := c.acquire(attempted)
		if ordinal < 0 {
			if last != nil {
				return nil, last
			}
			return nil, requestError(operation, CategoryNoClient, 0, nil)
		}
		attempted[ordinal] = true
		body, retry, requestErr := c.attempt(ctx, ordinal, operation, parsed)
		if requestErr == nil {
			return body, nil
		}
		last = requestErr
		if !retry || attempt == maxRequestAttempts-1 {
			return nil, requestErr
		}
		c.disable(ordinal)
		if !c.hasCandidate(attempted) {
			return nil, requestErr
		}
		if err := c.sleep(ctx, 500*time.Millisecond<<attempt); err != nil {
			return nil, requestError(operation, CategoryCanceled, 0, err)
		}
	}
	return nil, last
}

func (c *Client) attempt(ctx context.Context, ordinal int, operation Operation, parsed *url.URL) ([]byte, bool, *RequestError) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, false, requestError(operation, CategoryInvalidURL, 0, nil)
	}
	setBrowserHeaders(req)
	c.requests.Add(1)
	response, err := c.clients[ordinal].Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, requestError(operation, CategoryCanceled, 0, ctx.Err())
		}
		if errors.Is(err, errRedirectAuthority) || errors.Is(err, errRedirectLimit) {
			return nil, false, requestError(operation, CategoryRedirect, 0, nil)
		}
		return nil, true, requestError(operation, CategoryTransport, 0, nil)
	}
	if response.Body == nil {
		return nil, true, requestError(operation, CategoryResponseRead, 0, nil)
	}
	body, readErr := readResponse(response.Body, response.ContentLength)
	_ = response.Body.Close()
	if readErr != nil {
		if errors.Is(readErr, errResponseTooLarge) {
			return nil, false, requestError(operation, CategoryResponseLarge, 0, nil)
		}
		return nil, true, requestError(operation, CategoryResponseRead, 0, nil)
	}
	if response.StatusCode >= 500 {
		return nil, true, requestError(operation, CategoryServer, response.StatusCode, nil)
	}
	if (operation == OperationCinemas || operation == OperationProgram) && response.StatusCode == http.StatusNotFound {
		return nil, true, requestError(operation, CategoryStatus, response.StatusCode, nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, requestError(operation, CategoryStatus, response.StatusCode, nil)
	}
	if response.Request == nil || response.Request.URL.String() != parsed.String() || !allowedURL(response.Request.URL) {
		return nil, false, requestError(operation, CategoryRedirect, 0, nil)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" && !strings.HasSuffix(strings.ToLower(mediaType), "+json") {
		return nil, false, requestError(operation, CategoryContentType, 0, nil)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, false, requestError(operation, CategoryEmptyResponse, 0, nil)
	}
	if !json.Valid(body) {
		return nil, false, requestError(operation, CategoryInvalidJSON, 0, nil)
	}
	return body, false, nil
}

func requestError(operation Operation, category ErrorCategory, status int, cause error) *RequestError {
	if operation != OperationCinemas && operation != OperationProgram && operation != OperationSchedule && operation != OperationMovies {
		operation = "unknown"
	}
	return &RequestError{Operation: operation, Category: category, StatusCode: status, cause: cause}
}

func operationMatchesURL(operation Operation, parsed *url.URL) bool {
	if !allowedURL(parsed) || len(parsed.String()) > MaxRequestURLBytes {
		return false
	}
	if operation == OperationCinemas {
		return parsed.String() == CinemasURL
	}
	if parsed.Host != "www.cgrcinemas.fr" || !strings.HasPrefix(parsed.Path, "/api/gatsby-source-boxofficeapi/") {
		return false
	}
	query := parsed.Query()
	switch operation {
	case OperationProgram:
		return parsed.Path == "/api/gatsby-source-boxofficeapi/scheduledMovies" && len(query) == 1 && len(query["theaterId"]) == 1 && validTheaterID(query.Get("theaterId"))
	case OperationSchedule:
		if parsed.Path != "/api/gatsby-source-boxofficeapi/schedule" || len(query) != 3 || len(query["from"]) != 1 || len(query["to"]) != 1 || !validScheduleWindow(query.Get("from"), query.Get("to")) {
			return false
		}
		seen := make(map[string]bool, len(query["theaters"]))
		if len(query["theaters"]) == 0 {
			return false
		}
		for _, raw := range query["theaters"] {
			var theater struct {
				ID       string `json:"id"`
				TimeZone string `json:"timeZone"`
			}
			if json.Unmarshal([]byte(raw), &theater) != nil || !validTheaterID(theater.ID) || theater.TimeZone != "Europe/Paris" || seen[theater.ID] {
				return false
			}
			compact, _ := json.Marshal(theater)
			if raw != string(compact) {
				return false
			}
			seen[theater.ID] = true
		}
		return true
	case OperationMovies:
		if parsed.Path != "/api/gatsby-source-boxofficeapi/movies" || len(query) != 3 || len(query["basic"]) != 1 || query.Get("basic") != "false" || len(query["castingLimit"]) != 1 || query.Get("castingLimit") != "3" {
			return false
		}
		ids := query["ids"]
		if len(ids) == 0 || len(ids) > MovieBatchSize {
			return false
		}
		seen := make(map[string]bool, len(ids))
		for _, id := range ids {
			if !validMovieID(id) || seen[id] {
				return false
			}
			seen[id] = true
		}
		return true
	default:
		return false
	}
}

func allowedURL(parsed *url.URL) bool {
	if parsed == nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.ForceQuery || strings.Contains(parsed.Path, `\`) || hasTraversal(parsed.Path) {
		return false
	}
	return parsed.Host == "www.cgrcinemas.fr"
}

func setBrowserHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", APIBaseURL+"/")
	req.Header.Set("User-Agent", chromeUserAgent)
	req.Header.Set("Sec-CH-UA", `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"Linux"`)
}

func checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) > 3 {
		return errRedirectLimit
	}
	if request == nil || !allowedURL(request.URL) {
		return errRedirectAuthority
	}
	return nil
}

func hasTraversal(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return true
		}
	}
	return false
}

func readResponse(reader io.Reader, contentLength int64) ([]byte, error) {
	if contentLength > MaxResponseBytes {
		return nil, errResponseTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(reader, MaxResponseBytes+1))
	if len(body) > MaxResponseBytes {
		return nil, errResponseTooLarge
	}
	return body, err
}

func (c *Client) acquire(attempted []bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	for offset := range len(c.clients) {
		ordinal := (c.next + offset) % len(c.clients)
		if c.proxyBacked && attempted[ordinal] || c.unavailable[ordinal] {
			continue
		}
		c.next = (ordinal + 1) % len(c.clients)
		return ordinal
	}
	return -1
}

func (c *Client) disable(ordinal int) {
	if !c.proxyBacked {
		return
	}
	c.mu.Lock()
	c.unavailable[ordinal] = true
	c.mu.Unlock()
}

func (c *Client) hasCandidate(attempted []bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.proxyBacked {
		return len(c.clients) > 0
	}
	for ordinal := range c.clients {
		if !attempted[ordinal] && !c.unavailable[ordinal] {
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
