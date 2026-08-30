package schedulepg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/geocoding"
	"messeances/api/internal/schedule"
)

var ErrNoCompleteSnapshot = schedule.ErrNoCompleteSnapshot

type Dataset = schedule.Dataset
type Window = schedule.Window
type TheaterRecord = schedule.TheaterRecord
type MovieRecord = schedule.MovieRecord
type ShowtimeRecord = schedule.ShowtimeRecord

const (
	Timezone          = schedule.Timezone
	ProviderUGC       = schedule.ProviderUGC
	ProviderKinepolis = schedule.ProviderKinepolis
	ProviderPathe     = schedule.ProviderPathe
	ProviderCGR       = schedule.ProviderCGR
	ProviderCombined  = schedule.ProviderCombined
	ScopeAll          = schedule.ScopeAll
	ScopeSingle       = schedule.ScopeSingle
	LanguageAll       = schedule.LanguageAll
	LanguageVOSTFR    = schedule.LanguageVOSTFR
	LanguageVF        = schedule.LanguageVF
	Format2D          = schedule.Format2D
	Format3D          = schedule.Format3D
	FormatIMAX        = schedule.FormatIMAX
	FormatDolby       = schedule.FormatDolby
	FormatScreenX     = schedule.FormatScreenX
	FormatLaserUltra  = schedule.FormatLaserUltra
	Format4DX         = schedule.Format4DX
	FormatICE         = schedule.FormatICE
)

type PostgresSource = schedule.PostgresSource
type ServiceOptions = schedule.ServiceOptions
type TimelineQuery = schedule.TimelineQuery
type MovieCatalogQuery = schedule.MovieCatalogQuery
type MovieShowtimesQuery = schedule.MovieShowtimesQuery

var NewPostgresSource = schedule.NewPostgresSource
var NewService = schedule.NewService
var RuntimeDuration = schedule.RuntimeDuration

