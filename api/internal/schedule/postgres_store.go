package schedule

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoCompleteSnapshot = errors.New("no complete schedule snapshot")

const snapshotWriterLockID int64 = 6211428337968315

type SnapshotReader interface {
	CurrentVersion(context.Context) (int64, error)
	Load(context.Context) (Dataset, int64, error)
}

type SnapshotWriter interface {
	Replace(context.Context, Dataset) (int64, error)
}

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) CurrentVersion(ctx context.Context) (int64, error) {
	var version int64
	err := s.pool.QueryRow(ctx, "SELECT version FROM schedule_snapshot WHERE singleton = true").Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNoCompleteSnapshot
	}
	if err != nil {
		return 0, fmt.Errorf("read schedule snapshot version failed")
	}
	if version <= 0 {
		return 0, fmt.Errorf("invalid schedule snapshot version")
	}
	return version, nil
}

type movieRow struct {
	providerID string
	slug       string
	title      string
	runtime    int
	poster     string
}

func prepareMovies(data Dataset) ([]movieRow, error) {
	byID := make(map[string]movieRow)
	for _, showing := range data.Showtimes {
		candidate := movieRow{showing.Movie.ProviderID, showing.Movie.Slug, showing.Movie.Title, showing.Movie.RuntimeMinutes, showing.Movie.PosterURL}
		if prior, exists := byID[candidate.providerID]; exists && prior != candidate {
			return nil, fmt.Errorf("conflicting movie metadata")
		}
		byID[candidate.providerID] = candidate
	}
	rows := make([]movieRow, 0, len(byID))
	for _, movie := range byID {
		rows = append(rows, movie)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].providerID < rows[j].providerID })
	return rows, nil
}

