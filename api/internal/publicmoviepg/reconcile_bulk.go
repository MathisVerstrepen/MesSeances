package publicmoviepg

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// pgx encodes these typed payloads as JSON for jsonb_to_recordset. Do not omit
// null pointers or empty slices: SQL NULL and empty metadata are distinct.
type movieAssignment struct {
	ID               int64      `json:"id"`
	Title            string     `json:"title"`
	Runtime          int        `json:"runtime"`
	Poster           *string    `json:"poster"`
	Backdrop         *string    `json:"backdrop"`
	TrailerVF        *string    `json:"trailer_vf"`
	TrailerVO        *string    `json:"trailer_vo"`
	Overview         *string    `json:"overview"`
	ReleaseDate      *time.Time `json:"release_date"`
	Genres           []string   `json:"genres"`
	TMDBID           *int64     `json:"tmdb_id"`
	IMDBID           *string    `json:"imdb_id"`
	OriginalLanguage *string    `json:"original_language"`
}

func newMovieAssignment(item *component) movieAssignment {
	m := item.metadata
	var tmdbID *int64
	if m.tmdbID > 0 {
		tmdbID = &m.tmdbID
	}
	return movieAssignment{
		ID: item.publicID, Title: m.title, Runtime: m.runtime,
		Poster: m.poster, Backdrop: m.backdrop, TrailerVF: m.trailerVFYouTubeKey,
		TrailerVO: m.trailerVOYouTubeKey, Overview: m.overview, ReleaseDate: m.releaseDate,
		Genres: m.genres, TMDBID: tmdbID, IMDBID: m.imdbID, OriginalLanguage: m.originalLanguage,
	}
}

type sourceAssignment struct {
	Provider string `json:"provider"`
	SourceID string `json:"source_id"`
	Target   int64  `json:"target"`
}

type idAssignment struct {
	ID     int64 `json:"id"`
	Target int64 `json:"target"`
}

func writeAssignments(ctx context.Context, tx pgx.Tx, clearIDs []int64, movies []movieAssignment, sources, seen []sourceAssignment, catalog, redirects, flattened []idAssignment) error {
	// Release all changing confirmed IDs before claiming any of them. The
	// partial unique index is immediate, so combining these phases is unsafe.
	if _, err := tx.Exec(ctx, `UPDATE public_movies SET confirmed_tmdb_id=NULL, imdb_id=NULL, original_language=NULL,
    trailer_vf_youtube_key=NULL, trailer_vo_youtube_key=NULL
WHERE id=ANY($1::bigint[]) AND confirmed_tmdb_id IS NOT NULL`, clearIDs); err != nil {
		return fmt.Errorf("clear corrected public movie TMDB identity failed")
	}
	// Compute last-seen from explicit membership rather than intermediate
	// source ownership. This preserves the old component-order aggregation.
	if _, err := tx.Exec(ctx, `WITH desired AS (
    SELECT * FROM jsonb_to_recordset($1::jsonb) AS v(
        id bigint, title varchar, runtime integer, poster varchar, backdrop varchar,
        trailer_vf varchar, trailer_vo varchar, overview varchar, release_date date,
        genres text[], tmdb_id bigint, imdb_id varchar, original_language varchar)
), seen AS (
    SELECT v.target, max(source.last_seen_at) AS last_seen_at
    FROM jsonb_to_recordset($2::jsonb) AS v(provider text, source_id text, target bigint)
    JOIN public_movie_sources source ON source.source_provider=v.provider AND source.source_movie_id=v.source_id
    GROUP BY v.target
), changes AS (
    SELECT v.*, GREATEST(movie.last_seen_at, seen.last_seen_at) AS last_seen_at,
        ROW(movie.title, movie.runtime_minutes, movie.poster_url, movie.backdrop_url,
            movie.trailer_vf_youtube_key, movie.trailer_vo_youtube_key, movie.overview,
            movie.release_date, movie.genres, movie.confirmed_tmdb_id, movie.imdb_id, movie.original_language)
        IS DISTINCT FROM ROW(v.title, v.runtime, v.poster, v.backdrop, v.trailer_vf,
            v.trailer_vo, v.overview, v.release_date, v.genres, v.tmdb_id, v.imdb_id, v.original_language) AS metadata_changed
    FROM desired v JOIN public_movies movie ON movie.id=v.id
    LEFT JOIN seen ON seen.target=v.id
    WHERE movie.redirect_to_id IS NULL
)
UPDATE public_movies movie SET title=v.title, runtime_minutes=v.runtime,
    poster_url=v.poster, backdrop_url=v.backdrop, trailer_vf_youtube_key=v.trailer_vf,
    trailer_vo_youtube_key=v.trailer_vo, overview=v.overview, release_date=v.release_date,
    genres=v.genres, confirmed_tmdb_id=v.tmdb_id, imdb_id=v.imdb_id, original_language=v.original_language,
    updated_at=CASE WHEN v.metadata_changed THEN CURRENT_TIMESTAMP ELSE movie.updated_at END,
    last_seen_at=v.last_seen_at
FROM changes v WHERE movie.id=v.id
    AND (v.metadata_changed OR movie.last_seen_at IS DISTINCT FROM v.last_seen_at)`, movies, seen); err != nil {
		return fmt.Errorf("update canonical public movie failed")
	}
	if _, err := tx.Exec(ctx, `UPDATE tmdb_upcoming_movies upcoming SET public_movie_id=v.target
FROM jsonb_to_recordset($1::jsonb) AS v(id bigint, target bigint)
WHERE upcoming.tmdb_id=v.id AND upcoming.public_movie_id IS DISTINCT FROM v.target`, catalog); err != nil {
		return fmt.Errorf("transfer catalog evidence failed")
	}
	if _, err := tx.Exec(ctx, `UPDATE public_movie_sources source SET public_movie_id=v.target
FROM jsonb_to_recordset($1::jsonb) AS v(provider text, source_id text, target bigint)
WHERE source.source_provider=v.provider AND source.source_movie_id=v.source_id
    AND source.public_movie_id IS DISTINCT FROM v.target`, sources); err != nil {
		return fmt.Errorf("assign public movie source failed")
	}
	if _, err := tx.Exec(ctx, `UPDATE public_movies movie SET redirect_to_id=v.target,
    confirmed_tmdb_id=NULL, imdb_id=NULL, original_language=NULL, trailer_vf_youtube_key=NULL, trailer_vo_youtube_key=NULL,
    updated_at=CURRENT_TIMESTAMP
FROM jsonb_to_recordset($1::jsonb) AS v(id bigint, target bigint)
WHERE movie.id=v.id AND movie.redirect_to_id IS NULL`, redirects); err != nil {
		return fmt.Errorf("write public movie redirect tombstone failed")
	}
	if _, err := tx.Exec(ctx, `UPDATE public_movies movie SET redirect_to_id=v.target
FROM jsonb_to_recordset($1::jsonb) AS v(id bigint, target bigint)
WHERE movie.id=v.id AND movie.redirect_to_id IS DISTINCT FROM v.target`, flattened); err != nil {
		return fmt.Errorf("flatten public movie redirect failed")
	}
	if _, err := tx.Exec(ctx, `UPDATE movie_slug_aliases alias SET public_movie_id=movie.redirect_to_id,
    retargeted_at=CURRENT_TIMESTAMP
FROM public_movies movie WHERE alias.public_movie_id=movie.id AND movie.redirect_to_id IS NOT NULL
    AND alias.public_movie_id IS DISTINCT FROM movie.redirect_to_id`); err != nil {
		return fmt.Errorf("flatten public movie aliases failed")
	}
	return nil
}

