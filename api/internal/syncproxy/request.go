package syncproxy

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
)

type FailureKind uint8

const (
	FailureCanceled FailureKind = iota + 1
	FailureNoClient
	FailureInvalidURL
	FailureTransport
	FailureRedirect
	FailureResponseRead
	FailureResponseLarge
	FailureChallenge
	FailureServer
	FailureStatus
	FailureContentType
	FailureEmptyResponse
	FailureInvalidJSON
)

type Failure struct {
	Kind       FailureKind
	StatusCode int
	Cause      error
}

type ResponsePolicy struct {
	BeforeRead func(int) (*Failure, bool)
	AfterRead  func(int, []byte) (*Failure, bool)
}

type RetryPolicy struct {
	Sleep                    func(context.Context, time.Duration) error
	RetireFinalFailure       bool
	PreserveFailureAfterWait bool
}

type ExecutorConfig struct {
	Clients                       []*http.Client
	ProxyBacked                   bool
	Headers                       http.Header
	MaxResponseBytes              int64
	ValidURL                      func(*url.URL) bool
	AllowNonApplicationJSONSuffix bool
	Retry                         RetryPolicy
	AdvanceNextOnRetry            bool
	CancelReadOnContext           bool
}

type Executor struct {
	mu                            sync.Mutex
	clients                       []*http.Client
	unavailable                   []bool
	next                          int
	proxyBacked                   bool
	headers                       http.Header
	maxResponseBytes              int64
	validURL                      func(*url.URL) bool
	allowNonApplicationJSONSuffix bool
	retry                         RetryPolicy
	advanceNextOnRetry            bool
	cancelReadOnContext           bool
	requests                      atomic.Int64
	do                            func(*http.Client, *http.Request) (*http.Response, error)
}

var (
	errRedirectAuthority = errors.New("redirect authority rejected")
	errRedirectLimit     = errors.New("redirect limit exceeded")
)

func NewExecutor(config ExecutorConfig) (*Executor, error) {
	if config.MaxResponseBytes <= 0 {
		return nil, fmt.Errorf("max response bytes must be positive")
	}
	if config.ValidURL == nil {
		return nil, fmt.Errorf("valid URL policy is required")
	}
	for _, client := range config.Clients {
		if client == nil {
			return nil, fmt.Errorf("HTTP client must not be nil")
		}
	}
	clients := append([]*http.Client(nil), config.Clients...)
	retry := config.Retry
	if retry.Sleep == nil {
		retry.Sleep = sleepContext
	}
	return &Executor{
		clients:                       clients,
		unavailable:                   make([]bool, len(clients)),
		proxyBacked:                   config.ProxyBacked,
		headers:                       config.Headers.Clone(),
		maxResponseBytes:              config.MaxResponseBytes,
		validURL:                      config.ValidURL,
		allowNonApplicationJSONSuffix: config.AllowNonApplicationJSONSuffix,
		retry:                         retry,
		advanceNextOnRetry:            config.AdvanceNextOnRetry,
		cancelReadOnContext:           config.CancelReadOnContext,
		do:                            (*http.Client).Do,
	}, nil
}

func (e *Executor) RequestCount() int { return int(e.requests.Load()) }

func (e *Executor) Get(ctx context.Context, rawURL string, policy ResponsePolicy) ([]byte, *Failure) {
	attempted := make([]bool, len(e.clients))
	ordinal := -1
	var prior *Failure
	postWait := false

	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, &Failure{Kind: FailureCanceled, Cause: err}
		}
		ordinal = e.acquire(ordinal, attempted)
		if ordinal < 0 {
			return nil, noClientFailure(prior, postWait, e.retry.PreserveFailureAfterWait)
		}
		if e.proxyBacked {
			attempted[ordinal] = true
		}

		body, failure, retry := e.attempt(ctx, e.clients[ordinal], rawURL, policy)
		if failure == nil {
			return body, nil
		}
		if !retry {
			return nil, failure
		}
		prior = failure
		if attempt == 3 {
			if e.proxyBacked && e.retry.RetireFinalFailure {
				e.retire(ordinal)
			}
			return nil, failure
		}
		if e.proxyBacked {
			e.retire(ordinal)
		}
		if !e.hasCandidate(attempted) {
			return nil, failure
		}
		if err := e.retry.Sleep(ctx, 500*time.Millisecond<<attempt); err != nil {
			return nil, &Failure{Kind: FailureCanceled, Cause: err}
		}
		postWait = true
	}

	return nil, prior
}

