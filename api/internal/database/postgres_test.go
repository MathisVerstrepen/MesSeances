package database

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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

func TestValidateMigrationHistoryReturnsBoundedIncompatibilitySentinel(t *testing.T) {
	available := []migration{
		{version: 1, name: "001_initial.sql"},
		{version: 2, name: "002_movie_enrichment.sql"},
	}
	tests := []struct {
		name     string
		recorded []migration
		wantErr  bool
	}{
		{name: "matching prefix", recorded: available[:1]},
		{name: "ahead of checkout", recorded: append(append([]migration{}, available...), migration{version: 3, name: "003_future.sql"}), wantErr: true},
		{name: "changed history", recorded: []migration{{version: 1, name: "001_rewritten.sql"}}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateMigrationHistory(test.recorded, available)
			if test.wantErr {
				if !errors.Is(err, ErrMigrationHistoryIncompatible) || err.Error() != "database migration history is incompatible" {
					t.Fatalf("incompatibility error=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("compatible history error=%v", err)
			}
		})
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
		{29, "029_multi_sync_schedules.sql"},
		{30, "030_public_movie_metadata_overrides.sql"},
		{31, "031_megarama_provider.sql"},
		{32, "032_upcoming_movies.sql"},
		{33, "033_upcoming_movie_reviews.sql"},
		{34, "034_cineville_provider.sql"},
		{35, "035_mk2_provider.sql"},
		{36, "036_cinewest_provider.sql"},
		{37, "037_grandecran_provider.sql"},
		{38, "038_noecinemas_provider.sql"},
		{39, "039_screening_history.sql"},
		{40, "040_infinity_vision_format.sql"},
		{41, "041_movie_original_language.sql"},
		{42, "042_query_only_vof_language.sql"},
		{43, "043_accounts.sql"},
		{44, "044_account_oauth_continuation.sql"},
		{45, "045_account_registration_binding.sql"},
		{46, "046_account_avatars.sql"},
		{47, "047_account_avatar_webp.sql"},
		{48, "048_validate_account_avatar_webp.sql"},
		{49, "049_account_theater_preferences.sql"},
		{50, "050_account_watchlist.sql"},
		{51, "051_watchlist_french_releases.sql"},
		{52, "052_account_watchlist_sort.sql"},
		{53, "053_account_watchlist_tags.sql"},
		{54, "054_account_watchlist_tag_colors.sql"},
		{55, "055_account_watchlist_preferences.sql"},
		{56, "056_cinema_activity.sql"},
		{57, "057_movie_metacritic_id.sql"},
		{58, "058_theater_images.sql"},
		{59, "059_account_theater_follows.sql"},
		{60, "060_capitole_ticketingcine.sql"},
		{61, "061_public_page_content.sql"},
		{62, "062_account_watched.sql"},
		{63, "063_veo_provider.sql"},
		{64, "064_veo_checkout_source_paths.sql"},
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

func TestPublishedWatchedMigrationPrefix(t *testing.T) {
	available, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 64 || available[61].name != "062_account_watched.sql" || available[62].name != "063_veo_provider.sql" {
		t.Fatal("published watched062 must precede pending Véo063")
	}
	// Pin the published watched body and the pre-renumbering Véo body. This is
	// a source regression guard, not a change to runtime history validation.
	for _, test := range []struct {
		index int
		hash  string
	}{
		{61, "221220ee3e32026b8e224bb0d75b3a42ba8e9ce908fe4e33c6394f4810297ae7"},
		{62, "523a3f4ef7ed7f92ec8b769a4f8c6af6503f687f0bc55d991d8ef3c677f8b2b2"},
	} {
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(available[test.index].sql))); got != test.hash {
			t.Fatalf("migration %s body changed", available[test.index].name)
		}
	}
	recorded := append([]migration(nil), available[:62]...)
	if err := validateMigrationHistory(recorded, available); err != nil {
		t.Fatal("legitimate published watched062 prefix rejected", err)
	}
	recorded[61].name = "062_veo_provider.sql"
	if err := validateMigrationHistory(recorded, available); !errors.Is(err, ErrMigrationHistoryIncompatible) {
		t.Fatal("incompatible renumbered history must remain rejected", err)
	}
}

func TestVeoCheckoutMigrationOnlyBroadensSourceAlias(t *testing.T) {
	items, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	previous := items[62].sql
	start := strings.Index(previous, "CREATE FUNCTION veo_booking_valid(")
	end := strings.Index(previous[start:], "\nDO $$") + start
	want := strings.TrimSpace(previous[start:end])
	want = strings.Replace(want, "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
	want = strings.Replace(want, "*/r/[1-9]", "*/(reserver/)?r/[1-9]", 1)
	got := strings.TrimSpace(strings.SplitN(items[63].sql, "\n", 2)[1])
	if got != want {
		t.Fatal("064 must only replace the exact existing helper with the approved source path alternative")
	}
}
