package pathe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/syncproxy"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := f(request)
	if response != nil && response.Request == nil {
		response.Request = request
	}
	return response, err
}

type failingReadCloser struct {
	reader io.Reader
	err    error
	read   bool
	closed bool
}

func (r *failingReadCloser) Read(buffer []byte) (int, error) {
	if r.read {
		return 0, r.err
	}
	r.read = true
	return r.reader.Read(buffer)
}

func (r *failingReadCloser) Close() error { r.closed = true; return nil }

type cancelingReadCloser struct {
	ctx     context.Context
	started chan struct{}
	closed  bool
}

func (r *cancelingReadCloser) Read([]byte) (int, error) {
	close(r.started)
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func (r *cancelingReadCloser) Close() error { r.closed = true; return nil }

func testJSONResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func testProxies(t *testing.T, raw string) []syncproxy.Proxy {
	t.Helper()
	proxies, err := syncproxy.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return proxies
}

func testClient(t *testing.T, sleep func(context.Context, time.Duration) error, transports ...http.RoundTripper) *Client {
	t.Helper()
	clients := make([]*http.Client, len(transports))
	for index, transport := range transports {
		clients[index] = &http.Client{Transport: transport, CheckRedirect: checkRedirect}
	}
	client, err := newClientWithHTTPClients(clients, sleep)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func noWait(context.Context, time.Duration) error { return nil }

func TestClientRequiresProxy(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 5 * time.Second}); err == nil {
		t.Fatal("proxy-free client accepted")
	}
	client, err := NewClient(ClientConfig{Proxies: testProxies(t, "http://synthetic-user:synthetic-password@127.0.0.1:8080"), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if client.RequestCount() != 0 {
		t.Fatalf("requests=%d", client.RequestCount())
	}
}

func TestClientStrictRequestHeadersAndJSONResponse(t *testing.T) {
	client := testClient(t, noWait, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != CinemasURL || request.Header.Get("User-Agent") != chromeUserAgent || request.Header.Get("Accept") != "application/json, text/plain, */*" || request.Header.Get("Sec-CH-UA") == "" || request.Header.Get("Sec-Fetch-Mode") != "cors" {
			t.Fatalf("request=%s headers=%v", request.URL, request.Header)
		}
		return testJSONResponse(http.StatusOK, `[]`), nil
	}))
	body, err := client.Get(t.Context(), OperationCinemas, CinemasURL)
	if err != nil || string(body) != `[]` || client.RequestCount() != 1 {
		t.Fatalf("body=%q requests=%d err=%v", body, client.RequestCount(), err)
	}
}

func TestClientRetryCursorPolicy(t *testing.T) {
	order := []int{}
	calls := make([]int, 3)
	transports := make([]http.RoundTripper, 3)
	for ordinal := range transports {
		transports[ordinal] = roundTripFunc(func(*http.Request) (*http.Response, error) {
			order = append(order, ordinal)
			calls[ordinal]++
			if ordinal == 0 {
				return testJSONResponse(http.StatusInternalServerError, `{}`), nil
			}
			return testJSONResponse(http.StatusOK, `{}`), nil
		})
	}
	client := testClient(t, noWait, transports...)
	for range 2 {
		if _, err := client.Get(t.Context(), OperationShows, ShowsURL); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(order, []int{0, 1, 1}) {
		t.Fatalf("order=%v", order)
	}
}

func TestClientCancellationDuringResponseReadIsTerminalAndPrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	body := &cancelingReadCloser{ctx: ctx, started: make(chan struct{})}
	secondCalls := 0
	client := testClient(t, noWait,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			response := testJSONResponse(http.StatusOK, "")
			response.Body, response.ContentLength = body, -1
			return response, nil
		}),
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			secondCalls++
			return testJSONResponse(http.StatusOK, `{}`), nil
		}),
	)
	done := make(chan error, 1)
	go func() { _, err := client.Get(ctx, OperationShows, ShowsURL); done <- err }()
	<-body.started
	cancel()
	select {
	case err := <-done:
		var requestErr *RequestError
		if !errors.Is(err, context.Canceled) || !errors.As(err, &requestErr) || requestErr.Category != CategoryCanceled || secondCalls != 0 || !body.closed {
			t.Fatalf("second=%d closed=%v err=%v", secondCalls, body.closed, err)
		}
	case <-time.After(time.Second):
		t.Fatal("response read cancellation did not return promptly")
	}
}

