package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"messeances/api/internal/accountavatar"
)

type avatarCleaner interface {
	CleanupAvatars(context.Context) (accountavatar.SweepResult, error)
}

func runAccountAvatarCleanup(ctx context.Context, service avatarCleaner, logger *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		result, err := service.CleanupAvatars(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("account_avatar_cleanup_failed")
		} else {
			logger.Info("account_avatar_cleanup_completed", "examined", result.Examined, "deleted", result.Deleted, "errors", result.Errors, "backlog", result.Backlog)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type accountRequestDrain struct {
	next     http.Handler
	mu       sync.Mutex
	stopping bool
	wg       sync.WaitGroup
}

func (d *accountRequestDrain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	if d.stopping {
		d.mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	d.wg.Add(1)
	d.mu.Unlock()
	defer d.wg.Done()
	d.next.ServeHTTP(w, r)
}
func (d *accountRequestDrain) stop() { d.mu.Lock(); d.stopping = true; d.mu.Unlock() }
func (d *accountRequestDrain) wait() { d.wg.Wait() }
