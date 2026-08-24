package pathe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"messeances/api/internal/syncproxy"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

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

func (r *failingReadCloser) Close() error {
	r.closed = true
	return nil
}

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

func (r *cancelingReadCloser) Close() error {
	r.closed = true
	return nil
}

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := f(request)
	if response != nil && response.Request == nil {
		response.Request = request
	}
	return response, err
}

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

func directClient(transports ...http.RoundTripper) *Client {
	clients := make([]*http.Client, len(transports))
	for index, transport := range transports {
		clients[index] = &http.Client{Transport: transport, CheckRedirect: checkRedirect}
	}
	return &Client{clients: clients, unavailable: make([]bool, len(clients)), sleep: sleepContext}
}

func TestClientRequiresChromeFingerprintProxyClients(t *testing.T) {
	if _, err := NewClient(ClientConfig{Timeout: 5 * time.Second}); err == nil {
		t.Fatal("proxy-free client accepted")
	}
	secret := "synthetic-password"
	client, err := NewClient(ClientConfig{Proxies: testProxies(t, "http://synthetic-user:"+secret+"@127.0.0.1:8080"), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.clients[0].Transport.(*http2.Transport)
	if !ok || transport.DialTLSContext == nil {
		t.Fatal("Chrome-compatible fingerprint HTTP/2 transport missing")
	}
}

func TestClientStrictRequestHeadersAndJSONResponse(t *testing.T) {
	client := directClient(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != CinemasURL || request.Header.Get("User-Agent") != chromeUserAgent || request.Header.Get("Accept") != "application/json, text/plain, */*" || request.Header.Get("Sec-CH-UA") == "" || request.Header.Get("Sec-Fetch-Mode") != "cors" {
			t.Fatalf("request=%s headers=%v", request.URL, request.Header)
		}
		return testJSONResponse(http.StatusOK, `[]`), nil
	}))
	body, err := client.Get(context.Background(), OperationCinemas, CinemasURL)
	if err != nil || string(body) != `[]` || client.RequestCount() != 1 {
		t.Fatalf("body=%q requests=%d err=%v", body, client.RequestCount(), err)
	}
}

func TestClientRoundRobinInitialSelection(t *testing.T) {
	var mu sync.Mutex
	selected := []int{}
	transports := make([]http.RoundTripper, 3)
	for index := range transports {
		ordinal := index
		transports[index] = roundTripFunc(func(*http.Request) (*http.Response, error) {
			mu.Lock()
			selected = append(selected, ordinal)
			mu.Unlock()
			return testJSONResponse(http.StatusOK, `{}`), nil
		})
	}
	client := directClient(transports...)
	for range 6 {
		if _, err := client.Get(context.Background(), OperationShows, ShowsURL); err != nil {
			t.Fatal(err)
		}
	}
	if got := selected; len(got) != 6 || got[0] != 0 || got[1] != 1 || got[2] != 2 || got[3] != 0 || got[4] != 1 || got[5] != 2 {
		t.Fatalf("selection=%v", got)
	}
}

func TestClientRetriesFourDistinctProxiesWithExactWaits(t *testing.T) {
	secret := "synthetic-upstream-secret"
	order := []int{}
	transports := make([]http.RoundTripper, 5)
	for index := range transports {
		ordinal := index
		transports[index] = roundTripFunc(func(*http.Request) (*http.Response, error) {
			order = append(order, ordinal)
			return testJSONResponse(http.StatusInternalServerError, `{"secret":"`+secret+`"}`), nil
		})
	}
	client := directClient(transports...)
	waits := []time.Duration{}
	client.sleep = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}
	_, err := client.Get(context.Background(), OperationShows, ShowsURL)
	if err == nil || strings.Contains(err.Error(), secret) || client.RequestCount() != 4 {
		t.Fatalf("requests=%d err=%v", client.RequestCount(), err)
	}
	if len(order) != 4 || order[0] != 0 || order[1] != 1 || order[2] != 2 || order[3] != 3 {
		t.Fatalf("order=%v", order)
	}
	wantWaits := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}
	if len(waits) != len(wantWaits) {
		t.Fatalf("waits=%v", waits)
	}
	for index := range waits {
		if waits[index] != wantWaits[index] {
			t.Fatalf("waits=%v", waits)
		}
	}
}

