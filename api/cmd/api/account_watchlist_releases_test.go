package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accounts"
	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/tmdb"
)

type releaseSweepFunc func(context.Context) (accounts.WatchlistReleaseResult, error)

func (f releaseSweepFunc) SweepWatchlistReleases(ctx context.Context) (accounts.WatchlistReleaseResult, error) {
	return f(ctx)
}

func TestWatchlistReleaseRuntimeImmediateAndDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, done := make(chan struct{}), make(chan struct{})
	probe := releaseSweepFunc(func(ctx context.Context) (accounts.WatchlistReleaseResult, error) {
		close(started)
		<-ctx.Done()
		return accounts.WatchlistReleaseResult{}, ctx.Err()
	})
	go func() { defer close(done); runAccountWatchlistReleases(ctx, probe, slog.New(slog.DiscardHandler)) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("no immediate background cycle")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker not drained")
	}
}

func TestWatchlistReleaseRuntimeErrorSanitizationAndSerialCycles(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	calls := 0
	probe := releaseSweepFunc(func(context.Context) (accounts.WatchlistReleaseResult, error) {
		calls++
		if calls == 1 {
			return accounts.WatchlistReleaseResult{}, errors.New("private title and provider secret")
		}
		cancel()
		return accounts.WatchlistReleaseResult{}, nil
	})
	runAccountWatchlistReleaseCycles(ctx, probe, logger, 0)
	if calls != 2 || strings.Contains(output.String(), "private") || strings.Contains(output.String(), "secret") || !strings.Contains(output.String(), "account_watchlist_releases_failed") {
		t.Fatal("unsafe logging or loop", output.String())
	}
	// Already canceled runtime must never perform the immediate cycle.
	runAccountWatchlistReleases(ctx, probe, logger)
	if calls != 2 {
		t.Fatal("canceled worker started work")
	}
}

func TestWatchlistReleaseRuntimeSharedOptionalClientIntegration(t *testing.T) {
	pool := upcomingRuntimePool(t)
	for _, token := range []string{"", "synthetic-token-not-used"} {
		cfg := runtimeconfig.Config{}
		cfg.TMDB.Token = token
		r, err := newAdminRuntime(t.Context(), pool, cfg, slog.New(slog.DiscardHandler), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if r.upcomingManager != nil {
				r.upcomingManager.Close()
			}
			if r.metadataRefreshManager != nil {
				r.metadataRefreshManager.Close()
			}
			r.geocodingManager.Close()
		})
		if token == "" {
			if r.watchlistReleaseProvider != nil {
				t.Fatal("missing credentials enabled provider")
			}
			continue
		}
		client, ok := r.watchlistReleaseProvider.(*tmdb.Client)
		if !ok || r.enrichmentProvider != client || r.upcomingManager != nil {
			t.Fatal("client pacing not shared or admin gating changed")
		}
		// Disabled accounts must not construct a service even with a provider.
		if s, err := newAccountService(nil, cfg, nil, client, client, nil); err != nil || s != nil {
			t.Fatal("disabled accounts constructed worker service", err)
		}
	}
}
