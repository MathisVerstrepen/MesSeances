package enrichment

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *PostgresStore) IsLocallyMerged(ctx context.Context, sourceProvider, sourceMovieID string) (bool, error) {
	if !validSourceIdentity(sourceProvider, sourceMovieID) {
		return false, ErrLocalMovieInvalid
	}
	return isLocallyMerged(ctx, s.pool, sourceProvider, sourceMovieID)
}

type localMovieQueryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func isLocallyMerged(ctx context.Context, query localMovieQueryRow, sourceProvider, sourceMovieID string) (bool, error) {
	var exists bool
	if err := query.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM local_movie_group_members WHERE source_provider=$1 AND source_movie_id=$2)`, sourceProvider, sourceMovieID).Scan(&exists); err != nil {
		return false, fmt.Errorf("read local movie membership failed")
	}
	return exists, nil
}

func localMovieWriteError(message string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "23505" || postgresError.Code == "23503" || postgresError.Code == "23514") {
		return ErrLocalMovieConflict
	}
	return fmt.Errorf("%s", message)
}

func (s *PostgresStore) LocalMovieGroups(ctx context.Context, limit, offset int) ([]LocalMovieGroup, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrLocalMovieInvalid
	}
	rows, err := s.pool.Query(ctx, `WITH selected_groups AS (
    SELECT id, primary_source_provider, primary_source_movie_id
    FROM local_movie_groups ORDER BY id LIMIT $1 OFFSET $2
)
SELECT g.id, g.primary_source_provider, g.primary_source_movie_id,
       member.source_provider, member.source_movie_id,
       movie.provider IS NOT NULL, movie.title, movie.runtime_minutes, movie.poster_url