func TestClientTransportFailureDisablesProxyForLifetime(t *testing.T) {
	firstCalls, secondCalls := 0, 0
	client := directClient(
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			firstCalls++
			return nil, errors.New("synthetic transport detail")
		}),
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			secondCalls++
			return testJSONResponse(http.StatusOK, `{}`), nil
		}),
	)
	client.sleep = func(context.Context, time.Duration) error { return nil }
	for range 2 {
		if _, err := client.Get(context.Background(), OperationShows, ShowsURL); err != nil {
			t.Fatal(err)
		}
	}
	if firstCalls != 1 || secondCalls != 2 {
		t.Fatalf("calls=%d,%d", firstCalls, secondCalls)
	}
}

func TestClientResponseReadFailureRetriesAndDisablesProxy(t *testing.T) {
	readFailure := errors.New("synthetic body transport detail")
	failingBody := &failingReadCloser{reader: strings.NewReader(`{"partial":`), err: readFailure}
	firstCalls, secondCalls := 0, 0
	client := directClient(
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			firstCalls++
			response := testJSONResponse(http.StatusOK, "")
			response.Body = failingBody
			response.ContentLength = -1
			return response, nil
		}),
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			secondCalls++
			return testJSONResponse(http.StatusOK, `{}`), nil
		}),
	)
	client.sleep = func(context.Context, time.Duration) error { return nil }
	body, err := client.Get(context.Background(), OperationShows, ShowsURL)
	if err != nil || string(body) != `{}` || firstCalls != 1 || secondCalls != 1 || !failingBody.closed || !client.unavailable[0] {
		t.Fatalf("body=%q calls=%d,%d closed=%v unavailable=%v err=%v", body, firstCalls, secondCalls, failingBody.closed, client.unavailable, err)
	}
	if _, err := client.Get(context.Background(), OperationShows, ShowsURL); err != nil {
		t.Fatal(err)
	}
	if firstCalls != 1 || secondCalls != 2 {
		t.Fatalf("retired proxy reused: calls=%d,%d", firstCalls, secondCalls)
	}
}

func TestClientCancellationDuringResponseReadIsTerminalAndPrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := &cancelingReadCloser{ctx: ctx, started: make(chan struct{})}
	secondCalls := 0
	client := directClient(
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			response := testJSONResponse(http.StatusOK, "")
			response.Body = body
			response.ContentLength = -1
			return response, nil
		}),
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			secondCalls++
			return testJSONResponse(http.StatusOK, `{}`), nil
		}),
	)
	done := make(chan error, 1)
	go func() {
		_, err := client.Get(ctx, OperationShows, ShowsURL)
		done <- err
	}()
	<-body.started
	cancel()
	select {
	case err := <-done:
		var requestErr *RequestError
		if !errors.Is(err, context.Canceled) || !errors.As(err, &requestErr) || requestErr.Category != CategoryCanceled || secondCalls != 0 || !body.closed || client.unavailable[0] {
			t.Fatalf("second=%d closed=%v unavailable=%v err=%v", secondCalls, body.closed, client.unavailable, err)
		}
	case <-time.After(time.Second):
		t.Fatal("response read cancellation did not return promptly")
	}
}

func TestClientTerminalStatusChallengeAndResponseConstraints(t *testing.T) {
	tests := []struct {
		name         string
		first        *http.Response
		wantCategory ErrorCategory
	}{
		{"forbidden", testJSONResponse(http.StatusForbidden, `{}`), CategoryStatus},
		{"rate limited", testJSONResponse(http.StatusTooManyRequests, `{}`), CategoryStatus},
		{"challenge", testJSONResponse(http.StatusOK, `"<title>captcha</title> synthetic-body-secret"`), CategoryChallenge},
		{"non JSON type", func() *http.Response {
			r := testJSONResponse(http.StatusOK, `{}`)
			r.Header.Set("Content-Type", "text/html")
			return r
		}(), CategoryContentType},
		{"invalid JSON", testJSONResponse(http.StatusOK, `{`), CategoryInvalidJSON},
		{"empty", testJSONResponse(http.StatusOK, ` `), CategoryEmptyResponse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secondCalls := 0
			client := directClient(
				roundTripFunc(func(*http.Request) (*http.Response, error) { return test.first, nil }),
				roundTripFunc(func(*http.Request) (*http.Response, error) {
					secondCalls++
					return testJSONResponse(http.StatusOK, `{}`), nil
				}),
			)
			_, err := client.Get(context.Background(), OperationShows, ShowsURL)
			var requestErr *RequestError
			if !errors.As(err, &requestErr) || requestErr.Category != test.wantCategory || secondCalls != 0 || strings.Contains(err.Error(), "synthetic-body-secret") {
				t.Fatalf("second=%d err=%v", secondCalls, err)
			}
		})
	}
}

