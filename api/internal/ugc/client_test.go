package ugc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (body *trackingReadCloser) Close() error {
	body.closed = true
	return nil
}

type failingReader struct {
	remaining int
}

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }

func (reader *failingReader) Read(buffer []byte) (int, error) {
	if reader.remaining == 0 {
		return 0, errors.New("synthetic read failure")
	}
	n := min(len(buffer), reader.remaining)
	for index := range n {
		buffer[index] = 'x'
	}
	reader.remaining -= n
	return n, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := f(r)
	if response != nil && response.Request == nil {
		response.Request = r
	}
	return response, err
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func noRetrySleep(context.Context, time.Duration) error { return nil }

func TestClientSuccessfulRequestsAdvanceRoundRobin(t *testing.T) {
	order := []int{}
	clients := make([]*http.Client, 3)
	for index := range clients {
		ordinal := index + 1
		clients[index] = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			order = append(order, ordinal)
			return response(http.StatusOK, "ok"), nil
		})}
	}
	client := &Client{clients: clients, unavailable: make([]bool, len(clients))}
	for range 4 {
		if _, err := client.Get(context.Background(), "showings", "https://www.ugc.fr/test"); err != nil {
			t.Fatal(err)
		}
	}
	if got := client.RequestCount(); got != 4 {
		t.Fatalf("request count=%d", got)
	}
	want := []int{1, 2, 3, 1}
	if len(order) != len(want) {
		t.Fatalf("order=%v", order)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("order=%v want=%v", order, want)
		}
	}
}

func TestClientTerminalResponseDoesNotRotate(t *testing.T) {
	first, second := 0, 0
	c := &Client{clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { first++; return response(403, "blocked"), nil })}, {Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { second++; return response(200, "ok"), nil })}}, unavailable: make([]bool, 2)}
	_, err := c.Get(context.Background(), "showings", "https://www.ugc.fr/test")
	var terminal *RequestError
	if !errors.As(err, &terminal) {
		t.Fatalf("error=%v", err)
	}
	if first != 1 || second != 0 {
		t.Fatalf("requests=%d,%d", first, second)
	}
}
func TestClientChallengeRedactsSyntheticSecret(t *testing.T) {
	secret := "synthetic-password"
	c := &Client{clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `<title>DataDome CAPTCHA</title>`+secret), nil
	})}}, unavailable: make([]bool, 1)}
	_, err := c.Get(context.Background(), "cinema", "https://www.ugc.fr/test")
	if err == nil {
		t.Fatal("challenge accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("response content leaked")
	}
}
func TestClientRetriesOneServerFailure(t *testing.T) {
	calls := 0
	c := &Client{clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(500, "temporary"), nil })}, {Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, "ok"), nil })}}, unavailable: make([]bool, 2), sleep: noRetrySleep}
	result, err := c.Get(context.Background(), "sitemap", "https://www.ugc.fr/test")
	if err != nil || string(result.Body) != "ok" || result.FinalURL != "https://www.ugc.fr/test" || calls != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestClientBalancesTenConcurrentFirstAttemptsAcrossFiveProxies(t *testing.T) {
	started := make(chan int, 10)
	release := make(chan struct{})
	clients := make([]*http.Client, 5)
	for index := range clients {
		ordinal := index + 1
		clients[index] = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			started <- ordinal
			select {
			case <-release:
				return response(http.StatusOK, "ok"), nil
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		})}
	}
	client := &Client{clients: clients, unavailable: make([]bool, len(clients))}
	errors := make(chan error, 10)
	for range 10 {
		go func() {
			_, err := client.Get(context.Background(), "showings", "https://www.ugc.fr/test")
			errors <- err
		}()
	}
	counts := make([]int, len(clients))
	for range 10 {
		counts[(<-started)-1]++
	}
	for index, count := range counts {
		if count != maxRequestsPerProxy {
			t.Fatalf("proxy %d starts=%d all=%v", index+1, count, counts)
		}
	}
	close(release)
	for range 10 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
}

func TestClientLimitsEachProxyToTwoConcurrentRequests(t *testing.T) {
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	client := &Client{clients: []*http.Client{{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		started <- struct{}{}
		select {
		case <-release:
			return response(http.StatusOK, "ok"), nil
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
	})}}, unavailable: make([]bool, 1)}
	errors := make(chan error, 3)
	for range 3 {
		go func() {
			_, err := client.Get(context.Background(), "showings", "https://www.ugc.fr/test")
			errors <- err
		}()
	}
	for range maxRequestsPerProxy {
		<-started
	}
	select {
	case <-started:
		t.Fatal("third request started while both proxy slots occupied")
	default:
	}
	close(release)
	for range 3 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
}

