package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/httpapi"
	"messeances/api/internal/observability"
	"messeances/api/internal/schedule"
)

type testHistoryStatisticsProcessor func(context.Context) (bool, error)

func (f testHistoryStatisticsProcessor) ProcessHistoryStatistics(ctx context.Context) (bool, error) {
	return f(ctx)
}

func TestHistoryStatisticsWorkerRetryNotificationAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	updates := make(chan time.Time, 1)
	ticks := make(chan time.Time)
	calls := make(chan int, 1)
	var logs bytes.Buffer
	count := 0
	var previous context.Context
	processor := testHistoryStatisticsProcessor(func(ctx context.Context) (bool, error) {
		count++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 10*time.Minute {
			t.Error("missing bounded processing context")
		}
		if previous != nil && !errors.Is(previous.Err(), context.Canceled) {
			t.Error("previous context not released")
		}
		previous = ctx
		calls <- count
		if count == 1 {
			return false, errors.New("secret database detail")
		}
		return count <= 3, nil
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runHistoryStatisticsTicks(ctx, processor, updates, observability.NewLogger(&logs), ticks)
	}()
	wait := func(want int) {
		t.Helper()
		select {
		case got := <-calls:
			if got != want {
				t.Fatal(got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("worker did not run")
		}
	}
	tick := func() {
		t.Helper()
		select {
		case ticks <- time.Now():
		case <-time.After(3 * time.Second):
			t.Fatal("worker blocked")
		}
	}
	wait(1)
	tick()
	wait(2)
	tick()
	wait(3)
	tick()
	wait(4)
	// A full notification channel must not block processing; unchanged work does
	// not notify, and failed work is retried at the next tick.
	tick()
	select {
	case <-updates:
	default:
		t.Fatal("processed revision did not notify cache")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if strings.Contains(logs.String(), "secret") || strings.Count(logs.String(), "history_statistics_processing_failed") != 1 || strings.Count(logs.String(), "history_statistics_processed") != 2 {
		t.Fatal("unsafe or missing processing events")
	}
}

func TestHistoryCacheRefreshAfterProcessedRevision(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ticks := make(chan time.Time)
	updates := make(chan time.Time)
	calls := make(chan int, 8)
	count := 0
	reader := testHistoryCacheReader(func(context.Context, schedule.StatisticsQuery) (schedule.HistoryStatistics, error) {
		count++
		calls <- count
		return schedule.HistoryStatistics{Mode: "history", StatisticsRevision: int64(count)}, nil
	})
	done := make(chan struct{})
	var logs bytes.Buffer
	go func() {
		defer close(done)
		runHistoryCacheEvents(ctx, reader, &httpapi.HistoryCache{}, observability.NewLogger(&logs), ticks, updates)
	}()
	wait := func(want int) {
		t.Helper()
		select {
		case got := <-calls:
			if got != want {
				t.Fatal(got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("preset did not refresh")
		}
	}
	for i := 1; i <= 4; i++ {
		wait(i)
	}
	select {
	case updates <- time.Now():
	case <-time.After(3 * time.Second):
		t.Fatal("revision update not accepted")
	}
	for i := 5; i <= 8; i++ {
		wait(i)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cache did not stop")
	}
}
