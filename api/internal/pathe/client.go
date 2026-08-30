package pathe

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"messeances/api/internal/syncproxy"
)

const (
	APIBaseURL       = "https://www.pathe.fr"
	CinemasURL       = APIBaseURL + "/api/cinemas"
	ShowsURL         = APIBaseURL + "/api/shows"
	MaxResponseBytes = 8 << 20

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

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}

type Client struct {
	executor *syncproxy.Executor
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
	return newClientWithHTTPClients(clients, nil)
}

func newClientWithHTTPClients(clients []*http.Client, sleep func(context.Context, time.Duration) error) (*Client, error) {
	executor, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{
		Clients:          clients,
		ProxyBacked:      true,
		Headers:          browserHeaders(),
		MaxResponseBytes: MaxResponseBytes,
		ValidURL:         allowedAPIURL,
		Retry: syncproxy.RetryPolicy{
			Sleep:              sleep,
			RetireFinalFailure: true,
		},
		CancelReadOnContext: true,
	})
	if err != nil {
		return nil, fmt.Errorf("invalid request executor")
	}
	return &Client{executor: executor}, nil
}

func (c *Client) RequestCount() int { return c.executor.RequestCount() }

func (c *Client) Get(ctx context.Context, operation Operation, rawURL string) ([]byte, error) {
	if !validOperation(operation) {
		return nil, requestError(operation, CategoryInvalidURL, 0, nil)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !allowedAPIURL(parsed) || !operationMatchesURL(operation, parsed) {
		return nil, requestError(operation, CategoryInvalidURL, 0, nil)
	}

	body, failure := c.executor.Get(ctx, parsed.String(), syncproxy.ResponsePolicy{
		BeforeRead: func(status int) (*syncproxy.Failure, bool) {
			if status == http.StatusForbidden || status == http.StatusTooManyRequests {
				return &syncproxy.Failure{Kind: syncproxy.FailureStatus, StatusCode: status}, false
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
			if status < 200 || status >= 300 {
				return &syncproxy.Failure{Kind: syncproxy.FailureStatus, StatusCode: status}, false
			}
			return nil, false
		},
	})
	if failure != nil {
		return nil, mapFailure(operation, failure)
	}
	return body, nil
}

func mapFailure(operation Operation, failure *syncproxy.Failure) error {
	category := CategoryTransport
	switch failure.Kind {
	case syncproxy.FailureCanceled:
		category = CategoryCanceled
	case syncproxy.FailureNoClient:
		category = CategoryNoProxy
	case syncproxy.FailureInvalidURL:
		category = CategoryInvalidURL
	case syncproxy.FailureTransport:
		category = CategoryTransport
	case syncproxy.FailureRedirect:
		category = CategoryRedirect
	case syncproxy.FailureResponseRead:
		category = CategoryResponseRead
	case syncproxy.FailureResponseLarge:
		category = CategoryResponseLarge
	case syncproxy.FailureChallenge:
		category = CategoryChallenge
	case syncproxy.FailureServer:
		category = CategoryServer
	case syncproxy.FailureStatus:
		category = CategoryStatus
	case syncproxy.FailureContentType:
		category = CategoryContentType
	case syncproxy.FailureEmptyResponse:
		category = CategoryEmptyResponse
	case syncproxy.FailureInvalidJSON:
		category = CategoryInvalidJSON
	}
	cause := error(nil)
	if failure.Kind == syncproxy.FailureCanceled {
		cause = failure.Cause
	}
	return requestError(operation, category, failure.StatusCode, cause)
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

func browserHeaders() http.Header {
	return http.Header{
		"User-Agent":         {chromeUserAgent},
		"Accept":             {"application/json, text/plain, */*"},
		"Accept-Language":    {"fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7"},
		"Sec-Ch-Ua":          {`"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`},
		"Sec-Ch-Ua-Mobile":   {"?0"},
		"Sec-Ch-Ua-Platform": {`"Linux"`},
		"Sec-Fetch-Dest":     {"empty"},
		"Sec-Fetch-Mode":     {"cors"},
		"Sec-Fetch-Site":     {"same-origin"},
	}
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

var checkRedirect = syncproxy.NewRedirectChecker(allowedAPIURL)

func hasTraversal(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}