func TestClientRetriesFourDistinctProxiesWithExactBackoff(t *testing.T) {
	clients := []*http.Client{
		{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("transport") })},
		{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusBadGateway, "temporary"), nil })},
		{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("transport") })},
		{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusOK, "ok"), nil })},
	}
	delays := []time.Duration{}
	client := &Client{clients: clients, unavailable: make([]bool, 4), sleep: func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	}}
	result, err := client.Get(context.Background(), "showings", "https://www.ugc.fr/test")
	wantDelays := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}
	if err != nil || string(result.Body) != "ok" || client.RequestCount() != 4 || len(delays) != len(wantDelays) {
		t.Fatalf("result=%+v count=%d delays=%v error=%v", result, client.RequestCount(), delays, err)
	}
	for index := range wantDelays {
		if delays[index] != wantDelays[index] {
			t.Fatalf("delays=%v want=%v", delays, wantDelays)
		}
	}
}

func TestClientRetryAttemptBoundAndEarlyExhaustion(t *testing.T) {
	for _, proxyCount := range []int{1, 2, 4, 5} {
		t.Run(string(rune('0'+proxyCount))+" proxies", func(t *testing.T) {
			var calls atomic.Int32
			clients := make([]*http.Client, proxyCount)
			for index := range clients {
				clients[index] = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls.Add(1)
					return response(http.StatusServiceUnavailable, "temporary"), nil
				})}
			}
			delays := []time.Duration{}
			client := &Client{clients: clients, unavailable: make([]bool, proxyCount), sleep: func(_ context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				return nil
			}}
			if _, err := client.Get(context.Background(), "showings", "https://www.ugc.fr/test"); err == nil {
				t.Fatal("all-failure request succeeded")
			}
			wantAttempts := min(proxyCount, maxRequestAttempts)
			if int(calls.Load()) != wantAttempts || client.RequestCount() != wantAttempts || len(delays) != wantAttempts-1 {
				t.Fatalf("calls=%d count=%d delays=%v want attempts=%d", calls.Load(), client.RequestCount(), delays, wantAttempts)
			}
		})
	}
}

func TestClientCancellationDuringBackoffStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sleepStarted := make(chan struct{})
	var retries atomic.Int32
	client := &Client{
		clients: []*http.Client{
			{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("transport") })},
			{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				retries.Add(1)
				return response(http.StatusOK, "unexpected"), nil
			})},
		},
		unavailable: make([]bool, 2),
		sleep: func(ctx context.Context, _ time.Duration) error {
			close(sleepStarted)
			<-ctx.Done()
			return ctx.Err()
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := client.Get(ctx, "showings", "https://www.ugc.fr/test")
		done <- err
	}()
	<-sleepStarted
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) || client.RequestCount() != 1 || retries.Load() != 0 {
		t.Fatalf("error=%v count=%d retries=%d", err, client.RequestCount(), retries.Load())
	}
}

func TestClientCancellationDuringCapacityWaitStopsSafely(t *testing.T) {
	requestStarted := make(chan struct{}, maxRequestsPerProxy)
	release := make(chan struct{})
	client := &Client{clients: []*http.Client{{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestStarted <- struct{}{}
		select {
		case <-release:
			return response(http.StatusOK, "ok"), nil
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
	})}}, unavailable: make([]bool, 1)}
	activeDone := make(chan error, maxRequestsPerProxy)
	for range maxRequestsPerProxy {
		go func() {
			_, err := client.Get(context.Background(), "showings", "https://www.ugc.fr/test")
			activeDone <- err
		}()
	}
	for range maxRequestsPerProxy {
		<-requestStarted
	}
	waitCtx, cancel := context.WithCancel(context.Background())
	waitDone := make(chan error, 1)
	go func() {
		_, err := client.Get(waitCtx, "showings", "https://www.ugc.fr/test")
		waitDone <- err
	}()
	cancel()
	if err := <-waitDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error=%v", err)
	}
	close(release)
	for range maxRequestsPerProxy {
		if err := <-activeDone; err != nil {
			t.Fatal(err)
		}
	}
	if client.RequestCount() != maxRequestsPerProxy {
		t.Fatalf("count=%d", client.RequestCount())
	}
}

