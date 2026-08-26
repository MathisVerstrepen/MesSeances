package schedulepg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/geocoding"
	"messeances/api/internal/schedule"
)

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
	data.PublicMovies, data.MovieSources, data.MovieAliases, err = loadPublicMovieCatalog(ctx, tx)
	if err != nil {
		return schedule.Dataset{}, schedule.SnapshotRevision{}, err
	}
	if revision.ScheduleVersion <= 0 || revision.EnrichmentVersion < 0 || revision.TheaterLocationVersion < 0 {
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
	err := tx.QueryRow(ctx, `SELECT s.version, e.version, l.version, s.schema_version, s.provider, s.scope, s.generated_at, s.timezone, s.window_from, s.window_through FROM schedule_snapshot s CROSS JOIN movie_enrichment_state e CROSS JOIN theater_location_state l WHERE s.singleton=true AND e.singleton=true AND l.singleton=true`).Scan(&revision.ScheduleVersion, &revision.EnrichmentVersion, &revision.TheaterLocationVersion, &data.SchemaVersion, &provider, &scope, &data.GeneratedAt, &data.Timezone, &from, &through)
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
		showing.EndTime = showing.EndTime.In(location)
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
	rows, err := tx.Query(ctx, `SELECT t.provider, t.id, t.provider_id, t.slug, t.name, t.address, t.city, t.postal_code,
       l.latitude, l.longitude, l.status, l.address_hash
FROM theaters t
LEFT JOIN theater_locations l ON l.provider=t.provider AND l.provider_theater_id=t.provider_id
WHERE t.generation_id=$1
ORDER BY t.provider, t.provider_id, t.id`, version)
	if err != nil {
		return nil, fmt.Errorf("read theaters failed")
	}
	for rows.Next() {
		var theater schedule.TheaterRecord
		var provider string
		var latitude, longitude *float64
		var status, addressHash *string
		if err := rows.Scan(&provider, &theater.ID, &theater.ProviderID, &theater.Slug, &theater.Name, &theater.Address, &theater.City, &theater.PostalCode, &latitude, &longitude, &status, &addressHash); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read theaters failed")
		}
		theater.Provider = schedule.Provider(provider)
		if status != nil && (*status == string(geocoding.StatusManual) || *status == string(geocoding.StatusMatched) && addressHash != nil && *addressHash == geocoding.AddressHash(theater.Address, theater.PostalCode, theater.City)) {
			theater.Latitude, theater.Longitude = latitude, longitude
		}
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
	rows, err := tx.Query(ctx, `SELECT m.provider, m.provider_id, m.slug, m.title, m.runtime_minutes, COALESCE(m.poster_url, ''), COALESCE(m.source_overview,''), COALESCE(m.source_release_date::text,''), m.source_genres, source.public_movie_id
FROM movies m
JOIN public_movie_sources source ON source.source_provider=m.provider AND source.source_movie_id=m.provider_id
WHERE m.generation_id=$1
ORDER BY m.provider, m.provider_id`, version)
	if err != nil {
		return nil, fmt.Errorf("read movies failed")
	}
	for rows.Next() {
		var movie schedule.MovieRecord
		var provider string
		var sourceOverview, sourceReleaseDate string
		var sourceGenres []string
		if err := rows.Scan(&provider, &movie.ProviderID, &movie.Slug, &movie.Title, &movie.RuntimeMinutes, &movie.PosterURL, &sourceOverview, &sourceReleaseDate, &sourceGenres, &movie.PublicMovieID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read movies failed")
		}
		movie.Provider = schedule.Provider(provider)
		movie.Overview, movie.ReleaseDate, movie.Genres = sourceOverview, sourceReleaseDate, append([]string(nil), sourceGenres...)
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
	return movies, nil
}

func loadPublicMovieCatalog(ctx context.Context, tx pgx.Tx) ([]schedule.PublicMovieRecord, []schedule.PublicMovieSourceRecord, []schedule.MovieSlugAliasRecord, error) {
	movies := []schedule.PublicMovieRecord{}
	rows, err := tx.Query(ctx, `SELECT id, COALESCE(redirect_to_id,0), identity_anchor_provider, identity_anchor_source_movie_id,
       title, runtime_minutes, COALESCE(poster_url,''), COALESCE(backdrop_url,''), COALESCE(overview,''),
       COALESCE(release_date::text,''), genres, COALESCE(confirmed_tmdb_id,0), updated_at
FROM public_movies ORDER BY id`)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read public movies failed")
	}
	for rows.Next() {
		var movie schedule.PublicMovieRecord
		var provider string
		if err := rows.Scan(&movie.ID, &movie.RedirectToID, &provider, &movie.IdentityAnchorSourceID, &movie.Title, &movie.RuntimeMinutes, &movie.PosterURL, &movie.BackdropURL, &movie.Overview, &movie.ReleaseDate, &movie.Genres, &movie.TMDBID, &movie.UpdatedAt); err != nil {
			rows.Close()
			return nil, nil, nil, fmt.Errorf("read public movies failed")
		}
		movie.IdentityAnchorProvider = schedule.Provider(provider)
		movie.UpdatedAt = movie.UpdatedAt.UTC()
		movies = append(movies, movie)
	}
	if rows.Err() != nil {
		rows.Close()
		return nil, nil, nil, fmt.Errorf("read public movies failed")
	}
	rows.Close()

	sources := []schedule.PublicMovieSourceRecord{}
	rows, err = tx.Query(ctx, `SELECT source_provider, source_movie_id, public_movie_id, source_slug, title, runtime_minutes,
       COALESCE(poster_url,''), COALESCE(overview,''), COALESCE(release_date::text,''), genres
FROM public_movie_sources ORDER BY source_provider, source_movie_id`)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read public movie sources failed")
	}
	for rows.Next() {
		var source schedule.PublicMovieSourceRecord
		var provider string
		if err := rows.Scan(&provider, &source.SourceMovieID, &source.PublicMovieID, &source.SourceSlug, &source.Title, &source.RuntimeMinutes, &source.PosterURL, &source.Overview, &source.ReleaseDate, &source.Genres); err != nil {
			rows.Close()
			return nil, nil, nil, fmt.Errorf("read public movie sources failed")
		}
		source.Provider = schedule.Provider(provider)
		sources = append(sources, source)
	}
	if rows.Err() != nil {
		rows.Close()
		return nil, nil, nil, fmt.Errorf("read public movie sources failed")
	}
	rows.Close()

	aliases := []schedule.MovieSlugAliasRecord{}
	rows, err = tx.Query(ctx, `SELECT slug, public_movie_id, alias_kind, COALESCE(source_provider,''), COALESCE(source_movie_id,'') FROM movie_slug_aliases ORDER BY slug`)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read movie aliases failed")
	}
	for rows.Next() {
		var alias schedule.MovieSlugAliasRecord
		var provider string
		if err := rows.Scan(&alias.Slug, &alias.PublicMovieID, &alias.Kind, &provider, &alias.SourceMovieID); err != nil {
			rows.Close()
			return nil, nil, nil, fmt.Errorf("read movie aliases failed")
		}
		alias.Provider = schedule.Provider(provider)
		aliases = append(aliases, alias)
	}
	if rows.Err() != nil {
		rows.Close()
		return nil, nil, nil, fmt.Errorf("read movie aliases failed")
	}
	rows.Close()
	return movies, sources, aliases, nil
}
