package database

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTheaterLocationSuggestionsMigrationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "movieflow_suggestion_migration_test_")
	migrations := installMigrationPrefix(t, ctx, pool, 20, "020_theater_locations.sql")
	updatedAt := time.Date(2026, 8, 26, 12, 0, 0, 123000000, time.UTC)
	hash := strings.Repeat("a", 64)
	if _, err := pool.Exec(ctx, `INSERT INTO theater_locations (provider,provider_theater_id,source,matched_label,match_score,address_hash,status,updated_at)
		VALUES ('ugc','25','ign','Rue de Béthune',0.81,$1,'ambiguous',$2)`, hash, updatedAt); err != nil {
		t.Fatal("insert legacy ambiguous row failed")
	}
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal("apply suggestion migration failed")
	}
	assertCompleteMigrationHistory(t, ctx, pool, migrations)
	var label, storedHash string
	var score float64
	var storedUpdatedAt time.Time
	var candidateLatitude, candidateLongitude, candidatePostalCode, candidateCity, candidateType any
	err := pool.QueryRow(ctx, `SELECT matched_label,match_score,address_hash,updated_at,candidate_latitude,candidate_longitude,candidate_postal_code,candidate_city,candidate_type
		FROM theater_locations WHERE provider='ugc' AND provider_theater_id='25'`).Scan(&label, &score, &storedHash, &storedUpdatedAt, &candidateLatitude, &candidateLongitude, &candidatePostalCode, &candidateCity, &candidateType)
	if err != nil || label != "Rue de Béthune" || score != .81 || storedHash != hash || !storedUpdatedAt.Equal(updatedAt) || candidateLatitude != nil || candidateLongitude != nil || candidatePostalCode != nil || candidateCity != nil || candidateType != nil {
		t.Fatalf("legacy row changed label=%q score=%f hash=%q updated=%s candidates=%v/%v/%v/%v/%v err=%v", label, score, storedHash, storedUpdatedAt, candidateLatitude, candidateLongitude, candidatePostalCode, candidateCity, candidateType, err)
	}
	var version int64
	if err := pool.QueryRow(ctx, "SELECT version FROM theater_location_state WHERE singleton=true").Scan(&version); err != nil || version != 0 {
		t.Fatalf("location version=%d err=%v", version, err)
	}
	for _, statement := range []string{
		`UPDATE theater_locations SET candidate_latitude=50 WHERE provider='ugc' AND provider_theater_id='25'`,
		`UPDATE theater_locations SET candidate_latitude=91,candidate_longitude=3 WHERE provider='ugc' AND provider_theater_id='25'`,
		`UPDATE theater_locations SET candidate_postal_code=' ' WHERE provider='ugc' AND provider_theater_id='25'`,
	} {
		if _, err := pool.Exec(ctx, statement); err == nil {
			t.Fatalf("invalid suggestion accepted: %s", statement)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE theater_locations SET candidate_latitude=50,candidate_longitude=3,candidate_postal_code='59000',candidate_city='Lille',candidate_type='street' WHERE provider='ugc' AND provider_theater_id='25'`); err != nil {
		t.Fatal("valid suggestion rejected")
	}
	if _, err := pool.Exec(ctx, `UPDATE theater_locations SET status='not_found',matched_label=NULL,match_score=NULL WHERE provider='ugc' AND provider_theater_id='25'`); err == nil {
		t.Fatal("candidate metadata accepted outside ambiguous status")
	}
}
