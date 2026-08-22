package schedulepg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/schedule"
)

type localMovieGroupRow struct {
	id              int64
	primaryProvider schedule.Provider
	primaryMovieID  string
	members         []string
}

func (s *Store) Load(ctx context.Context) (schedule.Dataset, schedule.SnapshotRevision, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, fmt.Errorf("begin schedule load failed")
	}
	defer rollbackScheduleTx(tx)
	data, revision, err := loadMetadata(ctx, tx)
	if err != nil {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, err
	}
	data.Theaters, err = loadTheaterAggregate(ctx, tx, revision.ScheduleVersion)
	if err != nil {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, err
	}
	movies, err := loadMovieAggregate(ctx, tx, revision.ScheduleVersion)
	if err != nil {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, err
	}
	data.Showtimes, err = loadShowtimeAggregate(ctx, tx, revision.ScheduleVersion, movies)
	if err != nil {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, err
	}
	if revision.ScheduleVersion <= 0 || revision.EnrichmentVersion < 0 {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, fmt.Errorf("invalid schedule snapshot revision")
	}
	if err := schedule.ValidateDataset(data, true); err != nil {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, fmt.Errorf("loaded schedule dataset is invalid: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, fmt.Errorf("commit schedule load failed")
	}
	return data, revision, nil
}

func loadMetadata(ctx context.Context, tx pgx.Tx) (schedule.Dataset, schedule.SnapshotRevision, error) {
	var data schedule.Dataset
	var revision schedule.SnapshotRevision
	var from, through time.Time
	var provider, scope string
	err := tx.QueryRow(ctx, `SELECT s.version, e.version, s.schema_version, s.provider, s.scope, s.generated_at, s.timezone, s.window_from, s.window_through FROM schedule_snapshot s CROSS JOIN movie_enrichment_state e WHERE s.singleton=true AND e.singleton=true`).Scan(&revision.ScheduleVersion, &revision.EnrichmentVersion, &data.SchemaVersion, &provider, &scope, &data.GeneratedAt, &data.Timezone, &from, &through)
	if errors.Is(err, pgx.ErrNoRows) {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, schedule.ErrNoCompleteSnapshot
	}
	if err != nil {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, fmt.Errorf("read schedule snapshot metadata failed")
	}
	data.GeneratedAt = data.GeneratedAt.UTC()
	data.Provider, data.Scope = schedule.Provider(provider), schedule.Scope(scope)
	data.Window = schedule.Window{From: schedule.FormatServiceDate(from), Through: schedule.FormatServiceDate(through)}
	data.Showtimes = []schedule.ShowtimeRecord{}
	return data, revision, nil
}

