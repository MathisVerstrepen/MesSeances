package geocoding_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

func TestPostgresStoreIntegration(t *testing.T) {
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
	schema := "movieflow_geocoding_test_" + hex.EncodeToString(nonce)
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
		if strings.HasPrefix(schema, "movieflow_geocoding_test_") {
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
	location, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 8, 26, 12, 0, 0, 0, location)
	dataset := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderUGC, Scope: schedule.ScopeAll, GeneratedAt: time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC), Timezone: schedule.Timezone, Window: schedule.Window{From: "2026-08-26", Through: "2026-08-26"}, Theaters: []schedule.TheaterRecord{{ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC Lille", Address: "40 rue de Béthune", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-26"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}}, Showtimes: []schedule.ShowtimeRecord{{ID: "ugc-showing-1", ProviderShowingID: "1", ServiceDate: "2026-08-26", TheaterID: "ugc-25", Movie: schedule.MovieRecord{ProviderID: "1", Slug: "ugc-film-1", Title: "Film", RuntimeMinutes: 90}, StartTime: start, EndTime: start.Add(90 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: schedule.Format2D, BookingURL: "https://www.ugc.fr/reservationSeances.html?id=1"}}}
	if _, err := schedulepg.NewStore(pool).Replace(ctx, []schedule.Dataset{dataset}); err != nil {
		t.Fatal("publish schedule fixture failed")
	}
	store := geocoding.NewPostgresStore(pool)
	theaters, err := store.Select(ctx)
	if err != nil || len(theaters) != 1 || theaters[0].Location != nil {
		t.Fatalf("selected=%+v err=%v", theaters, err)
	}
	latitude, longitude, score := 50.6321, 3.0612, .91
	now := time.Date(2026, 8, 26, 12, 30, 0, 0, time.UTC)
	hash := geocoding.AddressHash(dataset.Theaters[0].Address, dataset.Theaters[0].PostalCode, dataset.Theaters[0].City)
	ambiguous := geocoding.Location{Provider: "ugc", ProviderTheaterID: "25", Source: geocoding.SourceIGN, MatchedLabel: "Rue de Béthune", MatchScore: &score, AddressHash: hash, Status: geocoding.StatusAmbiguous, UpdatedAt: now, Suggestion: &geocoding.CandidateSuggestion{Latitude: &latitude, Longitude: &longitude, PostalCode: "59000", City: "Lille", Type: "street"}}
	written, err := store.Save(ctx, nil, ambiguous)
	if err != nil || !written {
		t.Fatalf("save ambiguous written=%t err=%v", written, err)
	}
	theaters, err = store.Select(ctx)
	if err != nil || len(theaters) != 1 || theaters[0].Location == nil || theaters[0].Location.Suggestion == nil || theaters[0].Location.Suggestion.Latitude == nil || *theaters[0].Location.Suggestion.Latitude != latitude || theaters[0].Location.Suggestion.PostalCode != "59000" || theaters[0].Location.Suggestion.Type != "street" {
		t.Fatalf("ambiguous round trip=%+v err=%v", theaters, err)
	}
	notFound := geocoding.Location{Provider: "ugc", ProviderTheaterID: "25", Source: geocoding.SourceIGN, AddressHash: hash, Status: geocoding.StatusNotFound, UpdatedAt: now.Add(time.Minute)}
	written, err = store.Save(ctx, theaters[0].Location, notFound)
	if err != nil || !written {
		t.Fatalf("save not-found written=%t err=%v", written, err)
	}
	var candidateLatitude, candidateLongitude, candidatePostalCode, candidateCity, candidateType any
	if err := pool.QueryRow(ctx, `SELECT candidate_latitude,candidate_longitude,candidate_postal_code,candidate_city,candidate_type FROM theater_locations WHERE provider='ugc' AND provider_theater_id='25'`).Scan(&candidateLatitude, &candidateLongitude, &candidatePostalCode, &candidateCity, &candidateType); err != nil || candidateLatitude != nil || candidateLongitude != nil || candidatePostalCode != nil || candidateCity != nil || candidateType != nil {
		t.Fatalf("not-found suggestion not cleared: %v/%v/%v/%v/%v err=%v", candidateLatitude, candidateLongitude, candidatePostalCode, candidateCity, candidateType, err)
	}
	theaters, err = store.Select(ctx)
	if err != nil || len(theaters) != 1 || theaters[0].Location == nil || theaters[0].Location.Status != geocoding.StatusNotFound {
		t.Fatalf("not-found round trip=%+v err=%v", theaters, err)
	}
	ambiguous.UpdatedAt = now.Add(2 * time.Minute)
	written, err = store.Save(ctx, theaters[0].Location, ambiguous)
	if err != nil || !written {
		t.Fatalf("restore ambiguous written=%t err=%v", written, err)
	}
	theaters, err = store.Select(ctx)
	if err != nil || len(theaters) != 1 || theaters[0].Location == nil || theaters[0].Location.Suggestion == nil {
		t.Fatalf("restored ambiguous=%+v err=%v", theaters, err)
	}
	match := geocoding.Location{Provider: "ugc", ProviderTheaterID: "25", Latitude: &latitude, Longitude: &longitude, Source: geocoding.SourceIGN, MatchedLabel: "40 Rue de Béthune 59000 Lille", MatchScore: &score, AddressHash: geocoding.AddressHash(dataset.Theaters[0].Address, dataset.Theaters[0].PostalCode, dataset.Theaters[0].City), Status: geocoding.StatusMatched, UpdatedAt: now.Add(3 * time.Minute)}
	written, err = store.Save(ctx, theaters[0].Location, match)
	if err != nil || !written {
		t.Fatalf("save written=%t err=%v", written, err)
	}
	var version int64
	if err := pool.QueryRow(ctx, "SELECT version FROM theater_location_state WHERE singleton=true").Scan(&version); err != nil || version != 4 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	if err := pool.QueryRow(ctx, `SELECT candidate_latitude,candidate_longitude,candidate_postal_code,candidate_city,candidate_type FROM theater_locations WHERE provider='ugc' AND provider_theater_id='25'`).Scan(&candidateLatitude, &candidateLongitude, &candidatePostalCode, &candidateCity, &candidateType); err != nil || candidateLatitude != nil || candidateLongitude != nil || candidatePostalCode != nil || candidateCity != nil || candidateType != nil {
		t.Fatalf("matched suggestion not cleared: %v/%v/%v/%v/%v err=%v", candidateLatitude, candidateLongitude, candidatePostalCode, candidateCity, candidateType, err)
	}
	if written, err = store.Save(ctx, nil, match); err != nil || written {
		t.Fatalf("stale save written=%t err=%v", written, err)
	}
	if err := pool.QueryRow(ctx, "SELECT version FROM theater_location_state WHERE singleton=true").Scan(&version); err != nil || version != 4 {
		t.Fatalf("stale save version=%d err=%v", version, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE theater_locations SET latitude=50.63,longitude=3.06,source='manual',matched_label=NULL,match_score=NULL,address_hash=NULL,status='manual',updated_at=$1 WHERE provider='ugc' AND provider_theater_id='25'`, now.Add(time.Minute)); err != nil {
		t.Fatal("install manual fixture failed")
	}
	theaters, err = store.Select(ctx)
	if err != nil || len(theaters) != 1 || theaters[0].Location == nil || theaters[0].Location.Status != geocoding.StatusManual {
		t.Fatalf("manual selection=%+v err=%v", theaters, err)
	}
	if written, err = store.Save(ctx, theaters[0].Location, match); err != nil || written {
		t.Fatalf("manual overwrite written=%t err=%v", written, err)
	}
	for _, statement := range []string{
		`INSERT INTO theater_locations (provider,provider_theater_id,latitude,longitude,source,address_hash,status,updated_at) VALUES ('other','1',1,1,'ign',$1,'matched',$2)`,
		`INSERT INTO theater_locations (provider,provider_theater_id,latitude,source,address_hash,status,updated_at) VALUES ('ugc','2',1,'ign',$1,'matched',$2)`,
		`INSERT INTO theater_locations (provider,provider_theater_id,latitude,longitude,source,address_hash,status,updated_at) VALUES ('ugc','3',91,1,'ign',$1,'matched',$2)`,
		`INSERT INTO theater_locations (provider,provider_theater_id,source,address_hash,status,updated_at) VALUES ('ugc','4','ign','bad','not_found',$2)`,
	} {
		if _, err := pool.Exec(ctx, statement, match.AddressHash, now); err == nil {
			t.Fatalf("invalid location accepted: %s", statement)
		}
	}
}
