package ign

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/geocoding"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func response(status int, contentType, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestClientExactQueryMappingRateAndRetry(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	requests := 0
	var sleeps []time.Duration
	transport := roundTripper(func(request *http.Request) (*http.Response, error) {
		requests++
		query := request.URL.Query()
		if requests <= 2 && (query.Get("q") != "40 rue de Béthune 59000 Lille" || query.Get("postcode") != "59000" || query.Has("city") || query.Get("index") != "address" || query.Get("autocomplete") != "0" || query.Get("limit") != "3") {
			t.Fatalf("query=%v", query)
		}
		if requests == 1 {
			return response(503, "text/plain", "synthetic secret body"), nil
		}
		return response(200, "application/json; charset=utf-8", `{"type":"FeatureCollection","features":[{"geometry":{"type":"Point","coordinates":[3.0612,50.6321]},"properties":{"label":"40 Rue de Béthune","score":0.91,"postcode":"59000","city":"Lille","type":"housenumber"}}]}`), nil
	})
	client, err := NewClient(Config{Timeout: 5 * time.Second, Transport: transport, Now: func() time.Time { return now }, Sleep: func(_ context.Context, duration time.Duration) error {
		sleeps = append(sleeps, duration)
		now = now.Add(duration)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := client.Search(context.Background(), geocoding.Query{Address: "40 rue de Béthune", PostalCode: "59000", City: "Lille"})
	if err != nil || requests != 2 || len(candidates) != 1 || candidates[0].Longitude != 3.0612 || candidates[0].Latitude != 50.6321 || len(sleeps) != 1 || sleeps[0] != 500*time.Millisecond {
		t.Fatalf("requests=%d candidates=%+v sleeps=%v err=%v", requests, candidates, sleeps, err)
	}
	_, err = client.Search(context.Background(), geocoding.Query{Address: "1 rue", PostalCode: "59000", City: "Lille"})
	if err != nil || len(sleeps) != 2 || sleeps[1] != requestInterval {
		t.Fatalf("second request sleeps=%v err=%v", sleeps, err)
	}
}

func TestClientKeepsFreeFormCityOnlyInQueryText(t *testing.T) {
	transport := roundTripper(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("q") != "1 avenue du Cinéma 75014 Paris 14e - secteur Montparnasse" || query.Get("postcode") != "75014" || query.Has("city") {
			t.Fatalf("query=%v", query)
		}
		return response(200, "application/json", `{"type":"FeatureCollection","features":[]}`), nil
	})
	client, err := NewClient(Config{Timeout: 5 * time.Second, Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := client.Search(context.Background(), geocoding.Query{Address: "1 avenue du Cinéma", PostalCode: "75014", City: "Paris 14e - secteur Montparnasse"})
	if err != nil || len(candidates) != 0 {
		t.Fatalf("candidates=%+v err=%v", candidates, err)
	}
}

func TestClientRejectsTerminalAndInvalidResponsesWithoutLeakingBody(t *testing.T) {
	secret := "synthetic-provider-secret"
	for _, test := range []struct {
		name     string
		response *http.Response
	}{
		{name: "definitive 4xx", response: response(400, "text/plain", secret)},
		{name: "content type", response: response(200, "text/html", secret)},
		{name: "malformed", response: response(200, "application/json", `{`)},
		{name: "oversized", response: response(200, "application/json", strings.Repeat("x", maxResponseSize+1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client, err := NewClient(Config{Timeout: 5 * time.Second, Transport: roundTripper(func(*http.Request) (*http.Response, error) { calls++; return test.response, nil }), Sleep: func(context.Context, time.Duration) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Search(context.Background(), geocoding.Query{})
			if err == nil || calls != 1 || strings.Contains(err.Error(), secret) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestClientRetriesTransportErrorsAndHonorsCancellation(t *testing.T) {
	calls := 0
	client, _ := NewClient(Config{Timeout: 5 * time.Second, Transport: roundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("secret") }), Sleep: func(context.Context, time.Duration) error { return nil }})
	_, err := client.Search(context.Background(), geocoding.Query{})
	if err == nil || calls != 4 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	client, _ = NewClient(Config{Timeout: 5 * time.Second, Transport: roundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil })})
	if _, err := client.Search(canceled, geocoding.Query{}); err == nil {
		t.Fatal("canceled request succeeded")
	}
}
