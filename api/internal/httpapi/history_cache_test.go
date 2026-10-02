package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func warmHistoryCache(t *testing.T, result schedule.HistoryStatistics) *HistoryCache {
	t.Helper()
	cache := &HistoryCache{}
	if err := cache.Refresh(t.Context(), func(context.Context) (schedule.HistoryStatistics, error) { return result, nil }); err != nil {
		t.Fatal(err)
	}
	return cache
}

func cachedHistoryBody(cache *HistoryCache) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	cache.write(w)
	return w
}

func TestHistoryCacheHTTPHitAndCold(t *testing.T) {
	result := schedule.HistoryStatistics{Mode: "history", GeneratedAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Timezone: schedule.Timezone}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		cache *HistoryCache
		code  int
	}{
		{"missing", nil, http.StatusServiceUnavailable},
		{"cold", &HistoryCache{}, http.StatusServiceUnavailable},
		{"warm", warmHistoryCache(t, result), http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeHistoryReader{}
			h := NewHandlerWithOptions(nil, "", HandlerOptions{History: reader, HistoryCache: tc.cache})
			for _, suffix := range []string{"", "?", "?&&"} {
				r := performRequest(t, h, "/api/v1/statistics/history"+suffix)
				if r.Code != tc.code || reader.calls != 0 || r.Header().Get("Cache-Control") != "no-store" || r.Header().Get("Content-Type") != "application/json" {
					t.Fatal(r.Code, r.Header(), reader.calls)
				}
				if tc.code == http.StatusOK && r.Body.String() != string(encoded)+"\n" {
					t.Fatal("cached response changed schema or generated_at")
				}
				if tc.code == http.StatusServiceUnavailable && !strings.Contains(r.Body.String(), `"code":"history_unavailable"`) {
					t.Fatal(r.Body.String())
				}
			}
		})
	}
	// A warmed cache has no dependency on the live reader.
	r := performRequest(t, NewHandlerWithOptions(nil, "", HandlerOptions{HistoryCache: warmHistoryCache(t, result)}), "/api/v1/statistics/history")
	if r.Code != http.StatusOK {
		t.Fatal(r.Code)
	}
}

func TestHistoryCacheHTTPBypassAndValidation(t *testing.T) {
	cache := warmHistoryCache(t, schedule.HistoryStatistics{Mode: "cached"})
	for _, raw := range []string{
		"date=2026-01-01", "date=0001-01-01&date_to=9999-12-31", "date=2026-01-01&date_to=2026-01-02",
		"city=lille", "theater=ugc-25", "chain=ugc", "language=+VOF+", "format=IMAX", "genre=+DRAME+", "pass=ugc-illimite", "film=+film-1+",
	} {
		t.Run(raw, func(t *testing.T) {
			reader := &fakeHistoryReader{}
			h := NewHandlerWithOptions(nil, "", HandlerOptions{History: reader, HistoryCache: cache})
			r := performRequest(t, h, "/api/v1/statistics/history?"+raw)
			if r.Code != http.StatusOK || reader.calls != 1 || !strings.Contains(r.Body.String(), `"mode":"history"`) {
				t.Fatal(r.Code, r.Body.String(), reader.calls)
			}
		})
	}
	for _, raw := range []string{"date_to=2026-01-01", "date=", "date=bad", "chain=other", "language=", "city=+", "genre=x&genre=x", "unknown=x", "film=%00", "city=%FF", "city=%zz", strings.Repeat("&", 4097)} {
		t.Run(raw[:min(50, len(raw))], func(t *testing.T) {
			reader := &fakeHistoryReader{}
			h := NewHandlerWithOptions(nil, "", HandlerOptions{History: reader, HistoryCache: cache})
			r := performRequest(t, h, "/api/v1/statistics/history?"+raw)
			if r.Code != http.StatusBadRequest || reader.calls != 0 || !strings.Contains(r.Body.String(), `"code":"invalid_query"`) {
				t.Fatal(r.Code, r.Body.String(), reader.calls)
			}
		})
	}
	reader := &fakeHistoryReader{}
	h := NewHandlerWithOptions(nil, "", HandlerOptions{History: reader, HistoryCache: cache})
	r := performRequest(t, h, "/api/v1/statistics/history/options?kind=city")
	if r.Code != http.StatusOK || reader.calls != 1 || reader.options.Kind != "city" || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("options no longer live", r.Code, reader.calls)
	}
}