func testDataset() Dataset {
	location, _ := time.LoadLocation(Timezone)
	showing := func(id, theater, movieID, title, poster, clock string, language schedule.Language, runtime int) ShowtimeRecord {
		start, _ := time.ParseInLocation("2006-01-02 15:04", "2026-08-15 "+clock, location)
		if start.Hour() < 8 {
			start = start.AddDate(0, 0, 1)
		}
		return ShowtimeRecord{ID: "ugc-showing-" + id, ProviderShowingID: id, ServiceDate: "2026-08-15", TheaterID: theater, Movie: MovieRecord{ProviderID: movieID, Slug: "ugc-film-" + movieID, Title: title, RuntimeMinutes: runtime, PosterURL: poster}, StartTime: start, EndTime: start.Add(time.Duration(runtime) * time.Minute), Language: language, ProviderVersion: string(language), Format: Format2D, Room: "Salle 1", BookingURL: "https://www.ugc.fr/reservationSeances.html?id=" + id}
	}
	return Dataset{SchemaVersion: schedule.SchemaVersion, Provider: ProviderUGC, Scope: ScopeAll, GeneratedAt: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC), Timezone: Timezone, Window: Window{From: "2026-08-15", Through: "2026-08-15"}, Theaters: []TheaterRecord{{ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC Lille", Address: "Lille", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}, {ID: "ugc-26", ProviderID: "26", Slug: "ugc-26", Name: "UGC Villeneuve", Address: "Villeneuve", City: "Villeneuve d'Ascq", PostalCode: "59650", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}, {ID: "ugc-99", ProviderID: "99", Slug: "ugc-99", Name: "UGC Lyon", Address: "Lyon", City: "Lyon", PostalCode: "69000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}}, Showtimes: []ShowtimeRecord{showing("100", "ugc-25", "200", "Film A", "https://static.ugc.fr/posters/200.jpg", "12:00", LanguageVOSTFR, 100), showing("104", "ugc-26", "200", "Film A", "https://static.ugc.fr/posters/200.jpg", "18:00", LanguageVOSTFR, 100), showing("101", "ugc-25", "201", "Film B", "", "14:30", LanguageVF, 95), showing("102", "ugc-26", "202", "Film C", "", "00:15", LanguageVF, 75), showing("103", "ugc-99", "203", "Film D", "", "12:30", LanguageVF, 90)}}
}

func kinepolisTestDataset() Dataset {
	location, _ := time.LoadLocation(Timezone)
	start, _ := time.ParseInLocation("2006-01-02 15:04", "2026-08-15 20:00", location)
	return Dataset{SchemaVersion: schedule.SchemaVersion, Provider: ProviderKinepolis, Scope: ScopeAll, GeneratedAt: time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC), Timezone: Timezone, Window: Window{From: "2026-08-15", Through: "2026-08-15"}, Theaters: []TheaterRecord{{Provider: ProviderKinepolis, ID: "kinepolis-LOM", ProviderID: "LOM", Slug: "kinepolis-LOM", Name: "Kinepolis Lomme", City: "Lomme", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{}}}, Showtimes: []ShowtimeRecord{{Provider: ProviderKinepolis, ID: "kinepolis-showing-VS1", ProviderShowingID: "VS1", ServiceDate: "2026-08-15", TheaterID: "kinepolis-LOM", Movie: MovieRecord{Provider: ProviderKinepolis, ProviderID: "HO200", Slug: "kinepolis-film-HO200", Title: "Film A", RuntimeMinutes: 100, PosterURL: "https://cdn.kinepolis.fr/images/posters/ho200.jpg", Overview: "Résumé Kinepolis", ReleaseDate: "2026-01-02", Genres: []string{"Drame"}}, StartTime: start, EndTime: start.Add(100 * time.Minute), Language: LanguageVF, ProviderVersion: "VF", Format: FormatIMAX, Room: "7", BookingURL: "https://kinepolis.fr/direct-vista-redirect/VS1/0/LOM/0"}}}
}

func patheTestDataset() Dataset {
	location, _ := time.LoadLocation(Timezone)
	start, _ := time.ParseInLocation("2006-01-02 15:04", "2026-08-15 21:00", location)
	return Dataset{SchemaVersion: schedule.SchemaVersion, Provider: ProviderPathe, Scope: ScopeAll, GeneratedAt: time.Date(2026, 8, 14, 14, 0, 0, 0, time.UTC), Timezone: Timezone, Window: Window{From: "2026-08-15", Through: "2026-08-15"}, Theaters: []TheaterRecord{{Provider: ProviderPathe, ID: "pathe-lille", ProviderID: "lille", Slug: "pathe-lille", Name: "Pathé Lille", Address: "1 rue du Cinéma", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{}}}, Showtimes: []ShowtimeRecord{{Provider: ProviderPathe, ID: "pathe-showing-V3308S135392", ProviderShowingID: "V3308S135392", ServiceDate: "2026-08-15", TheaterID: "pathe-lille", Movie: MovieRecord{Provider: ProviderPathe, ProviderID: "film-a", Slug: "pathe-film-film-a", Title: "Film A", RuntimeMinutes: 100, PosterURL: "https://www.pathe.fr/media/poster.jpg", Genres: []string{"Drame"}}, StartTime: start, EndTime: start.Add(120 * time.Minute), Language: LanguageVF, ProviderVersion: "vf", Format: FormatICE, Room: "ICE", BookingURL: "https://s.pathe.fr/fr/V3308S135392/booking"}}}
}

func cgrTestDataset() Dataset {
	location, _ := time.LoadLocation(Timezone)
	start, _ := time.ParseInLocation("2006-01-02 15:04", "2026-08-15 19:00", location)
	showingID := "W8010-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	return Dataset{SchemaVersion: schedule.SchemaVersion, Provider: ProviderCGR, Scope: ScopeAll, GeneratedAt: time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC), Timezone: Timezone, Window: Window{From: "2026-08-15", Through: "2026-08-15"}, Theaters: []TheaterRecord{{Provider: ProviderCGR, ID: "cgr-W8010", ProviderID: "W8010", Slug: "cgr-W8010", Name: "CGR Lille", Address: "2 rue du Cinéma", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{}}}, Showtimes: []ShowtimeRecord{{Provider: ProviderCGR, ID: "cgr-showing-" + showingID, ProviderShowingID: showingID, ServiceDate: "2026-08-15", TheaterID: "cgr-W8010", Movie: MovieRecord{Provider: ProviderCGR, ProviderID: "1001", Slug: "cgr-film-1001", Title: "Conférence CGR", RuntimeMinutes: 0, PosterURL: "https://images.acsta.net/posters/1001.jpg"}, StartTime: start, EndTime: start, Language: schedule.Language("SPANISH"), ProviderVersion: "Localization.Language.Spanish", Format: Format2D, Room: "", BookingURL: "https://achat.cgrcinemas.fr/lille/r/12345"}}}
}

func TestPostgresStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate test schema nonce failed")
	}
	schema := "movieflow_test_" + hex.EncodeToString(nonce)
	identifier := pgx.Identifier{schema}.Sanitize()
	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create integration schema %s failed", schema)
	}
	t.Cleanup(func() {
		if schema == "" || !strings.HasPrefix(schema, "movieflow_test_") {
			t.Errorf("unsafe integration schema cleanup rejected")
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Errorf("drop integration schema %s failed", schema)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration pool configuration failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	var currentSchema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&currentSchema); err != nil || currentSchema != schema {
		t.Fatalf("isolated schema assertion failed for %s", schema)
	}
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("first migration run failed")
	}
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("repeat migration run failed")
	}
	var generationColumns, generationIndexes, generationFKs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name IN ('provider_snapshots','theaters','theater_dates','theater_passes','movies','showtimes') AND column_name='generation_id' AND is_nullable='NO'`).Scan(&generationColumns); err != nil || generationColumns != 6 {
		t.Fatalf("generation columns=%d err=%v", generationColumns, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_class i JOIN pg_namespace n ON n.oid=i.relnamespace JOIN pg_index x ON x.indexrelid=i.oid JOIN pg_attribute a ON a.attrelid=x.indrelid AND a.attnum=x.indkey[0] WHERE n.nspname=current_schema() AND i.relname IN ('theaters_city_lower_idx','theater_dates_service_date_idx','theater_passes_pass_code_idx','showtimes_service_theater_start_idx','showtimes_service_window_idx','showtimes_movie_service_start_idx') AND a.attname='generation_id'`).Scan(&generationIndexes); err != nil || generationIndexes != 6 {
		t.Fatalf("generation-leading indexes=%d err=%v", generationIndexes, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint c JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=c.conkey[1] WHERE c.connamespace=current_schema()::regnamespace AND c.contype='f' AND c.conrelid IN ('theater_dates'::regclass,'theater_passes'::regclass,'showtimes'::regclass) AND a.attname='generation_id'`).Scan(&generationFKs); err != nil || generationFKs != 4 {
		t.Fatalf("generation foreign keys=%d err=%v", generationFKs, err)
	}
	localTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin local group fixture failed")
	}
	var durableLocalID int64
	if err := localTx.QueryRow(ctx, "INSERT INTO local_movie_groups (primary_source_provider, primary_source_movie_id) VALUES ('ugc','900') RETURNING id").Scan(&durableLocalID); err != nil {
		t.Fatal("insert local group fixture failed")
	}
	if _, err := localTx.Exec(ctx, "INSERT INTO local_movie_group_members (local_movie_id, source_provider, source_movie_id) VALUES ($1,'ugc','900'),($1,'kinepolis','DURABLE-900')", durableLocalID); err != nil {
		t.Fatal("insert local member fixtures failed")
	}
	if err := localTx.Commit(ctx); err != nil {
		t.Fatal("commit local group fixture failed")
	}
	invalidPrimaryTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin invalid primary fixture failed")
	}
	var invalidPrimaryID int64
	if err := invalidPrimaryTx.QueryRow(ctx, "INSERT INTO local_movie_groups (primary_source_provider, primary_source_movie_id) VALUES ('ugc','901') RETURNING id").Scan(&invalidPrimaryID); err != nil {
		t.Fatal("insert invalid primary group failed")
	}
	if _, err := invalidPrimaryTx.Exec(ctx, "INSERT INTO local_movie_group_members (local_movie_id, source_provider, source_movie_id) VALUES ($1,'ugc','902')", invalidPrimaryID); err != nil {
		t.Fatal("insert invalid primary member failed")
	}
	if err := invalidPrimaryTx.Commit(ctx); err == nil {
		t.Fatal("primary outside member set accepted")
	}
	duplicateTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin duplicate member fixture failed")
	}
	var duplicateID int64
	if err := duplicateTx.QueryRow(ctx, "INSERT INTO local_movie_groups (primary_source_provider, primary_source_movie_id) VALUES ('ugc','903') RETURNING id").Scan(&duplicateID); err != nil {
		t.Fatal("insert duplicate group failed")
	}
	if _, err := duplicateTx.Exec(ctx, "INSERT INTO local_movie_group_members (local_movie_id, source_provider, source_movie_id) VALUES ($1,'ugc','903')", duplicateID); err != nil {
		t.Fatal("insert duplicate primary failed")
	}
	if _, err := duplicateTx.Exec(ctx, "INSERT INTO local_movie_group_members (local_movie_id, source_provider, source_movie_id) VALUES ($1,'kinepolis','DURABLE-900')", duplicateID); err == nil {
		t.Fatal("duplicate source membership accepted")
	}
	_ = duplicateTx.Rollback(ctx)
	store := NewStore(pool)
	if _, err := store.CurrentRevision(ctx); !errors.Is(err, ErrNoCompleteSnapshot) {
		t.Fatalf("missing current version error=%v", err)
	}
	if _, _, err := store.Load(ctx); !errors.Is(err, ErrNoCompleteSnapshot) {
		t.Fatalf("missing load error=%v", err)
	}

	t.Run("initial insert and load", func(t *testing.T) {
		version, err := store.Replace(ctx, []Dataset{testDataset()})
		if err != nil || version.Version != 1 {
			t.Fatalf("replace version=%+v error=%v", version, err)
		}
		metrics := version.Providers[ProviderUGC]
		if metrics.Movies != 4 || metrics.NewMovies != 4 || metrics.Showtimes != 5 || metrics.NewShowtimes != 5 {
			t.Fatalf("initial publication metrics=%+v", metrics)
		}
		var durableGroups int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM local_movie_groups WHERE id=$1", durableLocalID).Scan(&durableGroups); err != nil || durableGroups != 1 {
			t.Fatalf("provider replacement removed local group count=%d error=%v", durableGroups, err)
		}
		loaded, loadedVersion, err := store.Load(ctx)
		if err != nil || loadedVersion.ScheduleVersion != 1 || loadedVersion.EnrichmentVersion != 0 {
			t.Fatalf("load revision=%+v error=%v", loadedVersion, err)
		}
		if !loaded.GeneratedAt.Equal(testDataset().GeneratedAt) || loaded.GeneratedAt.Location() != time.UTC {
			t.Fatal("generated timestamp did not round trip in UTC")
		}
		nullPosterFound := false
		for _, showing := range loaded.Showtimes {
			if showing.StartTime.Location().String() != Timezone {
				t.Fatal("Paris timestamp did not round trip")
			}
			if showing.Movie.ProviderID == "201" && showing.Movie.PosterURL == "" {
				nullPosterFound = true
			}
		}
		if !nullPosterFound {
			t.Fatal("NULL poster did not round trip")
		}
		for _, format := range []schedule.Format{Format2D, Format3D, FormatIMAX, FormatDolby, FormatScreenX, FormatLaserUltra, Format4DX} {
			if _, err := pool.Exec(ctx, "UPDATE showtimes SET format=$1 WHERE id='ugc-showing-100'", string(format)); err != nil {
				t.Fatalf("canonical database format %q rejected: %v", format, err)
			}
		}
		if _, err := pool.Exec(ctx, "UPDATE showtimes SET format='ALL' WHERE id='ugc-showing-100'"); err == nil {
			t.Fatal("non-persisted ALL format accepted")
		}
		if _, err := pool.Exec(ctx, "UPDATE showtimes SET format='2D' WHERE id='ugc-showing-100'"); err != nil {
			t.Fatal("restore persisted format failed")
		}
	})

	t.Run("stable hash-aware theater locations", func(t *testing.T) {
		now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
		matchedHash := geocoding.AddressHash("Lille", "59000", "Lille")
		if _, err := pool.Exec(ctx, `INSERT INTO theater_locations (provider,provider_theater_id,latitude,longitude,source,matched_label,match_score,address_hash,status,updated_at) VALUES
('ugc','25',50.6321,3.0612,'ign','Lille',0.91,$1,'matched',$2),
('ugc','26',50.6200,3.1400,'manual',NULL,NULL,NULL,'manual',$2),
('ugc','99',NULL,NULL,'ign','Lyon',0.60,$3,'ambiguous',$2)`, matchedHash, now, geocoding.AddressHash("Lyon", "69000", "Lyon")); err != nil {
			t.Fatal("insert theater location fixtures failed")
		}
		if _, err := pool.Exec(ctx, "UPDATE theater_location_state SET version=version+1 WHERE singleton=true"); err != nil {
			t.Fatal("advance location fixture version failed")
		}
		loaded, revision, err := store.Load(ctx)
		if err != nil || revision.TheaterLocationVersion != 1 || loaded.Theaters[0].Latitude == nil || *loaded.Theaters[0].Latitude != 50.6321 || loaded.Theaters[1].Latitude == nil || *loaded.Theaters[1].Latitude != 50.62 || loaded.Theaters[2].Latitude != nil {
			t.Fatalf("revision=%+v theaters=%+v err=%v", revision, loaded.Theaters, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE theater_locations SET address_hash=$1 WHERE provider='ugc' AND provider_theater_id='25'`, geocoding.AddressHash("old", "59000", "Lille")); err != nil {
			t.Fatal("make location fixtures stale failed")
		}
		if _, err := pool.Exec(ctx, `UPDATE theater_locations SET matched_label=NULL,match_score=NULL,status='not_found' WHERE provider='ugc' AND provider_theater_id='99'`); err != nil {
			t.Fatal("make not-found fixture failed")
		}
		if _, err := pool.Exec(ctx, "UPDATE theater_location_state SET version=version+1 WHERE singleton=true"); err != nil {
			t.Fatal("advance stale location version failed")
		}
		loaded, revision, err = store.Load(ctx)
		if err != nil || revision.TheaterLocationVersion != 2 || loaded.Theaters[0].Latitude != nil || loaded.Theaters[1].Latitude == nil || loaded.Theaters[2].Latitude != nil {
			t.Fatalf("stale revision=%+v theaters=%+v err=%v", revision, loaded.Theaters, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE theater_locations SET address_hash=$1 WHERE provider='ugc' AND provider_theater_id='25'`, matchedHash); err != nil {
			t.Fatal("restore matching location failed")
		}
		if _, err := pool.Exec(ctx, "UPDATE theater_location_state SET version=version+1 WHERE singleton=true"); err != nil {
			t.Fatal("advance restored location version failed")
		}
	})

	t.Run("enrichment publication is durable and visible", func(t *testing.T) {
		now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
		match := enrichment.Match{SourceProvider: enrichment.SourceUGC, SourceMovieID: "200", MetadataProvider: enrichment.ProviderTMDB, Status: enrichment.StatusMatched, MetadataMovieID: 42, Score: 1, NormalizedSourceTitle: "film a", SourceRuntimeMinutes: 100, Candidates: []enrichment.Candidate{{ID: 42, Title: "Film A", Runtime: 100, Score: 1}}, EvaluatedAt: now, RetryAfter: now.Add(30 * 24 * time.Hour)}
		metadata := enrichment.Metadata{Provider: enrichment.ProviderTMDB, ProviderMovieID: 42, IMDBID: "tt1234567", Locale: enrichment.LocaleFrench, ProviderTitle: "Film A", LocalizedTitle: "Film A", Overview: "Résumé", ReleaseDate: "2026-01-02", PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/a.jpg", TrailerVFYouTubeKey: "FRoff123456", TrailerVOYouTubeKey: "ENoff123456", RuntimeMinutes: 100, Genres: []string{"Drame"}, FetchedAt: now, RefreshAfter: now.Add(30 * 24 * time.Hour)}
		if err := enrichment.NewPostgresStore(pool).Publish(ctx, match, metadata); err != nil {
			t.Fatal(err)
		}
		loaded, revision, err := store.Load(ctx)
		if err != nil || revision.EnrichmentVersion != 1 || len(loaded.PublicMovies) != 4 || loaded.PublicMovies[0].TMDBID != 42 || loaded.PublicMovies[0].IMDBID != metadata.IMDBID || loaded.PublicMovies[0].BackdropURL != metadata.BackdropURL || loaded.PublicMovies[0].TrailerVFYouTubeKey != metadata.TrailerVFYouTubeKey || loaded.PublicMovies[0].TrailerVOYouTubeKey != metadata.TrailerVOYouTubeKey {
			t.Fatalf("revision=%+v movie=%+v err=%v", revision, loaded.Showtimes[0].Movie, err)
		}
		publicID := loaded.PublicMovies[0].ID
		if _, err := pool.Exec(ctx, `INSERT INTO public_movie_metadata_overrides (
    public_movie_id,title,title_overridden,runtime_minutes,runtime_minutes_overridden,
    release_date,release_date_overridden,genres,genres_overridden,overview,overview_overridden,
    poster_url,poster_url_overridden,backdrop_url,backdrop_url_overridden,
    trailer_vf_youtube_key,trailer_vf_youtube_key_overridden,trailer_vo_youtube_key,trailer_vo_youtube_key_overridden
) VALUES ($1,'Titre manuel',true,111,true,NULL,true,'{}',true,NULL,true,
    'https://example.com/poster.jpg',true,'https://example.com/backdrop.jpg',true,NULL,true,'VOmanual123',true)`, publicID); err != nil {
			t.Fatal("insert effective metadata fixture failed")
		}
		loaded, _, err = store.Load(ctx)
		effective := loaded.PublicMovies[0]
		if err != nil || effective.Title != "Titre manuel" || effective.RuntimeMinutes != 111 || effective.ReleaseDate != "" || len(effective.Genres) != 0 || effective.Overview != "" || effective.PosterURL != "https://example.com/poster.jpg" || effective.BackdropURL != "https://example.com/backdrop.jpg" || effective.TrailerVFYouTubeKey != "" || effective.TrailerVOYouTubeKey != "VOmanual123" {
			t.Fatalf("effective movie=%+v err=%v", effective, err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM public_movie_metadata_overrides WHERE public_movie_id=$1", publicID); err != nil {
			t.Fatal("delete effective metadata fixture failed")
		}
	})

	var source *PostgresSource
	t.Run("source and complete replacement", func(t *testing.T) {
		var err error
		source, err = NewPostgresSource(ctx, store)
		if err != nil {
			t.Fatal(err)
		}
		replacement := testDataset()
		replacement.GeneratedAt = replacement.GeneratedAt.Add(time.Minute)
		replacement.Theaters = append([]TheaterRecord(nil), replacement.Theaters[0])
		replacement.Theaters[0].Name = "UGC Lille remplacé"
		replacement.Showtimes = append([]ShowtimeRecord(nil), replacement.Showtimes[0])
		version, err := store.Replace(ctx, []Dataset{replacement})
		if err != nil || version.Version != 2 {
			t.Fatalf("replace version=%+v error=%v", version, err)
		}
		metrics := version.Providers[ProviderUGC]
		if metrics.Movies != 1 || metrics.NewMovies != 0 || metrics.Showtimes != 1 || metrics.NewShowtimes != 0 {
			t.Fatalf("replacement publication metrics=%+v", metrics)
		}
		loaded, loadedVersion, err := store.Load(ctx)
		if err != nil || loadedVersion.ScheduleVersion != 2 || loadedVersion.EnrichmentVersion != 1 || loadedVersion.TheaterLocationVersion != 3 || len(loaded.Theaters) != 1 || len(loaded.Showtimes) != 1 || loaded.PublicMovies[0].TMDBID != 42 || loaded.Theaters[0].Latitude == nil || *loaded.Theaters[0].Latitude != 50.6321 {
			t.Fatalf("replacement load revision=%+v theaters=%d showtimes=%d error=%v", loadedVersion, len(loaded.Theaters), len(loaded.Showtimes), err)
		}
		var oldRows int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM theaters WHERE generation_id=2 AND id IN ('ugc-26', 'ugc-99')").Scan(&oldRows); err != nil || oldRows != 0 {
			t.Fatalf("old rows=%d", oldRows)
		}
		source, err = NewPostgresSource(ctx, store)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(source, ServiceOptions{DefaultCity: "Lille", CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}}})
		if err != nil {
			t.Fatal(err)
		}
		timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", Language: LanguageAll})
		if err != nil || len(timeline.Theaters) != 1 || timeline.Theaters[0].Name != "UGC Lille remplacé" {
			t.Fatalf("timeline=%+v error=%v", timeline, err)
		}
	})

	t.Run("provider scoped replacement preserves UGC", func(t *testing.T) {
		kinepolis := kinepolisTestDataset()
		kinepolis.Showtimes[0].Movie.RuntimeMinutes = 721
		kinepolis.Showtimes[0].EndTime = kinepolis.Showtimes[0].StartTime.Add(721 * time.Minute)
		version, err := store.Replace(ctx, []Dataset{kinepolis})
		if err != nil || version.Version != 3 {
			t.Fatalf("Kinepolis replace version=%+v error=%v", version, err)
		}
		loaded, revision, err := store.Load(ctx)
		if err != nil || revision.ScheduleVersion != 3 || loaded.Provider != ProviderCombined || len(loaded.Theaters) != 2 || len(loaded.Showtimes) != 2 {
			t.Fatalf("combined revision=%+v dataset=%+v error=%v", revision, loaded, err)
		}
		providers := map[schedule.Provider]bool{}
		marathonLoaded := false
		for _, showing := range loaded.Showtimes {
			marathonLoaded = marathonLoaded || showing.Movie.ProviderID == "HO200" && showing.Movie.RuntimeMinutes == 721 && showing.EndTime.Equal(showing.StartTime.Add(721*time.Minute))
		}
		if !marathonLoaded {
			t.Fatal("721-minute movie did not round trip")
		}
		for _, theater := range loaded.Theaters {
			providers[theater.Provider] = true
		}
		if !providers[ProviderUGC] || !providers[ProviderKinepolis] {
			t.Fatalf("providers=%v", providers)
		}
		for _, showing := range loaded.Showtimes {
			if showing.Provider == ProviderKinepolis && showing.Movie.Enrichment != nil {
				t.Fatal("UGC enrichment leaked into Kinepolis movie")
			}
		}
		ugc := testDataset()
		ugc.Theaters = append([]TheaterRecord(nil), ugc.Theaters[0])
		ugc.Theaters[0].Name = "UGC Lille encore"
		ugc.Showtimes = append([]ShowtimeRecord(nil), ugc.Showtimes[0])
		version, err = store.Replace(ctx, []Dataset{ugc})
		if err != nil || version.Version != 4 {
			t.Fatalf("UGC scoped replace version=%+v error=%v", version, err)
		}
		loaded, _, err = store.Load(ctx)
		if err != nil || len(loaded.Theaters) != 2 {
			t.Fatalf("second combined load=%+v error=%v", loaded, err)
		}
		foundKinepolis := false
		for _, theater := range loaded.Theaters {
			foundKinepolis = foundKinepolis || theater.Provider == ProviderKinepolis
		}
		if !foundKinepolis {
			t.Fatal("UGC replacement removed Kinepolis")
		}
	})

	t.Run("pre SQL rejection and rollback", func(t *testing.T) {
		single := testDataset()
		single.Scope = ScopeSingle
		if _, err := store.Replace(ctx, []Dataset{single}); err == nil {
			t.Fatal("single scope replacement accepted")
		}
		conflict := testDataset()
		conflict.Showtimes[2].Movie.ProviderID = conflict.Showtimes[0].Movie.ProviderID
		conflict.Showtimes[2].Movie.Slug = conflict.Showtimes[0].Movie.Slug
		if _, err := store.Replace(ctx, []Dataset{conflict}); err == nil {
			t.Fatal("conflicting movie replacement accepted")
		}
		invalidSQL := testDataset()
		invalidSQL.Theaters[0].Name = " "
		if _, err := store.Replace(ctx, []Dataset{invalidSQL}); err == nil {
			t.Fatal("constraint-breaking replacement accepted")
		}
		version, err := store.CurrentVersion(ctx)
		if err != nil || version != 4 {
			t.Fatalf("version after rollback=%d error=%v", version, err)
		}
		loaded, _, err := store.Load(ctx)
		if err != nil || len(loaded.Theaters) != 2 {
			t.Fatalf("last good after rollback=%+v error=%v", loaded.Theaters, err)
		}
	})

	t.Run("local movie materialization survives replacement and unmerge", func(t *testing.T) {
		serviceNow := func() time.Time { return time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC) }

		if _, err := pool.Exec(ctx, "DELETE FROM movie_matches WHERE source_provider='ugc' AND source_movie_id='200'"); err != nil {
			t.Fatal("clear prior TMDB match failed")
		}

		ugcCanonical := testDataset()
		ugcCanonical.GeneratedAt = ugcCanonical.GeneratedAt.Add(2 * time.Minute)
		for index := range ugcCanonical.Showtimes {
			if ugcCanonical.Showtimes[index].Movie.ProviderID != "200" {
				continue
			}
			movie := &ugcCanonical.Showtimes[index].Movie
			movie.Title = "Canonique UGC"
			movie.RuntimeMinutes = 120
			movie.PosterURL = "https://static.ugc.fr/posters/canonical.jpg"
			movie.Overview = "Résumé canonique UGC"
			movie.ReleaseDate = "2026-03-04"
			movie.Genres = []string{"Comédie", "Famille"}
			ugcCanonical.Showtimes[index].EndTime = ugcCanonical.Showtimes[index].StartTime.Add(120 * time.Minute)
		}
		if version, err := store.Replace(ctx, []Dataset{ugcCanonical}); err != nil || version.Version != 5 {
			t.Fatalf("canonical UGC replace version=%+v err=%v", version, err)
		}

		kinepolisFallback := kinepolisTestDataset()
		kinepolisFallback.GeneratedAt = kinepolisFallback.GeneratedAt.Add(2 * time.Minute)
		fallbackMovie := &kinepolisFallback.Showtimes[0].Movie
		fallbackMovie.Title = "Fallback Kinepolis"
		fallbackMovie.RuntimeMinutes = 80
		fallbackMovie.PosterURL = "https://cdn.kinepolis.fr/images/posters/fallback.jpg"
		fallbackMovie.Overview = "Résumé fallback Kinepolis"
		fallbackMovie.ReleaseDate = "2025-05-06"
		fallbackMovie.Genres = []string{"Drame"}
		kinepolisFallback.Showtimes[0].EndTime = kinepolisFallback.Showtimes[0].StartTime.Add(80 * time.Minute)
		if version, err := store.Replace(ctx, []Dataset{kinepolisFallback}); err != nil || version.Version != 6 {
			t.Fatalf("fallback Kinepolis replace version=%+v err=%v", version, err)
		}

		localService := enrichment.NewLocalMovieService(enrichment.NewPostgresStore(pool))
		ugcSource := enrichment.LocalMovieSource{SourceProvider: enrichment.SourceUGC, SourceMovieID: "200"}
		kinepolisSource := enrichment.LocalMovieSource{SourceProvider: enrichment.SourceKinepolis, SourceMovieID: "HO200"}
		group, err := localService.Merge(ctx, []enrichment.LocalMovieSource{ugcSource, kinepolisSource}, ugcSource)
		if err != nil || group.ID <= 0 || group.LocalMovieID == "" {
			t.Fatalf("merge group=%+v err=%v", group, err)
		}
		localSlug := group.LocalMovieID
		loaded, _, err := store.Load(ctx)
		if err != nil || len(loaded.Showtimes) != len(ugcCanonical.Showtimes)+len(kinepolisFallback.Showtimes) {
			t.Fatalf("merged snapshot showtimes=%d err=%v", len(loaded.Showtimes), err)
		}
		var canonicalID int64
		for _, showing := range loaded.Showtimes {
			if showing.Movie.ProviderID != "200" && showing.Movie.ProviderID != "HO200" {
				continue
			}
			if canonicalID == 0 {
				canonicalID = showing.Movie.PublicMovieID
			}
			if showing.Movie.PublicMovieID != canonicalID || !showing.EndTime.Equal(showing.StartTime.Add(time.Duration(showing.Movie.RuntimeMinutes)*time.Minute)) {
				t.Fatalf("source timing or canonical mapping lost: %+v", showing)
			}
		}
		canonicalSlug := "film-" + strconv.FormatInt(canonicalID, 10)
		source, err = NewPostgresSource(ctx, store)
		if err != nil {
			t.Fatal(err)
		}
		publicService, err := NewService(source, ServiceOptions{Now: serviceNow})
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := publicService.Movies(MovieCatalogQuery{Search: "Canonique", PageSize: 10})
		if err != nil || catalog.Total != 1 || len(catalog.Items) != 1 || catalog.Items[0].Slug != canonicalSlug || catalog.Items[0].TMDBID != nil {
			t.Fatalf("local catalog=%+v err=%v", catalog, err)
		}
		detail, err := publicService.MovieShowtimes(MovieShowtimesQuery{Slug: localSlug, Date: "2026-08-15"})
		if err != nil || len(detail.Theaters) != 3 {
			t.Fatalf("local detail=%+v err=%v", detail, err)
		}
		providers := map[schedule.Provider]bool{}
		for _, theater := range detail.Theaters {
			for _, showing := range theater.Showtimes {
				providers[showing.Provider] = true
				if showing.ID == "" || showing.BookingURL == nil || showing.Movie.Slug != canonicalSlug {
					t.Fatalf("source showtime identity lost: %+v", showing)
				}
			}
		}
		if !providers[ProviderUGC] || !providers[ProviderKinepolis] {
			t.Fatalf("local detail providers=%v", providers)
		}

		ugcCanonical.GeneratedAt = ugcCanonical.GeneratedAt.Add(time.Minute)
		if version, err := store.Replace(ctx, []Dataset{ugcCanonical}); err != nil || version.Version != 7 {
			t.Fatalf("grouped UGC replace version=%+v err=%v", version, err)
		}
		loaded, _, err = store.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		kinepolisFallback.GeneratedAt = kinepolisFallback.GeneratedAt.Add(time.Minute)
		if version, err := store.Replace(ctx, []Dataset{kinepolisFallback}); err != nil || version.Version != 8 {
			t.Fatalf("grouped Kinepolis replace version=%+v err=%v", version, err)
		}
		if _, err := publicService.MovieShowtimes(MovieShowtimesQuery{Slug: localSlug, Date: "2026-08-15"}); err != nil {
			t.Fatalf("local ID after Kinepolis replace: %v", err)
		}

		ugcWithoutPrimary := testDataset()
		ugcWithoutPrimary.GeneratedAt = ugcWithoutPrimary.GeneratedAt.Add(4 * time.Minute)
		ugcWithoutPrimary.Theaters = append([]TheaterRecord(nil), ugcWithoutPrimary.Theaters[0])
		ugcWithoutPrimary.Showtimes = append([]ShowtimeRecord(nil), ugcWithoutPrimary.Showtimes[2])
		if version, err := store.Replace(ctx, []Dataset{ugcWithoutPrimary}); err != nil || version.Version != 9 {
			t.Fatalf("remove primary version=%+v err=%v", version, err)
		}
		fallbackSnapshot, _, err := store.Load(ctx)
		if err != nil || len(fallbackSnapshot.Showtimes) != 2 {
			t.Fatalf("fallback snapshot=%+v err=%v", fallbackSnapshot.Showtimes, err)
		}

		ugcCanonical.GeneratedAt = ugcCanonical.GeneratedAt.Add(time.Minute)
		if version, err := store.Replace(ctx, []Dataset{ugcCanonical}); err != nil || version.Version != 10 {
			t.Fatalf("restore primary version=%+v err=%v", version, err)
		}
		if _, _, err := store.Load(ctx); err != nil {
			t.Fatal(err)
		}

		if err := localService.Unmerge(ctx, localSlug); err != nil {
			t.Fatalf("unmerge failed: %v", err)
		}
		unmerged, _, err := store.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		seenIDs := map[int64]bool{}
		for _, showing := range unmerged.Showtimes {
			if showing.Movie.ProviderID != "200" && showing.Movie.ProviderID != "HO200" {
				continue
			}
			seenIDs[showing.Movie.PublicMovieID] = true
			providerRuntime, _ := RuntimeDuration(showing.Movie.RuntimeMinutes)
			if !showing.EndTime.Equal(showing.StartTime.Add(providerRuntime)) {
				t.Fatalf("provider runtime not restored: %+v", showing)
			}
		}
		if len(seenIDs) != 2 {
			t.Fatalf("split public identities=%v", seenIDs)
		}
		source, err = NewPostgresSource(ctx, store)
		if err != nil {
			t.Fatal(err)
		}
		publicService, err = NewService(source, ServiceOptions{Now: serviceNow})
		if err != nil {
			t.Fatal(err)
		}
		if detail, err := publicService.MovieShowtimes(MovieShowtimesQuery{Slug: localSlug, Date: "2026-08-15"}); err != nil || detail.Movie.Slug != canonicalSlug {
			t.Fatalf("permanent local alias detail=%+v err=%v", detail, err)
		}
	})

	t.Run("candidate SQL failure rolls back staged generation", func(t *testing.T) {
		before, _, err := store.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_generation_11() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.generation_id=11 THEN RAISE EXCEPTION 'synthetic candidate failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_generation_11 BEFORE INSERT ON movies FOR EACH ROW EXECUTE FUNCTION reject_generation_11()`, pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatal("install candidate failure trigger failed")
		}
		if _, err := store.Replace(ctx, []Dataset{testDataset()}); err == nil {
			t.Fatal("candidate failure publication succeeded")
		}
		if _, err := pool.Exec(ctx, `DROP TRIGGER reject_generation_11 ON movies; DROP FUNCTION reject_generation_11()`, pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatal("remove candidate failure trigger failed")
		}
		after, revision, err := store.Load(ctx)
		if err != nil || revision.ScheduleVersion != 10 || len(after.Theaters) != len(before.Theaters) || after.GeneratedAt != before.GeneratedAt {
			t.Fatalf("rollback revision=%+v before=%d after=%d err=%v", revision, len(before.Theaters), len(after.Theaters), err)
		}
		var candidateRows int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_snapshots WHERE generation_id=11) + (SELECT count(*) FROM theaters WHERE generation_id=11) + (SELECT count(*) FROM movies WHERE generation_id=11) + (SELECT count(*) FROM showtimes WHERE generation_id=11)`).Scan(&candidateRows); err != nil || candidateRows != 0 {
			t.Fatalf("candidate rows=%d err=%v", candidateRows, err)
		}
	})

	t.Run("fresh combined publication recovers stale generation and bounds retention", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE movies SET title='INACTIVE POISON', runtime_minutes=1 WHERE generation_id=10 AND provider='ugc' AND provider_id='200'`); err != nil {
			t.Fatalf("poison inactive generation movie failed: %v", err)
		}
		ugc := shiftedDataset(testDataset(), 15)
		kinepolis := shiftedDataset(kinepolisTestDataset(), 15)
		version, err := store.Replace(ctx, []Dataset{ugc, kinepolis})
		if err != nil || version.Version != 11 {
			t.Fatalf("combined recovery version=%+v err=%v", version, err)
		}
		loaded, revision, err := store.Load(ctx)
		if err != nil || revision.ScheduleVersion != 11 || loaded.Provider != ProviderCombined || len(loaded.Theaters) != 4 {
			t.Fatalf("combined recovery revision=%+v provider=%q theaters=%d err=%v", revision, loaded.Provider, len(loaded.Theaters), err)
		}
		var duplicateGenerations int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM movies WHERE provider='ugc' AND provider_id='200' AND generation_id IN (10,11)`).Scan(&duplicateGenerations); err != nil || duplicateGenerations != 2 {
			t.Fatalf("duplicate identity generations=%d err=%v", duplicateGenerations, err)
		}
		var inactiveTitle, activeTitle string
		var inactiveRuntime, activeRuntime int
		if err := pool.QueryRow(ctx, `SELECT max(title) FILTER (WHERE generation_id=10), max(runtime_minutes) FILTER (WHERE generation_id=10), max(title) FILTER (WHERE generation_id=11), max(runtime_minutes) FILTER (WHERE generation_id=11) FROM movies WHERE provider='ugc' AND provider_id='200' AND generation_id IN (10,11)`).Scan(&inactiveTitle, &inactiveRuntime, &activeTitle, &activeRuntime); err != nil || inactiveTitle != "INACTIVE POISON" || inactiveRuntime != 1 || activeTitle != "Film A" || activeRuntime != 100 {
			t.Fatalf("inactive=(%q,%d) active=(%q,%d) err=%v", inactiveTitle, inactiveRuntime, activeTitle, activeRuntime, err)
		}
		activeMovieFound := false
		for _, showing := range loaded.Showtimes {
			if showing.Movie.ProviderID == "200" {
				activeMovieFound = true
				if showing.Movie.Title != "Film A" || showing.Movie.RuntimeMinutes != 100 || showing.Movie.Slug != "ugc-film-200" {
					t.Fatalf("loaded inactive movie value: %+v", showing.Movie)
				}
			}
		}
		if !activeMovieFound {
			t.Fatal("active UGC movie 200 missing from load")
		}
		for _, table := range []string{"provider_snapshots", "theaters", "theater_dates", "theater_passes", "movies", "showtimes"} {
			var generations, oldRows int
			query := fmt.Sprintf("SELECT count(DISTINCT generation_id), count(*) FILTER (WHERE generation_id < 10) FROM %s", table)
			if err := pool.QueryRow(ctx, query).Scan(&generations, &oldRows); err != nil || generations != 2 || oldRows != 0 {
				t.Fatalf("table=%s retained generations=%d old_rows=%d err=%v", table, generations, oldRows, err)
			}
		}
	})

	t.Run("concurrent publications serialize complete generations", func(t *testing.T) {
		start := make(chan struct{})
		versions := make(chan int64, 2)
		errors := make(chan error, 2)
		for _, days := range []int{16, 17} {
			days := days
			go func() {
				<-start
				version, err := store.Replace(ctx, []Dataset{shiftedDataset(testDataset(), days), shiftedDataset(kinepolisTestDataset(), days)})
				versions <- version.Version
				errors <- err
			}()
		}
		close(start)
		firstVersion, secondVersion := <-versions, <-versions
		if firstVersion > secondVersion {
			firstVersion, secondVersion = secondVersion, firstVersion
		}
		if firstErr, secondErr := <-errors, <-errors; firstErr != nil || secondErr != nil || firstVersion != 12 || secondVersion != 13 {
			t.Fatalf("versions=(%d,%d) errors=(%v,%v)", firstVersion, secondVersion, firstErr, secondErr)
		}
		loaded, revision, err := store.Load(ctx)
		if err != nil || revision.ScheduleVersion != 13 || loaded.Provider != ProviderCombined || len(loaded.Theaters) != 4 {
			t.Fatalf("serialized load revision=%+v provider=%q theaters=%d err=%v", revision, loaded.Provider, len(loaded.Theaters), err)
		}
		for _, table := range []string{"provider_snapshots", "theaters", "theater_dates", "theater_passes", "movies", "showtimes"} {
			var generations int
			if err := pool.QueryRow(ctx, fmt.Sprintf("SELECT count(DISTINCT generation_id) FROM %s", table)).Scan(&generations); err != nil || generations != 2 {
				t.Fatalf("table=%s retained generations=%d err=%v", table, generations, err)
			}
		}
	})

	t.Run("all-provider long window publication round trips", func(t *testing.T) {
		ugc := testDataset()
		kinepolis := kinepolisTestDataset()
		for _, data := range []*Dataset{&ugc, &kinepolis} {
			data.Window = Window{From: "2026-08-15", Through: "2026-09-15"}
		}
		publication, err := store.Replace(ctx, []Dataset{ugc, kinepolis})
		if err != nil || publication.Version != 14 {
			t.Fatalf("long-window publication=%+v error=%v", publication, err)
		}

		loaded, revision, err := store.Load(ctx)
		if err != nil || revision.ScheduleVersion != 14 || loaded.Provider != ProviderCombined || loaded.Window != ugc.Window {
			t.Fatalf("long-window load revision=%+v provider=%q window=%+v error=%v", revision, loaded.Provider, loaded.Window, err)
		}

		var providerSnapshots int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM provider_snapshots WHERE generation_id=$1 AND window_from=$2 AND window_through=$3`, publication.Version, ugc.Window.From, ugc.Window.Through).Scan(&providerSnapshots); err != nil || providerSnapshots != 2 {
			t.Fatalf("long-window provider snapshots=%d error=%v", providerSnapshots, err)
		}
		var combinedProvider, combinedFrom, combinedThrough string
		if err := pool.QueryRow(ctx, `SELECT provider, window_from::text, window_through::text FROM schedule_snapshot WHERE singleton=true AND version=$1`, publication.Version).Scan(&combinedProvider, &combinedFrom, &combinedThrough); err != nil || combinedProvider != string(ProviderCombined) || combinedFrom != ugc.Window.From || combinedThrough != ugc.Window.Through {
			t.Fatalf("long-window combined snapshot provider=%q window=%s..%s error=%v", combinedProvider, combinedFrom, combinedThrough, err)
		}
	})
}

func TestFourProviderPostgresStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate schema nonce failed")
	}
	schema := "movieflow_pathe_store_test_" + hex.EncodeToString(nonce)
	identifier := pgx.Identifier{schema}.Sanitize()
	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal("create integration schema failed")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE")
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration pool failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("run migrations failed")
	}
	store := NewStore(pool)

	pathe := patheTestDataset()
	publication, err := store.Replace(ctx, []Dataset{pathe})
	if err != nil || publication.Version != 1 || publication.Providers[ProviderPathe].Showtimes != 1 {
		t.Fatalf("Pathé publication=%+v err=%v", publication, err)
	}
	loaded, revision, err := store.Load(ctx)
	if err != nil || revision.ScheduleVersion != 1 || loaded.Provider != ProviderPathe || loaded.Showtimes[0].Format != FormatICE || loaded.Showtimes[0].Movie.RuntimeMinutes != 100 || loaded.Showtimes[0].EndTime.Sub(loaded.Showtimes[0].StartTime) != 120*time.Minute {
		t.Fatalf("Pathé load revision=%+v dataset=%+v err=%v", revision, loaded, err)
	}

	ugc, kinepolis, cgr := testDataset(), kinepolisTestDataset(), cgrTestDataset()
	publication, err = store.Replace(ctx, []Dataset{ugc})
	if err != nil || publication.Version != 2 {
		t.Fatalf("UGC copy-forward publication=%+v err=%v", publication, err)
	}
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	metadata := enrichment.Metadata{Provider: enrichment.ProviderTMDB, ProviderMovieID: 42, Locale: enrichment.LocaleFrench, ProviderTitle: "Film A", LocalizedTitle: "Film A", RuntimeMinutes: 100, Genres: []string{}, FetchedAt: now, RefreshAfter: now.Add(24 * time.Hour)}
	match := func(provider, id string) enrichment.Match {
		return enrichment.Match{SourceProvider: provider, SourceMovieID: id, MetadataProvider: enrichment.ProviderTMDB, Status: enrichment.StatusMatched, MetadataMovieID: 42, Score: 1, NormalizedSourceTitle: "film a", SourceRuntimeMinutes: 100, Candidates: []enrichment.Candidate{{ID: 42, Title: "Film A", Runtime: 100, Score: 1}}, EvaluatedAt: now, RetryAfter: now.Add(24 * time.Hour)}
	}
	enrichmentStore := enrichment.NewPostgresStore(pool)
	if err := enrichmentStore.Publish(ctx, match(enrichment.SourceUGC, "200"), metadata); err != nil {
		t.Fatalf("publish UGC identity evidence: %v", err)
	}
	pathe.Showtimes[0].Movie.ProviderID = "film-b"
	pathe.Showtimes[0].Movie.Slug = "pathe-film-film-b"
	publication, err = store.Replace(ctx, []Dataset{ugc, kinepolis, pathe, cgr})
	if err != nil || publication.Version != 3 || len(publication.Providers) != 4 {
		t.Fatalf("four-provider publication=%+v err=%v", publication, err)
	}
	loaded, revision, err = store.Load(ctx)
	if err != nil || revision.ScheduleVersion != 3 || loaded.Provider != ProviderCombined || len(loaded.Theaters) != 6 || len(loaded.Showtimes) != 8 {
		t.Fatalf("four-provider load revision=%+v theaters=%d showtimes=%d err=%v", revision, len(loaded.Theaters), len(loaded.Showtimes), err)
	}
	var loadedCGR *ShowtimeRecord
	for index := range loaded.Showtimes {
		if loaded.Showtimes[index].Provider == ProviderCGR {
			loadedCGR = &loaded.Showtimes[index]
		}
	}
	if loadedCGR == nil || loadedCGR.Movie.RuntimeMinutes != 0 || !loadedCGR.EndTime.Equal(loadedCGR.StartTime) || loadedCGR.Room != "" || loadedCGR.Language != schedule.Language("SPANISH") {
		t.Fatalf("loaded CGR showing=%+v", loadedCGR)
	}
	if err := enrichmentStore.Publish(ctx, match(enrichment.SourcePathe, "film-b"), metadata); err != nil {
		t.Fatalf("publish Pathé identity evidence: %v", err)
	}
	var anchorProvider, anchorID string
	if err := pool.QueryRow(ctx, `SELECT movie.identity_anchor_provider,movie.identity_anchor_source_movie_id
FROM public_movies movie
JOIN public_movie_sources source ON source.public_movie_id=movie.id
WHERE source.source_provider='pathe' AND source.source_movie_id='film-b'`).Scan(&anchorProvider, &anchorID); err != nil || anchorProvider != "ugc" || anchorID != "200" {
		t.Fatalf("merged public anchor=%s/%s err=%v", anchorProvider, anchorID, err)
	}

	pathe.GeneratedAt = pathe.GeneratedAt.Add(time.Minute)
	pathe.Theaters[0].Name = "Pathé Lille remplacé"
	publication, err = store.Replace(ctx, []Dataset{pathe})
	if err != nil || publication.Version != 4 {
		t.Fatalf("Pathé copy-forward publication=%+v err=%v", publication, err)
	}
	loaded, revision, err = store.Load(ctx)
	if err != nil || revision.ScheduleVersion != 4 || len(loaded.Theaters) != 6 || len(loaded.Showtimes) != 8 {
		t.Fatalf("Pathé copy-forward load revision=%+v theaters=%d showtimes=%d err=%v", revision, len(loaded.Theaters), len(loaded.Showtimes), err)
	}
	providers := map[schedule.Provider]bool{}
	for _, theater := range loaded.Theaters {
		providers[theater.Provider] = true
	}
	if !providers[ProviderUGC] || !providers[ProviderKinepolis] || !providers[ProviderPathe] || !providers[ProviderCGR] {
		t.Fatalf("copy-forward providers=%v", providers)
	}
}

