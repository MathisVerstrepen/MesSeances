package cgr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
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
	err     error
	closed  bool
	ctx     context.Context
	started chan struct{}
}

func (r *failingReadCloser) Read([]byte) (int, error) {
	if r.started != nil {
		close(r.started)
		<-r.ctx.Done()
	}
	return 0, r.err
}

func (r *failingReadCloser) Close() error { r.closed = true; return nil }

func testClient(t *testing.T, proxyBacked bool, sleep func(context.Context, time.Duration) error, transports ...http.RoundTripper) *Client {
	t.Helper()
	clients := make([]*http.Client, len(transports))
	for index, transport := range transports {
		clients[index] = &http.Client{Transport: transport, CheckRedirect: checkRedirect}
	}
	client, err := newClientWithHTTPClients(clients, proxyBacked, sleep)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func noWait(context.Context, time.Duration) error { return nil }

func testResponse(status int, mediaType, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {mediaType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestClientAcceptsBoundedJSONAndRedactsBody(t *testing.T) {
	client, err := NewClient(ClientConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if client.RequestCount() != 0 {
		t.Fatalf("requests=%d", client.RequestCount())
	}
	for _, mediaType := range []string{"application/json", "text/problem+json"} {
		t.Run(mediaType, func(t *testing.T) {
			client := testClient(t, false, noWait, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Header.Get("Referer") != APIBaseURL+"/" || request.Header.Get("User-Agent") != chromeUserAgent || request.Header.Get("Sec-CH-UA") == "" {
					t.Fatalf("headers=%v", request.Header)
				}
				return testResponse(http.StatusOK, mediaType, `{"scheduledDays":{}}`), nil
			}))
			body, err := client.Get(t.Context(), OperationProgram, programURL("W8010"))
			if err != nil || string(body) != `{"scheduledDays":{}}` || client.RequestCount() != 1 {
				t.Fatalf("body=%q count=%d err=%v", body, client.RequestCount(), err)
			}
			_, err = client.Get(t.Context(), OperationProgram, "https://evil.example/api?secret=synthetic-secret")
			if err == nil || strings.Contains(err.Error(), "synthetic-secret") || client.RequestCount() != 1 {
				t.Fatalf("count=%d err=%v", client.RequestCount(), err)
			}
		})
	}
}

func TestOperationURLValidationAndMovieLimit(t *testing.T) {
	if parsed, _ := url.Parse(programURL("W8010")); !operationMatchesURL(OperationProgram, parsed) {
		t.Fatal("valid program URL rejected")
	}
	if parsed, _ := url.Parse(scheduleURL("W8010", "Europe/Paris", "2026-08-25", "2026-09-01")); !operationMatchesURL(OperationSchedule, parsed) {
		t.Fatal("valid schedule URL rejected")
	}
	repeated := url.Values{"from": {"2026-08-25T03:00:00"}, "to": {"2026-08-26T03:00:00"}, "theaters": {`{"id":"W8010","timeZone":"Europe/Paris"}`, `{"id":"P0867","timeZone":"Europe/Paris"}`}}
	if parsed, _ := url.Parse(APIBaseURL + "/api/gatsby-source-boxofficeapi/schedule?" + repeated.Encode()); !operationMatchesURL(OperationSchedule, parsed) {
		t.Fatal("valid repeated theater objects rejected")
	}
	invalidSchedules := []url.Values{
		{"from": {"2026-08-25"}, "to": {"2026-08-26"}, "theaters": {`{"id":"W8010","timeZone":"Europe/Paris"}`}},
		{"from": {"2026-08-25T03:00:00"}, "to": {"2026-08-26T03:00:00"}, "theaters": {`["W8010"]`}},
		{"from": {"2026-08-25T03:00:00"}, "to": {"2026-08-26T03:00:00"}, "theaters": {`{"id":"W8010","timeZone":"UTC"}`}},
		{"from": {"2026-08-25T03:00:00"}, "to": {"2026-08-26T03:00:00"}, "theaters": {`{"id":"W8010","timeZone":"Europe/Paris","extra":true}`}},
		{"from": {"2026-08-25T03:00:00"}, "to": {"2026-08-26T03:00:00"}, "theaters": {`{"id":"W8010","timeZone":"Europe/Paris"}`}, "includeAllMovies": {"true"}},
	}
	for _, query := range invalidSchedules {
		parsed, _ := url.Parse(APIBaseURL + "/api/gatsby-source-boxofficeapi/schedule?" + query.Encode())
		if operationMatchesURL(OperationSchedule, parsed) {
			t.Fatalf("invalid schedule query accepted: keys=%v", sortedQueryKeys(query))
		}
	}
	ids := make([]string, 51)
	for index := range ids {
		ids[index] = fmt.Sprint(index + 1)
	}
	if parsed, _ := url.Parse(moviesURL(ids)); operationMatchesURL(OperationMovies, parsed) {
		t.Fatal("51-ID movie URL accepted")
	}
	if parsed, _ := url.Parse(moviesURL([]string{"1" + strings.Repeat("0", 128)})); operationMatchesURL(OperationMovies, parsed) {
		t.Fatal("oversized movie identity accepted")
	}
}

func sortedQueryKeys(query url.Values) []string {
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestClientRetriesDirectTransportAndServerFailures(t *testing.T) {
	attempts := 0
	client := testClient(t, false, noWait, roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("synthetic transport failure")
		}
		if attempts == 2 {
			return testResponse(http.StatusInternalServerError, "application/json", `{}`), nil
		}
		return testResponse(http.StatusOK, "application/json", `{"scheduledDays":{}}`), nil
	}))
	body, err := client.Get(t.Context(), OperationProgram, programURL("W8010"))
	if err != nil || attempts != 3 || client.RequestCount() != 3 || string(body) != `{"scheduledDays":{}}` {
		t.Fatalf("attempts=%d count=%d body=%q err=%v", attempts, client.RequestCount(), body, err)
	}
}

