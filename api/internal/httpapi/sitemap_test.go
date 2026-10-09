package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type sitemapObserverFunc func(context.Context) (schedule.SitemapData, error)

func (f sitemapObserverFunc) ObserveSitemapData(ctx context.Context) (schedule.SitemapData, error) {
	return f(ctx)
}

func TestSitemapDataCoherentContractWithoutSchedule(t *testing.T) {
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	observer := sitemapObserverFunc(func(ctx context.Context) (schedule.SitemapData, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > sitemapReadTimeout {
			t.Fatal("missing bounded context")
		}
		return schedule.SitemapData{AsOf: now, Revision: "schedule:0;enrichment:9007199254740993;location:4", Movies: []schedule.MovieCatalogItem{{Slug: "film-1", Title: "Catalogue only"}}, MovieTotal: 1, LastmodByPath: map[string]*time.Time{"/film/film-1": nil}}, nil
	})
	handler := NewHandlerWithOptions(nil, "http://localhost:3000", HandlerOptions{Sitemap: observer})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/sitemap-data", nil))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 7 || string(body["cities"]) != "null" || string(body["lastmod_by_path"]) != `{"/film/film-1":null}` || !strings.Contains(string(body["revision"]), "9007199254740993") {
		t.Fatalf("body=%s", response.Body)
	}
	if strings.Contains(response.Body.String(), "fingerprint") {
		t.Fatal("fingerprints exposed")
	}
}

func TestSitemapDataErrorsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		observer  SitemapObserver
		cancel    bool
		status    int
	}{
		{name: "missing repository", url: "/api/v1/sitemap-data", status: http.StatusServiceUnavailable},
		{name: "load failed", url: "/api/v1/sitemap-data", observer: sitemapObserverFunc(func(context.Context) (schedule.SitemapData, error) {
			return schedule.SitemapData{}, errors.New("private-failure")
		}), status: http.StatusServiceUnavailable},
		{name: "query rejected", url: "/api/v1/sitemap-data?path=/film/evil", status: http.StatusBadRequest},
		{name: "cancelled", url: "/api/v1/sitemap-data", observer: sitemapObserverFunc(func(ctx context.Context) (schedule.SitemapData, error) {
			if ctx.Err() == nil {
				t.Fatal("request cancellation lost")
			}
			return schedule.SitemapData{}, ctx.Err()
		}), cancel: true, status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewHandlerWithOptions(nil, "http://localhost:3000", HandlerOptions{Sitemap: tc.observer})
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.url, nil)
			if tc.cancel {
				ctx, cancel := context.WithCancel(request.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "private-failure") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}

func TestSitemapDataExpensiveReadLimiterAndMethod(t *testing.T) {
	now := time.Now()
	calls := 0
	observer := sitemapObserverFunc(func(context.Context) (schedule.SitemapData, error) {
		calls++
		return schedule.SitemapData{Movies: []schedule.MovieCatalogItem{}, LastmodByPath: map[string]*time.Time{}}, nil
	})
	handler := NewHandlerWithOptions(nil, "http://localhost:3000", HandlerOptions{Sitemap: observer, RateLimitClock: func() time.Time { return now }})
	for i := 0; i <= expensiveReadBurst; i++ {
		response := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/sitemap-data", nil)
		request.RemoteAddr = "192.0.2.7:4321"
		handler.ServeHTTP(response, request)
		want := http.StatusOK
		if i == expensiveReadBurst {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("request %d status %d", i, response.Code)
		}
		if want == http.StatusTooManyRequests && response.Header().Get("Retry-After") == "" {
			t.Fatal("retry absent")
		}
	}
	if calls != expensiveReadBurst {
		t.Fatal("limited request reached repository")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/sitemap-data", strings.NewReader(`{"path":"evil"}`)))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", response.Code)
	}
}