func TestClientBlockedStatusDoesNotRetryOnUnreadableBody(t *testing.T) {
	body := &failingReadCloser{reader: strings.NewReader("blocked"), err: errors.New("synthetic read failure")}
	secondCalls := 0
	client := directClient(
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
	_, err := client.Get(context.Background(), OperationShows, ShowsURL)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.StatusCode != http.StatusForbidden || secondCalls != 0 || !body.closed {
		t.Fatalf("second=%d closed=%v err=%v", secondCalls, body.closed, err)
	}
}

func TestClientBodyLimitAndRedirectRejection(t *testing.T) {
	client := directClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testJSONResponse(http.StatusOK, strings.Repeat("x", MaxResponseBytes+1)), nil
	}))
	_, err := client.Get(context.Background(), OperationShows, ShowsURL)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.Category != CategoryResponseLarge {
		t.Fatalf("err=%v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://evil.example/api/shows", nil)
	if !errors.Is(checkRedirect(request, nil), errRedirectAuthority) {
		t.Fatal("cross-authority redirect accepted")
	}
	request, _ = http.NewRequest(http.MethodGet, "https://www.pathe.fr/api/other", nil)
	response := testJSONResponse(http.StatusOK, `{}`)
	response.Request = request
	client = directClient(roundTripFunc(func(*http.Request) (*http.Response, error) { return response, nil }))
	_, err = client.Get(context.Background(), OperationShows, ShowsURL)
	if !errors.As(err, &requestErr) || requestErr.Category != CategoryRedirect {
		t.Fatalf("unexpected final URL err=%v", err)
	}
}

func TestClientRejectsUnsafeAPIURLsBeforeTransport(t *testing.T) {
	calls := 0
	client := directClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return testJSONResponse(http.StatusOK, `{}`), nil
	}))
	for _, raw := range []string{
		"http://www.pathe.fr/api/shows",
		"https://pathe.fr/api/shows",
		"https://user@www.pathe.fr/api/shows",
		"https://www.pathe.fr:443/api/shows",
		"https://www.pathe.fr/api/shows?secret=value",
		"https://www.pathe.fr/api/../secret",
		"https://www.pathe.fr/api/other",
		"https://www.pathe.fr/api/shows#fragment",
		"https://www.pathe.fr/cinemas",
	} {
		if _, err := client.Get(context.Background(), OperationShows, raw); err == nil {
			t.Fatalf("unsafe URL accepted: %q", raw)
		}
	}
	if calls != 0 {
		t.Fatalf("transport calls=%d", calls)
	}
	if _, err := client.Get(context.Background(), OperationCinemas, ShowsURL); err == nil {
		t.Fatal("operation and endpoint mismatch accepted")
	}
}

func TestClientCancellationInterruptsRetryWait(t *testing.T) {
	client := directClient(
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testJSONResponse(http.StatusInternalServerError, `{}`), nil
		}),
		roundTripFunc(func(*http.Request) (*http.Response, error) { return testJSONResponse(http.StatusOK, `{}`), nil }),
	)
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan struct{})
	client.sleep = func(ctx context.Context, _ time.Duration) error {
		close(waiting)
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, err := client.Get(ctx, OperationShows, ShowsURL)
		done <- err
	}()
	<-waiting
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || client.RequestCount() != 1 {
			t.Fatalf("requests=%d err=%v", client.RequestCount(), err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled request leaked goroutine")
	}
}