FROM selected_groups g
JOIN local_movie_group_members member ON member.local_movie_id=g.id
LEFT JOIN schedule_snapshot ss ON ss.singleton=true
LEFT JOIN movies movie ON movie.generation_id=ss.version AND movie.provider=member.source_provider AND movie.provider_id=member.source_movie_id
ORDER BY g.id, member.source_provider, member.source_movie_id`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("read local movie groups failed")
	}
	defer rows.Close()
	groups := make([]LocalMovieGroup, 0)
	for rows.Next() {
		var id int64
		var primary, source LocalMovieSource
		var member LocalMovieMember
		if err := rows.Scan(&id, &primary.SourceProvider, &primary.SourceMovieID, &source.SourceProvider, &source.SourceMovieID, &member.Available, &member.SourceTitle, &member.SourceRuntimeMinutes, &member.SourcePosterURL); err != nil {
			return nil, fmt.Errorf("read local movie groups failed")
		}
		if len(groups) == 0 || groups[len(groups)-1].ID != id {
			groups = append(groups, LocalMovieGroup{ID: id, LocalMovieID: LocalMovieID(id), Primary: primary, Members: []LocalMovieMember{}})
		}
		member.LocalMovieSource = source
		groups[len(groups)-1].Members = append(groups[len(groups)-1].Members, member)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read local movie groups failed")
	}
	for index := range groups {
		prepareLocalMovieGroup(&groups[index])
	}
	return groups, nil
}

func (s *PostgresStore) MergeLocalMovies(ctx context.Context, members []LocalMovieSource, primary LocalMovieSource) (LocalMovieGroup, error) {
	if err := validateLocalMovieMerge(members, primary); err != nil {
		return LocalMovieGroup{}, err
	}
	members = append([]LocalMovieSource(nil), members...)
	sort.Slice(members, func(i, j int) bool { return localMovieSourceKey(members[i]) < localMovieSourceKey(members[j]) })
	var group LocalMovieGroup
	err := s.withWriteTransaction(ctx, "begin local movie merge failed", func(ctx context.Context, tx pgx.Tx, _ int64) (*writeFinalization, error) {
		lockedMembers, err := lockLocalMovieMembers(ctx, tx, members)
		if err != nil {
			return nil, err
		}
		group = LocalMovieGroup{Primary: primary, Members: lockedMembers}
		if err := tx.QueryRow(ctx, `INSERT INTO local_movie_groups (primary_source_provider, primary_source_movie_id) VALUES ($1,$2) RETURNING id`, primary.SourceProvider, primary.SourceMovieID).Scan(&group.ID); err != nil {
			return nil, localMovieWriteError("create local movie group failed", err)
		}
		for _, source := range members {
			if _, err := tx.Exec(ctx, `INSERT INTO local_movie_group_members (local_movie_id, source_provider, source_movie_id) VALUES ($1,$2,$3)`, group.ID, source.SourceProvider, source.SourceMovieID); err != nil {
				return nil, localMovieWriteError("write local movie member failed", err)
			}
		}
		return &writeFinalization{
			reconcileError: "reconcile public movies after local merge",
			advanceVersion: true,
			mapCommitError: func(err error) error {
				return localMovieWriteError("commit local movie merge failed", err)
			},
		}, nil
	})
	if err != nil {
		return LocalMovieGroup{}, err
	}
	prepareLocalMovieGroup(&group)
	return group, nil
}

func lockLocalMovieMembers(ctx context.Context, tx pgx.Tx, members []LocalMovieSource) ([]LocalMovieMember, error) {
	locked := make([]LocalMovieMember, 0, len(members))
	for _, source := range members {
		member := LocalMovieMember{LocalMovieSource: source}
		if err := tx.QueryRow(ctx, `SELECT m.title, m.runtime_minutes, m.poster_url FROM movies m JOIN schedule_snapshot ss ON ss.singleton=true AND m.generation_id=ss.version WHERE m.provider=$1 AND m.provider_id=$2 FOR UPDATE OF m`, source.SourceProvider, source.SourceMovieID).Scan(&member.SourceTitle, &member.SourceRuntimeMinutes, &member.SourcePosterURL); errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLocalMovieConflict
		} else if err != nil {
			return nil, fmt.Errorf("lock local movie source failed")
		}
		member.Available = true
		var status, normalizedTitle string
		var sourceRuntime int
		err := tx.QueryRow(ctx, `SELECT status, normalized_source_title, source_runtime_minutes FROM movie_matches WHERE source_provider=$1 AND source_movie_id=$2 AND metadata_provider='tmdb' FOR UPDATE`, source.SourceProvider, source.SourceMovieID).Scan(&status, &normalizedTitle, &sourceRuntime)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("lock local movie decision failed")
		}
		if err == nil && (status == StatusMatched || status != StatusReviewRequired && status != StatusUnmatched && status != StatusRejected || normalizedTitle != NormalizeTitle(*member.SourceTitle) || sourceRuntime != *member.SourceRuntimeMinutes) {
			return nil, ErrLocalMovieConflict
		}
		if merged, err := isLocallyMerged(ctx, tx, source.SourceProvider, source.SourceMovieID); err != nil {
			return nil, err
		} else if merged {
			return nil, ErrLocalMovieConflict
		}
		locked = append(locked, member)
	}
	return locked, nil
}

func (s *PostgresStore) AddLocalMovieMembers(ctx context.Context, id int64, members []LocalMovieSource) error {
	if id <= 0 || validateLocalMovieMembers(members) != nil {
		return ErrLocalMovieInvalid
	}
	members = append([]LocalMovieSource(nil), members...)
	sort.Slice(members, func(i, j int) bool { return localMovieSourceKey(members[i]) < localMovieSourceKey(members[j]) })
	return s.withWriteTransaction(ctx, "begin local movie member append failed", func(ctx context.Context, tx pgx.Tx, _ int64) (*writeFinalization, error) {
		var lockedID int64
		if err := tx.QueryRow(ctx, "SELECT id FROM local_movie_groups WHERE id=$1 FOR UPDATE", id).Scan(&lockedID); errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLocalMovieNotFound
		} else if err != nil {
			return nil, fmt.Errorf("lock local movie group failed")
		}
		if _, err := lockLocalMovieMembers(ctx, tx, members); err != nil {
			return nil, err
		}
		for _, source := range members {
			if _, err := tx.Exec(ctx, `INSERT INTO local_movie_group_members (local_movie_id, source_provider, source_movie_id) VALUES ($1,$2,$3)`, lockedID, source.SourceProvider, source.SourceMovieID); err != nil {
				return nil, localMovieWriteError("write local movie member failed", err)
			}
		}
		return &writeFinalization{
			reconcileError: "reconcile public movies after local member append",
			advanceVersion: true,
			mapCommitError: func(err error) error {
				return localMovieWriteError("commit local movie member append failed", err)
			},
		}, nil
	})
}

func (s *PostgresStore) UnmergeLocalMovie(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrLocalMovieInvalid
	}
	return s.withWriteTransaction(ctx, "begin local movie unmerge failed", func(ctx context.Context, tx pgx.Tx, _ int64) (*writeFinalization, error) {
		var lockedID int64
		if err := tx.QueryRow(ctx, "SELECT id FROM local_movie_groups WHERE id=$1 FOR UPDATE", id).Scan(&lockedID); errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLocalMovieNotFound
		} else if err != nil {
			return nil, fmt.Errorf("lock local movie group failed")
		}
		if _, err := tx.Exec(ctx, "DELETE FROM local_movie_groups WHERE id=$1", lockedID); err != nil {
			return nil, fmt.Errorf("delete local movie group failed")
		}
		return &writeFinalization{
			reconcileError: "reconcile public movies after local unmerge",
			advanceVersion: true,
			mapCommitError: func(error) error { return fmt.Errorf("commit local movie unmerge failed") },
		}, nil
	})
}