func TestHistoryCacheRefreshPreservesImmutableSnapshot(t *testing.T) {
	result := schedule.HistoryStatistics{Mode: "history", GeneratedAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Chains: []schedule.HistoryChainRank{{Chain: schedule.ProviderUGC, ShowtimeCount: 1}}}
	cache := warmHistoryCache(t, result)
	initial := cachedHistoryBody(cache).Body.String()
	result.Chains[0].ShowtimeCount = 99
	if cachedHistoryBody(cache).Body.String() != initial {
		t.Fatal("snapshot aliases reader slices")
	}
	for _, tc := range []struct {
		name string
		load func(context.Context) (schedule.HistoryStatistics, error)
	}{
		{"read failure", func(context.Context) (schedule.HistoryStatistics, error) { return result, errors.New("read failed") }},
		{"encoding failure", func(context.Context) (schedule.HistoryStatistics, error) {
			return schedule.HistoryStatistics{GeneratedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := cache.Refresh(t.Context(), tc.load); err == nil {
				t.Fatal("failed refresh accepted")
			}
			if cachedHistoryBody(cache).Body.String() != initial {
				t.Fatal("failed refresh replaced last good bytes/timestamp")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := cache.Refresh(ctx, func(context.Context) (schedule.HistoryStatistics, error) { cancel(); return result, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation not honored", err)
	}
	if cachedHistoryBody(cache).Body.String() != initial {
		t.Fatal("canceled refresh published")
	}
	called := false
	if err := cache.Refresh(ctx, func(context.Context) (schedule.HistoryStatistics, error) { called = true; return result, nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatal("canceled refresh called loader", err, called)
	}
	result.GeneratedAt = result.GeneratedAt.Add(time.Minute)
	if err := cache.Refresh(t.Context(), func(context.Context) (schedule.HistoryStatistics, error) { return result, nil }); err != nil {
		t.Fatal(err)
	}
	if cachedHistoryBody(cache).Body.String() == initial {
		t.Fatal("successful refresh did not replace snapshot")
	}
}

func TestHistoryCacheRefreshNoOverlap(t *testing.T) {
	cache := &HistoryCache{}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- cache.Refresh(ctx, func(ctx context.Context) (schedule.HistoryStatistics, error) {
			close(started)
			<-ctx.Done()
			return schedule.HistoryStatistics{}, ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("refresh did not start")
	}
	called := false
	err := cache.Refresh(ctx, func(context.Context) (schedule.HistoryStatistics, error) {
		called = true
		return schedule.HistoryStatistics{}, nil
	})
	if !errors.Is(err, schedule.ErrHistoryBusy) || called {
		t.Fatal("overlapping refresh was admitted", err, called)
	}
	if cachedHistoryBody(cache).Code != http.StatusServiceUnavailable {
		t.Fatal("in-progress first refresh must stay cold")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := cache.Refresh(t.Context(), func(context.Context) (schedule.HistoryStatistics, error) { return schedule.HistoryStatistics{}, nil }); err != nil {
		t.Fatal("refresh admission leaked", err)
	}
}

func TestHistoryCacheConcurrentReadsAndRefresh(t *testing.T) {
	first := schedule.HistoryStatistics{Mode: "first", GeneratedAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Chains: []schedule.HistoryChainRank{{ShowtimeCount: 1}}}
	second := schedule.HistoryStatistics{Mode: "second", GeneratedAt: first.GeneratedAt.Add(time.Minute), Chains: []schedule.HistoryChainRank{{ShowtimeCount: 2}}}
	cache := warmHistoryCache(t, first)
	firstBytes := cachedHistoryBody(cache).Body.String()
	secondBytes := cachedHistoryBody(warmHistoryCache(t, second)).Body.String()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				response := cachedHistoryBody(cache)
				body := response.Body.String()
				if response.Code != http.StatusOK || body != firstBytes && body != secondBytes {
					t.Error("read observed partial snapshot")
					return
				}
			}
		}()
	}
	for i := range 100 {
		result := first
		if i%2 == 0 {
			result = second
		}
		if err := cache.Refresh(t.Context(), func(context.Context) (schedule.HistoryStatistics, error) { return result, nil }); err != nil {
			t.Error(err)
			break
		}
	}
	wg.Wait()
}