type aliasAssignment struct {
	Slug     string `json:"slug"`
	Target   int64  `json:"target"`
	Kind     string `json:"kind"`
	Provider string `json:"provider"`
	SourceID string `json:"source_id"`
}

func writeAliases(ctx context.Context, tx pgx.Tx, aliases []aliasAssignment, sources []sourceAssignment) error {
	// INSERT ON CONFLICT cannot affect one slug twice. Repeated identical
	// evidence is harmless; conflicting owners or kinds must fail closed.
	unique := make([]aliasAssignment, 0, len(aliases))
	bySlug := make(map[string]aliasAssignment, len(aliases))
	for _, alias := range aliases {
		if prior, ok := bySlug[alias.Slug]; ok {
			if prior != alias {
				return fmt.Errorf("conflicting public movie aliases")
			}
			continue
		}
		bySlug[alias.Slug] = alias
		unique = append(unique, alias)
	}
	// Validate coverage independently of physical updates. Unchanged valid
	// aliases count, collisions never do. The existing-row branch sees the
	// statement snapshot; RETURNING covers inserts and actual retargets.
	var covered int
	err := tx.QueryRow(ctx, `WITH desired AS (
    SELECT slug, target, kind, NULLIF(provider,'') AS provider, NULLIF(source_id,'') AS source_id
    FROM jsonb_to_recordset($1::jsonb) AS v(slug text, target bigint, kind text, provider text, source_id text)
), written AS (
    INSERT INTO movie_slug_aliases (slug, public_movie_id, alias_kind, source_provider, source_movie_id)
    SELECT slug, target, kind, provider, source_id FROM desired
    ON CONFLICT (slug) DO UPDATE SET public_movie_id=EXCLUDED.public_movie_id,
        retargeted_at=CURRENT_TIMESTAMP
    WHERE movie_slug_aliases.alias_kind=EXCLUDED.alias_kind
        AND movie_slug_aliases.source_provider IS NOT DISTINCT FROM EXCLUDED.source_provider
        AND movie_slug_aliases.source_movie_id IS NOT DISTINCT FROM EXCLUDED.source_movie_id
        AND movie_slug_aliases.public_movie_id IS DISTINCT FROM EXCLUDED.public_movie_id
    RETURNING slug
)
SELECT count(*) FROM desired v
WHERE EXISTS (SELECT 1 FROM written WHERE written.slug=v.slug)
    OR EXISTS (SELECT 1 FROM movie_slug_aliases alias WHERE alias.slug=v.slug
        AND alias.alias_kind=v.kind AND alias.public_movie_id=v.target
        AND alias.source_provider IS NOT DISTINCT FROM v.provider
        AND alias.source_movie_id IS NOT DISTINCT FROM v.source_id)`, unique).Scan(&covered)
	if err != nil || covered != len(unique) {
		return fmt.Errorf("write public movie aliases failed")
	}
	if _, err := tx.Exec(ctx, `UPDATE movie_slug_aliases alias SET public_movie_id=v.target,
    retargeted_at=CURRENT_TIMESTAMP
FROM jsonb_to_recordset($1::jsonb) AS v(provider text, source_id text, target bigint)
WHERE alias.alias_kind='source' AND alias.source_provider=v.provider AND alias.source_movie_id=v.source_id
    AND alias.public_movie_id IS DISTINCT FROM v.target`, sources); err != nil {
		return fmt.Errorf("retarget source movie aliases failed")
	}
	return nil
}
