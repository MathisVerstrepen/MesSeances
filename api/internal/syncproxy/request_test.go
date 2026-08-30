package syncproxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const requestTestURL = "https://example.test/data"

type requestRoundTripFunc func(*http.Request) (*http.Response, error)

func (f requestRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := f(request)
	if response != nil && response.Request == nil {
		response.Request = request
	}
	return response, err
}

type trackingReadCloser struct {
	reader io.Reader
	err    error
	closed bool
}

func (r *trackingReadCloser) Read(buffer []byte) (int, error) {
	if r.reader != nil {
		read, err := r.reader.Read(buffer)
		if err != io.EOF || read > 0 {
			return read, err
		}
		r.reader = nil
	}
	if r.err != nil {
		err := r.err
		r.err = nil
		return 0, err
	}
	return 0, io.EOF
}

func (r *trackingReadCloser) Close() error { r.closed = true; return nil }

type blockingReadCloser struct {
	ctx     context.Context
	started chan struct{}
	closed  bool
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	close(r.started)
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func (r *blockingReadCloser) Close() error { r.closed = true; return nil }

func requestClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: transport}
}

func requestJSONResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func requestPolicy() ResponsePolicy {
	return ResponsePolicy{AfterRead: func(status int, _ []byte) (*Failure, bool) {
		if status >= 500 {
			return &Failure{Kind: FailureServer, StatusCode: status}, true
		}
		if status < 200 || status >= 300 {
			return &Failure{Kind: FailureStatus, StatusCode: status}, false
		}
		return nil, false
	}}
}

func newRequestExecutor(t *testing.T, config ExecutorConfig) *Executor {
	t.Helper()
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = 1024
	}
	if config.ValidURL == nil {
		config.ValidURL = func(candidate *url.URL) bool {
			return candidate != nil && candidate.Scheme == "https" && candidate.Host == "example.test"
		}
	}
	executor, err := NewExecutor(config)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func noRequestWait(context.Context, time.Duration) error { return nil }

func TestNewExecutor(t *testing.T) {
	client := requestClient(requestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("X-Test") != "original" {
			t.Fatalf("headers=%v", request.Header)
		}
		return requestJSONResponse(http.StatusOK, `{}`), nil
	}))
	validURL := func(*url.URL) bool { return true }
	for _, config := range []ExecutorConfig{
		{MaxResponseBytes: -1, ValidURL: validURL},
		{Clients: []*http.Client{nil}, MaxResponseBytes: 1, ValidURL: validURL},
		{Clients: []*http.Client{client}, MaxResponseBytes: 1},
	} {
		if _, err := NewExecutor(config); err == nil {
			t.Fatal("invalid config accepted")
		}
	}

	clients := []*http.Client{client}
	headers := http.Header{"X-Test": {"original"}}
	executor := newRequestExecutor(t, ExecutorConfig{Clients: clients, Headers: headers})
	clients[0] = nil
	headers.Set("X-Test", "changed")
	body, failure := executor.Get(t.Context(), requestTestURL, requestPolicy())
	if failure != nil || string(body) != `{}` {
		t.Fatalf("body=%q failure=%+v", body, failure)
	}
}

func TestExecutorRequestAccountingAndHeaders(t *testing.T) {
	calls := 0
	executor := newRequestExecutor(t, ExecutorConfig{
		Clients: []*http.Client{requestClient(requestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			if request.Header.Get("X-Test") != "original" {
				t.Fatalf("headers=%v", request.Header)
			}
			request.Header.Set("X-Test", "mutated")
			return requestJSONResponse(http.StatusOK, `{}`), nil
		}))},
		Headers: http.Header{"X-Test": {"original"}},
	})
	for range 2 {
		if _, failure := executor.Get(t.Context(), requestTestURL, requestPolicy()); failure != nil {
			t.Fatal(failure)
		}
	}
	if _, failure := executor.Get(t.Context(), "://invalid", requestPolicy()); failure == nil || failure.Kind != FailureInvalidURL {
		t.Fatalf("failure=%+v", failure)
	}
	if calls != 2 || executor.RequestCount() != 2 {
		t.Fatalf("calls=%d count=%d", calls, executor.RequestCount())
	}
}

