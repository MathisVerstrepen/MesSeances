package geocoding_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
	"messeances/api/internal/geocoding"
	"messeances/api/internal/schedule"
	"messeances/api/internal/schedulepg"
)

func TestPostgresResolutionStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := newResolutionIntegrationPool(t, ctx, databaseURL)
	publishResolutionSchedule(t, ctx, pool)

	initial := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	currentHash := geocoding.AddressHash("Rue du cinéma", "59000", "Lille")
	rows := []struct {
		providerID string
		values     string
		arguments  []any
	}{
		{providerID: "1", values: `NULL,NULL,'ign','Rue du cinéma',0.81,$2,'ambiguous',$3,50.63,3.06,'59000','Lille','street'`, arguments: []any{currentHash, initial}},
		{providerID: "2", values: `NULL,NULL,'ign',NULL,NULL,$2,'not_found',$3,NULL,NULL,NULL,NULL,NULL`, arguments: []any{currentHash, initial}},
		{providerID: "3", values: `48.5,2.2,'manual',NULL,NULL,NULL,'manual',$2,NULL,NULL,NULL,NULL,NULL`, arguments: []any{initial}},
		{providerID: "4", values: `50.1,3.1,'ign','2 Rue du cinéma',0.9,$2,'matched',$3,NULL,NULL,NULL,NULL,NULL`, arguments: []any{currentHash, initial}},
		{providerID: "5", values: `NULL,NULL,'ign','Rue du cinéma',0.8,$2,'ambiguous',$3,NULL,NULL,'59000','Lille','street'`, arguments: []any{currentHash, initial}},
		{providerID: "6", values: `NULL,NULL,'ign','Rue du cinéma',0.8,$2,'ambiguous',$3,50.6,3.1,'59000','Lille','street'`, arguments: []any{geocoding.AddressHash("old", "59000", "Lille"), initial}},
		{providerID: "99", values: `NULL,NULL,'ign',NULL,NULL,$2,'not_found',$3,NULL,NULL,NULL,NULL,NULL`, arguments: []any{currentHash, initial}},
	}
	for _, row := range rows {
		query := `INSERT INTO theater_locations (provider,provider_theater_id,latitude,longitude,source,matched_label,match_score,address_hash,status,updated_at,candidate_latitude,candidate_longitude,candidate_postal_code,candidate_city,candidate_type) VALUES ('ugc',$1,` + row.values + `)`
		arguments := append([]any{row.providerID}, row.arguments...)
		if _, err := pool.Exec(ctx, query, arguments...); err != nil {
			t.Fatalf("insert location %s failed: %v", row.providerID, err)
		}
	}

	now := time.Date(2026, 8, 26, 13, 0, 0, 0, time.UTC)
	service := geocoding.NewResolutionService(geocoding.NewPostgresResolutionStore(pool), func() time.Time { return now })
	items, err := service.Pending(ctx, 100, 0)
	if err != nil || len(items) != 4 {
		t.Fatalf("pending count=%d items=%+v err=%v", len(items), items, err)
	}
	if items[0].ProviderTheaterID != "1" || !items[0].CanAcceptSuggestion || items[0].Suggestion == nil || items[0].Suggestion.Latitude == nil || *items[0].Suggestion.Latitude != 50.63 {
		t.Fatalf("accept-ready item=%+v", items[0])
	}
	if items[1].ProviderTheaterID != "2" || items[1].Suggestion != nil || items[1].CanAcceptSuggestion || items[2].ProviderTheaterID != "5" || items[2].CanAcceptSuggestion || items[3].ProviderTheaterID != "6" || items[3].CanAcceptSuggestion {
		t.Fatalf("pending ordering/availability=%+v", items)
	}
	page, err := service.Pending(ctx, 2, 1)
	if err != nil || len(page) != 2 || page[0].ProviderTheaterID != "2" || page[1].ProviderTheaterID != "5" {
		t.Fatalf("deterministic page=%+v err=%v", page, err)
	}

	if err := service.AcceptSuggestion(ctx, "ugc", "1", initial); err != nil {
		t.Fatalf("accept stored suggestion failed: %v", err)
	}
	if err := service.AcceptSuggestion(ctx, "ugc", "1", initial); !errors.Is(err, geocoding.ErrResolutionConflict) {
		t.Fatalf("stale accept err=%v", err)
	}
	if err := service.SetManual(ctx, "ugc", "2", initial, -90, 180); err != nil {
		t.Fatalf("manual resolution failed: %v", err)
	}
	if err := service.SetManual(ctx, "ugc", "2", initial, 1, 1); !errors.Is(err, geocoding.ErrResolutionConflict) {
		t.Fatalf("stale manual err=%v", err)
	}
	for providerID, wantErr := range map[string]error{"5": geocoding.ErrResolutionConflict, "6": geocoding.ErrResolutionConflict} {
		if err := service.AcceptSuggestion(ctx, "ugc", providerID, initial); !errors.Is(err, wantErr) {
			t.Fatalf("accept %s err=%v", providerID, err)
		}
	}
	for _, input := range []struct {
		providerID string
		expected   time.Time
	}{{"2", now}, {"3", initial}, {"4", initial}, {"98", initial}, {"99", initial}} {
		if err := service.SetManual(ctx, "ugc", input.providerID, input.expected, 1, 1); !errors.Is(err, geocoding.ErrResolutionNotFound) {
			t.Fatalf("manual refusal id=%s err=%v", input.providerID, err)
		}
	}

	assertResolvedLocation(t, ctx, pool, "1", 50.63, 3.06, now)
	assertResolvedLocation(t, ctx, pool, "2", -90, 180, now)
	var manualLatitude, manualLongitude float64
	if err := pool.QueryRow(ctx, `SELECT latitude,longitude FROM theater_locations WHERE provider='ugc' AND provider_theater_id='3'`).Scan(&manualLatitude, &manualLongitude); err != nil || manualLatitude != 48.5 || manualLongitude != 2.2 {
		t.Fatalf("existing manual changed latitude=%f longitude=%f err=%v", manualLatitude, manualLongitude, err)
	}
	var version int64
	if err := pool.QueryRow(ctx, "SELECT version FROM theater_location_state WHERE singleton=true").Scan(&version); err != nil || version != 2 {
		t.Fatalf("location version=%d err=%v", version, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE theater_location_state SET version=$1 WHERE singleton=true", int64(^uint64(0)>>1)); err != nil {
		t.Fatal("install version overflow fixture failed")
	}
	if err := service.SetManual(ctx, "ugc", "5", initial, 1, 1); err == nil || errors.Is(err, geocoding.ErrResolutionConflict) || errors.Is(err, geocoding.ErrResolutionNotFound) {
		t.Fatalf("version overflow err=%v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM theater_locations WHERE provider='ugc' AND provider_theater_id='5'`).Scan(&status); err != nil || status != string(geocoding.StatusAmbiguous) {
		t.Fatalf("overflow changed location status=%q err=%v", status, err)
	}
}

func newResolutionIntegrationPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal("generate schema nonce failed")
	}
	schema := "movieflow_resolution_test_" + hex.EncodeToString(nonce)
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
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if strings.HasPrefix(schema, "movieflow_resolution_test_") {
			_, _ = bootstrap.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE")
		}
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
	return pool
}