func shiftedDataset(data Dataset, days int) Dataset {
	publication, _ := schedule.PreparePublication(data)
	data = publication.Dataset
	data.GeneratedAt = data.GeneratedAt.AddDate(0, 0, days)
	shiftDate := func(value string) string {
		parsed, _ := schedule.ParseServiceDate(value)
		return schedule.FormatServiceDate(parsed.AddDate(0, 0, days))
	}
	data.Window.From = shiftDate(data.Window.From)
	data.Window.Through = shiftDate(data.Window.Through)
	for i := range data.Theaters {
		for j := range data.Theaters[i].AvailableDates {
			data.Theaters[i].AvailableDates[j] = shiftDate(data.Theaters[i].AvailableDates[j])
		}
	}
	for i := range data.Showtimes {
		data.Showtimes[i].ServiceDate = shiftDate(data.Showtimes[i].ServiceDate)
		data.Showtimes[i].StartTime = data.Showtimes[i].StartTime.AddDate(0, 0, days)
		data.Showtimes[i].EndTime = data.Showtimes[i].EndTime.AddDate(0, 0, days)
	}
	return data
}

func TestMigration006RepairsRecordedStale005Integration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate test schema nonce failed")
	}
	schema := "movieflow_migration006_test_" + hex.EncodeToString(nonce)
	identifier := pgx.Identifier{schema}.Sanitize()
	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal("create integration schema failed")
	}
	t.Cleanup(func() {
		if !strings.HasPrefix(schema, "movieflow_migration006_test_") {
			t.Error("unsafe integration schema cleanup rejected")
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("drop integration schema failed")
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration pool configuration failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create integration pool failed")
	}
	t.Cleanup(pool.Close)
	var currentSchema string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&currentSchema); err != nil || currentSchema != schema {
		t.Fatalf("isolated schema assertion failed: schema=%q err=%v", currentSchema, err)
	}

	if _, err := pool.Exec(ctx, `CREATE TABLE movieflow_schema_migrations (version bigint PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal("create migration history failed")
	}
	for version := 1; version <= 5; version++ {
		name := fmt.Sprintf("%03d_", version)
		matches, err := filepath.Glob(filepath.Join("..", "database", "migrations", name+"*.sql"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("migration %d fixture files=%v err=%v", version, matches, err)
		}
		sql, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatalf("read migration %d failed: %v", version, err)
		}
		if _, err := pool.Exec(ctx, string(sql), pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("apply migration %d failed: %v", version, err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO movieflow_schema_migrations (version, name) VALUES ($1,$2)`, version, filepath.Base(matches[0])); err != nil {
			t.Fatalf("record migration %d failed", version)
		}
	}
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	metadata := enrichment.Metadata{Provider: enrichment.ProviderTMDB, ProviderMovieID: 42, Locale: enrichment.LocaleFrench, ProviderTitle: "Film A", LocalizedTitle: "Film A", Overview: "Résumé préservé", ReleaseDate: "2026-01-02", PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/a.jpg", RuntimeMinutes: 100, Genres: []string{"Drame"}, FetchedAt: now, RefreshAfter: now.Add(30 * 24 * time.Hour)}
	if _, err := pool.Exec(ctx, `
INSERT INTO schedule_snapshot (version,schema_version,provider,scope,generated_at,timezone,window_from,window_through) VALUES (1,1,'ugc','all_cinemas',$1,'Europe/Paris','2026-08-15','2026-08-15');
INSERT INTO provider_snapshots (provider,schema_version,scope,generated_at,timezone,window_from,window_through) VALUES ('ugc',1,'all_cinemas',$1,'Europe/Paris','2026-08-15','2026-08-15');
INSERT INTO passes (code) VALUES ('UGC_ILLIMITE');
INSERT INTO theaters (id,provider_id,slug,name,address,city,postal_code,provider) VALUES ('ugc-25','25','ugc-25','UGC Lille','1 rue','Lille','59000','ugc');
INSERT INTO theater_dates (theater_id,service_date) VALUES ('ugc-25','2026-08-15');
INSERT INTO theater_passes (theater_id,pass_code) VALUES ('ugc-25','UGC_ILLIMITE');
INSERT INTO movies (provider_id,slug,title,runtime_minutes,provider,source_overview,source_release_date,source_genres) VALUES ('200','ugc-film-200','Film A',100,'ugc',NULL,NULL,'{}');
INSERT INTO showtimes (id,provider_showing_id,service_date,theater_id,movie_provider_id,start_time,end_time,language,provider_version,format,room,booking_url,provider) VALUES ('ugc-showing-100','100','2026-08-15','ugc-25','200','2026-08-15T12:00:00+02','2026-08-15T13:40:00+02','VF','VF','2D','Salle 1','https://www.ugc.fr/reservationSeances.html?id=100','ugc');
INSERT INTO movie_matches (source_provider,source_movie_id,metadata_provider,status,metadata_movie_id,score,normalized_source_title,source_runtime_minutes,candidates,evaluated_at,retry_after,updated_at) VALUES ('ugc','200','tmdb','matched',42,1,'film a',100,'[]',$1,$2,$1);
INSERT INTO movie_metadata_cache (provider,provider_movie_id,locale,provider_title,localized_title,overview,release_date,poster_url,backdrop_url,runtime_minutes,genres,fetched_at,refresh_after) VALUES ('tmdb',42,'fr-FR','Film A','Film A',$3,'2026-01-02','https://image.tmdb.org/t/p/w500/a.jpg','https://image.tmdb.org/t/p/w780/a.jpg',100,ARRAY['Drame'],$1,$2);
UPDATE movie_enrichment_state SET version=1 WHERE singleton=true;
`, pgx.QueryExecModeSimpleProtocol, now, metadata.RefreshAfter, metadata.Overview); err != nil {
		t.Fatalf("seed actual migration 005 fixture failed: %v", err)
	}
	var migrationCount, missingColumns, scheduleRows, matchRows, metadataRows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM movieflow_schema_migrations").Scan(&migrationCount); err != nil || migrationCount != 5 {
		t.Fatalf("stale fixture migration count=%d err=%v", migrationCount, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='movies' AND column_name IN ('source_overview','source_release_date','source_genres')`).Scan(&missingColumns); err != nil || missingColumns != 3 {
		t.Fatalf("stale fixture source columns=%d err=%v", missingColumns, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM showtimes").Scan(&scheduleRows); err != nil || scheduleRows == 0 {
		t.Fatalf("stale fixture showtimes=%d err=%v", scheduleRows, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM movie_matches WHERE source_provider='ugc' AND source_movie_id='200'").Scan(&matchRows); err != nil || matchRows != 1 {
		t.Fatalf("stale fixture matches=%d err=%v", matchRows, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM movie_metadata_cache WHERE provider='tmdb' AND provider_movie_id=42").Scan(&metadataRows); err != nil || metadataRows != 1 {
		t.Fatalf("stale fixture metadata=%d err=%v", metadataRows, err)
	}

	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("run migration 006 on stale 005 failed")
	}
	store := NewStore(pool)
	var sourceColumns, sourceConstraints int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='movies' AND ((column_name='source_overview' AND data_type='character varying' AND character_maximum_length=10000 AND is_nullable='YES') OR (column_name='source_release_date' AND data_type='date' AND is_nullable='YES') OR (column_name='source_genres' AND data_type='ARRAY' AND udt_name='_text' AND is_nullable='NO'))`).Scan(&sourceColumns); err != nil || sourceColumns != 3 {
		t.Fatalf("repaired source columns=%d err=%v", sourceColumns, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid='movies'::regclass AND conname IN ('movies_source_overview_check','movies_source_genres_check')`).Scan(&sourceConstraints); err != nil || sourceConstraints != 2 {
		t.Fatalf("repaired source constraints=%d err=%v", sourceConstraints, err)
	}
	var preservedOverview string
	if err := pool.QueryRow(ctx, "SELECT overview FROM movie_metadata_cache WHERE provider='tmdb' AND provider_movie_id=42").Scan(&preservedOverview); err != nil || preservedOverview != metadata.Overview {
		t.Fatalf("preserved metadata overview=%q err=%v", preservedOverview, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM showtimes").Scan(&scheduleRows); err != nil || scheduleRows == 0 {
		t.Fatalf("preserved showtimes=%d err=%v", scheduleRows, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE movies SET source_overview=repeat('x',10001) WHERE provider='ugc' AND provider_id='200'"); err == nil {
		t.Fatal("oversized source overview accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE movies SET source_genres=array_fill('x'::text, ARRAY[33]) WHERE provider='ugc' AND provider_id='200'"); err == nil {
		t.Fatal("oversized source genres accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE theaters SET address='' WHERE provider='ugc'"); err == nil {
		t.Fatal("empty UGC address accepted")
	}

	if version, err := store.Replace(ctx, []Dataset{kinepolisTestDataset()}); err != nil || version.Version != 2 {
		t.Fatalf("Kinepolis replacement after repair version=%+v err=%v", version, err)
	}
	ugcReplacement := testDataset()
	ugcReplacement.GeneratedAt = ugcReplacement.GeneratedAt.Add(time.Minute)
	if version, err := store.Replace(ctx, []Dataset{ugcReplacement}); err != nil || version.Version != 3 {
		t.Fatalf("UGC replacement after repair version=%+v err=%v", version, err)
	}
	loaded, revision, err := store.Load(ctx)
	if err != nil || revision.ScheduleVersion != 3 || revision.EnrichmentVersion != 1 || loaded.Provider != ProviderCombined {
		t.Fatalf("combined load after repair revision=%+v provider=%q err=%v", revision, loaded.Provider, err)
	}
	providers := map[schedule.Provider]bool{}
	for _, theater := range loaded.Theaters {
		providers[theater.Provider] = true
	}
	if !providers[ProviderUGC] || !providers[ProviderKinepolis] {
		t.Fatalf("providers after replacements=%v", providers)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM movie_matches WHERE source_provider='ugc' AND source_movie_id='200'").Scan(&matchRows); err != nil || matchRows != 1 {
		t.Fatalf("preserved match rows after replacements=%d err=%v", matchRows, err)
	}
}

func TestPostgresStoreRecordCountsIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate test schema nonce failed")
	}
	schema := "movieflow_count_test_" + hex.EncodeToString(nonce)
	identifier := pgx.Identifier{schema}.Sanitize()
	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal("connect integration bootstrap failed")
	}
	t.Cleanup(func() { _ = bootstrap.Close(context.Background()) })
	if _, err := bootstrap.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal("create integration schema failed")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = identifier
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	data := testDataset()
	template := data.Theaters[0]
	data.Theaters = make([]TheaterRecord, 257)
	for index := range data.Theaters {
		id := strconv.Itoa(index + 1)
		theater := template
		theater.ID = "ugc-" + id
		theater.ProviderID = id
		theater.Slug = theater.ID
		data.Theaters[index] = theater
	}
	store := NewStore(pool)
	if _, err := store.Replace(ctx, []Dataset{data}); err != nil {
		t.Fatalf("replace 257 theaters: %v", err)
	}
	loaded, _, err := store.Load(ctx)
	if err != nil || len(loaded.Theaters) != 257 || len(loaded.Showtimes) != len(data.Showtimes) {
		t.Fatalf("loaded theaters=%d showtimes=%d err=%v", len(loaded.Theaters), len(loaded.Showtimes), err)
	}
}
