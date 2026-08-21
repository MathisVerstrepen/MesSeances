package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type localMovieGroupRow struct {
	id              int64
	primaryProvider string
	primaryMovieID  string
	members         []string
}

func (s *PostgresStore) Load(ctx context.Context) (Dataset, SnapshotRevision, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("begin schedule load failed")
	}
	defer rollbackScheduleTx(tx)
	var data Dataset
	var revision SnapshotRevision
	var from, through time.Time
	err = tx.QueryRow(ctx, `SELECT s.version, e.version, s.schema_version, s.provider, s.scope, s.generated_at, s.timezone, s.window_from, s.window_through FROM schedule_snapshot s CROSS JOIN movie_enrichment_state e WHERE s.singleton=true AND e.singleton=true`).Scan(&revision.ScheduleVersion, &revision.EnrichmentVersion, &data.SchemaVersion, &data.Provider, &data.Scope, &data.GeneratedAt, &data.Timezone, &from, &through)
	if errors.Is(err, pgx.ErrNoRows) {
		return Dataset{}, SnapshotRevision{}, ErrNoCompleteSnapshot
	}
	if err != nil {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read schedule snapshot metadata failed")
	}
	data.GeneratedAt = data.GeneratedAt.UTC()
	data.Window = Window{From: from.Format(dateLayout), Through: through.Format(dateLayout)}
	data.Theaters = []TheaterRecord{}
	data.Showtimes = []ShowtimeRecord{}
	theaterIndex := map[string]int{}
	rows, err := tx.Query(ctx, `SELECT provider, id, provider_id, slug, name, address, city, postal_code FROM theaters ORDER BY provider, provider_id, id`)
	if err != nil {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read theaters failed")
	}
	for rows.Next() {
		var theater TheaterRecord
		if err := rows.Scan(&theater.Provider, &theater.ID, &theater.ProviderID, &theater.Slug, &theater.Name, &theater.Address, &theater.City, &theater.PostalCode); err != nil {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("read theaters failed")
		}
		if _, exists := theaterIndex[theater.ID]; exists {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("duplicate theater row")
		}
		theater.AvailableDates = []string{}
		theater.AcceptedPasses = []string{}
		theaterIndex[theater.ID] = len(data.Theaters)
		data.Theaters = append(data.Theaters, theater)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read theaters failed")
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT theater_id, service_date FROM theater_dates ORDER BY theater_id, service_date`)
	if err != nil {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read theater dates failed")
	}
	for rows.Next() {
		var theaterID string
		var date time.Time
		if err := rows.Scan(&theaterID, &date); err != nil {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("read theater dates failed")
		}
		index, ok := theaterIndex[theaterID]
		if !ok {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("unrepresentable theater date row")
		}
		data.Theaters[index].AvailableDates = append(data.Theaters[index].AvailableDates, date.Format(dateLayout))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read theater dates failed")
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT tp.theater_id, tp.pass_code FROM theater_passes tp ORDER BY tp.theater_id, tp.pass_code`)
	if err != nil {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read theater passes failed")
	}
	linkedPasses := 0
	for rows.Next() {
		var theaterID, code string
		if err := rows.Scan(&theaterID, &code); err != nil {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("read theater passes failed")
		}
		index, ok := theaterIndex[theaterID]
		if !ok || code != "UGC_ILLIMITE" || recordProvider(data.Theaters[index].Provider, theaterID) != ProviderUGC || len(data.Theaters[index].AcceptedPasses) != 0 {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("unrepresentable theater pass row")
		}
		data.Theaters[index].AcceptedPasses = append(data.Theaters[index].AcceptedPasses, code)
		linkedPasses++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read theater passes failed")
	}
	rows.Close()
	var passCount int
	ugcTheaters := 0
	for _, theater := range data.Theaters {
		if theater.Provider == ProviderUGC {
			ugcTheaters++
		}
	}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM passes").Scan(&passCount); err != nil || passCount != 1 || linkedPasses != ugcTheaters {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("unrepresentable pass rows")
	}
	movies := map[string]MovieRecord{}
	rows, err = tx.Query(ctx, `SELECT m.provider, m.provider_id, m.slug, m.title, m.runtime_minutes, COALESCE(m.poster_url, ''), COALESCE(m.source_overview,''), COALESCE(m.source_release_date::text,''), m.source_genres, c.provider_movie_id, COALESCE(c.overview, ''), COALESCE(c.release_date::text, ''), COALESCE(c.genres, '{}'), COALESCE(c.poster_url, ''), COALESCE(c.backdrop_url, '')
FROM movies m
LEFT JOIN movie_matches mm ON mm.source_provider=m.provider AND mm.source_movie_id=m.provider_id AND mm.metadata_provider='tmdb' AND mm.status='matched'
LEFT JOIN movie_metadata_cache c ON c.provider='tmdb' AND c.provider_movie_id=mm.metadata_movie_id AND c.locale='fr-FR'
ORDER BY m.provider, m.provider_id`)
	if err != nil {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read movies failed")
	}
	for rows.Next() {
		var movie MovieRecord
		var tmdbID *int64
		var sourceOverview, sourceReleaseDate string
		var sourceGenres []string
		var overview, releaseDate, poster, backdrop string
		var genres []string
		if err := rows.Scan(&movie.Provider, &movie.ProviderID, &movie.Slug, &movie.Title, &movie.RuntimeMinutes, &movie.PosterURL, &sourceOverview, &sourceReleaseDate, &sourceGenres, &tmdbID, &overview, &releaseDate, &genres, &poster, &backdrop); err != nil {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("read movies failed")
		}
		movie.Overview, movie.ReleaseDate, movie.Genres = sourceOverview, sourceReleaseDate, append([]string(nil), sourceGenres...)
		if tmdbID != nil && *tmdbID > 0 {
			movie.Enrichment = &MovieEnrichment{TMDBID: *tmdbID, Overview: overview, ReleaseDate: releaseDate, Genres: append([]string(nil), genres...), PosterURL: poster, BackdropURL: backdrop}
		}
		movieKey := movie.Provider + "\x00" + movie.ProviderID
		if _, exists := movies[movieKey]; exists {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("duplicate movie row")
		}
		movies[movieKey] = movie
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read movies failed")
	}
	rows.Close()
	if err := materializeLocalMovies(ctx, tx, movies); err != nil {
		return Dataset{}, SnapshotRevision{}, err
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("load schedule timezone failed")
	}
	referencedMovies := map[string]bool{}
	rows, err = tx.Query(ctx, `SELECT provider, id, provider_showing_id, service_date, theater_id, movie_provider_id, start_time, end_time, language, provider_version, format, room, booking_url FROM showtimes ORDER BY theater_id, service_date, start_time, id`)
	if err != nil {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read showtimes failed")
	}
	for rows.Next() {
		var showing ShowtimeRecord
		var date time.Time
		var movieID string
		if err := rows.Scan(&showing.Provider, &showing.ID, &showing.ProviderShowingID, &date, &showing.TheaterID, &movieID, &showing.StartTime, &showing.EndTime, &showing.Language, &showing.ProviderVersion, &showing.Format, &showing.Room, &showing.BookingURL); err != nil {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("read showtimes failed")
		}
		movieKey := showing.Provider + "\x00" + movieID
		movie, ok := movies[movieKey]
		if !ok {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("showtime references missing movie")
		}
		showing.ServiceDate = date.Format(dateLayout)
		showing.Movie = movie
		showing.StartTime = showing.StartTime.In(location)
		if movie.LocalMovieID > 0 {
			runtime, ok := RuntimeDuration(movie.RuntimeMinutes)
			if !ok {
				rows.Close()
				return Dataset{}, SnapshotRevision{}, fmt.Errorf("invalid local movie runtime")
			}
			showing.EndTime = showing.StartTime.Add(runtime)
		} else {
			showing.EndTime = showing.EndTime.In(location)
		}
		referencedMovies[movieKey] = true
		data.Showtimes = append(data.Showtimes, showing)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("read showtimes failed")
	}
	rows.Close()
	if len(referencedMovies) != len(movies) {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("unreferenced movie row")
	}
	if revision.ScheduleVersion <= 0 || revision.EnrichmentVersion < 0 {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("invalid schedule snapshot revision")
	}
	if err := ValidateDataset(data, true); err != nil {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("loaded schedule dataset is invalid: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Dataset{}, SnapshotRevision{}, fmt.Errorf("commit schedule load failed")
	}
	return data, revision, nil
}

func materializeLocalMovies(ctx context.Context, tx pgx.Tx, movies map[string]MovieRecord) error {
	groups := make(map[int64]*localMovieGroupRow)
	rows, err := tx.Query(ctx, `SELECT id, primary_source_provider, primary_source_movie_id FROM local_movie_groups ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read local movie groups failed")
	}
	for rows.Next() {
		group := localMovieGroupRow{}
		if err := rows.Scan(&group.id, &group.primaryProvider, &group.primaryMovieID); err != nil {
			rows.Close()
			return fmt.Errorf("read local movie groups failed")
		}
		if group.id <= 0 || !validProvider(group.primaryProvider, false) || !validProviderIdentity(group.primaryProvider, "movie", group.primaryMovieID) {
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
		key := provider + "\x00" + movieID
		if group == nil || !validProvider(provider, false) || !validProviderIdentity(provider, "movie", movieID) {
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
		primaryKey := group.primaryProvider + "\x00" + group.primaryMovieID
		if len(group.members) < 2 || !contains(group.members, primaryKey) {
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
