package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/cinemaimage"
	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/syncproxy"
)

func openCinemaImages(cfg runtimeconfig.Config) (*cinemaimage.Store, error) {
	if cfg.CinemaImageDir == "" {
		return nil, nil
	}
	if cfg.Accounts.Enabled {
		a, err := filepath.EvalSymlinks(cfg.CinemaImageDir)
		if err != nil {
			return nil, fmt.Errorf("configuration error")
		}
		b, err := filepath.EvalSymlinks(cfg.Accounts.AvatarDir)
		if err != nil {
			return nil, fmt.Errorf("configuration error")
		}
		if a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/") {
			return nil, fmt.Errorf("configuration error")
		}
	}
	store, err := cinemaimage.Open(cfg.CinemaImageDir)
	if err != nil {
		return nil, fmt.Errorf("configuration error")
	}
	return store, nil
}
func newCinemaImageService(pool *pgxpool.Pool, media *cinemaimage.Store, proxies []syncproxy.Proxy, logger *slog.Logger) *cinemaimage.Service {
	var importer cinemaimage.Importer
	if len(proxies) > 0 {
		importer = cinemaimage.NewFetcher(proxies)
	}
	return cinemaimage.NewService(cinemaimage.NewPostgresRepository(pool), media, importer, logger)
}

type cinemaImageCleaner interface {
	Cleanup(context.Context) (cinemaimage.SweepResult, error)
}

func runCinemaImageCleanup(ctx context.Context, service cinemaImageCleaner, logger *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		r, err := service.Cleanup(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			logger.Warn("cinema_image_cleanup_failed")
		} else {
			logger.Info("cinema_image_cleanup_completed", "examined", r.Examined, "deleted", r.Deleted, "errors", r.Errors, "backlog", r.Backlog)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
