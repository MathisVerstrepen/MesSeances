package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"messeances/api/internal/accounts"
)

type testAccountCleaner struct{ started chan struct{} }

func (c testAccountCleaner) Cleanup(ctx context.Context) (accounts.CleanupResult, error) {
	close(c.started)
	<-ctx.Done()
	return accounts.CleanupResult{}, ctx.Err()
}

func TestAccountCleanupStartupAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleaner := testAccountCleaner{started: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAccountCleanup(ctx, cleaner, slog.New(slog.NewTextHandler(t.Output(), nil)))
	}()
	select {
	case <-cleaner.started:
	case <-time.After(time.Second):
		t.Fatal("startup cleanup not run")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup worker ignored shutdown")
	}
}
