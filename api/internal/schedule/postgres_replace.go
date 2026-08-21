package schedule

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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

func prepareMovies(data Dataset) ([]movieRow, error) {
	byID := make(map[string]movieRow)
	for _, showing := range data.Showtimes {
		provider := recordProvider(showing.Movie.Provider, showing.Movie.Slug)
		candidate := movieRow{provider, showing.Movie.ProviderID, showing.Movie.Slug, showing.Movie.Title, showing.Movie.RuntimeMinutes, showing.Movie.PosterURL, showing.Movie.Overview, showing.Movie.ReleaseDate, append([]string{}, showing.Movie.Genres...)}
		key := provider + "\x00" + candidate.providerID
		if prior, exists := byID[key]; exists && (prior.provider != candidate.provider || prior.providerID != candidate.providerID || prior.slug != candidate.slug || prior.title != candidate.title || prior.runtime != candidate.runtime || prior.poster != candidate.poster || prior.overview != candidate.overview || prior.releaseDate != candidate.releaseDate || strings.Join(prior.genres, "\x00") != strings.Join(candidate.genres, "\x00")) {
			return nil, fmt.Errorf("conflicting movie metadata")
		}
		byID[key] = candidate
	}
	rows := make([]movieRow, 0, len(byID))
	for _, movie := range byID {
		rows = append(rows, movie)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].provider+"\x00"+rows[i].providerID < rows[j].provider+"\x00"+rows[j].providerID
	})
	return rows, nil
}

func copyRows(ctx context.Context, tx pgx.Tx, table string, columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{table}, columns, pgx.CopyFromRows(rows))
	return err
}

