package enrichment

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const adminMovieEffectiveCTE = `WITH effective AS (
    SELECT movie.id, movie.updated_at,
        movie.title AS automatic_title,
        movie.runtime_minutes AS automatic_runtime_minutes,
        movie.release_date AS automatic_release_date,
        movie.genres AS automatic_genres,
        movie.overview AS automatic_overview,
        movie.poster_url AS automatic_poster_url,
        movie.backdrop_url AS automatic_backdrop_url,
        movie.trailer_vf_youtube_key AS automatic_trailer_vf_youtube_key,
        movie.trailer_vo_youtube_key AS automatic_trailer_vo_youtube_key,
        CASE WHEN override.title_overridden THEN override.title ELSE movie.title END AS title,
        CASE WHEN override.runtime_minutes_overridden THEN override.runtime_minutes ELSE movie.runtime_minutes END AS runtime_minutes,
        CASE WHEN override.release_date_overridden THEN override.release_date ELSE movie.release_date END AS release_date,
        CASE WHEN override.genres_overridden THEN override.genres ELSE movie.genres END AS genres,
        CASE WHEN override.overview_overridden THEN override.overview ELSE movie.overview END AS overview,
        CASE WHEN override.poster_url_overridden THEN override.poster_url ELSE movie.poster_url END AS poster_url,
        CASE WHEN override.backdrop_url_overridden THEN override.backdrop_url ELSE movie.backdrop_url END AS backdrop_url,
        CASE WHEN override.trailer_vf_youtube_key_overridden THEN override.trailer_vf_youtube_key ELSE movie.trailer_vf_youtube_key END AS trailer_vf_youtube_key,
        CASE WHEN override.trailer_vo_youtube_key_overridden THEN override.trailer_vo_youtube_key ELSE movie.trailer_vo_youtube_key END AS trailer_vo_youtube_key,
        override.public_movie_id IS NOT NULL AS has_overrides,
        COALESCE(override.title_overridden, false) AS title_overridden,
        COALESCE(override.runtime_minutes_overridden, false) AS runtime_minutes_overridden,
        COALESCE(override.release_date_overridden, false) AS release_date_overridden,
        COALESCE(override.genres_overridden, false) AS genres_overridden,
        COALESCE(override.overview_overridden, false) AS overview_overridden,
        COALESCE(override.poster_url_overridden, false) AS poster_url_overridden,
        COALESCE(override.backdrop_url_overridden, false) AS backdrop_url_overridden,
        COALESCE(override.trailer_vf_youtube_key_overridden, false) AS trailer_vf_youtube_key_overridden,
        COALESCE(override.trailer_vo_youtube_key_overridden, false) AS trailer_vo_youtube_key_overridden
    FROM public_movies movie
    LEFT JOIN public_movie_metadata_overrides override ON override.public_movie_id=movie.id
    WHERE movie.redirect_to_id IS NULL
)
`

type adminMovieOverrideState struct {
	title               *string
	titleOverridden     bool
	runtimeMinutes      *int
	runtimeOverridden   bool
	releaseDate         *string
	releaseOverridden   bool
	genres              *[]string
	genresOverridden    bool
	overview            *string
	overviewOverridden  bool
	posterURL           *string
	posterOverridden    bool
	backdropURL         *string
	backdropOverridden  bool
	trailerVF           *string
	trailerVFOverridden bool
	trailerVO           *string
	trailerVOOverridden bool
}

