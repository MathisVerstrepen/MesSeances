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
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoCompleteSnapshot = errors.New("no complete schedule snapshot")

const snapshotWriterLockID int64 = 6211428337968315

type SnapshotReader interface {
	CurrentRevision(context.Context) (SnapshotRevision, error)
	Load(context.Context) (Dataset, SnapshotRevision, error)
}

type SnapshotRevision struct {
	ScheduleVersion   int64
	EnrichmentVersion int64
}

type SnapshotWriter interface {
	Replace(context.Context, Dataset) (int64, error)
}

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) CurrentRevision(ctx context.Context) (SnapshotRevision, error) {
	var revision SnapshotRevision
	err := s.pool.QueryRow(ctx, `SELECT s.version, e.version FROM schedule_snapshot s CROSS JOIN movie_enrichment_state e WHERE s.singleton=true AND e.singleton=true`).Scan(&revision.ScheduleVersion, &revision.EnrichmentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return SnapshotRevision{}, ErrNoCompleteSnapshot
	}
	if err != nil {
		return SnapshotRevision{}, fmt.Errorf("read schedule snapshot revision failed")
	}
	if revision.ScheduleVersion <= 0 || revision.EnrichmentVersion < 0 {
		return SnapshotRevision{}, fmt.Errorf("invalid schedule snapshot revision")
	}
	return revision, nil
}

func (s *PostgresStore) CurrentVersion(ctx context.Context) (int64, error) {
	revision, err := s.CurrentRevision(ctx)
	return revision.ScheduleVersion, err
}

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