func loadShowtimeAggregate(ctx context.Context, tx pgx.Tx, version int64, movies map[string]schedule.MovieRecord) ([]schedule.ShowtimeRecord, error) {
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load schedule timezone failed")
	}
	showtimes := []schedule.ShowtimeRecord{}
	referencedMovies := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT provider, id, provider_showing_id, service_date, theater_id, movie_provider_id, start_time, end_time, language, provider_version, format, room, booking_url FROM showtimes WHERE generation_id=$1 ORDER BY theater_id, service_date, start_time, id`, version)
	if err != nil {
		return nil, fmt.Errorf("read showtimes failed")
	}
	for rows.Next() {
		var showing schedule.ShowtimeRecord
		var date time.Time
		var movieID string
		var provider, language, format string
		if err := rows.Scan(&provider, &showing.ID, &showing.ProviderShowingID, &date, &showing.TheaterID, &movieID, &showing.StartTime, &showing.EndTime, &language, &showing.ProviderVersion, &format, &showing.Room, &showing.BookingURL); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read showtimes failed")
		}
		showing.Provider, showing.Language, showing.Format = schedule.Provider(provider), schedule.Language(language), schedule.Format(format)
		movieKey := string(showing.Provider) + "\x00" + movieID
		movie, ok := movies[movieKey]
		if !ok {
			rows.Close()
			return nil, fmt.Errorf("showtime references missing movie")
		}
		showing.ServiceDate = schedule.FormatServiceDate(date)
		showing.Movie = movie
		showing.StartTime = showing.StartTime.In(location)
		if movie.LocalMovieID > 0 {
			runtime, ok := schedule.RuntimeDuration(movie.RuntimeMinutes)
			if !ok {
				rows.Close()
				return nil, fmt.Errorf("invalid local movie runtime")
			}
			showing.EndTime = showing.StartTime.Add(runtime)
		} else {
			showing.EndTime = showing.EndTime.In(location)
		}
		referencedMovies[movieKey] = true
		showtimes = append(showtimes, showing)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read showtimes failed")
	}
	rows.Close()
	if len(referencedMovies) != len(movies) {
		return nil, fmt.Errorf("unreferenced movie row")
	}
	return showtimes, nil
}

func loadTheaterAggregate(ctx context.Context, tx pgx.Tx, version int64) ([]schedule.TheaterRecord, error) {
	theaters := []schedule.TheaterRecord{}
	indexByID := map[string]int{}
	rows, err := tx.Query(ctx, `SELECT provider, id, provider_id, slug, name, address, city, postal_code FROM theaters WHERE generation_id=$1 ORDER BY provider, provider_id, id`, version)
	if err != nil {
		return nil, fmt.Errorf("read theaters failed")
	}
	for rows.Next() {
		var theater schedule.TheaterRecord
		var provider string
		if err := rows.Scan(&provider, &theater.ID, &theater.ProviderID, &theater.Slug, &theater.Name, &theater.Address, &theater.City, &theater.PostalCode); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read theaters failed")
		}
		theater.Provider = schedule.Provider(provider)
		if _, exists := indexByID[theater.ID]; exists {
			rows.Close()
			return nil, fmt.Errorf("duplicate theater row")
		}
		theater.AvailableDates = []string{}
		theater.AcceptedPasses = []string{}
		indexByID[theater.ID] = len(theaters)
		theaters = append(theaters, theater)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read theaters failed")
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT theater_id, service_date FROM theater_dates WHERE generation_id=$1 ORDER BY theater_id, service_date`, version)
	if err != nil {
		return nil, fmt.Errorf("read theater dates failed")
	}
	for rows.Next() {
		var theaterID string
		var date time.Time
		if err := rows.Scan(&theaterID, &date); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read theater dates failed")
		}
		index, ok := indexByID[theaterID]
		if !ok {
			rows.Close()
			return nil, fmt.Errorf("unrepresentable theater date row")
		}
		theaters[index].AvailableDates = append(theaters[index].AvailableDates, schedule.FormatServiceDate(date))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read theater dates failed")
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT tp.theater_id, tp.pass_code FROM theater_passes tp WHERE tp.generation_id=$1 ORDER BY tp.theater_id, tp.pass_code`, version)
	if err != nil {
		return nil, fmt.Errorf("read theater passes failed")
	}
	linkedPasses := 0
	for rows.Next() {
		var theaterID, code string
		if err := rows.Scan(&theaterID, &code); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read theater passes failed")
		}
		index, ok := indexByID[theaterID]
		if !ok || code != "UGC_ILLIMITE" || theaters[index].Provider != schedule.ProviderUGC || len(theaters[index].AcceptedPasses) != 0 {
			rows.Close()
			return nil, fmt.Errorf("unrepresentable theater pass row")
		}
		theaters[index].AcceptedPasses = append(theaters[index].AcceptedPasses, code)
		linkedPasses++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read theater passes failed")
	}
	rows.Close()
	var passCount int
	ugcTheaters := 0
	for _, theater := range theaters {
		if theater.Provider == schedule.ProviderUGC {
			ugcTheaters++
		}
	}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM passes").Scan(&passCount); err != nil || passCount != 1 || linkedPasses != ugcTheaters {
		return nil, fmt.Errorf("unrepresentable pass rows")
	}
	return theaters, nil
}

func loadMovieAggregate(ctx context.Context, tx pgx.Tx, version int64) (map[string]schedule.MovieRecord, error) {
	movies := map[string]schedule.MovieRecord{}
	rows, err := tx.Query(ctx, `SELECT m.provider, m.provider_id, m.slug, m.title, m.runtime_minutes, COALESCE(m.poster_url, ''), COALESCE(m.source_overview,''), COALESCE(m.source_release_date::text,''), m.source_genres, c.provider_movie_id, COALESCE(c.overview, ''), COALESCE(c.release_date::text, ''), COALESCE(c.genres, '{}'), COALESCE(c.poster_url, ''), COALESCE(c.backdrop_url, '')