func (e *Executor) attempt(ctx context.Context, client *http.Client, rawURL string, policy ResponsePolicy) ([]byte, *Failure, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, &Failure{Kind: FailureInvalidURL}, false
	}
	request.Header = e.headers.Clone()
	e.requests.Add(1)
	response, err := e.do(client, request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, &Failure{Kind: FailureCanceled, Cause: ctxErr}, false
		}
		if errors.Is(err, errRedirectAuthority) || errors.Is(err, errRedirectLimit) {
			return nil, &Failure{Kind: FailureRedirect}, false
		}
		return nil, &Failure{Kind: FailureTransport, Cause: err}, true
	}
	if response == nil {
		return nil, &Failure{Kind: FailureTransport}, true
	}
	if policy.BeforeRead != nil {
		if failure, retry := policy.BeforeRead(response.StatusCode); failure != nil {
			closeResponse(response)
			return nil, failure, retry
		}
	}
	if response.Body == nil {
		return nil, &Failure{Kind: FailureResponseRead}, true
	}

	body, readErr := readBounded(response.Body, response.ContentLength, e.maxResponseBytes)
	_ = response.Body.Close()
	if readErr != nil {
		if errors.Is(readErr, errResponseTooLarge) {
			return nil, &Failure{Kind: FailureResponseLarge}, false
		}
		if e.cancelReadOnContext {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, &Failure{Kind: FailureCanceled, Cause: ctxErr}, false
			}
		}
		return nil, &Failure{Kind: FailureResponseRead, Cause: readErr}, true
	}
	if policy.AfterRead != nil {
		if failure, retry := policy.AfterRead(response.StatusCode, body); failure != nil {
			return nil, failure, retry
		}
	}
	if response.Request == nil || response.Request.URL.String() != rawURL || !e.validURL(response.Request.URL) {
		return nil, &Failure{Kind: FailureRedirect}, false
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !isJSONMediaType(mediaType, e.allowNonApplicationJSONSuffix) {
		return nil, &Failure{Kind: FailureContentType}, false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, &Failure{Kind: FailureEmptyResponse}, false
	}
	if !json.Valid(body) {
		return nil, &Failure{Kind: FailureInvalidJSON}, false
	}
	return body, nil, false
}

func (e *Executor) acquire(previous int, attempted []bool) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.clients) == 0 {
		return -1
	}
	if !e.proxyBacked && previous >= 0 {
		return previous
	}
	start := e.next
	advance := previous < 0 || e.advanceNextOnRetry
	if previous >= 0 && !e.advanceNextOnRetry {
		start = (previous + 1) % len(e.clients)
	}
	for offset := 0; offset < len(e.clients); offset++ {
		ordinal := (start + offset) % len(e.clients)
		if e.unavailable[ordinal] || e.proxyBacked && attempted[ordinal] {
			continue
		}
		if advance {
			e.next = (ordinal + 1) % len(e.clients)
		}
		return ordinal
	}
	return -1
}

func (e *Executor) retire(ordinal int) {
	e.mu.Lock()
	e.unavailable[ordinal] = true
	e.mu.Unlock()
}

func (e *Executor) hasCandidate(attempted []bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.proxyBacked {
		return len(e.clients) > 0
	}
	for ordinal := range e.clients {
		if !e.unavailable[ordinal] && !attempted[ordinal] {
			return true
		}
	}
	return false
}

func noClientFailure(prior *Failure, postWait, preserveFailure bool) *Failure {
	if prior == nil {
		return &Failure{Kind: FailureNoClient}
	}
	if !postWait || preserveFailure {
		return prior
	}
	return &Failure{Kind: prior.Kind}
}

func closeResponse(response *http.Response) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}

var errResponseTooLarge = errors.New("response too large")

func readBounded(reader io.Reader, contentLength, limit int64) ([]byte, error) {
	if contentLength > limit {
		return nil, errResponseTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if int64(len(body)) > limit {
		return nil, errResponseTooLarge
	}
	return body, err
}

func isJSONMediaType(mediaType string, allowNonApplicationSuffix bool) bool {
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") && (allowNonApplicationSuffix || strings.HasPrefix(mediaType, "application/"))
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

func NewRedirectChecker(validURL func(*url.URL) bool) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errRedirectLimit
		}
		if request == nil || validURL == nil || !validURL(request.URL) {
			return errRedirectAuthority
		}
		return nil
	}
}
