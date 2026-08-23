package schedulepg

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/publicmoviepg"
	"messeances/api/internal/schedule"
)

type movieRow struct {
	provider    string
	providerID  string
	slug        string
	title       string
	runtime     int
	poster      string
	overview    string
	releaseDate string
	genres      []string
}

func publicationMovieRows(movies []schedule.MovieRecord) []movieRow {
	rows := make([]movieRow, 0, len(movies))
	for _, movie := range movies {
		rows = append(rows, movieRow{string(movie.Provider), movie.ProviderID, movie.Slug, movie.Title, movie.RuntimeMinutes, movie.PosterURL, movie.Overview, movie.ReleaseDate, append([]string{}, movie.Genres...)})
	}
	return rows
}

func copyRows(ctx context.Context, tx pgx.Tx, table string, columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{table}, columns, pgx.CopyFromRows(rows))
	return err
}

func (s *Store) Replace(ctx context.Context, datasets []schedule.Dataset) (schedule.PublicationResult, error) {
	if len(datasets) == 0 || len(datasets) > 2 {
		return schedule.PublicationResult{}, fmt.Errorf("invalid schedule replacement batch")
	}
	datasets = append([]schedule.Dataset(nil), datasets...)
	movieSets := make([][]movieRow, len(datasets))
	providers := make(map[schedule.Provider]bool, len(datasets))
	for i := range datasets {
		publication, err := schedule.PreparePublication(datasets[i])
		if err != nil {
			return schedule.PublicationResult{}, err
		}
		datasets[i] = publication.Dataset
		if datasets[i].Provider != schedule.ProviderUGC && datasets[i].Provider != schedule.ProviderKinepolis || providers[datasets[i].Provider] {
			return schedule.PublicationResult{}, fmt.Errorf("invalid schedule replacement providers")
		}
		if i > 0 && (datasets[i].Scope != datasets[0].Scope || datasets[i].Timezone != datasets[0].Timezone || datasets[i].SchemaVersion != datasets[0].SchemaVersion) {
			return schedule.PublicationResult{}, fmt.Errorf("incompatible schedule replacement datasets")
		}
		providers[datasets[i].Provider] = true
		movieSets[i] = publicationMovieRows(publication.Movies)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return schedule.PublicationResult{}, fmt.Errorf("begin schedule replacement failed")
	}
	defer rollbackScheduleTx(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", snapshotWriterLockID); err != nil {
		return schedule.PublicationResult{}, fmt.Errorf("lock schedule replacement failed")
	}
	version, current := int64(1), int64(0)
	err = tx.QueryRow(ctx, "SELECT version FROM schedule_snapshot WHERE singleton = true").Scan(&current)
	if err == nil {
		if current <= 0 || current == math.MaxInt64 {
			return schedule.PublicationResult{}, fmt.Errorf("schedule snapshot version exhausted")
		}
		version = current + 1
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return schedule.PublicationResult{}, fmt.Errorf("read schedule replacement version failed")
	}
	result := schedule.PublicationResult{Version: version, Providers: make(map[schedule.Provider]schedule.PublicationMetrics, len(datasets))}
	for i, data := range datasets {
		movieIDs := make([]string, 0, len(movieSets[i]))
		for _, movie := range movieSets[i] {
			movieIDs = append(movieIDs, movie.providerID)
		}
		showingIDs := make([]string, 0, len(data.Showtimes))
		for _, showing := range data.Showtimes {
			showingIDs = append(showingIDs, showing.ProviderShowingID)
		}
		metrics := schedule.PublicationMetrics{Movies: len(movieIDs), NewMovies: len(movieIDs), Showtimes: len(showingIDs), NewShowtimes: len(showingIDs)}
		if current > 0 {
			var existingMovies, existingShowtimes int
			if err := tx.QueryRow(ctx, `SELECT count(DISTINCT provider_id) FROM movies WHERE generation_id=$1 AND provider=$2 AND provider_id=ANY($3)`, current, string(data.Provider), movieIDs).Scan(&existingMovies); err != nil {
				return schedule.PublicationResult{}, fmt.Errorf("count existing provider movies failed")
			}
			if err := tx.QueryRow(ctx, `SELECT count(DISTINCT provider_showing_id) FROM showtimes WHERE generation_id=$1 AND provider=$2 AND provider_showing_id=ANY($3)`, current, string(data.Provider), showingIDs).Scan(&existingShowtimes); err != nil {
				return schedule.PublicationResult{}, fmt.Errorf("count existing provider showtimes failed")
			}
			metrics.NewMovies -= existingMovies
			metrics.NewShowtimes -= existingShowtimes
		}
		result.Providers[data.Provider] = metrics
	}
	if current > 0 {
		for _, table := range []string{"showtimes", "theater_passes", "theater_dates", "movies", "theaters", "provider_snapshots"} {
			if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE generation_id < $1", current); err != nil {
				return schedule.PublicationResult{}, fmt.Errorf("prune schedule generations failed")
			}
		}
		copies := []string{
			`INSERT INTO provider_snapshots SELECT provider, schema_version, scope, generated_at, timezone, window_from, window_through, $1 FROM provider_snapshots WHERE generation_id=$2`,
			`INSERT INTO theaters SELECT id, provider_id, slug, name, address, city, postal_code, provider, $1 FROM theaters WHERE generation_id=$2`,
			`INSERT INTO theater_dates SELECT theater_id, service_date, $1 FROM theater_dates WHERE generation_id=$2`,
			`INSERT INTO theater_passes SELECT theater_id, pass_code, $1 FROM theater_passes WHERE generation_id=$2`,
			`INSERT INTO movies SELECT provider_id, slug, title, runtime_minutes, poster_url, provider, source_overview, source_release_date, source_genres, $1 FROM movies WHERE generation_id=$2`,
			`INSERT INTO showtimes SELECT id, provider_showing_id, service_date, theater_id, movie_provider_id, start_time, end_time, language, provider_version, format, room, booking_url, provider, $1 FROM showtimes WHERE generation_id=$2`,
		}
		for _, query := range copies {
			if _, err := tx.Exec(ctx, query, version, current); err != nil {
				return schedule.PublicationResult{}, fmt.Errorf("copy active schedule generation failed")
			}
		}
	}
	for batchIndex, data := range datasets {
		if _, err := tx.Exec(ctx, "DELETE FROM showtimes WHERE generation_id=$1 AND provider=$2", version, string(data.Provider)); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("clear candidate provider showtimes failed")
		}
		if _, err := tx.Exec(ctx, "DELETE FROM theaters WHERE generation_id=$1 AND provider=$2", version, string(data.Provider)); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("clear candidate provider theaters failed")
		}
		if _, err := tx.Exec(ctx, "DELETE FROM movies WHERE generation_id=$1 AND provider=$2", version, string(data.Provider)); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("clear candidate provider movies failed")
		}
		if _, err := tx.Exec(ctx, "DELETE FROM provider_snapshots WHERE generation_id=$1 AND provider=$2", version, string(data.Provider)); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("clear candidate provider metadata failed")
		}
		theaterRows := make([][]any, 0, len(data.Theaters))
		dateRows := make([][]any, 0)
		passLinkRows := make([][]any, 0, len(data.Theaters))
		for _, theater := range data.Theaters {
			theaterRows = append(theaterRows, []any{theater.ID, theater.ProviderID, theater.Slug, theater.Name, theater.Address, theater.City, theater.PostalCode, string(theater.Provider), version})
			for _, date := range theater.AvailableDates {
				parsed, _ := schedule.ParseServiceDate(date)
				dateRows = append(dateRows, []any{theater.ID, parsed, version})
			}
			for _, pass := range theater.AcceptedPasses {
				passLinkRows = append(passLinkRows, []any{theater.ID, pass, version})
			}
		}
		if err := copyRows(ctx, tx, "theaters", []string{"id", "provider_id", "slug", "name", "address", "city", "postal_code", "provider", "generation_id"}, theaterRows); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("insert theaters failed")
		}
		if err := copyRows(ctx, tx, "theater_dates", []string{"theater_id", "service_date", "generation_id"}, dateRows); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("insert theater dates failed")
		}
		if _, err := tx.Exec(ctx, "INSERT INTO passes (code) VALUES ('UGC_ILLIMITE') ON CONFLICT DO NOTHING"); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("insert passes failed")
		}
		if err := copyRows(ctx, tx, "theater_passes", []string{"theater_id", "pass_code", "generation_id"}, passLinkRows); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("insert theater passes failed")
		}
		movieRows := make([][]any, 0, len(movieSets[batchIndex]))
		for _, movie := range movieSets[batchIndex] {
			var poster any
			if movie.poster != "" {
				poster = movie.poster
			}
			var overview, releaseDate any
			if movie.overview != "" {
				overview = movie.overview
			}
			if movie.releaseDate != "" {
				releaseDate = movie.releaseDate
			}
			movieRows = append(movieRows, []any{movie.providerID, movie.slug, movie.title, movie.runtime, poster, movie.provider, overview, releaseDate, movie.genres, version})
		}
		if err := copyRows(ctx, tx, "movies", []string{"provider_id", "slug", "title", "runtime_minutes", "poster_url", "provider", "source_overview", "source_release_date", "source_genres", "generation_id"}, movieRows); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("insert movies failed")
		}
		showtimeRows := make([][]any, 0, len(data.Showtimes))
		for _, showing := range data.Showtimes {
			serviceDate, _ := schedule.ParseServiceDate(showing.ServiceDate)
			showtimeRows = append(showtimeRows, []any{showing.ID, showing.ProviderShowingID, serviceDate, showing.TheaterID, showing.Movie.ProviderID, showing.StartTime, showing.EndTime, string(showing.Language), showing.ProviderVersion, string(showing.Format), showing.Room, showing.BookingURL, string(showing.Provider), version})
		}
		if err := copyRows(ctx, tx, "showtimes", []string{"id", "provider_showing_id", "service_date", "theater_id", "movie_provider_id", "start_time", "end_time", "language", "provider_version", "format", "room", "booking_url", "provider", "generation_id"}, showtimeRows); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("insert showtimes failed")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO provider_snapshots (generation_id, provider, schema_version, scope, generated_at, timezone, window_from, window_through) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, version, string(data.Provider), data.SchemaVersion, string(data.Scope), data.GeneratedAt, data.Timezone, data.Window.From, data.Window.Through); err != nil {
			return schedule.PublicationResult{}, fmt.Errorf("write provider snapshot metadata failed")
		}
	}
	var combinedProvider string
	var combinedGenerated time.Time
	var combinedFrom, combinedThrough time.Time
	if err := tx.QueryRow(ctx, `SELECT CASE WHEN count(*)=1 THEN min(provider) ELSE 'combined' END, max(generated_at), min(window_from), max(window_through) FROM provider_snapshots WHERE generation_id=$1`, version).Scan(&combinedProvider, &combinedGenerated, &combinedFrom, &combinedThrough); err != nil {
		return schedule.PublicationResult{}, fmt.Errorf("read combined provider failed")
	}
	if !schedule.ValidInclusiveDateWindow(combinedFrom, combinedThrough) {
		return schedule.PublicationResult{}, fmt.Errorf("combined schedule window exceeded")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schedule_snapshot (singleton, version, schema_version, provider, scope, generated_at, timezone, window_from, window_through)
	VALUES (true, $1, $2, $3, $4, $5, $6, $7, $8)
	ON CONFLICT (singleton) DO UPDATE SET version=EXCLUDED.version, schema_version=EXCLUDED.schema_version, provider=EXCLUDED.provider, scope=EXCLUDED.scope, generated_at=EXCLUDED.generated_at, timezone=EXCLUDED.timezone, window_from=EXCLUDED.window_from, window_through=EXCLUDED.window_through`, version, datasets[0].SchemaVersion, combinedProvider, string(datasets[0].Scope), combinedGenerated, datasets[0].Timezone, combinedFrom, combinedThrough); err != nil {
		return schedule.PublicationResult{}, fmt.Errorf("write schedule snapshot metadata failed")
	}
	if err := publicmoviepg.Reconcile(ctx, tx); err != nil {
		return schedule.PublicationResult{}, fmt.Errorf("reconcile public movies during schedule replacement: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return schedule.PublicationResult{}, fmt.Errorf("commit schedule replacement failed")
	}
	return result, nil
}