func TestClientRetriesOnlyTransientProgramNotFound(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation Operation
		statuses  []int
		wantError bool
	}{
		{"program recovers", OperationProgram, []int{http.StatusNotFound, http.StatusOK}, false},
		{"cinemas recovers", OperationCinemas, []int{http.StatusNotFound, http.StatusOK}, false},
		{"program exhausts", OperationProgram, []int{404, 404, 404, 404}, true},
		{"movies terminal", OperationMovies, []int{http.StatusNotFound}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempt := 0
			client := testClient(t, false, noWait, roundTripFunc(func(*http.Request) (*http.Response, error) {
				status := test.statuses[attempt]
				attempt++
				return testResponse(status, "application/json", `{}`), nil
			}))
			rawURL := programURL("W8010")
			switch test.operation {
			case OperationCinemas:
				rawURL = CinemasURL
			case OperationMovies:
				rawURL = moviesURL([]string{"1001"})
			}
			_, err := client.Get(t.Context(), test.operation, rawURL)
			if (err != nil) != test.wantError || attempt != len(test.statuses) {
				t.Fatalf("attempts=%d err=%v", attempt, err)
			}
		})
	}
}

func TestClientRejectsUnsafeResponsesWithoutLeakingBodies(t *testing.T) {
	for _, test := range []struct {
		name     string
		response func(*http.Request) *http.Response
		category ErrorCategory
	}{
		{"oversized", func(*http.Request) *http.Response {
			response := testResponse(http.StatusOK, "application/json", "synthetic-secret")
			response.ContentLength = MaxResponseBytes + 1
			return response
		}, CategoryResponseLarge},
		{"content type", func(*http.Request) *http.Response {
			return testResponse(http.StatusOK, "text/html", "synthetic-secret")
		}, CategoryContentType},
		{"final URL", func(request *http.Request) *http.Response {
			redirected := request.Clone(request.Context())
			redirected.URL, _ = url.Parse("https://evil.example/synthetic-secret")
			response := testResponse(http.StatusOK, "application/json", `{}`)
			response.Request = redirected
			return response
		}, CategoryRedirect},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, false, noWait, roundTripFunc(func(request *http.Request) (*http.Response, error) { return test.response(request), nil }))
			_, err := client.Get(t.Context(), OperationProgram, programURL("W8010"))
			var requestErr *RequestError
			if !errors.As(err, &requestErr) || requestErr.Category != test.category || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestClientRetryCursorPolicy(t *testing.T) {
	order := []int{}
	transports := make([]http.RoundTripper, 3)
	for ordinal := range transports {
		transports[ordinal] = roundTripFunc(func(*http.Request) (*http.Response, error) {
			order = append(order, ordinal)
			if ordinal == 0 {
				return testResponse(http.StatusInternalServerError, "application/json", `{}`), nil
			}
			return testResponse(http.StatusOK, "application/json", `{}`), nil
		})
	}
	client := testClient(t, true, noWait, transports...)
	for range 2 {
		if _, err := client.Get(t.Context(), OperationProgram, programURL("W8010")); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(order, []int{0, 1, 2}) {
		t.Fatalf("order=%v", order)
	}
}

func TestClientProxyResponseReadPolicy(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation Operation
		status    int
		cancel    bool
	}{
		{"canceled read", OperationProgram, http.StatusOK, true},
		{"unreadable 404 before status", OperationMovies, http.StatusNotFound, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			cancel := func() {}
			body := &failingReadCloser{err: errors.New("synthetic read failure")}
			if test.cancel {
				ctx, cancel = context.WithCancel(ctx)
				body.ctx, body.started = ctx, make(chan struct{})
			}
			defer cancel()
			client := testClient(t, true, noWait, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: body, ContentLength: -1}, nil
			}))
			rawURL := programURL("W8010")
			if test.operation == OperationMovies {
				rawURL = moviesURL([]string{"1001"})
			}
			result := make(chan error, 1)
			go func() { _, err := client.Get(ctx, test.operation, rawURL); result <- err }()
			if test.cancel {
				<-body.started
				cancel()
			}
			err := <-result
			var requestErr *RequestError
			if !errors.As(err, &requestErr) || requestErr.Category != CategoryResponseRead || requestErr.StatusCode != 0 || errors.Unwrap(requestErr) != nil || !body.closed || client.RequestCount() != 1 {
				t.Fatalf("closed=%v count=%d err=%v", body.closed, client.RequestCount(), err)
			}
		})
	}
}