type localMovieGroupRow struct {
	id              int64
	primaryProvider string
	primaryMovieID  string
	members         []string
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
	if _, err := tx.Exec(ctx, "DELETE FROM showtimes WHERE provider=$1", data.Provider); err != nil {
		return 0, fmt.Errorf("clear provider showtimes failed")
	}
	if _, err := tx.Exec(ctx, "DELETE FROM theaters WHERE provider=$1", data.Provider); err != nil {
		return 0, fmt.Errorf("clear provider theaters failed")
	}
	if _, err := tx.Exec(ctx, "DELETE FROM movies WHERE provider=$1", data.Provider); err != nil {
		return 0, fmt.Errorf("clear provider movies failed")
	}
	theaterRows := make([][]any, 0, len(data.Theaters))
	dateRows := make([][]any, 0)
	passLinkRows := make([][]any, 0, len(data.Theaters))
	location, _ := time.LoadLocation(Timezone)
	for _, theater := range data.Theaters {
		theaterRows = append(theaterRows, []any{theater.ID, theater.ProviderID, theater.Slug, theater.Name, theater.Address, theater.City, theater.PostalCode, recordProvider(theater.Provider, theater.ID)})
		for _, date := range theater.AvailableDates {
			parsed, _ := time.ParseInLocation(dateLayout, date, location)
			dateRows = append(dateRows, []any{theater.ID, parsed})
		}
		for _, pass := range theater.AcceptedPasses {
			passLinkRows = append(passLinkRows, []any{theater.ID, pass})
		}
	}
	if err := copyRows(ctx, tx, "theaters", []string{"id", "provider_id", "slug", "name", "address", "city", "postal_code", "provider"}, theaterRows); err != nil {
		return 0, fmt.Errorf("insert theaters failed")
	}
	if err := copyRows(ctx, tx, "theater_dates", []string{"theater_id", "service_date"}, dateRows); err != nil {
		return 0, fmt.Errorf("insert theater dates failed")
	}
	if _, err := tx.Exec(ctx, "INSERT INTO passes (code) VALUES ('UGC_ILLIMITE') ON CONFLICT DO NOTHING"); err != nil {
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
		var overview, releaseDate any
		if movie.overview != "" {
			overview = movie.overview
		}
		if movie.releaseDate != "" {
			releaseDate = movie.releaseDate
		}
		movieRows = append(movieRows, []any{movie.providerID, movie.slug, movie.title, movie.runtime, poster, movie.provider, overview, releaseDate, movie.genres})
	}
	if err := copyRows(ctx, tx, "movies", []string{"provider_id", "slug", "title", "runtime_minutes", "poster_url", "provider", "source_overview", "source_release_date", "source_genres"}, movieRows); err != nil {
		return 0, fmt.Errorf("insert movies failed")
	}
	showtimeRows := make([][]any, 0, len(data.Showtimes))
	for _, showing := range data.Showtimes {
		serviceDate, _ := time.ParseInLocation(dateLayout, showing.ServiceDate, location)
		showtimeRows = append(showtimeRows, []any{showing.ID, showing.ProviderShowingID, serviceDate, showing.TheaterID, showing.Movie.ProviderID, showing.StartTime, showing.EndTime, showing.Language, showing.ProviderVersion, showing.Format, showing.Room, showing.BookingURL, recordProvider(showing.Provider, showing.ID)})
	}
	if err := copyRows(ctx, tx, "showtimes", []string{"id", "provider_showing_id", "service_date", "theater_id", "movie_provider_id", "start_time", "end_time", "language", "provider_version", "format", "room", "booking_url", "provider"}, showtimeRows); err != nil {
		return 0, fmt.Errorf("insert showtimes failed")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO provider_snapshots (provider, schema_version, scope, generated_at, timezone, window_from, window_through) VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (provider) DO UPDATE SET schema_version=EXCLUDED.schema_version,scope=EXCLUDED.scope,generated_at=EXCLUDED.generated_at,timezone=EXCLUDED.timezone,window_from=EXCLUDED.window_from,window_through=EXCLUDED.window_through`, data.Provider, data.SchemaVersion, data.Scope, data.GeneratedAt, data.Timezone, data.Window.From, data.Window.Through); err != nil {
		return 0, fmt.Errorf("write provider snapshot metadata failed")
	}
	var combinedProvider string
	var combinedGenerated time.Time
	var combinedFrom, combinedThrough time.Time
	if err := tx.QueryRow(ctx, `SELECT CASE WHEN count(*)=1 THEN min(provider) ELSE 'combined' END, max(generated_at), min(window_from), max(window_through) FROM provider_snapshots`).Scan(&combinedProvider, &combinedGenerated, &combinedFrom, &combinedThrough); err != nil {
		return 0, fmt.Errorf("read combined provider failed")
	}
	var theaterCount, showtimeCount int
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM theaters), (SELECT count(*) FROM showtimes)`).Scan(&theaterCount, &showtimeCount); err != nil || !validDatasetRecordCounts(theaterCount, showtimeCount) || !ValidInclusiveDateWindow(combinedFrom, combinedThrough) {
		return 0, fmt.Errorf("combined schedule limit exceeded")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schedule_snapshot (singleton, version, schema_version, provider, scope, generated_at, timezone, window_from, window_through)
VALUES (true, $1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (singleton) DO UPDATE SET version=EXCLUDED.version, schema_version=EXCLUDED.schema_version, provider=EXCLUDED.provider, scope=EXCLUDED.scope, generated_at=EXCLUDED.generated_at, timezone=EXCLUDED.timezone, window_from=EXCLUDED.window_from, window_through=EXCLUDED.window_through`, version, data.SchemaVersion, combinedProvider, data.Scope, combinedGenerated, data.Timezone, combinedFrom, combinedThrough); err != nil {
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
		if len(data.Theaters) >= MaxTheaters {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("schedule theater limit exceeded")
		}
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
		if !ok || len(data.Theaters[index].AvailableDates) >= MaxAdvertisedDatesPerTheater {
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
		if len(movies) >= MaxShowtimes {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("schedule movie limit exceeded")
		}
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
		if len(data.Showtimes) >= MaxShowtimes {
			rows.Close()
			return Dataset{}, SnapshotRevision{}, fmt.Errorf("schedule showing limit exceeded")
		}
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
		if len(groups) >= MaxShowtimes {
			rows.Close()
			return fmt.Errorf("local movie group limit exceeded")
		}
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
	memberCount := 0
	memberGroups := make(map[string]int64)
	for rows.Next() {
		memberCount++
		if memberCount > MaxShowtimes {
			rows.Close()
			return fmt.Errorf("local movie member limit exceeded")
		}
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

func rollbackScheduleTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
