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

type testHistoryCacheReader func(context.Context, schedule.StatisticsQuery) (schedule.HistoryStatistics, error)

func (read testHistoryCacheReader) BackgroundHistoryStatistics(ctx context.Context, query schedule.StatisticsQuery) (schedule.HistoryStatistics, error) {
	return read(ctx, query)
}

func historyCacheResponse(t *testing.T, cache *httpapi.HistoryCache, period schedule.HistoryPeriod) *httptest.ResponseRecorder {
	t.Helper()
	handler := httpapi.NewHandlerWithOptions(nil, "", httpapi.HandlerOptions{HistoryCache: cache})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/statistics/history?period="+string(period), nil))
	return response
}

func TestHistoryCacheWorkerImmediatePeriodicAndFailure(t *testing.T) {
	if historyCacheRefreshInterval != 15*time.Minute || schedulepg.HistoryCacheTimeout != 2*time.Minute {
		t.Fatal("cache cadence or budget changed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cache := &httpapi.HistoryCache{}
	calls := make(chan int, 1)
	release := make(chan struct{})
	count := 0
	var previous context.Context
	reader := testHistoryCacheReader(func(ctx context.Context, query schedule.StatisticsQuery) (schedule.HistoryStatistics, error) {
		count++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < schedulepg.HistoryCacheTimeout-time.Second || time.Until(deadline) > schedulepg.HistoryCacheTimeout {
			t.Error("worker did not bound refresh")
		}
		if previous != nil && !errors.Is(previous.Err(), context.Canceled) {
			t.Error("previous attempt context not released")
		}
		previous = ctx
		periods := schedule.HistoryPeriods()
		want, err := schedule.HistoryPeriodQuery(periods[(count-1)%4], time.Now())
		if err != nil || query.Date != want.Date || query.DateTo != want.DateTo {
			t.Error("wrong preset bounds", query, want, err)
		}
		calls <- count
		select {
		case <-release:
		case <-ctx.Done():
			return schedule.HistoryStatistics{}, ctx.Err()
		}
		if count == 1 || count == 6 || count == 9 {
			return schedule.HistoryStatistics{}, errors.New("secret database detail")
		}
		return schedule.HistoryStatistics{Mode: "history", GeneratedAt: time.Date(2026, 10, 2, 0, count, 0, 0, time.UTC), Range: &schedule.Window{From: query.Date, Through: query.DateTo}}, nil
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
		case <-time.After(3 * time.Second):
			t.Fatal("refresh did not start")
		}
	}
	finish := func() {
		t.Helper()
		select {
		case release <- struct{}{}:
		case <-time.After(3 * time.Second):
			t.Fatal("refresh did not finish")
		}
	}
	previousBodies := make(map[schedule.HistoryPeriod]string)
	for round := range 3 {
		for i, period := range schedule.HistoryPeriods() {
			waitCall(round*4 + i + 1) // First cycle starts without a tick.
			got := historyCacheResponse(t, cache, period)
			if round == 0 && got.Code != http.StatusServiceUnavailable {
				t.Fatal("preset not independently cold while warming", period)
			}
			// Reads during refresh retain the preceding response, never block.
			if old, ok := previousBodies[period]; ok && got.Body.String() != old {
				t.Fatal("in-progress refresh changed previous snapshot", period)
			}
			select {
			case ticks <- time.Now():
				t.Fatal("worker accepted overlapping cycle")
			default:
			}
			finish()
		}
		// Tick handshake proves the entire previous cycle finished.
		select {
		case ticks <- time.Now():
		case <-time.After(3 * time.Second):
			t.Fatal("worker did not finish all presets")
		}
		// Next cycle blocks in its first loader, so snapshots are stable here.
		for _, period := range schedule.HistoryPeriods() {
			got := historyCacheResponse(t, cache, period)
			if round == 0 && period == schedule.HistoryNext7 {
				if got.Code != http.StatusServiceUnavailable {
					t.Fatal("failed preset did not stay cold")
				}
				continue
			}
			if got.Code != http.StatusOK {
				t.Fatal("one failure skipped another preset", period)
			}
			if round == 1 && period == schedule.HistoryNext30 || round == 2 && period == schedule.HistoryNext7 {
				if got.Body.String() != previousBodies[period] {
					t.Fatal("failed refresh replaced last successful bytes", period)
				}
			}
			previousBodies[period] = got.Body.String()
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if strings.Contains(logs.String(), "secret") || strings.Count(logs.String(), "history_cache_refresh_failed") != 3 || !strings.Contains(logs.String(), "history_cache_refreshed") {
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
	reader := testHistoryCacheReader(func(ctx context.Context, _ schedule.StatisticsQuery) (schedule.HistoryStatistics, error) {
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
	if historyCacheResponse(t, cache, schedule.HistoryNext7).Code != http.StatusServiceUnavailable {
		t.Fatal("canceled calculation published response")
	}
}

func TestHistoryCacheWorkerCanceledStartupAndClosedTicks(t *testing.T) {
	calls := 0
	reader := testHistoryCacheReader(func(ctx context.Context, _ schedule.StatisticsQuery) (schedule.HistoryStatistics, error) {
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
	if calls != 4 {
		t.Fatal("closed ticks spun worker")
	}
}
