package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/httpapi"
	"messeances/api/internal/observability"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
)

type testHistoryCacheReader func(context.Context) (schedule.HistoryStatistics, error)

func (read testHistoryCacheReader) AllTimeHistoryStatistics(ctx context.Context) (schedule.HistoryStatistics, error) {
	return read(ctx)
}

func historyCacheResponse(t *testing.T, cache *httpapi.HistoryCache) *httptest.ResponseRecorder {
	t.Helper()
	handler := httpapi.NewHandlerWithOptions(nil, "", httpapi.HandlerOptions{HistoryCache: cache})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/statistics/history", nil))
	return response
}

func TestHistoryCacheWorkerImmediatePeriodicAndFailure(t *testing.T) {
	if historyCacheRefreshInterval != 15*time.Minute || schedulepg.HistoryCacheTimeout != 2*time.Minute {
		t.Fatal("cache cadence or budget changed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cache := &httpapi.HistoryCache{}
	calls := make(chan int, 1)
	release := make(chan struct{})
	count := 0
	reader := testHistoryCacheReader(func(ctx context.Context) (schedule.HistoryStatistics, error) {
		count++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > schedulepg.HistoryCacheTimeout {
			t.Error("worker did not bound refresh")
		}
		calls <- count
		select {
		case <-release:
		case <-ctx.Done():
			return schedule.HistoryStatistics{}, ctx.Err()
		}
		if count == 1 || count == 3 {
			return schedule.HistoryStatistics{}, errors.New("secret database detail")
		}
		return schedule.HistoryStatistics{Mode: "history", GeneratedAt: time.Date(2026, 10, 2, 0, count, 0, 0, time.UTC)}, nil
	})
	var logs bytes.Buffer
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runHistoryCacheTicks(ctx, reader, cache, observability.NewLogger(&logs), ticks)
	}()
	waitCall := func(want int) {
		t.Helper()
		select {
		case got := <-calls:
			if got != want {
				t.Fatalf("call=%d want=%d", got, want)
			}
		case <-ctx.Done():
			t.Fatal("refresh did not start")
		}
	}
	advance := func() {
		t.Helper()
		select {
		case release <- struct{}{}:
		case <-ctx.Done():
			t.Fatal("refresh did not finish")
		}
		select {
		case ticks <- time.Now():
		case <-ctx.Done():
			t.Fatal("worker did not accept tick")
		}
	}
	waitCall(1) // No tick sent: immediate startup warming.
	if historyCacheResponse(t, cache).Code != http.StatusServiceUnavailable {
		t.Fatal("cache not cold while warming")
	}
	advance()
	waitCall(2)
	if historyCacheResponse(t, cache).Code != http.StatusServiceUnavailable {
		t.Fatal("failed first refresh must leave cold cache")
	}
	advance()
	waitCall(3)
	good := historyCacheResponse(t, cache)
	if good.Code != http.StatusOK {
		t.Fatal("successful retry did not warm cache")
	}
	advance()
	waitCall(4)
	if got := historyCacheResponse(t, cache); got.Body.String() != good.Body.String() {
		t.Fatal("failed refresh replaced last good response")
	}
	// An unbuffered tick cannot be accepted while loading.
	select {
	case ticks <- time.Now():
		t.Fatal("worker accepted overlapping refresh")
	default:
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if strings.Contains(logs.String(), "secret") || strings.Count(logs.String(), "history_cache_refresh_failed") != 2 || !strings.Contains(logs.String(), "history_cache_refreshed") {
		t.Fatal("refresh events missing or unsafe")
	}
}

func TestHistoryCacheWorkerCancellationAndShutdownJoin(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cache := &httpapi.HistoryCache{}
	entered := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	reader := testHistoryCacheReader(func(ctx context.Context) (schedule.HistoryStatistics, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release // Simulate database cleanup after cancellation.
		return schedule.HistoryStatistics{Mode: "must-not-publish"}, nil
	})
	var polling sync.WaitGroup
	polling.Add(1)
	go func() {
		defer polling.Done()
		runHistoryCacheTicks(ctx, reader, cache, observability.NewLogger(&bytes.Buffer{}), nil)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	done := make(chan struct{})
	go func() {
		shutdownWorkers(cancel, nil, nil, nil, nil, &polling)
		close(done)
	}()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel database calculation")
	}
	select {
	case <-done:
		t.Fatal("shutdown returned before database calculation joined")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not join cache worker")
	}
	if historyCacheResponse(t, cache).Code != http.StatusServiceUnavailable {
		t.Fatal("canceled calculation published response")
	}
}

func TestHistoryCacheWorkerCanceledStartupAndClosedTicks(t *testing.T) {
	calls := 0
	reader := testHistoryCacheReader(func(ctx context.Context) (schedule.HistoryStatistics, error) {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < schedulepg.HistoryCacheTimeout-time.Second || time.Until(deadline) > schedulepg.HistoryCacheTimeout {
			t.Error("worker refresh budget must be two minutes")
		}
		return schedule.HistoryStatistics{}, nil
	})
	logger := observability.NewLogger(&bytes.Buffer{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runHistoryCacheTicks(ctx, reader, &httpapi.HistoryCache{}, logger, nil)
	if calls != 0 {
		t.Fatal("canceled startup computed history")
	}
	ticks := make(chan time.Time)
	close(ticks)
	runHistoryCacheTicks(t.Context(), reader, &httpapi.HistoryCache{}, logger, ticks)
	if calls != 1 {
		t.Fatal("closed ticks spun worker")
	}
}