func (s *PostgresStore) Replace(ctx context.Context, data Dataset) (int64, error) {
	if err := ValidateDataset(data, true); err != nil {
		return 0, err
	}
	movies, err := prepareMovies(data)
	if err != nil {
		return 0, err
	}
	data = cloneDataset(data)
	normalizeDataset(&data)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin schedule replacement failed")
	}
	defer rollbackScheduleTx(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", snapshotWriterLockID); err != nil {
		return 0, fmt.Errorf("lock schedule replacement failed")
	}
	version := int64(1)
	var current int64
	err = tx.QueryRow(ctx, "SELECT version FROM schedule_snapshot WHERE singleton = true").Scan(&current)
	if err == nil {
		if current <= 0 || current == math.MaxInt64 {
			return 0, fmt.Errorf("schedule snapshot version exhausted")
		}
		version = current + 1
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("read schedule replacement version failed")
	}
	for _, table := range []string{"showtimes", "theater_passes", "theater_dates", "passes", "movies", "theaters"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+table); err != nil {
			return 0, fmt.Errorf("clear schedule table failed")
		}
	}
	theaterRows := make([][]any, 0, len(data.Theaters))
	dateRows := make([][]any, 0)
	passLinkRows := make([][]any, 0, len(data.Theaters))
	location, _ := time.LoadLocation(Timezone)
	for _, theater := range data.Theaters {
		theaterRows = append(theaterRows, []any{theater.ID, theater.ProviderID, theater.Slug, theater.Name, theater.Address, theater.City, theater.PostalCode})
		for _, date := range theater.AvailableDates {
			parsed, _ := time.ParseInLocation(dateLayout, date, location)
			dateRows = append(dateRows, []any{theater.ID, parsed})
		}
		passLinkRows = append(passLinkRows, []any{theater.ID, "UGC_ILLIMITE"})
	}
	if err := copyRows(ctx, tx, "theaters", []string{"id", "provider_id", "slug", "name", "address", "city", "postal_code"}, theaterRows); err != nil {
		return 0, fmt.Errorf("insert theaters failed")
	}
	if err := copyRows(ctx, tx, "theater_dates", []string{"theater_id", "service_date"}, dateRows); err != nil {
		return 0, fmt.Errorf("insert theater dates failed")
	}
	if _, err := tx.Exec(ctx, "INSERT INTO passes (code) VALUES ('UGC_ILLIMITE')"); err != nil {
		return 0, fmt.Errorf("insert passes failed")
	}
	if err := copyRows(ctx, tx, "theater_passes", []string{"theater_id", "pass_code"}, passLinkRows); err != nil {
		return 0, fmt.Errorf("insert theater passes failed")
	}
	movieRows := make([][]any, 0, len(movies))
	for _, movie := range movies {
		var poster any
		if movie.poster != "" {
			poster = movie.poster
		}
		movieRows = append(movieRows, []any{movie.providerID, movie.slug, movie.title, int16(movie.runtime), poster})
	}
	if err := copyRows(ctx, tx, "movies", []string{"provider_id", "slug", "title", "runtime_minutes", "poster_url"}, movieRows); err != nil {
		return 0, fmt.Errorf("insert movies failed")
	}
	showtimeRows := make([][]any, 0, len(data.Showtimes))
	for _, showing := range data.Showtimes {
		serviceDate, _ := time.ParseInLocation(dateLayout, showing.ServiceDate, location)
		showtimeRows = append(showtimeRows, []any{showing.ID, showing.ProviderShowingID, serviceDate, showing.TheaterID, showing.Movie.ProviderID, showing.StartTime, showing.EndTime, showing.Language, showing.ProviderVersion, showing.Format, showing.Room, showing.BookingURL})
	}
	if err := copyRows(ctx, tx, "showtimes", []string{"id", "provider_showing_id", "service_date", "theater_id", "movie_provider_id", "start_time", "end_time", "language", "provider_version", "format", "room", "booking_url"}, showtimeRows); err != nil {
		return 0, fmt.Errorf("insert showtimes failed")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schedule_snapshot (singleton, version, schema_version, provider, scope, generated_at, timezone, window_from, window_through)
VALUES (true, $1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (singleton) DO UPDATE SET version=EXCLUDED.version, schema_version=EXCLUDED.schema_version, provider=EXCLUDED.provider, scope=EXCLUDED.scope, generated_at=EXCLUDED.generated_at, timezone=EXCLUDED.timezone, window_from=EXCLUDED.window_from, window_through=EXCLUDED.window_through`, version, data.SchemaVersion, data.Provider, data.Scope, data.GeneratedAt, data.Timezone, data.Window.From, data.Window.Through); err != nil {
		return 0, fmt.Errorf("write schedule snapshot metadata failed")
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit schedule replacement failed")
	}
	return version, nil
}

func copyRows(ctx context.Context, tx pgx.Tx, table string, columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{table}, columns, pgx.CopyFromRows(rows))
	return err
}

func (s *PostgresStore) Load(ctx context.Context) (Dataset, int64, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Dataset{}, 0, fmt.Errorf("begin schedule load failed")
	}
	defer rollbackScheduleTx(tx)
	var data Dataset
	var version int64
	var from, through time.Time
	err = tx.QueryRow(ctx, `SELECT version, schema_version, provider, scope, generated_at, timezone, window_from, window_through FROM schedule_snapshot WHERE singleton = true`).Scan(&version, &data.SchemaVersion, &data.Provider, &data.Scope, &data.GeneratedAt, &data.Timezone, &from, &through)
	if errors.Is(err, pgx.ErrNoRows) {
		return Dataset{}, 0, ErrNoCompleteSnapshot
	}
	if err != nil {
		return Dataset{}, 0, fmt.Errorf("read schedule snapshot metadata failed")
	}
	data.GeneratedAt = data.GeneratedAt.UTC()
	data.Window = Window{From: from.Format(dateLayout), Through: through.Format(dateLayout)}
	data.Theaters = []TheaterRecord{}
	data.Showtimes = []ShowtimeRecord{}
	theaterIndex := map[string]int{}
	rows, err := tx.Query(ctx, `SELECT id, provider_id, slug, name, address, city, postal_code FROM theaters ORDER BY provider_id, id`)
	if err != nil {
		return Dataset{}, 0, fmt.Errorf("read theaters failed")
	}
	for rows.Next() {
		if len(data.Theaters) >= MaxTheaters {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("schedule theater limit exceeded")
		}
		var theater TheaterRecord
		if err := rows.Scan(&theater.ID, &theater.ProviderID, &theater.Slug, &theater.Name, &theater.Address, &theater.City, &theater.PostalCode); err != nil {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("read theaters failed")
		}
		if _, exists := theaterIndex[theater.ID]; exists {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("duplicate theater row")
		}
		theater.AvailableDates = []string{}
		theater.AcceptedPasses = []string{}
		theaterIndex[theater.ID] = len(data.Theaters)
		data.Theaters = append(data.Theaters, theater)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Dataset{}, 0, fmt.Errorf("read theaters failed")
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT theater_id, service_date FROM theater_dates ORDER BY theater_id, service_date`)
	if err != nil {
		return Dataset{}, 0, fmt.Errorf("read theater dates failed")
	}
	for rows.Next() {
		var theaterID string
		var date time.Time
		if err := rows.Scan(&theaterID, &date); err != nil {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("read theater dates failed")
		}
		index, ok := theaterIndex[theaterID]
		if !ok || len(data.Theaters[index].AvailableDates) >= MaxAdvertisedDatesPerTheater {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("unrepresentable theater date row")
		}
		data.Theaters[index].AvailableDates = append(data.Theaters[index].AvailableDates, date.Format(dateLayout))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Dataset{}, 0, fmt.Errorf("read theater dates failed")
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT tp.theater_id, tp.pass_code FROM theater_passes tp ORDER BY tp.theater_id, tp.pass_code`)
	if err != nil {
		return Dataset{}, 0, fmt.Errorf("read theater passes failed")
	}
	linkedPasses := 0
	for rows.Next() {
		var theaterID, code string
		if err := rows.Scan(&theaterID, &code); err != nil {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("read theater passes failed")
		}
		index, ok := theaterIndex[theaterID]
		if !ok || code != "UGC_ILLIMITE" || len(data.Theaters[index].AcceptedPasses) != 0 {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("unrepresentable theater pass row")
		}
		data.Theaters[index].AcceptedPasses = append(data.Theaters[index].AcceptedPasses, code)
		linkedPasses++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Dataset{}, 0, fmt.Errorf("read theater passes failed")
	}
	rows.Close()
	var passCount int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM passes").Scan(&passCount); err != nil || passCount != 1 || linkedPasses != len(data.Theaters) {
		return Dataset{}, 0, fmt.Errorf("unrepresentable pass rows")
	}
	movies := map[string]MovieRecord{}
	rows, err = tx.Query(ctx, `SELECT provider_id, slug, title, runtime_minutes, COALESCE(poster_url, '') FROM movies ORDER BY provider_id`)
	if err != nil {
		return Dataset{}, 0, fmt.Errorf("read movies failed")
	}
	for rows.Next() {
		if len(movies) >= MaxShowtimes {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("schedule movie limit exceeded")
		}
		var movie MovieRecord
		if err := rows.Scan(&movie.ProviderID, &movie.Slug, &movie.Title, &movie.RuntimeMinutes, &movie.PosterURL); err != nil {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("read movies failed")
		}
		if _, exists := movies[movie.ProviderID]; exists {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("duplicate movie row")
		}
		movies[movie.ProviderID] = movie
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Dataset{}, 0, fmt.Errorf("read movies failed")
	}
	rows.Close()
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return Dataset{}, 0, fmt.Errorf("load schedule timezone failed")
	}
	referencedMovies := map[string]bool{}
	rows, err = tx.Query(ctx, `SELECT id, provider_showing_id, service_date, theater_id, movie_provider_id, start_time, end_time, language, provider_version, format, room, booking_url FROM showtimes ORDER BY theater_id, service_date, start_time, id`)
	if err != nil {
		return Dataset{}, 0, fmt.Errorf("read showtimes failed")
	}
	for rows.Next() {
		if len(data.Showtimes) >= MaxShowtimes {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("schedule showing limit exceeded")
		}
		var showing ShowtimeRecord
		var date time.Time
		var movieID string
		if err := rows.Scan(&showing.ID, &showing.ProviderShowingID, &date, &showing.TheaterID, &movieID, &showing.StartTime, &showing.EndTime, &showing.Language, &showing.ProviderVersion, &showing.Format, &showing.Room, &showing.BookingURL); err != nil {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("read showtimes failed")
		}
		movie, ok := movies[movieID]
		if !ok {
			rows.Close()
			return Dataset{}, 0, fmt.Errorf("showtime references missing movie")
		}
		showing.ServiceDate = date.Format(dateLayout)
		showing.Movie = movie
		showing.StartTime = showing.StartTime.In(location)
		showing.EndTime = showing.EndTime.In(location)
		referencedMovies[movieID] = true
		data.Showtimes = append(data.Showtimes, showing)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Dataset{}, 0, fmt.Errorf("read showtimes failed")
	}
	rows.Close()
	if len(referencedMovies) != len(movies) {
		return Dataset{}, 0, fmt.Errorf("unreferenced movie row")
	}
	if version <= 0 {
		return Dataset{}, 0, fmt.Errorf("invalid schedule snapshot version")
	}
	if err := ValidateDataset(data, true); err != nil {
		return Dataset{}, 0, fmt.Errorf("loaded schedule dataset is invalid: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Dataset{}, 0, fmt.Errorf("commit schedule load failed")
	}
	return data, version, nil
}

func rollbackScheduleTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
