package kinepolis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"messeances/api/internal/syncproxy"
)

const (
	ScheduleURL = "https://kinepolis.fr/?main_section=tous+les+films"
	MaxBodySize = 32 << 20
	userAgent   = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	accept      = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"
	acceptLang  = "fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7"
)

var (
	errRedirectHost     = errors.New("redirect host rejected")
	errRedirectLimit    = errors.New("redirect limit exceeded")
	errResponseTooLarge = errors.New("response too large")
	cinemaPathPattern   = regexp.MustCompile(`^/(?:cinemas/[a-z0-9]+(?:-[a-z0-9]+)*/(?:info|infos)/|cinémas/[a-z0-9]+(?:-[a-z0-9]+)*/info/)$`)
)

type Operation string

const (
	OperationSchedule Operation = "schedule"
	OperationCinema   Operation = "cinema"
)

type ErrorCategory string

const (
	CategoryCanceled       ErrorCategory = "canceled"
	CategoryInvalidURL     ErrorCategory = "invalid URL"
	CategoryNoProxy        ErrorCategory = "no proxy available"
	CategoryTransport      ErrorCategory = "transport"
	CategoryRedirect       ErrorCategory = "redirect rejected"
	CategoryResponseRead   ErrorCategory = "response unreadable"
	CategoryResponseLarge  ErrorCategory = "response too large"
	CategoryChallenge      ErrorCategory = "challenge response"
	CategoryServer         ErrorCategory = "server error"
	CategoryStatus         ErrorCategory = "HTTP status"
	CategoryContentType    ErrorCategory = "unexpected content type"
	CategoryInvalidPayload ErrorCategory = "invalid payload"
	CategoryEmptyResponse  ErrorCategory = "empty response"
)

type RequestError struct {
	Operation  Operation
	Category   ErrorCategory
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	operation := safeErrorOperation(e.Operation)
	category := safeErrorCategory(e.Category)
	if (e.Category == CategoryServer || e.Category == CategoryStatus) && e.StatusCode >= 100 && e.StatusCode <= 599 {
		return fmt.Sprintf("Kinepolis %s request failed: HTTP %d", operation, e.StatusCode)
	}
	return fmt.Sprintf("Kinepolis %s request failed: %s", operation, category)
}

func (e *RequestError) Unwrap() error { return e.cause }

func requestError(operation Operation, category ErrorCategory, status int, cause error) error {
	return &RequestError{Operation: operation, Category: category, StatusCode: status, cause: cause}
}

func safeErrorOperation(operation Operation) string {
	switch operation {
	case OperationSchedule, OperationCinema:
		return string(operation)
	default:
		return "unknown"
	}
}

func safeErrorCategory(category ErrorCategory) string {
	switch category {
	case CategoryCanceled, CategoryInvalidURL, CategoryNoProxy, CategoryTransport, CategoryRedirect,
		CategoryResponseRead, CategoryResponseLarge, CategoryChallenge, CategoryServer, CategoryStatus,
		CategoryContentType, CategoryInvalidPayload, CategoryEmptyResponse:
		return string(category)
	default:
		return "unknown"
	}
}

type ClientConfig struct {
	Proxies         []syncproxy.Proxy
	RequestInterval time.Duration
	Timeout         time.Duration
}

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
	target, _ := url.Parse(ScheduleURL)
	return c.fetch(ctx, OperationSchedule, target, validFinalScheduleRequest)
}

func (c *Client) FetchCinema(ctx context.Context, source string) ([]byte, error) {
	target, err := parseCinemaURLSource(source)
	if err != nil {
		return nil, requestError(OperationCinema, CategoryInvalidURL, 0, err)
	}
	return c.fetch(ctx, OperationCinema, &target.url, func(request *http.Request) bool {
		return validFinalCinemaRequest(request, target)
	})
}

