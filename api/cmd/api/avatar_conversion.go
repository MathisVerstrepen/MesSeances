package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"messeances/api/internal/accountavatar"
	"messeances/api/internal/accounts"
	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/database"
)

// Dispatch precedes dotenv loading. Maintenance accepts only explicit process
// environment and cannot construct the normal API's clients or workers.
func dispatch(ctx context.Context, args []string, logger *slog.Logger) error {
	if len(args) != 0 {
		if len(args) != 1 || args[0] != "migrate-account-avatars" {
			return fmt.Errorf("configuration error")
		}
		return migrateAccountAvatars(ctx, os.Getenv, logger)
	}
	if err := runtimeconfig.LoadDotEnv(); err != nil {
		logDotEnvFailure(logger)
		return fmt.Errorf("configuration error")
	}
	return run(ctx)
}

func migrateAccountAvatars(ctx context.Context, getenv func(string) string, logger *slog.Logger) error {
	url, root := getenv("DATABASE_URL"), getenv("ACCOUNT_AVATAR_DIR")
	if strings.TrimSpace(url) == "" || !filepath.IsAbs(root) || filepath.Clean(root) == "/" || strings.ContainsAny(root, "\x00\r\n") {
		return fmt.Errorf("configuration error")
	}
	media, err := accountavatar.Open(root)
	if err != nil {
		return fmt.Errorf("configuration error")
	}
	defer func() {
		if err := media.Close(); err != nil {
			logger.Warn("account_avatar_close_failed")
		}
	}()
	startupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := database.OpenPool(startupCtx, url)
	if err != nil {
		return fmt.Errorf("database startup failed")
	}
	defer pool.Close()
	if err = database.RunMigrations(startupCtx, pool); err != nil {
		if errors.Is(err, database.ErrMigrationHistoryIncompatible) {
			return migrationHistoryIncompatibleProcessFailure
		}
		return fmt.Errorf("database migration failed")
	}
	result, err := accounts.NewPostgresStore(pool).ConvertAvatars(ctx, media)
	logger.Info("account_avatar_conversion", "examined", result.Examined, "converted", result.Converted,
		"already_webp", result.AlreadyWebP, "cleanup_failures", result.CleanupFailures)
	if err != nil {
		return fmt.Errorf("account avatar conversion failed")
	}
	return nil
}
