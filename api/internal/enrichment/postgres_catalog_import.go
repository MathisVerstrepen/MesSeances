package enrichment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/publicmoviepg"
	"messeances/api/internal/tmdb"
)

var ErrMovieNotImportable = errors.New("movie is not importable")

// ImportCatalogMovie publishes inside the caller's transaction. The caller must
// authorize and lock its account before entering this catalog writer section.
// No account locks, provider calls, nested transactions or commit occur here.
func ImportCatalogMovie(ctx context.Context, tx pgx.Tx, details tmdb.Details, now time.Time) (int64, error) {
	if details.Adult == nil || *details.Adult || details.ID <= 0 || now.IsZero() {
		return 0, ErrMovieNotImportable
	}
	metadata := metadataFromDetails(details, 0, now.UTC())
	if err := validateMetadata(metadata); err != nil {
		return 0, ErrMovieNotImportable
	}
	if err := lockScheduleGeneration(ctx, tx); err != nil {
		return 0, err
	}
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := writeMetadata(ctx, tx, metadata); err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(ctx, `SELECT id FROM public_movies WHERE confirmed_tmdb_id=$1 AND redirect_to_id IS NULL`, details.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO public_movies(identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes) VALUES($1,$1,$2,$3) RETURNING id`, details.ID, details.Title, details.Runtime).Scan(&id)
	}
	if err != nil {
		return 0, fmt.Errorf("allocate imported movie failed")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tmdb_catalog_imports(tmdb_id,public_movie_id,published_at) VALUES($1,$2,$3) ON CONFLICT(tmdb_id) DO NOTHING`, details.ID, id, now.UTC()); err != nil {
		return 0, fmt.Errorf("publish imported movie failed")
	}
	if err := publicmoviepg.Reconcile(ctx, tx); err != nil {
		return 0, fmt.Errorf("reconcile imported movie failed")
	}
	if _, err := tx.Exec(ctx, `UPDATE movie_enrichment_state SET version=$1 WHERE singleton`, version+1); err != nil {
		return 0, fmt.Errorf("publish imported movie revision failed")
	}
	if err := tx.QueryRow(ctx, `SELECT public_movie_id FROM tmdb_catalog_imports WHERE tmdb_id=$1`, details.ID).Scan(&id); err != nil {
		return 0, fmt.Errorf("read imported movie identity failed")
	}
	return id, nil
}
