package database

import (
	"context"
	"strings"
	"testing"
)

func TestOpenPoolRejectsBlankAndRedactsInvalidURL(t *testing.T) {
	for _, value := range []string{"", "   "} {
		if _, err := OpenPool(context.Background(), value); err == nil || err.Error() != "database configuration is missing" {
			t.Fatalf("blank URL error=%v", err)
		}
	}
	secret := "synthetic-password"
	_, err := OpenPool(context.Background(), "postgres://user:"+secret+"@[")
	if err == nil || err.Error() != "database configuration is invalid" || strings.Contains(err.Error(), secret) {
		t.Fatalf("invalid URL error=%q", err)
	}
}

func TestEmbeddedMigrations(t *testing.T) {
	// Append new migrations to this manifest. Existing entries are compatibility history.
	want := []struct {
		version int64
		name    string
	}{
		{1, "001_initial.sql"},
		{2, "002_movie_enrichment.sql"},
		{3, "003_admin_match_review.sql"},
		{4, "004_movie_backdrop.sql"},
		{5, "005_multi_provider.sql"},
		{6, "006_repair_multi_provider_schema.sql"},
		{7, "007_screening_formats.sql"},
		{8, "008_local_movie_groups.sql"},
		{9, "009_widen_runtime_minutes.sql"},
		{10, "010_schedule_generations.sql"},
		{11, "011_short_links.sql"},
		{12, "012_sync_runs.sql"},
		{13, "013_unbounded_schedule_windows.sql"},
		{14, "014_public_movie_catalog.sql"},
		{15, "015_sync_schedules.sql"},
		{16, "016_pathe_provider.sql"},
		{17, "017_pathe_showing_identity.sql"},
		{18, "018_cgr_provider.sql"},
		{19, "019_repair_cgr_unknown_runtime.sql"},
		{20, "020_theater_locations.sql"},
		{21, "021_theater_location_suggestions.sql"},
		{22, "022_theater_geocoding_runs.sql"},
		{23, "023_allow_unknown_runtime.sql"},
		{24, "024_sync_run_retention.sql"},
		{25, "025_movie_trailers.sql"},
		{26, "026_dual_movie_trailers.sql"},
		{27, "027_short_link_retention.sql"},
		{28, "028_movie_imdb_id.sql"},
	}

	items, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != len(want) {
		t.Fatalf("migration count=%d want=%d", len(items), len(want))
	}
	for i, item := range items {
		if item.version != want[i].version || item.name != want[i].name {
			t.Fatalf("migration[%d]=(%d,%q) want=(%d,%q)", i, item.version, item.name, want[i].version, want[i].name)
		}
	}
	wantRetentionSQL := "CREATE INDEX short_links_retention_idx ON short_links (created_at);\n\nDELETE FROM short_links\nWHERE created_at < CURRENT_TIMESTAMP - INTERVAL '90 days';\n"
	var retentionSQL string
	for _, item := range items {
		if item.version == 27 {
			retentionSQL = item.sql
			break
		}
	}
	if retentionSQL != wantRetentionSQL {
		t.Fatalf("short-link retention migration=%q", retentionSQL)
	}
}