func TestExecutorRetryModes(t *testing.T) {
	t.Run("cursor", func(t *testing.T) {
		for _, test := range []struct {
			name    string
			advance bool
			want    []int
		}{{"initial", false, []int{0, 1, 1}}, {"every acquisition", true, []int{0, 1, 2}}} {
			t.Run(test.name, func(t *testing.T) {
				order := []int{}
				calls := make([]int, 3)
				clients := make([]*http.Client, 3)
				for ordinal := range clients {
					clients[ordinal] = requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) {
						order = append(order, ordinal)
						calls[ordinal]++
						if ordinal == 0 && calls[ordinal] == 1 {
							return requestJSONResponse(http.StatusInternalServerError, `{}`), nil
						}
						return requestJSONResponse(http.StatusOK, `{}`), nil
					}))
				}
				executor := newRequestExecutor(t, ExecutorConfig{Clients: clients, ProxyBacked: true, AdvanceNextOnRetry: test.advance, Retry: RetryPolicy{Sleep: noRequestWait}})
				for range 2 {
					if _, failure := executor.Get(t.Context(), requestTestURL, requestPolicy()); failure != nil {
						t.Fatal(failure)
					}
				}
				if !slices.Equal(order, test.want) {
					t.Fatalf("order=%v", order)
				}
			})
		}
	})

	t.Run("distinct proxies and waits", func(t *testing.T) {
		order := []int{}
		waits := []time.Duration{}
		clients := make([]*http.Client, 5)
		for ordinal := range clients {
			clients[ordinal] = requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) {
				order = append(order, ordinal)
				if ordinal < 3 {
					return requestJSONResponse(http.StatusInternalServerError, `{}`), nil
				}
				return requestJSONResponse(http.StatusOK, `{}`), nil
			}))
		}
		executor := newRequestExecutor(t, ExecutorConfig{Clients: clients, ProxyBacked: true, Retry: RetryPolicy{Sleep: func(_ context.Context, wait time.Duration) error {
			waits = append(waits, wait)
			return nil
		}}})
		for range 2 {
			if _, failure := executor.Get(t.Context(), requestTestURL, requestPolicy()); failure != nil {
				t.Fatal(failure)
			}
		}
		if !slices.Equal(order, []int{0, 1, 2, 3, 3}) || !slices.Equal(waits, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}) {
			t.Fatalf("order=%v waits=%v", order, waits)
		}
	})

	t.Run("final retirement", func(t *testing.T) {
		for _, retire := range []bool{false, true} {
			calls := make([]int, 4)
			clients := make([]*http.Client, 4)
			for ordinal := range clients {
				clients[ordinal] = requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) {
					calls[ordinal]++
					if ordinal == 3 && calls[ordinal] == 2 {
						return requestJSONResponse(http.StatusOK, `{}`), nil
					}
					return requestJSONResponse(http.StatusBadGateway, `{}`), nil
				}))
			}
			executor := newRequestExecutor(t, ExecutorConfig{Clients: clients, ProxyBacked: true, Retry: RetryPolicy{Sleep: noRequestWait, RetireFinalFailure: retire}})
			if _, failure := executor.Get(t.Context(), requestTestURL, requestPolicy()); failure == nil || failure.StatusCode != http.StatusBadGateway {
				t.Fatalf("first=%+v", failure)
			}
			_, failure := executor.Get(t.Context(), requestTestURL, requestPolicy())
			if retire && (failure == nil || failure.Kind != FailureNoClient) || !retire && failure != nil {
				t.Fatalf("retire=%v calls=%v failure=%+v", retire, calls, failure)
			}
			wantCalls := []int{1, 1, 1, 2}
			if retire {
				wantCalls[3] = 1
			}
			if !slices.Equal(calls, wantCalls) {
				t.Fatalf("retire=%v calls=%v", retire, calls)
			}
		}
	})

	t.Run("direct reuse", func(t *testing.T) {
		calls := 0
		executor := newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls < 4 {
				return nil, errors.New("synthetic transport detail")
			}
			return requestJSONResponse(http.StatusOK, `{}`), nil
		}))}, Retry: RetryPolicy{Sleep: noRequestWait, RetireFinalFailure: true}})
		for range 2 {
			if _, failure := executor.Get(t.Context(), requestTestURL, requestPolicy()); failure != nil {
				t.Fatal(failure)
			}
		}
		if calls != 5 {
			t.Fatalf("calls=%d", calls)
		}
	})
}