func TestClientTerminalStatusChallengeAndResponseConstraints(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		mediaType  string
		body       string
		want       ErrorCategory
		wantStatus int
		wantBody   string
	}{
		{"forbidden", http.StatusForbidden, "application/json", `{}`, CategoryStatus, http.StatusForbidden, ""},
		{"rate limited", http.StatusTooManyRequests, "application/json", `{}`, CategoryStatus, http.StatusTooManyRequests, ""},
		{"challenge before server", http.StatusInternalServerError, "application/json", `"<title>captcha</title> synthetic-body-secret"`, CategoryChallenge, 0, ""},
		{"non JSON", http.StatusOK, "text/html", `{}`, CategoryContentType, 0, ""},
		{"non-application suffix", http.StatusOK, "text/problem+json", `{}`, CategoryContentType, 0, ""},
		{"invalid JSON", http.StatusOK, "application/json", `{`, CategoryInvalidJSON, 0, ""},
		{"empty", http.StatusOK, "application/json", ` `, CategoryEmptyResponse, 0, ""},
		{"application suffix", http.StatusOK, "application/problem+json", `{}`, "", 0, `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := testClient(t, noWait, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := testJSONResponse(test.status, test.body)
				response.Header.Set("Content-Type", test.mediaType)
				return response, nil
			}))
			body, err := client.Get(t.Context(), OperationShows, ShowsURL)
			if test.want == "" {
				if err != nil || string(body) != test.wantBody {
					t.Fatalf("body=%q err=%v", body, err)
				}
			} else {
				var requestErr *RequestError
				if !errors.As(err, &requestErr) || requestErr.Category != test.want || requestErr.StatusCode != test.wantStatus || strings.Contains(err.Error(), "synthetic-body-secret") {
					t.Fatalf("err=%v", err)
				}
			}
			if calls != 1 || client.RequestCount() != 1 {
				t.Fatalf("calls=%d count=%d", calls, client.RequestCount())
			}
		})
	}
}

func TestClientBlockedStatusDoesNotRetryOnUnreadableBody(t *testing.T) {
	body := &failingReadCloser{reader: strings.NewReader("blocked"), err: errors.New("synthetic read failure")}
	secondCalls := 0
	client := testClient(t, noWait,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			response := testJSONResponse(http.StatusForbidden, "")
			response.Body = body
			return response, nil
		}),
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			secondCalls++
			return testJSONResponse(http.StatusOK, `{}`), nil
		}),
	)
	_, err := client.Get(t.Context(), OperationShows, ShowsURL)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.StatusCode != http.StatusForbidden || secondCalls != 0 || !body.closed {
		t.Fatalf("second=%d closed=%v err=%v", secondCalls, body.closed, err)
	}
}

func TestClientBodyLimitAndRedirectRejection(t *testing.T) {
	client := testClient(t, noWait, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testJSONResponse(http.StatusOK, strings.Repeat("x", MaxResponseBytes+1)), nil
	}))
	_, err := client.Get(t.Context(), OperationShows, ShowsURL)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.Category != CategoryResponseLarge {
		t.Fatalf("err=%v", err)
	}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://evil.example/api/shows", nil)
	if checkRedirect(request, nil) == nil {
		t.Fatal("cross-authority redirect accepted")
	}
	request, _ = http.NewRequestWithContext(t.Context(), http.MethodGet, "https://www.pathe.fr/api/other", nil)
	response := testJSONResponse(http.StatusOK, `{}`)
	response.Request = request
	client = testClient(t, noWait, roundTripFunc(func(*http.Request) (*http.Response, error) { return response, nil }))
	if _, err = client.Get(t.Context(), OperationShows, ShowsURL); !errors.As(err, &requestErr) || requestErr.Category != CategoryRedirect {
		t.Fatalf("err=%v", err)
	}
}

func TestClientRejectsUnsafeAPIURLsBeforeTransport(t *testing.T) {
	calls := 0
	client := testClient(t, noWait, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return testJSONResponse(http.StatusOK, `{}`), nil
	}))
	for _, raw := range []string{"http://www.pathe.fr/api/shows", "https://pathe.fr/api/shows", "https://user@www.pathe.fr/api/shows", "https://www.pathe.fr:443/api/shows", "https://www.pathe.fr/api/shows?secret=value", "https://www.pathe.fr/api/../secret", "https://www.pathe.fr/api/other", "https://www.pathe.fr/api/shows#fragment", "https://www.pathe.fr/cinemas"} {
		if _, err := client.Get(t.Context(), OperationShows, raw); err == nil {
			t.Fatalf("unsafe URL accepted: %q", raw)
		}
	}
	if _, err := client.Get(t.Context(), OperationCinemas, ShowsURL); err == nil || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestClientFinalRetryFailureRetiresFinalProxy(t *testing.T) {
	const attempts = 4
	transports := make([]http.RoundTripper, attempts)
	for ordinal := range transports {
		transports[ordinal] = roundTripFunc(func(*http.Request) (*http.Response, error) { return testJSONResponse(http.StatusBadGateway, `{}`), nil })
	}
	client := testClient(t, noWait, transports...)
	_, err := client.Get(t.Context(), OperationShows, ShowsURL)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.Category != CategoryServer || requestErr.StatusCode != http.StatusBadGateway || client.RequestCount() != attempts {
		t.Fatalf("count=%d err=%v", client.RequestCount(), err)
	}
	_, err = client.Get(t.Context(), OperationShows, ShowsURL)
	if !errors.As(err, &requestErr) || requestErr.Category != CategoryNoProxy || requestErr.StatusCode != 0 || errors.Unwrap(requestErr) != nil || client.RequestCount() != attempts {
		t.Fatalf("count=%d err=%v", client.RequestCount(), err)
	}
}

func TestClientPostWaitNoProxyPreservesCategoryAndClearsStatus(t *testing.T) {
	waiting, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	client := testClient(t, func(context.Context, time.Duration) error { once.Do(func() { close(waiting) }); <-release; return nil },
		roundTripFunc(func(*http.Request) (*http.Response, error) { return testJSONResponse(http.StatusBadGateway, `{}`), nil }),
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testJSONResponse(http.StatusServiceUnavailable, `{}`), nil
		}),
	)
	result := make(chan error, 1)
	go func() { _, err := client.Get(t.Context(), OperationShows, ShowsURL); result <- err }()
	<-waiting
	_, concurrentErr := client.Get(t.Context(), OperationShows, ShowsURL)
	var concurrentRequestErr *RequestError
	if !errors.As(concurrentErr, &concurrentRequestErr) || concurrentRequestErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("concurrent=%v", concurrentErr)
	}
	close(release)
	err := <-result
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.Category != CategoryServer || requestErr.StatusCode != 0 || errors.Unwrap(requestErr) != nil || client.RequestCount() != 2 {
		t.Fatalf("count=%d err=%v", client.RequestCount(), err)
	}
}