FROM movies m
LEFT JOIN movie_matches mm ON mm.source_provider=m.provider AND mm.source_movie_id=m.provider_id AND mm.metadata_provider='tmdb' AND mm.status='matched'
LEFT JOIN movie_metadata_cache c ON c.provider='tmdb' AND c.provider_movie_id=mm.metadata_movie_id AND c.locale='fr-FR'
WHERE m.generation_id=$1
ORDER BY m.provider, m.provider_id`, version)
	if err != nil {
		return nil, fmt.Errorf("read movies failed")
	}
	for rows.Next() {
		var movie schedule.MovieRecord
		var provider string
		var tmdbID *int64
		var sourceOverview, sourceReleaseDate string
		var sourceGenres []string
		var overview, releaseDate, poster, backdrop string
		var genres []string
		if err := rows.Scan(&provider, &movie.ProviderID, &movie.Slug, &movie.Title, &movie.RuntimeMinutes, &movie.PosterURL, &sourceOverview, &sourceReleaseDate, &sourceGenres, &tmdbID, &overview, &releaseDate, &genres, &poster, &backdrop); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read movies failed")
		}
		movie.Provider = schedule.Provider(provider)
		movie.Overview, movie.ReleaseDate, movie.Genres = sourceOverview, sourceReleaseDate, append([]string(nil), sourceGenres...)
		if tmdbID != nil && *tmdbID > 0 {
			movie.Enrichment = &schedule.MovieEnrichment{TMDBID: *tmdbID, Overview: overview, ReleaseDate: releaseDate, Genres: append([]string(nil), genres...), PosterURL: poster, BackdropURL: backdrop}
		}
		movieKey := string(movie.Provider) + "\x00" + movie.ProviderID
		if _, exists := movies[movieKey]; exists {
			rows.Close()
			return nil, fmt.Errorf("duplicate movie row")
		}
		movies[movieKey] = movie
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read movies failed")
	}
	rows.Close()
	if err := materializeLocalMovies(ctx, tx, movies); err != nil {
		return nil, err
	}
	return movies, nil
}

func materializeLocalMovies(ctx context.Context, tx pgx.Tx, movies map[string]schedule.MovieRecord) error {
	groups := make(map[int64]*localMovieGroupRow)
	rows, err := tx.Query(ctx, `SELECT id, primary_source_provider, primary_source_movie_id FROM local_movie_groups ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read local movie groups failed")
	}
	for rows.Next() {
		group := localMovieGroupRow{}
		var provider string
		if err := rows.Scan(&group.id, &provider, &group.primaryMovieID); err != nil {
			rows.Close()
			return fmt.Errorf("read local movie groups failed")
		}
		group.primaryProvider = schedule.Provider(provider)
		if group.id <= 0 || (schedule.MovieIdentity{Provider: group.primaryProvider, ProviderID: group.primaryMovieID}).Validate() != nil {
			rows.Close()
			return fmt.Errorf("invalid local movie group")
		}
		if _, exists := groups[group.id]; exists {
			rows.Close()
			return fmt.Errorf("duplicate local movie group")
		}
		groups[group.id] = &group
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read local movie groups failed")
	}
	rows.Close()

	rows, err = tx.Query(ctx, `SELECT local_movie_id, source_provider, source_movie_id FROM local_movie_group_members ORDER BY local_movie_id, source_provider, source_movie_id`)
	if err != nil {
		return fmt.Errorf("read local movie members failed")
	}
	memberGroups := make(map[string]int64)
	for rows.Next() {
		var localMovieID int64
		var provider, movieID string
		if err := rows.Scan(&localMovieID, &provider, &movieID); err != nil {
			rows.Close()
			return fmt.Errorf("read local movie members failed")
		}
		group := groups[localMovieID]
		identity := schedule.MovieIdentity{Provider: schedule.Provider(provider), ProviderID: movieID}
		key := provider + "\x00" + movieID
		if group == nil || identity.Validate() != nil {
			rows.Close()
			return fmt.Errorf("invalid local movie member")
		}
		if _, exists := memberGroups[key]; exists {
			rows.Close()
			return fmt.Errorf("duplicate local movie membership")
		}
		memberGroups[key] = localMovieID
		group.members = append(group.members, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read local movie members failed")
	}
	rows.Close()

	for _, group := range groups {
		primaryKey := string(group.primaryProvider) + "\x00" + group.primaryMovieID
		if len(group.members) < 2 || !containsString(group.members, primaryKey) {
			return fmt.Errorf("invalid local movie group membership")
		}
		canonicalKey := ""
		if _, available := movies[primaryKey]; available {
			canonicalKey = primaryKey
		} else {
			for _, memberKey := range group.members {
				if _, available := movies[memberKey]; available {
					canonicalKey = memberKey
					break
				}
			}
		}
		if canonicalKey == "" {
			continue
		}
		canonical := movies[canonicalKey]
		if canonical.Enrichment != nil {
			return fmt.Errorf("local movie metadata source has TMDB enrichment")
		}
		for _, memberKey := range group.members {
			member, available := movies[memberKey]
			if !available {
				continue
			}
			if member.Enrichment != nil {
				return fmt.Errorf("local movie member has TMDB enrichment")
			}
			member.Title = canonical.Title
			member.RuntimeMinutes = canonical.RuntimeMinutes
			member.PosterURL = canonical.PosterURL
			member.Overview = canonical.Overview
			member.ReleaseDate = canonical.ReleaseDate
			member.Genres = append([]string(nil), canonical.Genres...)
			member.LocalMovieID = group.id
			member.LocalMetadataProvider = canonical.Provider
			movies[memberKey] = member
		}
	}
	return nil
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