func TestExecutorExhaustionModes(t *testing.T) {
	empty := newRequestExecutor(t, ExecutorConfig{ProxyBacked: true})
	if _, failure := empty.Get(t.Context(), requestTestURL, requestPolicy()); failure == nil || failure.Kind != FailureNoClient || failure.StatusCode != 0 || failure.Cause != nil || empty.RequestCount() != 0 {
		t.Fatalf("count=%d failure=%+v", empty.RequestCount(), failure)
	}

	cause := errors.New("synthetic transport detail")
	one := newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, cause }))}, ProxyBacked: true, Retry: RetryPolicy{Sleep: noRequestWait}})
	if _, failure := one.Get(t.Context(), requestTestURL, requestPolicy()); failure == nil || failure.Kind != FailureTransport || !errors.Is(failure.Cause, cause) || one.RequestCount() != 1 {
		t.Fatalf("count=%d failure=%+v", one.RequestCount(), failure)
	}

	for _, preserve := range []bool{false, true} {
		t.Run(map[bool]string{false: "clear", true: "preserve"}[preserve], func(t *testing.T) {
			readFailure := errors.New("synthetic read detail")
			clients := make([]*http.Client, 2)
			for ordinal := range clients {
				clients[ordinal] = requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: &trackingReadCloser{err: readFailure}}, nil
				}))
			}
			waiting, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			executor := newRequestExecutor(t, ExecutorConfig{Clients: clients, ProxyBacked: true, Retry: RetryPolicy{PreserveFailureAfterWait: preserve, Sleep: func(context.Context, time.Duration) error {
				once.Do(func() { close(waiting) })
				<-release
				return nil
			}}})
			result := make(chan *Failure, 1)
			go func() { _, failure := executor.Get(t.Context(), requestTestURL, requestPolicy()); result <- failure }()
			<-waiting
			_, concurrent := executor.Get(t.Context(), requestTestURL, requestPolicy())
			if concurrent == nil || concurrent.Kind != FailureResponseRead || !errors.Is(concurrent.Cause, readFailure) {
				t.Fatalf("concurrent=%+v", concurrent)
			}
			close(release)
			failure := <-result
			if failure == nil || failure.Kind != FailureResponseRead || failure.StatusCode != 0 || preserve != errors.Is(failure.Cause, readFailure) || executor.RequestCount() != 2 {
				t.Fatalf("count=%d failure=%+v", executor.RequestCount(), failure)
			}
		})
	}
}

func TestExecutorCancellation(t *testing.T) {
	t.Run("Do", func(t *testing.T) {
		entered := make(chan struct{})
		executor := newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{requestClient(requestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			close(entered)
			<-request.Context().Done()
			return nil, request.Context().Err()
		}))}})
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan *Failure, 1)
		go func() { _, failure := executor.Get(ctx, requestTestURL, requestPolicy()); result <- failure }()
		<-entered
		cancel()
		failure := <-result
		if failure == nil || failure.Kind != FailureCanceled || !errors.Is(failure.Cause, context.Canceled) || executor.RequestCount() != 1 {
			t.Fatalf("count=%d failure=%+v", executor.RequestCount(), failure)
		}
	})

	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "retryable read", true: "terminal read"}[terminal], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			body := &blockingReadCloser{ctx: ctx, started: make(chan struct{})}
			executor := newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}, nil
			}))}, ProxyBacked: true, CancelReadOnContext: terminal, Retry: RetryPolicy{Sleep: noRequestWait}})
			result := make(chan *Failure, 1)
			go func() { _, failure := executor.Get(ctx, requestTestURL, requestPolicy()); result <- failure }()
			<-body.started
			cancel()
			failure := <-result
			want := FailureResponseRead
			if terminal {
				want = FailureCanceled
			}
			if failure == nil || failure.Kind != want || !body.closed || terminal && !errors.Is(failure.Cause, context.Canceled) || executor.RequestCount() != 1 {
				t.Fatalf("closed=%v count=%d failure=%+v", body.closed, executor.RequestCount(), failure)
			}
		})
	}

	t.Run("sleep", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		executor := newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{
			requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return requestJSONResponse(http.StatusInternalServerError, `{}`), nil
			})),
			requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) { return requestJSONResponse(http.StatusOK, `{}`), nil })),
		}, ProxyBacked: true, Retry: RetryPolicy{Sleep: func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }}})
		_, failure := executor.Get(ctx, requestTestURL, requestPolicy())
		if failure == nil || failure.Kind != FailureCanceled || !errors.Is(failure.Cause, context.Canceled) || executor.RequestCount() != 1 {
			t.Fatalf("count=%d failure=%+v", executor.RequestCount(), failure)
		}
	})
}

