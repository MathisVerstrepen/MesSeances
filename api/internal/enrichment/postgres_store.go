package enrichment

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/publicmoviepg"
)

type PostgresStore struct{ pool *pgxpool.Pool }

const scheduleGenerationLockID int64 = 6211428337968315

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

type writeFinalization struct {
	reconcileError string
	advanceVersion bool
	mapCommitError func(error) error
}

func (s *PostgresStore) withWriteTransaction(ctx context.Context, beginError string, work func(context.Context, pgx.Tx, int64) (*writeFinalization, error)) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%s", beginError)
	}
	defer rollback(tx)
	if err := lockScheduleGeneration(ctx, tx); err != nil {
		return err
	}
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return err
	}
	finalization, err := work(ctx, tx, version)
	if err != nil || finalization == nil {
		return err
	}
	if finalization.reconcileError != "" {
		if err := publicmoviepg.Reconcile(ctx, tx); err != nil {
			return fmt.Errorf("%s: %w", finalization.reconcileError, err)
		}
	}
	if finalization.advanceVersion {
		if _, err := tx.Exec(ctx, "UPDATE movie_enrichment_state SET version=$1 WHERE singleton=true", version+1); err != nil {
			return fmt.Errorf("publish enrichment version failed")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		if finalization.mapCommitError != nil {
			return finalization.mapCommitError(err)
		}
		return err
	}
	return nil
}

func lockScheduleGeneration(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", scheduleGenerationLockID); err != nil {
		return fmt.Errorf("lock active schedule generation failed")
	}
	return nil
}

func lockEnrichmentVersion(ctx context.Context, tx pgx.Tx) (int64, error) {
	var version int64
	if err := tx.QueryRow(ctx, "SELECT version FROM movie_enrichment_state WHERE singleton=true FOR UPDATE").Scan(&version); err != nil || version < 0 || version == math.MaxInt64 {
		return 0, fmt.Errorf("read enrichment version failed")
	}
	return version, nil
}

func writeMetadata(ctx context.Context, tx pgx.Tx, metadata Metadata) error {
	var imdbID, overview, releaseDate, poster, backdrop, trailerVFYouTubeKey, trailerVOYouTubeKey any
	genres := metadata.Genres
	if genres == nil {
		genres = []string{}
	}
	if metadata.Overview != "" {
		overview = metadata.Overview
	}
	if metadata.IMDBID != "" {
		imdbID = metadata.IMDBID
	}
	if metadata.ReleaseDate != "" {
		releaseDate = metadata.ReleaseDate
	}
	if metadata.PosterURL != "" {
		poster = metadata.PosterURL
	}
	if metadata.BackdropURL != "" {
		backdrop = metadata.BackdropURL
	}
	if metadata.TrailerVFYouTubeKey != "" {
		trailerVFYouTubeKey = metadata.TrailerVFYouTubeKey
	}
	if metadata.TrailerVOYouTubeKey != "" {
		trailerVOYouTubeKey = metadata.TrailerVOYouTubeKey
	}
	_, err := tx.Exec(ctx, `INSERT INTO movie_metadata_cache (provider, provider_movie_id, imdb_id, locale, provider_title, localized_title, overview, release_date, poster_url, backdrop_url, trailer_vf_youtube_key, trailer_vo_youtube_key, runtime_minutes, genres, fetched_at, refresh_after)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT (provider, provider_movie_id, locale) DO UPDATE SET imdb_id=EXCLUDED.imdb_id, provider_title=EXCLUDED.provider_title, localized_title=EXCLUDED.localized_title, overview=EXCLUDED.overview, release_date=EXCLUDED.release_date, poster_url=EXCLUDED.poster_url, backdrop_url=EXCLUDED.backdrop_url, trailer_vf_youtube_key=EXCLUDED.trailer_vf_youtube_key, trailer_vo_youtube_key=EXCLUDED.trailer_vo_youtube_key, runtime_minutes=EXCLUDED.runtime_minutes, genres=EXCLUDED.genres, fetched_at=EXCLUDED.fetched_at, refresh_after=EXCLUDED.refresh_after`, metadata.Provider, metadata.ProviderMovieID, imdbID, metadata.Locale, metadata.ProviderTitle, metadata.LocalizedTitle, overview, releaseDate, poster, backdrop, trailerVFYouTubeKey, trailerVOYouTubeKey, metadata.RuntimeMinutes, genres, metadata.FetchedAt, metadata.RefreshAfter)
	if err != nil {
		return fmt.Errorf("write movie metadata failed")
	}
	return nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
