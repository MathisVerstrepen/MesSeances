package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"messeances/api/internal/tmdb"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) RetainedUpcomingIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.pool.Query(ctx, "SELECT tmdb_id FROM tmdb_upcoming_movies ORDER BY tmdb_id")
	if err != nil {
		return nil, fmt.Errorf("read upcoming IDs failed")
	}
	defer rows.Close()
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("read upcoming IDs failed")
	}
	return ids, nil
}

func (s *PostgresStore) PublishUpcoming(ctx context.Context, publication UpcomingPublication) error {
	if publication.CompletedAt.IsZero() {
		return fmt.Errorf("upcoming publication is invalid")
	}
	for _, date := range []string{publication.Window.From, publication.Window.Through} {
		if parsed, err := time.Parse(time.DateOnly, date); err != nil || parsed.Format(time.DateOnly) != date {
			return fmt.Errorf("upcoming window is invalid")
		}
	}
	if publication.Window.From > publication.Window.Through {
		return fmt.Errorf("upcoming window is invalid")
	}
	for _, metadata := range publication.Metadata {
		if err := validateMetadata(metadata); err != nil {
			return err
		}
	}
	seen := map[int64]bool{}
	for _, release := range publication.Releases {
		if release.TMDBID <= 0 || seen[release.TMDBID] {
			return fmt.Errorf("upcoming release is invalid")
		}
		seen[release.TMDBID] = true
		evidence, err := tmdb.NormalizeFrenchReleases(release.FrenchReleases)
		if err != nil || !slices.Equal(evidence.Rows, release.FrenchReleases) || evidence.FrenchReleaseDate != release.FrenchReleaseDate || !slices.Equal(AssessUpcoming(evidence.Rows, evidence.FrenchReleaseDate), release.ReasonCodes) {
			return fmt.Errorf("upcoming review evidence is invalid")
		}
		if release.FrenchReleaseDate != "" {
			if parsed, err := time.Parse(time.DateOnly, release.FrenchReleaseDate); err != nil || parsed.Format(time.DateOnly) != release.FrenchReleaseDate {
				return fmt.Errorf("upcoming date is invalid")
			}
		}
		if release.Active != (release.FrenchReleaseDate >= publication.Window.From && release.FrenchReleaseDate <= publication.Window.Through) {
			return fmt.Errorf("upcoming membership is invalid")
		}
	}
	return s.withWriteTransaction(ctx, "begin upcoming publication failed", func(ctx context.Context, tx pgx.Tx, _ int64) (*writeFinalization, error) {
		for _, metadata := range publication.Metadata {
			if err := writeMetadata(ctx, tx, metadata); err != nil {
				return nil, err
			}
		}
		// Every retained identity must be assessed; never turn a partial payload into withdrawal.
		ids := make([]int64, 0, len(publication.Releases))
		for _, release := range publication.Releases {
			ids = append(ids, release.TMDBID)
		}
		var omitted bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tmdb_upcoming_movies WHERE NOT(tmdb_id=ANY($1::bigint[])))", ids).Scan(&omitted); err != nil || omitted {
			return nil, fmt.Errorf("upcoming publication is incomplete")
		}
		for _, release := range publication.Releases {
			var publicID int64
			err := tx.QueryRow(ctx, "SELECT public_movie_id FROM tmdb_upcoming_movies WHERE tmdb_id=$1", release.TMDBID).Scan(&publicID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("read upcoming identity failed")
			}
			if errors.Is(err, pgx.ErrNoRows) {
				if !release.Active {
					continue
				}
				err = tx.QueryRow(ctx, "SELECT id FROM public_movies WHERE confirmed_tmdb_id=$1 AND redirect_to_id IS NULL", release.TMDBID).Scan(&publicID)
				if errors.Is(err, pgx.ErrNoRows) {
					err = tx.QueryRow(ctx, `INSERT INTO public_movies(identity_anchor_tmdb_id,confirmed_tmdb_id,title,runtime_minutes)
SELECT provider_movie_id,provider_movie_id,localized_title,runtime_minutes FROM movie_metadata_cache WHERE provider='tmdb' AND locale='fr-FR' AND provider_movie_id=$1 RETURNING id`, release.TMDBID).Scan(&publicID)
				}
				if err != nil {
					return nil, fmt.Errorf("allocate upcoming identity failed")
				}
			}
			rows := release.FrenchReleases
			if rows == nil {
				rows = []tmdb.FrenchReleaseRow{}
			}
			reasons := release.ReasonCodes
			if reasons == nil {
				reasons = []string{}
			}
			encoded, err := json.Marshal(rows)
			if err != nil {
				return nil, fmt.Errorf("encode upcoming evidence failed")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO tmdb_upcoming_movies(tmdb_id,public_movie_id,french_release_date,active,verified_at,french_releases,reason_codes,assessed_at)
VALUES($1,$2,NULLIF($3,'')::date,$4,$5,$6::jsonb,$7,$5) ON CONFLICT(tmdb_id) DO UPDATE SET
review_revision=tmdb_upcoming_movies.review_revision + CASE WHEN
 (tmdb_upcoming_movies.french_release_date,tmdb_upcoming_movies.active,tmdb_upcoming_movies.french_releases,tmdb_upcoming_movies.reason_codes,tmdb_upcoming_movies.assessed_at IS NULL)
 IS DISTINCT FROM (EXCLUDED.french_release_date,EXCLUDED.active,EXCLUDED.french_releases,EXCLUDED.reason_codes,false) THEN 1 ELSE 0 END,
french_release_date=EXCLUDED.french_release_date,active=EXCLUDED.active,verified_at=EXCLUDED.verified_at,
french_releases=EXCLUDED.french_releases,reason_codes=EXCLUDED.reason_codes,assessed_at=EXCLUDED.assessed_at`, release.TMDBID, publicID, release.FrenchReleaseDate, release.Active, publication.CompletedAt, encoded, reasons); err != nil {
				return nil, fmt.Errorf("write upcoming release failed")
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tmdb_upcoming_state(singleton,completed_at,window_from,window_through) VALUES(true,$1,$2::date,$3::date)
ON CONFLICT(singleton) DO UPDATE SET completed_at=EXCLUDED.completed_at,window_from=EXCLUDED.window_from,window_through=EXCLUDED.window_through`, publication.CompletedAt, publication.Window.From, publication.Window.Through); err != nil {
			return nil, fmt.Errorf("write upcoming publication marker failed")
		}
		return &writeFinalization{reconcileError: "reconcile upcoming movies failed", advanceVersion: true, mapCommitError: func(error) error { return fmt.Errorf("commit upcoming publication failed") }}, nil
	})
}