func publishResolutionSchedule(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	paris, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 8, 26, 12, 0, 0, 0, paris)
	dataset := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderUGC, Scope: schedule.ScopeAll, GeneratedAt: start.UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: "2026-08-26", Through: "2026-08-26"}}
	for id := 1; id <= 6; id++ {
		providerID := fmt.Sprintf("%d", id)
		theaterID := "ugc-" + providerID
		dataset.Theaters = append(dataset.Theaters, schedule.TheaterRecord{ID: theaterID, ProviderID: providerID, Slug: theaterID, Name: "Cinéma " + providerID, Address: "Rue du cinéma", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-26"}, AcceptedPasses: []string{"UGC_ILLIMITE"}})
		dataset.Showtimes = append(dataset.Showtimes, schedule.ShowtimeRecord{ID: "ugc-showing-" + providerID, ProviderShowingID: providerID, ServiceDate: "2026-08-26", TheaterID: theaterID, Movie: schedule.MovieRecord{ProviderID: providerID, Slug: "ugc-film-" + providerID, Title: "Film " + providerID, RuntimeMinutes: 90}, StartTime: start.Add(time.Duration(id) * time.Hour), EndTime: start.Add(time.Duration(id)*time.Hour + 90*time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: schedule.Format2D, BookingURL: "https://www.ugc.fr/reservationSeances.html?id=" + providerID})
	}
	if _, err := schedulepg.NewStore(pool).Replace(ctx, []schedule.Dataset{dataset}); err != nil {
		t.Fatalf("publish schedule fixture failed: %v", err)
	}
}

func assertResolvedLocation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerID string, wantLatitude, wantLongitude float64, wantUpdatedAt time.Time) {
	t.Helper()
	var latitude, longitude float64
	var source, status string
	var label, score, hash, candidateLatitude, candidateLongitude, candidatePostalCode, candidateCity, candidateType any
	var updatedAt time.Time
	err := pool.QueryRow(ctx, `SELECT latitude,longitude,source,status,matched_label,match_score,address_hash,updated_at,candidate_latitude,candidate_longitude,candidate_postal_code,candidate_city,candidate_type
		FROM theater_locations WHERE provider='ugc' AND provider_theater_id=$1`, providerID).Scan(&latitude, &longitude, &source, &status, &label, &score, &hash, &updatedAt, &candidateLatitude, &candidateLongitude, &candidatePostalCode, &candidateCity, &candidateType)
	if err != nil || latitude != wantLatitude || longitude != wantLongitude || source != geocoding.SourceManual || status != string(geocoding.StatusManual) || label != nil || score != nil || hash != nil || !updatedAt.Equal(wantUpdatedAt) || candidateLatitude != nil || candidateLongitude != nil || candidatePostalCode != nil || candidateCity != nil || candidateType != nil {
		t.Fatalf("resolved id=%s lat=%f long=%f source=%s status=%s metadata=%v/%v/%v candidates=%v/%v/%v/%v/%v updated=%s err=%v", providerID, latitude, longitude, source, status, label, score, hash, candidateLatitude, candidateLongitude, candidatePostalCode, candidateCity, candidateType, updatedAt, err)
	}
}