func (s *PostgresStore) Replace(ctx context.Context, datasets []Dataset) (int64, error) {
	if len(datasets) == 0 || len(datasets) > 2 {
		return 0, fmt.Errorf("invalid schedule replacement batch")
	}
	datasets = append([]Dataset(nil), datasets...)
	movieSets := make([][]movieRow, len(datasets))
	providers := make(map[string]bool, len(datasets))
	for i := range datasets {
		if err := ValidateDataset(datasets[i], true); err != nil {
			return 0, err
		}
		if datasets[i].Provider != ProviderUGC && datasets[i].Provider != ProviderKinepolis || providers[datasets[i].Provider] {
			return 0, fmt.Errorf("invalid schedule replacement providers")
		}
		if i > 0 && (datasets[i].Scope != datasets[0].Scope || datasets[i].Timezone != datasets[0].Timezone || datasets[i].SchemaVersion != datasets[0].SchemaVersion) {
			return 0, fmt.Errorf("incompatible schedule replacement datasets")
		}
		providers[datasets[i].Provider] = true
		var err error
		movieSets[i], err = prepareMovies(datasets[i])
		if err != nil {
			return 0, err
		}
		datasets[i] = cloneDataset(datasets[i])
		normalizeDataset(&datasets[i])
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin schedule replacement failed")
	}
	defer rollbackScheduleTx(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", snapshotWriterLockID); err != nil {
		return 0, fmt.Errorf("lock schedule replacement failed")
	}
	version, current := int64(1), int64(0)
	err = tx.QueryRow(ctx, "SELECT version FROM schedule_snapshot WHERE singleton = true").Scan(&current)
	if err == nil {
		if current <= 0 || current == math.MaxInt64 {
			return 0, fmt.Errorf("schedule snapshot version exhausted")
		}
		version = current + 1
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("read schedule replacement version failed")
	}
	if current > 0 {
		for _, table := range []string{"showtimes", "theater_passes", "theater_dates", "movies", "theaters", "provider_snapshots"} {
			if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE generation_id < $1", current); err != nil {
				return 0, fmt.Errorf("prune schedule generations failed")
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
				return 0, fmt.Errorf("copy active schedule generation failed")
			}
		}
	}
	for batchIndex, data := range datasets {
		if _, err := tx.Exec(ctx, "DELETE FROM showtimes WHERE generation_id=$1 AND provider=$2", version, data.Provider); err != nil {
			return 0, fmt.Errorf("clear candidate provider showtimes failed")
		}
		if _, err := tx.Exec(ctx, "DELETE FROM theaters WHERE generation_id=$1 AND provider=$2", version, data.Provider); err != nil {
			return 0, fmt.Errorf("clear candidate provider theaters failed")
		}
		if _, err := tx.Exec(ctx, "DELETE FROM movies WHERE generation_id=$1 AND provider=$2", version, data.Provider); err != nil {
			return 0, fmt.Errorf("clear candidate provider movies failed")
		}
		if _, err := tx.Exec(ctx, "DELETE FROM provider_snapshots WHERE generation_id=$1 AND provider=$2", version, data.Provider); err != nil {
			return 0, fmt.Errorf("clear candidate provider metadata failed")
		}
		theaterRows := make([][]any, 0, len(data.Theaters))
		dateRows := make([][]any, 0)
		passLinkRows := make([][]any, 0, len(data.Theaters))
		location, _ := time.LoadLocation(Timezone)
		for _, theater := range data.Theaters {
			theaterRows = append(theaterRows, []any{theater.ID, theater.ProviderID, theater.Slug, theater.Name, theater.Address, theater.City, theater.PostalCode, recordProvider(theater.Provider, theater.ID), version})
			for _, date := range theater.AvailableDates {
				parsed, _ := time.ParseInLocation(dateLayout, date, location)
				dateRows = append(dateRows, []any{theater.ID, parsed, version})
			}
			for _, pass := range theater.AcceptedPasses {
				passLinkRows = append(passLinkRows, []any{theater.ID, pass, version})
			}
		}
		if err := copyRows(ctx, tx, "theaters", []string{"id", "provider_id", "slug", "name", "address", "city", "postal_code", "provider", "generation_id"}, theaterRows); err != nil {
			return 0, fmt.Errorf("insert theaters failed")
		}
		if err := copyRows(ctx, tx, "theater_dates", []string{"theater_id", "service_date", "generation_id"}, dateRows); err != nil {
			return 0, fmt.Errorf("insert theater dates failed")
		}
		if _, err := tx.Exec(ctx, "INSERT INTO passes (code) VALUES ('UGC_ILLIMITE') ON CONFLICT DO NOTHING"); err != nil {
			return 0, fmt.Errorf("insert passes failed")
		}
		if err := copyRows(ctx, tx, "theater_passes", []string{"theater_id", "pass_code", "generation_id"}, passLinkRows); err != nil {
			return 0, fmt.Errorf("insert theater passes failed")
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
			return 0, fmt.Errorf("insert movies failed")
		}
		showtimeRows := make([][]any, 0, len(data.Showtimes))
		for _, showing := range data.Showtimes {
			serviceDate, _ := time.ParseInLocation(dateLayout, showing.ServiceDate, location)
			showtimeRows = append(showtimeRows, []any{showing.ID, showing.ProviderShowingID, serviceDate, showing.TheaterID, showing.Movie.ProviderID, showing.StartTime, showing.EndTime, showing.Language, showing.ProviderVersion, showing.Format, showing.Room, showing.BookingURL, recordProvider(showing.Provider, showing.ID), version})
		}
		if err := copyRows(ctx, tx, "showtimes", []string{"id", "provider_showing_id", "service_date", "theater_id", "movie_provider_id", "start_time", "end_time", "language", "provider_version", "format", "room", "booking_url", "provider", "generation_id"}, showtimeRows); err != nil {
			return 0, fmt.Errorf("insert showtimes failed")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO provider_snapshots (generation_id, provider, schema_version, scope, generated_at, timezone, window_from, window_through) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, version, data.Provider, data.SchemaVersion, data.Scope, data.GeneratedAt, data.Timezone, data.Window.From, data.Window.Through); err != nil {
			return 0, fmt.Errorf("write provider snapshot metadata failed")
		}
	}
	var combinedProvider string
	var combinedGenerated time.Time
	var combinedFrom, combinedThrough time.Time
	if err := tx.QueryRow(ctx, `SELECT CASE WHEN count(*)=1 THEN min(provider) ELSE 'combined' END, max(generated_at), min(window_from), max(window_through) FROM provider_snapshots WHERE generation_id=$1`, version).Scan(&combinedProvider, &combinedGenerated, &combinedFrom, &combinedThrough); err != nil {
		return 0, fmt.Errorf("read combined provider failed")
	}
	if !ValidInclusiveDateWindow(combinedFrom, combinedThrough) {
		return 0, fmt.Errorf("combined schedule window exceeded")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schedule_snapshot (singleton, version, schema_version, provider, scope, generated_at, timezone, window_from, window_through)
	VALUES (true, $1, $2, $3, $4, $5, $6, $7, $8)
	ON CONFLICT (singleton) DO UPDATE SET version=EXCLUDED.version, schema_version=EXCLUDED.schema_version, provider=EXCLUDED.provider, scope=EXCLUDED.scope, generated_at=EXCLUDED.generated_at, timezone=EXCLUDED.timezone, window_from=EXCLUDED.window_from, window_through=EXCLUDED.window_through`, version, datasets[0].SchemaVersion, combinedProvider, datasets[0].Scope, combinedGenerated, datasets[0].Timezone, combinedFrom, combinedThrough); err != nil {
		return 0, fmt.Errorf("write schedule snapshot metadata failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit schedule replacement failed")
	}
	return version, nil
}