func TestExecutorResponseValidation(t *testing.T) {
	t.Run("nil response and body", func(t *testing.T) {
		for _, nilResponse := range []bool{false, true} {
			executor := newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) { return requestJSONResponse(http.StatusOK, `{}`), nil }))}, ProxyBacked: true, Retry: RetryPolicy{Sleep: noRequestWait}})
			executor.do = func(*http.Client, *http.Request) (*http.Response, error) {
				if nilResponse {
					return nil, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}}, nil
			}
			_, failure := executor.Get(t.Context(), requestTestURL, requestPolicy())
			want := FailureResponseRead
			if nilResponse {
				want = FailureTransport
			}
			if failure == nil || failure.Kind != want || executor.RequestCount() != 1 {
				t.Fatalf("nilResponse=%v count=%d failure=%+v", nilResponse, executor.RequestCount(), failure)
			}
		}
	})

	for _, test := range []struct {
		name          string
		contentLength int64
		body          string
		want          FailureKind
	}{{"declared limit", 9, `{}`, FailureResponseLarge}, {"stream limit", -1, strings.Repeat("x", 9), FailureResponseLarge}, {"at limit", 8, strings.Repeat("x", 8), FailureInvalidJSON}} {
		t.Run(test.name, func(t *testing.T) {
			body := &trackingReadCloser{reader: strings.NewReader(test.body)}
			executor := newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, ContentLength: test.contentLength, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}, nil
			}))}, MaxResponseBytes: 8})
			_, failure := executor.Get(t.Context(), requestTestURL, requestPolicy())
			if failure == nil || failure.Kind != test.want || !body.closed {
				t.Fatalf("closed=%v failure=%+v", body.closed, failure)
			}
		})
	}

	t.Run("classification order", func(t *testing.T) {
		body := &trackingReadCloser{reader: strings.NewReader(`{}`)}
		executor := newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusForbidden, Body: body}, nil
		}))}})
		_, failure := executor.Get(t.Context(), requestTestURL, ResponsePolicy{BeforeRead: func(status int) (*Failure, bool) {
			return &Failure{Kind: FailureStatus, StatusCode: status}, false
		}, AfterRead: func(int, []byte) (*Failure, bool) { t.Fatal("after read called"); return nil, false }})
		if failure == nil || failure.StatusCode != http.StatusForbidden || !body.closed || body.reader == nil {
			t.Fatalf("closed=%v failure=%+v", body.closed, failure)
		}

		sequence := []string{}
		executor = newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) { return requestJSONResponse(http.StatusOK, `{}`), nil }))}, ValidURL: func(*url.URL) bool { sequence = append(sequence, "URL"); return true }})
		if _, failure := executor.Get(t.Context(), requestTestURL, ResponsePolicy{AfterRead: func(int, []byte) (*Failure, bool) { sequence = append(sequence, "after"); return nil, false }}); failure != nil {
			t.Fatal(failure)
		}
		if !slices.Equal(sequence, []string{"after", "URL"}) {
			t.Fatalf("sequence=%v", sequence)
		}
	})

	for _, test := range []struct {
		name      string
		mediaType string
		body      string
		broadJSON bool
		finalURL  string
		validURL  bool
		want      FailureKind
	}{
		{"exact URL", "application/json", `{}`, false, "https://example.test/other", true, FailureRedirect},
		{"disallowed URL", "application/json", `{}`, false, requestTestURL, false, FailureRedirect},
		{"application suffix", "application/problem+json", `{}`, false, requestTestURL, true, 0},
		{"strict non-application suffix", "text/problem+json", `{}`, false, requestTestURL, true, FailureContentType},
		{"broad suffix", "text/problem+json", `{}`, true, requestTestURL, true, 0},
		{"malformed media", `application/json; charset="`, `{}`, false, requestTestURL, true, FailureContentType},
		{"rejected media", "text/html", `{}`, false, requestTestURL, true, FailureContentType},
		{"blank", "application/json", " \n", false, requestTestURL, true, FailureEmptyResponse},
		{"invalid JSON", "application/json", `{`, false, requestTestURL, true, FailureInvalidJSON},
		{"success", "application/json", `{}`, false, requestTestURL, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &trackingReadCloser{reader: strings.NewReader(test.body)}
			executor := newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{requestClient(requestRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				request = request.Clone(request.Context())
				request.URL, _ = url.Parse(test.finalURL)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {test.mediaType}}, Body: body, Request: request}, nil
			}))}, AllowNonApplicationJSONSuffix: test.broadJSON, ValidURL: func(*url.URL) bool { return test.validURL }})
			_, failure := executor.Get(t.Context(), requestTestURL, requestPolicy())
			if test.want == 0 && failure != nil || test.want != 0 && (failure == nil || failure.Kind != test.want) || !body.closed {
				t.Fatalf("closed=%v failure=%+v", body.closed, failure)
			}
		})
	}
}

func TestRedirectChecker(t *testing.T) {
	checker := NewRedirectChecker(func(candidate *url.URL) bool { return candidate != nil && candidate.Host == "example.test" })
	valid, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, requestTestURL, nil)
	invalid, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://evil.test/data", nil)
	if checker(valid, make([]*http.Request, 3)) != nil || checker(invalid, nil) == nil || checker(nil, nil) == nil || checker(valid, make([]*http.Request, 4)) == nil {
		t.Fatal("redirect policy mismatch")
	}
	executor := newRequestExecutor(t, ExecutorConfig{Clients: []*http.Client{requestClient(requestRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: requestTestURL, Err: errRedirectAuthority}
	}))}})
	_, failure := executor.Get(t.Context(), requestTestURL, requestPolicy())
	if failure == nil || failure.Kind != FailureRedirect || failure.Cause != nil || executor.RequestCount() != 1 {
		t.Fatalf("count=%d failure=%+v", executor.RequestCount(), failure)
	}
}
