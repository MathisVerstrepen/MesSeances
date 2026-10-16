package database

import (
	"fmt"
	"testing"
)

// Compose the exact append-only 066 delta with the independently expected 063
// predicates. Do not derive expectations from migration SQL or exclude whole
// classes of constraints: every excluded table/name is checked in full.
func withMajesticConstraintChanges(t *testing.T, changes []providerConstraintChange) []providerConstraintChange {
	t.Helper()
	indices := make(map[string]int, len(changes))
	for i, change := range changes {
		indices[change.table+"."+change.name] = i
	}
	add := func(table, name, expression string) {
		i, ok := indices[table+"."+name]
		if !ok || changes[i].newCheck || changes[i].majesticAddition != "" {
			t.Fatal("unexpected 066 amendment boundary", table, name)
		}
		changes[i].majesticAddition = expression
	}
	for _, table := range []string{"schedule_snapshot", "provider_snapshots", "theaters", "movies", "showtimes", "screening_history_providers", "theater_images", "cinema_activity_coverage"} {
		add(table, table+"_provider_check", "provider = 'majestic'")
	}
	for _, item := range []struct{ table, name, column string }{
		{"movie_matches", "movie_matches_source_provider_check", "source_provider"},
		{"local_movie_groups", "local_movie_groups_primary_source_provider_check", "primary_source_provider"},
		{"local_movie_group_members", "local_movie_group_members_source_provider_check", "source_provider"},
		{"public_movies", "public_movies_identity_anchor_provider_check", "identity_anchor_provider"},
		{"public_movie_sources", "public_movie_sources_source_provider_check", "source_provider"},
		{"theater_locations", "theater_locations_provider_check", "provider"},
		{"sync_runs", "sync_runs_target_check", "target"},
		{"sync_schedules", "sync_schedules_target_check", "target"},
	} {
		add(item.table, item.name, item.column+" = 'majestic'")
	}
	add("theaters", "theaters_provider_identity_check", "provider = 'majestic' AND majestic_identity_valid('theater',provider_id)")
	add("showtimes", "showtimes_provider_identity_check", "provider = 'majestic' AND majestic_identity_valid('showing',provider_showing_id) AND theater_id = 'majestic-EMS' || substring(provider_showing_id,5,4)")
	add("showtimes", "showtimes_check", "provider = 'majestic' AND first_part_duration_minutes >= 0")
	add("movie_slug_aliases", "movie_slug_aliases_check", "alias_kind = 'source' AND source_provider = 'majestic' AND source_movie_id IS NOT NULL")
	add("sync_runs", "sync_runs_trigger_occurrence_check", "trigger_source = 'scheduled' AND target = 'majestic' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2")
	for _, item := range []struct{ table, name, provider, id string }{
		{"movies", "movies_provider_identity_check", "provider", "provider_id"},
		{"movie_matches", "movie_matches_source_movie_id_check", "source_provider", "source_movie_id"},
		{"local_movie_groups", "local_movie_groups_check", "primary_source_provider", "primary_source_movie_id"},
		{"local_movie_group_members", "local_movie_group_members_check", "source_provider", "source_movie_id"},
		{"public_movies", "public_movies_check1", "identity_anchor_provider", "identity_anchor_source_movie_id"},
		{"public_movie_sources", "public_movie_sources_check", "source_provider", "source_movie_id"},
		{"movie_slug_aliases", "movie_slug_aliases_check1", "source_provider", "source_movie_id"},
	} {
		add(item.table, item.name, fmt.Sprintf("%s = 'majestic' AND majestic_identity_valid('movie',%s)", item.provider, item.id))
	}
	for i := range changes {
		change := &changes[i]
		if change.table == "screening_history_theaters" || change.table == "screening_history_showtimes" {
			if original, ok := indices[change.table[len("screening_history_"):]+"."+change.name]; ok {
				change.majesticAddition = changes[original].majesticAddition
			}
		}
	}
	for _, table := range []string{"showtimes", "screening_history_showtimes"} {
		changes = append(changes, providerConstraintChange{table: table, name: "showtimes_majestic_shape_check", newCheck: true, addition: `provider <> 'majestic' OR (
majestic_booking_valid(booking_url,substring(theater_id,10),provider_showing_id,movie_provider_id)
AND btrim(room) <> '' AND first_part_duration_minutes >= 0)`})
	}
	for _, item := range []struct{ table, name, expression string }{
		{"movie_slug_aliases", "movie_slug_aliases_majestic_shape_check", "source_provider IS DISTINCT FROM 'majestic' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'majestic-film-' || source_movie_id)"},
		{"public_movie_sources", "public_movie_sources_majestic_shape_check", "source_provider <> 'majestic' OR (source_slug = 'majestic-film-' || source_movie_id AND majestic_poster_valid(coalesce(poster_url,''),source_movie_id))"},
		{"movies", "movies_majestic_poster_check", "provider <> 'majestic' OR majestic_poster_valid(coalesce(poster_url,''),provider_id)"},
		{"theater_locations", "theater_locations_majestic_identity_check", "provider <> 'majestic' OR majestic_identity_valid('theater',provider_theater_id)"},
		{"theater_images", "theater_images_majestic_identity_check", "provider <> 'majestic' OR majestic_identity_valid('theater',provider_theater_id)"},
	} {
		changes = append(changes, providerConstraintChange{table: item.table, name: item.name, addition: item.expression, newCheck: true})
	}
	return changes
}