func TestClientTerminalResponsesNeverRetryOrSleep(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "forbidden", status: http.StatusForbidden, body: "blocked"},
		{name: "rate limited", status: http.StatusTooManyRequests, body: "blocked"},
		{name: "challenge", status: http.StatusOK, body: `<title>DataDome CAPTCHA</title>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var sleeps atomic.Int32
			client := &Client{
				clients:     []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(test.status, test.body), nil })}, {Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("terminal response retried"); return nil, nil })}},
				unavailable: make([]bool, 2),
				sleep:       func(context.Context, time.Duration) error { sleeps.Add(1); return nil },
			}
			_, err := client.Get(context.Background(), "showings", "https://www.ugc.fr/test")
			var terminal *RequestError
			if !errors.As(err, &terminal) || client.RequestCount() != 1 || sleeps.Load() != 0 {
				t.Fatalf("error=%v count=%d sleeps=%d", err, client.RequestCount(), sleeps.Load())
			}
		})
	}
}

func TestClientConcurrentRequestCount(t *testing.T) {
	const requests = 40
	clients := make([]*http.Client, 2)
	for index := range clients {
		clients[index] = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusOK, "ok"), nil })}
	}
	client := &Client{clients: clients, unavailable: make([]bool, 2)}
	var workers sync.WaitGroup
	workers.Add(requests)
	for range requests {
		go func() {
			defer workers.Done()
			if _, err := client.Get(context.Background(), "showings", "https://www.ugc.fr/test"); err != nil {
				t.Errorf("Get: %v", err)
			}
		}()
	}
	workers.Wait()
	if client.RequestCount() != requests {
		t.Fatalf("count=%d want=%d", client.RequestCount(), requests)
	}
}

func TestClientUsesContentLengthHintAndClosesBody(t *testing.T) {
	body := &trackingReadCloser{Reader: strings.NewReader("public response")}
	client := &Client{clients: []*http.Client{{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: body, ContentLength: int64(len("public response")), Request: request}, nil
	})}}, unavailable: make([]bool, 1)}
	result, err := client.Get(context.Background(), "showings", "https://www.ugc.fr/test")
	if err != nil || string(result.Body) != "public response" || !body.closed {
		t.Fatalf("result=%+v closed=%v error=%v", result, body.closed, err)
	}
}

func TestReadBoundedContentLengthIsOnlyAHint(t *testing.T) {
	tests := []struct {
		name          string
		bodyBytes     int
		contentLength int64
		wantError     bool
	}{
		{name: "honest known length", bodyBytes: 1024, contentLength: 1024},
		{name: "unknown length", bodyBytes: 1024, contentLength: -1},
		{name: "understated length", bodyBytes: 2048, contentLength: 32},
		{name: "overstated length", bodyBytes: 1024, contentLength: maxResponseBytes * 2},
		{name: "exact limit", bodyBytes: maxResponseBytes, contentLength: maxResponseBytes},
		{name: "limit plus one", bodyBytes: maxResponseBytes + 1, contentLength: 1, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := readBounded(strings.NewReader(strings.Repeat("x", test.bodyBytes)), test.contentLength)
			if test.wantError {
				if !errors.Is(err, errResponseLarge) {
					t.Fatalf("body=%d error=%v", len(body), err)
				}
				return
			}
			if err != nil || len(body) != test.bodyBytes {
				t.Fatalf("body=%d want=%d error=%v", len(body), test.bodyBytes, err)
			}
		})
	}
}

func TestReadBoundedReturnsReadFailure(t *testing.T) {
	_, err := readBounded(&failingReader{remaining: 700}, 700)
	if err == nil || err.Error() != "synthetic read failure" {
		t.Fatalf("error=%v", err)
	}
}

func TestClientExposesSanitizedFinalRedirectURL(t *testing.T) {
	calls := 0
	c := &Client{clients: []*http.Client{{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Path == "/cinema.html" {
			result := response(http.StatusFound, "redirect")
			result.Header.Set("Location", "https://www.ugc.fr/cinemas.html?id=1")
			return result, nil
		}
		return response(http.StatusOK, "directory"), nil
	})}}, unavailable: make([]bool, 1)}
	result, err := c.Get(context.Background(), OperationCinema, "https://www.ugc.fr/cinema.html?id=2")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || string(result.Body) != "directory" || result.FinalURL != "https://www.ugc.fr/cinemas.html?id=1" {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestClientRejectsNonExactInitialAuthority(t *testing.T) {
	urls := []string{
		"https://synthetic-user:synthetic-password@www.ugc.fr/test",
		"https://www.ugc.fr:443/test",
		"https://www.ugc.fr:8443/test",
		"https://ugc.fr/test",
	}
	for _, raw := range urls {
		t.Run(raw, func(t *testing.T) {
			calls := 0
			client := &Client{clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return response(200, "ok"), nil
			})}}, unavailable: make([]bool, 1)}
			if _, err := client.Get(context.Background(), "test", raw); err == nil {
				t.Fatal("non-exact authority accepted")
			}
			if calls != 0 {
				t.Fatalf("transport calls=%d", calls)
			}
		})
	}
}

func TestRedirectPolicyRejectsCrossAuthority(t *testing.T) {
	invalid := []string{
		"https://synthetic-user@www.ugc.fr/next",
		"https://www.ugc.fr:443/next",
		"https://www.ugc.fr:8443/next",
		"https://assets.ugc.fr/next",
		"https://example.test/next",
	}
	for _, raw := range invalid {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkUGCRedirect(&http.Request{URL: parsed}, []*http.Request{{URL: parsed}}); !errors.Is(err, errRedirectHost) {
			t.Fatalf("redirect %q error=%v", raw, err)
		}
	}
	allowed, _ := url.Parse("https://www.ugc.fr/next")
	if err := checkUGCRedirect(&http.Request{URL: allowed}, []*http.Request{{URL: allowed}}); err != nil {
		t.Fatalf("valid redirect rejected: %v", err)
	}
}

func TestClientReturnsBoundedTypedDiagnostics(t *testing.T) {
	const secret = "https://user:proxy-password@proxy.example/path?token=token-secret cookie=session-secret body=provider-body-secret cause=underlying-secret"
	tests := []struct {
		name      string
		operation Operation
		rawURL    string
		clients   []*http.Client
		context   func() context.Context
		category  ErrorCategory
		status    int
		attempt   int
		wantCalls int
	}{
		{name: "invalid URL", operation: OperationShowings, rawURL: secret, category: CategoryInvalidURL},
		{name: "invalid operation", operation: Operation(secret), rawURL: "https://www.ugc.fr/test", category: CategoryInvalidURL},
		{name: "canceled", operation: OperationCinema, rawURL: "https://www.ugc.fr/test", context: func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }, clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("canceled request sent"); return nil, nil })}}, category: CategoryCanceled},
		{name: "transport", operation: OperationCinema, rawURL: "https://www.ugc.fr/test", clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New(secret) })}, {Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New(secret) })}}, category: CategoryTransport, attempt: 2, wantCalls: 2},
		{name: "redirect", operation: OperationCinema, rawURL: "https://www.ugc.fr/test", clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errRedirectHost })}}, category: CategoryRedirect, attempt: 1, wantCalls: 1},
		{name: "forbidden", operation: OperationShowings, rawURL: "https://www.ugc.fr/test", clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusForbidden, secret), nil })}}, category: CategoryHTTPStatus, status: 403, attempt: 1, wantCalls: 1},
		{name: "rate limited", operation: OperationShowings, rawURL: "https://www.ugc.fr/test", clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusTooManyRequests, secret), nil })}}, category: CategoryHTTPStatus, status: 429, attempt: 1, wantCalls: 1},
		{name: "server exhausted", operation: OperationSitemap, rawURL: "https://www.ugc.fr/test", clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusServiceUnavailable, secret), nil
		})}}, category: CategoryHTTPStatus, status: 503, attempt: 1, wantCalls: 1},
		{name: "challenge", operation: OperationCinema, rawURL: "https://www.ugc.fr/test", clients: []*http.Client{{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, `<title>DataDome CAPTCHA</title>`+secret), nil
		})}}, category: CategoryChallenge, attempt: 1, wantCalls: 1},
		{name: "response read", operation: OperationCinema, rawURL: "https://www.ugc.fr/test", clients: []*http.Client{{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(errorReader{err: errors.New(secret)}), Request: request}, nil
		})}}, category: CategoryResponseRead, attempt: 1, wantCalls: 1},
		{name: "response too large", operation: OperationCinema, rawURL: "https://www.ugc.fr/test", clients: []*http.Client{{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(errorReader{err: errResponseLarge}), Request: request}, nil
		})}}, category: CategoryResponseLarge, attempt: 1, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.context != nil {
				ctx = test.context()
			}
			client := &Client{clients: test.clients, unavailable: make([]bool, len(test.clients)), sleep: noRetrySleep}
			_, err := client.Get(ctx, test.operation, test.rawURL)
			var requestErr *RequestError
			if !errors.As(err, &requestErr) || requestErr.Category != test.category || requestErr.StatusCode != test.status || requestErr.Attempt != test.attempt || client.RequestCount() != test.wantCalls {
				t.Fatalf("error=%+v count=%d", requestErr, client.RequestCount())
			}
			if test.operation == Operation(secret) && requestErr.Operation != OperationUnknown {
				t.Fatalf("unbounded operation=%q", requestErr.Operation)
			}
			if test.attempt > 0 && requestErr.AttemptLimit != maxRequestAttempts || test.attempt == 0 && requestErr.AttemptLimit != 0 {
				t.Fatalf("attempt=%d/%d", requestErr.Attempt, requestErr.AttemptLimit)
			}
			for _, forbidden := range []string{secret, "proxy-password", "token-secret", "session-secret", "provider-body-secret", "underlying-secret"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("error leaked %q: %s", forbidden, err)
				}
			}
		})
	}
}