func (s *PostgresStore) AdminMovies(ctx context.Context, query AdminMovieQuery) (AdminMovieList, error) {
	if !validAdminMovieQuery(query) {
		return AdminMovieList{}, ErrAdminMovieInvalid
	}
	where, args := adminMovieWhere(query)
	var total int64
	if err := s.pool.QueryRow(ctx, adminMovieEffectiveCTE+"SELECT count(*) FROM effective WHERE "+where, args...).Scan(&total); err != nil {
		return AdminMovieList{}, fmt.Errorf("read admin movie count failed")
	}
	order := adminMovieOrder(query)
	limitPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	offsetPlaceholder := fmt.Sprintf("$%d", len(args)+2)
	listArgs := append(append([]any(nil), args...), query.Limit, query.Offset)
	rows, err := s.pool.Query(ctx, adminMovieEffectiveCTE+`SELECT id, updated_at,
    automatic_title, automatic_runtime_minutes, automatic_release_date, automatic_genres, automatic_overview,
    automatic_poster_url, automatic_backdrop_url, automatic_trailer_vf_youtube_key, automatic_trailer_vo_youtube_key,
    title, runtime_minutes, release_date, genres, overview, poster_url, backdrop_url,
    trailer_vf_youtube_key, trailer_vo_youtube_key,
    title_overridden, runtime_minutes_overridden, release_date_overridden, genres_overridden, overview_overridden,
    poster_url_overridden, backdrop_url_overridden, trailer_vf_youtube_key_overridden, trailer_vo_youtube_key_overridden
FROM effective WHERE `+where+" ORDER BY "+order+" LIMIT "+limitPlaceholder+" OFFSET "+offsetPlaceholder, listArgs...)
	if err != nil {
		return AdminMovieList{}, fmt.Errorf("read admin movies failed")
	}
	defer rows.Close()
	items := make([]AdminMovieItem, 0)
	for rows.Next() {
		item, err := scanAdminMovie(rows)
		if err != nil {
			return AdminMovieList{}, fmt.Errorf("read admin movies failed")
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return AdminMovieList{}, fmt.Errorf("read admin movies failed")
	}
	return AdminMovieList{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func adminMovieWhere(query AdminMovieQuery) (string, []any) {
	conditions := []string{"true"}
	args := make([]any, 0, 8)
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if query.Search != "" {
		placeholder := add(query.Search)
		conditions = append(conditions, "strpos(lower(title), lower("+placeholder+")) > 0")
	}
	if query.RuntimeMin != nil {
		conditions = append(conditions, "runtime_minutes >= "+add(*query.RuntimeMin))
	}
	if query.RuntimeMax != nil {
		conditions = append(conditions, "runtime_minutes <= "+add(*query.RuntimeMax))
	}
	if query.ReleaseDateFrom != nil {
		conditions = append(conditions, "release_date >= "+add(*query.ReleaseDateFrom)+"::date")
	}
	if query.ReleaseDateTo != nil {
		conditions = append(conditions, "release_date <= "+add(*query.ReleaseDateTo)+"::date")
	}
	if query.Genre != "" {
		placeholder := add(query.Genre)
		conditions = append(conditions, "EXISTS (SELECT 1 FROM unnest(genres) genre_value WHERE strpos(lower(genre_value), lower("+placeholder+")) > 0)")
	}
	switch query.OverrideStatus {
	case "overridden":
		conditions = append(conditions, "has_overrides")
	case "automatic":
		conditions = append(conditions, "NOT has_overrides")
	}
	if query.OverrideField != "" {
		conditions = append(conditions, string(query.OverrideField)+"_overridden")
	}
	return strings.Join(conditions, " AND "), args
}

func adminMovieOrder(query AdminMovieQuery) string {
	expression := map[string]string{
		"title":           "title",
		"runtime_minutes": "runtime_minutes",
		"release_date":    "release_date",
		"updated_at":      "updated_at",
		"id":              "id",
	}[query.Sort]
	order := expression + " " + query.Direction
	if query.Sort == "release_date" {
		order += " NULLS LAST"
	}
	return order + ", id ASC"
}

type adminMovieScanner interface {
	Scan(...any) error
}

func scanAdminMovie(row adminMovieScanner) (AdminMovieItem, error) {
	var id int64
	var updatedAt time.Time
	var automatic, effective AdminMovieMetadata
	var automaticRelease, effectiveRelease *time.Time
	flags := make([]bool, len(adminMovieFields))
	if err := row.Scan(
		&id, &updatedAt,
		&automatic.Title, &automatic.RuntimeMinutes, &automaticRelease, &automatic.Genres, &automatic.Overview,
		&automatic.PosterURL, &automatic.BackdropURL, &automatic.TrailerVFYouTubeKey, &automatic.TrailerVOYouTubeKey,
		&effective.Title, &effective.RuntimeMinutes, &effectiveRelease, &effective.Genres, &effective.Overview,
		&effective.PosterURL, &effective.BackdropURL, &effective.TrailerVFYouTubeKey, &effective.TrailerVOYouTubeKey,
		&flags[0], &flags[1], &flags[2], &flags[3], &flags[4], &flags[5], &flags[6], &flags[7], &flags[8],
	); err != nil {
		return AdminMovieItem{}, err
	}
	automatic.ReleaseDate = adminMovieDateString(automaticRelease)
	effective.ReleaseDate = adminMovieDateString(effectiveRelease)
	if automatic.Genres == nil {
		automatic.Genres = []string{}
	}
	if effective.Genres == nil {
		effective.Genres = []string{}
	}
	fields := make([]AdminMovieField, 0, len(adminMovieFields))
	for index, active := range flags {
		if active {
			fields = append(fields, adminMovieFields[index])
		}
	}
	return AdminMovieItem{
		ID: strconv.FormatInt(id, 10), UpdatedAt: updatedAt.UTC().Format(time.RFC3339Nano),
		Automatic: automatic, Values: effective, OverriddenFields: fields,
	}, nil
}

func (s *PostgresStore) UpdateAdminMovie(ctx context.Context, id int64, patch AdminMoviePatch) (AdminMovieItem, error) {
	if id <= 0 || !normalizeAdminMoviePatch(&patch) {
		return AdminMovieItem{}, ErrAdminMovieInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AdminMovieItem{}, fmt.Errorf("begin admin movie update failed")
	}
	defer rollback(tx)
	if err := lockScheduleGeneration(ctx, tx); err != nil {
		return AdminMovieItem{}, err
	}
	version, err := lockEnrichmentVersion(ctx, tx)
	if err != nil {
		return AdminMovieItem{}, err
	}
	var redirectTo *int64
	var updatedAt time.Time
	var automatic AdminMovieMetadata
	var automaticRelease *time.Time
	err = tx.QueryRow(ctx, `SELECT redirect_to_id, updated_at, title, runtime_minutes, release_date, genres, overview,
    poster_url, backdrop_url, trailer_vf_youtube_key, trailer_vo_youtube_key
FROM public_movies WHERE id=$1 FOR UPDATE`, id).Scan(
		&redirectTo, &updatedAt, &automatic.Title, &automatic.RuntimeMinutes, &automaticRelease, &automatic.Genres,
		&automatic.Overview, &automatic.PosterURL, &automatic.BackdropURL, &automatic.TrailerVFYouTubeKey, &automatic.TrailerVOYouTubeKey,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminMovieItem{}, ErrAdminMovieNotFound
	}
	if err != nil {
		return AdminMovieItem{}, fmt.Errorf("lock admin movie failed")
	}
	if redirectTo != nil || !updatedAt.Equal(patch.ExpectedUpdatedAt) {
		return AdminMovieItem{}, ErrAdminMovieConflict
	}
	automatic.ReleaseDate = adminMovieDateString(automaticRelease)
	if automatic.Genres == nil {
		automatic.Genres = []string{}
	}
	state, err := loadAdminMovieOverrideState(ctx, tx, id)
	if err != nil {
		return AdminMovieItem{}, err
	}
	next := state
	applyAdminMoviePatch(&next, patch)
	effective := effectiveAdminMovieMetadata(automatic, next)
	if effective.TrailerVFYouTubeKey != nil && effective.TrailerVOYouTubeKey != nil && *effective.TrailerVFYouTubeKey == *effective.TrailerVOYouTubeKey {
		return AdminMovieItem{}, ErrAdminMovieInvalid
	}
	if reflect.DeepEqual(state, next) {
		if err := tx.Commit(ctx); err != nil {
			return AdminMovieItem{}, fmt.Errorf("commit unchanged admin movie update failed")
		}
		return makeAdminMovieItem(id, updatedAt, automatic, next), nil
	}
	if next.any() {
		if err := upsertAdminMovieOverrideState(ctx, tx, id, next); err != nil {
			return AdminMovieItem{}, err
		}
	} else if _, err := tx.Exec(ctx, "DELETE FROM public_movie_metadata_overrides WHERE public_movie_id=$1", id); err != nil {
		return AdminMovieItem{}, fmt.Errorf("delete admin movie overrides failed")
	}
	if err := tx.QueryRow(ctx, "UPDATE public_movies SET updated_at=CURRENT_TIMESTAMP WHERE id=$1 RETURNING updated_at", id).Scan(&updatedAt); err != nil {
		return AdminMovieItem{}, fmt.Errorf("touch admin movie failed")
	}
	if _, err := tx.Exec(ctx, "UPDATE movie_enrichment_state SET version=$1 WHERE singleton=true", version+1); err != nil {
		return AdminMovieItem{}, fmt.Errorf("publish enrichment version failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminMovieItem{}, fmt.Errorf("commit admin movie update failed")
	}
	return makeAdminMovieItem(id, updatedAt, automatic, next), nil
}

func loadAdminMovieOverrideState(ctx context.Context, tx pgx.Tx, id int64) (adminMovieOverrideState, error) {
	var state adminMovieOverrideState
	var releaseDate *time.Time
	var genres []string
	err := tx.QueryRow(ctx, `SELECT title, title_overridden, runtime_minutes, runtime_minutes_overridden,
    release_date, release_date_overridden, genres, genres_overridden, overview, overview_overridden,
    poster_url, poster_url_overridden, backdrop_url, backdrop_url_overridden,
    trailer_vf_youtube_key, trailer_vf_youtube_key_overridden, trailer_vo_youtube_key, trailer_vo_youtube_key_overridden
FROM public_movie_metadata_overrides WHERE public_movie_id=$1`, id).Scan(
		&state.title, &state.titleOverridden, &state.runtimeMinutes, &state.runtimeOverridden,
		&releaseDate, &state.releaseOverridden, &genres, &state.genresOverridden,
		&state.overview, &state.overviewOverridden, &state.posterURL, &state.posterOverridden,
		&state.backdropURL, &state.backdropOverridden, &state.trailerVF, &state.trailerVFOverridden,
		&state.trailerVO, &state.trailerVOOverridden,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("read admin movie overrides failed")
	}
	state.releaseDate = adminMovieDateString(releaseDate)
	if state.genresOverridden {
		state.genres = &genres
	}
	return state, nil
}

func upsertAdminMovieOverrideState(ctx context.Context, tx pgx.Tx, id int64, state adminMovieOverrideState) error {
	_, err := tx.Exec(ctx, `INSERT INTO public_movie_metadata_overrides (
    public_movie_id, title, title_overridden, runtime_minutes, runtime_minutes_overridden,
    release_date, release_date_overridden, genres, genres_overridden, overview, overview_overridden,
    poster_url, poster_url_overridden, backdrop_url, backdrop_url_overridden,
    trailer_vf_youtube_key, trailer_vf_youtube_key_overridden, trailer_vo_youtube_key, trailer_vo_youtube_key_overridden
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT (public_movie_id) DO UPDATE SET
    title=EXCLUDED.title, title_overridden=EXCLUDED.title_overridden,
    runtime_minutes=EXCLUDED.runtime_minutes, runtime_minutes_overridden=EXCLUDED.runtime_minutes_overridden,
    release_date=EXCLUDED.release_date, release_date_overridden=EXCLUDED.release_date_overridden,
    genres=EXCLUDED.genres, genres_overridden=EXCLUDED.genres_overridden,
    overview=EXCLUDED.overview, overview_overridden=EXCLUDED.overview_overridden,
    poster_url=EXCLUDED.poster_url, poster_url_overridden=EXCLUDED.poster_url_overridden,
    backdrop_url=EXCLUDED.backdrop_url, backdrop_url_overridden=EXCLUDED.backdrop_url_overridden,
    trailer_vf_youtube_key=EXCLUDED.trailer_vf_youtube_key, trailer_vf_youtube_key_overridden=EXCLUDED.trailer_vf_youtube_key_overridden,
    trailer_vo_youtube_key=EXCLUDED.trailer_vo_youtube_key, trailer_vo_youtube_key_overridden=EXCLUDED.trailer_vo_youtube_key_overridden`,
		id, state.title, state.titleOverridden, state.runtimeMinutes, state.runtimeOverridden,
		state.releaseDate, state.releaseOverridden, nullableAdminMovieGenres(state), state.genresOverridden,
		state.overview, state.overviewOverridden, state.posterURL, state.posterOverridden,
		state.backdropURL, state.backdropOverridden, state.trailerVF, state.trailerVFOverridden,
		state.trailerVO, state.trailerVOOverridden)
	if err != nil {
		return fmt.Errorf("write admin movie overrides failed: %w", err)
	}
	return nil
}

func nullableAdminMovieGenres(state adminMovieOverrideState) any {
	if !state.genresOverridden {
		return nil
	}
	return *state.genres
}

func applyAdminMoviePatch(state *adminMovieOverrideState, patch AdminMoviePatch) {
	for _, field := range patch.Restore {
		state.clear(field)
	}
	if patch.Overrides.Title.Present {
		state.title, state.titleOverridden = clonePointer(patch.Overrides.Title.Value), true
	}
	if patch.Overrides.RuntimeMinutes.Present {
		state.runtimeMinutes, state.runtimeOverridden = clonePointer(patch.Overrides.RuntimeMinutes.Value), true
	}
	if patch.Overrides.ReleaseDate.Present {
		state.releaseDate, state.releaseOverridden = clonePointer(patch.Overrides.ReleaseDate.Value), true
	}
	if patch.Overrides.Genres.Present {
		genres := append(make([]string, 0, len(*patch.Overrides.Genres.Value)), (*patch.Overrides.Genres.Value)...)
		state.genres, state.genresOverridden = &genres, true
	}
	if patch.Overrides.Overview.Present {
		state.overview, state.overviewOverridden = clonePointer(patch.Overrides.Overview.Value), true
	}
	if patch.Overrides.PosterURL.Present {
		state.posterURL, state.posterOverridden = clonePointer(patch.Overrides.PosterURL.Value), true
	}
	if patch.Overrides.BackdropURL.Present {
		state.backdropURL, state.backdropOverridden = clonePointer(patch.Overrides.BackdropURL.Value), true
	}
	if patch.Overrides.TrailerVFYouTubeKey.Present {
		state.trailerVF, state.trailerVFOverridden = clonePointer(patch.Overrides.TrailerVFYouTubeKey.Value), true
	}
	if patch.Overrides.TrailerVOYouTubeKey.Present {
		state.trailerVO, state.trailerVOOverridden = clonePointer(patch.Overrides.TrailerVOYouTubeKey.Value), true
	}
}

func (state *adminMovieOverrideState) clear(field AdminMovieField) {
	switch field {
	case AdminMovieFieldTitle:
		state.title, state.titleOverridden = nil, false
	case AdminMovieFieldRuntimeMinutes:
		state.runtimeMinutes, state.runtimeOverridden = nil, false
	case AdminMovieFieldReleaseDate:
		state.releaseDate, state.releaseOverridden = nil, false
	case AdminMovieFieldGenres:
		state.genres, state.genresOverridden = nil, false
	case AdminMovieFieldOverview:
		state.overview, state.overviewOverridden = nil, false
	case AdminMovieFieldPosterURL:
		state.posterURL, state.posterOverridden = nil, false
	case AdminMovieFieldBackdropURL:
		state.backdropURL, state.backdropOverridden = nil, false
	case AdminMovieFieldTrailerVFYouTubeKey:
		state.trailerVF, state.trailerVFOverridden = nil, false
	case AdminMovieFieldTrailerVOYouTubeKey:
		state.trailerVO, state.trailerVOOverridden = nil, false
	}
}

func (state adminMovieOverrideState) any() bool {
	return state.titleOverridden || state.runtimeOverridden || state.releaseOverridden || state.genresOverridden ||
		state.overviewOverridden || state.posterOverridden || state.backdropOverridden ||
		state.trailerVFOverridden || state.trailerVOOverridden
}

func effectiveAdminMovieMetadata(automatic AdminMovieMetadata, state adminMovieOverrideState) AdminMovieMetadata {
	effective := automatic
	effective.Genres = append(make([]string, 0, len(automatic.Genres)), automatic.Genres...)
	if state.titleOverridden {
		effective.Title = *state.title
	}
	if state.runtimeOverridden {
		effective.RuntimeMinutes = *state.runtimeMinutes
	}
	if state.releaseOverridden {
		effective.ReleaseDate = clonePointer(state.releaseDate)
	}
	if state.genresOverridden {
		effective.Genres = append(make([]string, 0, len(*state.genres)), (*state.genres)...)
	}
	if state.overviewOverridden {
		effective.Overview = clonePointer(state.overview)
	}
	if state.posterOverridden {
		effective.PosterURL = clonePointer(state.posterURL)
	}
	if state.backdropOverridden {
		effective.BackdropURL = clonePointer(state.backdropURL)
	}
	if state.trailerVFOverridden {
		effective.TrailerVFYouTubeKey = clonePointer(state.trailerVF)
	}
	if state.trailerVOOverridden {
		effective.TrailerVOYouTubeKey = clonePointer(state.trailerVO)
	}
	return effective
}

func makeAdminMovieItem(id int64, updatedAt time.Time, automatic AdminMovieMetadata, state adminMovieOverrideState) AdminMovieItem {
	fields := make([]AdminMovieField, 0, len(adminMovieFields))
	for _, field := range adminMovieFields {
		if state.active(field) {
			fields = append(fields, field)
		}
	}
	return AdminMovieItem{
		ID: strconv.FormatInt(id, 10), UpdatedAt: updatedAt.UTC().Format(time.RFC3339Nano),
		Automatic: automatic, Values: effectiveAdminMovieMetadata(automatic, state), OverriddenFields: fields,
	}
}

func (state adminMovieOverrideState) active(field AdminMovieField) bool {
	switch field {
	case AdminMovieFieldTitle:
		return state.titleOverridden
	case AdminMovieFieldRuntimeMinutes:
		return state.runtimeOverridden
	case AdminMovieFieldReleaseDate:
		return state.releaseOverridden
	case AdminMovieFieldGenres:
		return state.genresOverridden
	case AdminMovieFieldOverview:
		return state.overviewOverridden
	case AdminMovieFieldPosterURL:
		return state.posterOverridden
	case AdminMovieFieldBackdropURL:
		return state.backdropOverridden
	case AdminMovieFieldTrailerVFYouTubeKey:
		return state.trailerVFOverridden
	case AdminMovieFieldTrailerVOYouTubeKey:
		return state.trailerVOOverridden
	default:
		return false
	}
}

func adminMovieDateString(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.DateOnly)
	return &formatted
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
