package cgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"messeances/api/internal/syncproxy"
)

const (
	APIBaseURL         = "https://www.cgrcinemas.fr"
	CinemasURL         = APIBaseURL + "/page-data/sq/d/2506275789.json"
	MaxResponseBytes   = 16 << 20
	MaxRequestURLBytes = 8 << 10

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

type ClientConfig struct {
	Proxies []syncproxy.Proxy
	Timeout time.Duration
}

type Client struct {
	executor *syncproxy.Executor
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
	return newClientWithHTTPClients(clients, len(config.Proxies) > 0, nil)
}

func newClientWithHTTPClients(clients []*http.Client, proxyBacked bool, sleep func(context.Context, time.Duration) error) (*Client, error) {
	executor, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{
		Clients:                       clients,
		ProxyBacked:                   proxyBacked,
		Headers:                       browserHeaders(),
		MaxResponseBytes:              MaxResponseBytes,
		ValidURL:                      allowedURL,
		AllowNonApplicationJSONSuffix: true,
		AdvanceNextOnRetry:            true,
		Retry: syncproxy.RetryPolicy{
			Sleep:                    sleep,
			PreserveFailureAfterWait: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("invalid request executor")
	}
	return &Client{executor: executor}, nil
}

func (c *Client) RequestCount() int { return c.executor.RequestCount() }

func (c *Client) Get(ctx context.Context, operation Operation, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !operationMatchesURL(operation, parsed) {
		return nil, requestError(operation, CategoryInvalidURL, 0, nil)
	}
	body, failure := c.executor.Get(ctx, parsed.String(), syncproxy.ResponsePolicy{
		AfterRead: func(status int, _ []byte) (*syncproxy.Failure, bool) {
			if status >= 500 {
				return &syncproxy.Failure{Kind: syncproxy.FailureServer, StatusCode: status}, true
			}
			if (operation == OperationCinemas || operation == OperationProgram) && status == http.StatusNotFound {
				return &syncproxy.Failure{Kind: syncproxy.FailureStatus, StatusCode: status}, true
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

func mapFailure(operation Operation, failure *syncproxy.Failure) *RequestError {
	category := CategoryTransport
	switch failure.Kind {
	case syncproxy.FailureCanceled:
		category = CategoryCanceled
	case syncproxy.FailureNoClient:
		category = CategoryNoClient
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
		category = CategoryTransport
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

func browserHeaders() http.Header {
	return http.Header{
		"Accept":             {"application/json"},
		"Referer":            {APIBaseURL + "/"},
		"User-Agent":         {chromeUserAgent},
		"Sec-Ch-Ua":          {`"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`},
		"Sec-Ch-Ua-Mobile":   {"?0"},
		"Sec-Ch-Ua-Platform": {`"Linux"`},
	}
}

var checkRedirect = syncproxy.NewRedirectChecker(allowedURL)

func hasTraversal(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return true
		}
	}
	return false
}
