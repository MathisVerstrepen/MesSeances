package cgr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestClientAcceptsBoundedJSONAndRedactsBody(t *testing.T) {
	client, err := NewClient(ClientConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.clients[0].Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatalf("direct transport=%T proxy_configured=%v", client.clients[0].Transport, ok && transport.Proxy != nil)
	}
	client.clients[0].Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Referer") != APIBaseURL+"/" || request.Header.Get("User-Agent") != chromeUserAgent || request.Header.Get("Sec-CH-UA") == "" {
			t.Fatalf("browser headers are incomplete")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"scheduledDays":{}}`)), Request: request}, nil
	})
	body, err := client.Get(context.Background(), OperationProgram, programURL("W8010"))
	if err != nil || string(body) != `{"scheduledDays":{}}` || client.RequestCount() != 1 {
		t.Fatalf("body=%q count=%d err=%v", body, client.RequestCount(), err)
	}
	_, err = client.Get(context.Background(), OperationProgram, "https://evil.example/api?secret=synthetic-secret")
	if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatalf("error=%v", err)
	}
}

func TestOperationURLValidationAndMovieLimit(t *testing.T) {
	if parsed, _ := url.Parse(programURL("W8010")); !operationMatchesURL(OperationProgram, parsed) {
		t.Fatal("valid program URL rejected")
	}
	if parsed, _ := url.Parse(scheduleURL("W8010", "Europe/Paris", "2026-08-25", "2026-09-01")); !operationMatchesURL(OperationSchedule, parsed) {
		t.Fatal("valid schedule URL rejected")
	}
	repeated := url.Values{
		"from":     {"2026-08-25T03:00:00"},
		"to":       {"2026-08-26T03:00:00"},
		"theaters": {`{"id":"W8010","timeZone":"Europe/Paris"}`, `{"id":"P0867","timeZone":"Europe/Paris"}`},
	}
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
	client, err := NewClient(ClientConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	client.sleep = func(context.Context, time.Duration) error { return nil }
	attempts := 0
	client.clients[0].Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("synthetic transport failure")
		}
		status := http.StatusInternalServerError
		body := `{}`
		if attempts == 3 {
			status = http.StatusOK
			body = `{"scheduledDays":{}}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	body, err := client.Get(context.Background(), OperationProgram, programURL("W8010"))
	if err != nil || attempts != 3 || client.RequestCount() != 3 || string(body) != `{"scheduledDays":{}}` {
		t.Fatalf("attempts=%d count=%d body=%q err=%v", attempts, client.RequestCount(), body, err)
	}
}

func TestClientRetriesOnlyTransientProgramNotFound(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation Operation
		statuses  []int
		wantCount int
		wantError bool
	}{
		{name: "program recovers", operation: OperationProgram, statuses: []int{http.StatusNotFound, http.StatusOK}, wantCount: 2},
		{name: "cinemas recovers", operation: OperationCinemas, statuses: []int{http.StatusNotFound, http.StatusOK}, wantCount: 2},
		{name: "program exhausts", operation: OperationProgram, statuses: []int{http.StatusNotFound, http.StatusNotFound, http.StatusNotFound, http.StatusNotFound}, wantCount: 4, wantError: true},
		{name: "movies does not retry", operation: OperationMovies, statuses: []int{http.StatusNotFound}, wantCount: 1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(ClientConfig{Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			client.sleep = func(context.Context, time.Duration) error { return nil }
			attempt := 0
			client.clients[0].Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				status := test.statuses[attempt]
				attempt++
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: request}, nil
			})
			rawURL := programURL("W8010")
			switch test.operation {
			case OperationCinemas:
				rawURL = CinemasURL
			case OperationMovies:
				rawURL = moviesURL([]string{"1001"})
			}
			_, err = client.Get(context.Background(), test.operation, rawURL)
			if (err != nil) != test.wantError || attempt != test.wantCount {
				t.Fatalf("attempts=%d err=%v", attempt, err)
			}
		})
	}
}

func TestClientRejectsUnsafeResponsesWithoutLeakingBodies(t *testing.T) {
	tests := []struct {
		name     string
		response func(*http.Request) *http.Response
		category ErrorCategory
	}{
		{name: "oversized", category: CategoryResponseLarge, response: func(request *http.Request) *http.Response {
			return &http.Response{StatusCode: http.StatusOK, ContentLength: MaxResponseBytes + 1, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("synthetic-secret")), Request: request}
		}},
		{name: "content type", category: CategoryContentType, response: func(request *http.Request) *http.Response {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("synthetic-secret")), Request: request}
		}},
		{name: "final URL", category: CategoryRedirect, response: func(request *http.Request) *http.Response {
			redirected := request.Clone(request.Context())
			redirected.URL, _ = url.Parse("https://evil.example/synthetic-secret")
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: redirected}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(ClientConfig{Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			client.sleep = func(context.Context, time.Duration) error { return nil }
			client.clients[0].Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) { return test.response(request), nil })
			_, err = client.Get(context.Background(), OperationProgram, programURL("W8010"))
			var requestErr *RequestError
			if !errors.As(err, &requestErr) || requestErr.Category != test.category || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