func TestClientFinalRetryFailureRetainsFinalProxy(t *testing.T) {
	const attempts = 4
	calls := make([]int, attempts)
	transports := make([]http.RoundTripper, attempts)
	for ordinal := range transports {
		transports[ordinal] = roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls[ordinal]++
			if ordinal == attempts-1 && calls[ordinal] == 2 {
				return testResponse(http.StatusOK, "application/json", `{}`), nil
			}
			return testResponse(http.StatusBadGateway, "application/json", `{}`), nil
		})
	}
	client := testClient(t, true, noWait, transports...)
	_, err := client.Get(t.Context(), OperationProgram, programURL("W8010"))
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.StatusCode != http.StatusBadGateway || client.RequestCount() != attempts {
		t.Fatalf("count=%d err=%v", client.RequestCount(), err)
	}
	body, err := client.Get(t.Context(), OperationProgram, programURL("W8010"))
	if err != nil || string(body) != `{}` || client.RequestCount() != attempts+1 {
		t.Fatalf("calls=%v body=%q err=%v", calls, body, err)
	}
}

func TestClientNoClientBeforeAttempt(t *testing.T) {
	client := testClient(t, true, noWait)
	_, err := client.Get(t.Context(), OperationProgram, programURL("W8010"))
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.Category != CategoryNoClient || requestErr.StatusCode != 0 || errors.Unwrap(requestErr) != nil || client.RequestCount() != 0 {
		t.Fatalf("count=%d err=%v", client.RequestCount(), err)
	}
}

func TestClientPostWaitNoClientPreservesCompleteFailure(t *testing.T) {
	waiting, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	response := func(status int) roundTripFunc {
		return func(*http.Request) (*http.Response, error) {
			return testResponse(status, "application/json", `{}`), nil
		}
	}
	client := testClient(t, true, func(context.Context, time.Duration) error { once.Do(func() { close(waiting) }); <-release; return nil }, response(http.StatusBadGateway), response(http.StatusServiceUnavailable))
	result := make(chan error, 1)
	go func() { _, err := client.Get(t.Context(), OperationProgram, programURL("W8010")); result <- err }()
	<-waiting
	_, concurrentErr := client.Get(t.Context(), OperationProgram, programURL("W8010"))
	var concurrentRequestErr *RequestError
	if !errors.As(concurrentErr, &concurrentRequestErr) || concurrentRequestErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("concurrent=%v", concurrentErr)
	}
	close(release)
	err := <-result
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.Category != CategoryServer || requestErr.StatusCode != http.StatusBadGateway || errors.Unwrap(requestErr) != nil || client.RequestCount() != 2 {
		t.Fatalf("count=%d err=%v", client.RequestCount(), err)
	}
}