func (c *Client) fetch(ctx context.Context, operation Operation, target *url.URL, validFinal func(*http.Request) bool) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	start := c.next
	if c.availableFrom(start) < 0 {
		return nil, requestError(operation, CategoryNoProxy, 0, nil)
	}
	for {
		ordinal := c.availableFrom(start)
		if ordinal < 0 {
			return nil, requestError(operation, CategoryNoProxy, 0, nil)
		}
		start = (ordinal + 1) % len(c.clients)
		if err := c.pace(ctx); err != nil {
			return nil, requestError(operation, CategoryCanceled, 0, err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return nil, requestError(operation, CategoryInvalidURL, 0, err)
		}
		req.URL.RawPath = ""
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", accept)
		req.Header.Set("Accept-Language", acceptLang)
		c.lastStart = time.Now()
		c.requestCount++
		response, requestErr := c.clients[ordinal].Do(req)
		if requestErr != nil {
			if ctx.Err() != nil {
				return nil, requestError(operation, CategoryCanceled, 0, ctx.Err())
			}
			if errors.Is(requestErr, errRedirectHost) || errors.Is(requestErr, errRedirectLimit) {
				return nil, requestError(operation, CategoryRedirect, 0, requestErr)
			}
			c.unavailable[ordinal] = true
			if c.availableFrom(start) >= 0 {
				continue
			}
			return nil, requestError(operation, CategoryTransport, 0, requestErr)
		}
		body, readErr := readBounded(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			if errors.Is(readErr, errResponseTooLarge) {
				return nil, requestError(operation, CategoryResponseLarge, 0, readErr)
			}
			c.unavailable[ordinal] = true
			if c.availableFrom(start) >= 0 {
				continue
			}
			return nil, requestError(operation, CategoryResponseRead, 0, readErr)
		}
		if syncproxy.IsChallenge(body) {
			return nil, requestError(operation, CategoryChallenge, 0, nil)
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			c.unavailable[ordinal] = true
			if c.availableFrom(start) >= 0 {
				continue
			}
			return nil, requestError(operation, CategoryStatus, response.StatusCode, nil)
		}
		if response.StatusCode >= 500 {
			c.unavailable[ordinal] = true
			if c.availableFrom(start) >= 0 {
				continue
			}
			return nil, requestError(operation, CategoryServer, response.StatusCode, nil)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, requestError(operation, CategoryStatus, response.StatusCode, nil)
		}
		if !validFinal(response.Request) {
			return nil, requestError(operation, CategoryRedirect, 0, nil)
		}
		contentType, _, contentTypeErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		contentType = strings.ToLower(contentType)
		if contentTypeErr != nil || contentType != "text/html" && contentType != "application/xhtml+xml" {
			return nil, requestError(operation, CategoryContentType, 0, contentTypeErr)
		}
		if len(body) == 0 {
			return nil, requestError(operation, CategoryEmptyResponse, 0, nil)
		}
		c.next = (ordinal + 1) % len(c.clients)
		return body, nil
	}
}

type cinemaTarget struct {
	source string
	path   string
	url    url.URL
}

func parseCinemaURLSource(raw string) (cinemaTarget, error) {
	invalid := func() (cinemaTarget, error) { return cinemaTarget{}, fmt.Errorf("invalid cinema URL") }
	if raw == "" || raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, "?#%\\") {
		return invalid()
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return invalid()
	}
	if strings.HasPrefix(raw, "/") {
		if strings.HasPrefix(raw, "//") || parsed.Scheme != "" || parsed.Host != "" {
			return invalid()
		}
	} else if !strings.HasPrefix(raw, "https://kinepolis.fr/") || parsed.Scheme != "https" || parsed.Host != "kinepolis.fr" {
		return invalid()
	}
	if !cinemaPathPattern.MatchString(parsed.Path) {
		return invalid()
	}
	sourcePath := parsed.Path
	if strings.Contains(sourcePath, "cinémas") {
		if parsed.RawPath != sourcePath {
			return invalid()
		}
	} else if parsed.RawPath != "" {
		return invalid()
	}
	normalized := url.URL{Scheme: "https", Host: "kinepolis.fr", Path: sourcePath}
	return cinemaTarget{source: raw, path: sourcePath, url: normalized}, nil
}

func readBounded(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, MaxBodySize+1)
	body, err := io.ReadAll(limited)
	if len(body) > MaxBodySize {
		return nil, errResponseTooLarge
	}
	if err != nil {
		return nil, err
	}
	return body, nil
}
func validFinalScheduleRequest(request *http.Request) bool {
	if request == nil {
		return false
	}
	want, _ := url.Parse(ScheduleURL)
	actual := request.URL
	return actual != nil && actual.Scheme == "https" && actual.Host == want.Host && actual.User == nil && actual.Path == want.Path && actual.RawQuery == want.RawQuery && actual.Fragment == "" && actual.Opaque == ""
}

func validFinalCinemaRequest(request *http.Request, target cinemaTarget) bool {
	if request == nil || request.URL == nil {
		return false
	}
	actual := request.URL
	if actual.Scheme != target.url.Scheme || actual.Host != target.url.Host || actual.User != nil || actual.Path != target.path || actual.Opaque != "" || actual.RawQuery != "" || actual.ForceQuery || actual.Fragment != "" || actual.RawFragment != "" {
		return false
	}
	if strings.Contains(target.path, "cinémas") {
		if actual.RawPath != "" && actual.RawPath != target.path && actual.RawPath != target.url.EscapedPath() {
			return false
		}
		if actual.RawPath != "" {
			unescaped, err := url.PathUnescape(actual.RawPath)
			if err != nil || unescaped != target.path {
				return false
			}
		}
	} else if actual.RawPath != "" {
		return false
	}
	normalized := *actual
	normalized.RawPath = ""
	normalized.RawFragment = ""
	return normalized.EscapedPath() == target.url.EscapedPath() && normalized.RequestURI() == target.url.RequestURI()
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
