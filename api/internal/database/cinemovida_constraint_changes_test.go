package database

import (
	"fmt"
	"testing"
)

// Independent complete 068 delta, layered over the exact preceding expressions.
func withCinemovidaConstraintChanges(t *testing.T, changes []providerConstraintChange) []providerConstraintChange {
	t.Helper()
	add := func(table, name, expression string) {
		for i := range changes {
			if changes[i].table == table && changes[i].name == name {
				if changes[i].newCheck || changes[i].cinemovidaAddition != "" {
					t.Fatal("unexpected 068 boundary", table, name)
				}
				changes[i].cinemovidaAddition = expression
				return
			}
		}
		changes = append(changes, providerConstraintChange{table: table, name: name, cinemovidaAddition: expression})
	}
	for _, table := range []string{"schedule_snapshot", "provider_snapshots", "theaters", "movies", "showtimes", "screening_history_providers", "theater_images", "cinema_activity_coverage"} {
		add(table, table+"_provider_check", "provider = 'cinemovida'")
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
		add(item.table, item.name, item.column+" = 'cinemovida'")
	}
	add("theaters", "theaters_provider_identity_check", "provider = 'cinemovida' AND cinemovida_identity_valid('theater',provider_id)")
	add("showtimes", "showtimes_provider_identity_check", "provider = 'cinemovida' AND cinemovida_identity_valid('showing',provider_showing_id) AND theater_id = 'cinemovida-' || split_part(provider_showing_id,'-',1)")
	add("movie_slug_aliases", "movie_slug_aliases_check", "alias_kind = 'source' AND source_provider = 'cinemovida' AND source_movie_id IS NOT NULL")
	add("sync_runs", "sync_runs_trigger_occurrence_check", "trigger_source = 'scheduled' AND target = 'cinemovida' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2")
	add("cinema_activity_coverage", "cinema_activity_coverage_check", "provider = 'cinemovida' AND status = 'complete' AND basis = 'date_response'")
	for _, item := range []struct{ table, name, provider, id string }{
		{"movies", "movies_provider_identity_check", "provider", "provider_id"},
		{"movie_matches", "movie_matches_source_movie_id_check", "source_provider", "source_movie_id"},
		{"local_movie_groups", "local_movie_groups_check", "primary_source_provider", "primary_source_movie_id"},
		{"local_movie_group_members", "local_movie_group_members_check", "source_provider", "source_movie_id"},
		{"public_movies", "public_movies_check1", "identity_anchor_provider", "identity_anchor_source_movie_id"},
		{"public_movie_sources", "public_movie_sources_check", "source_provider", "source_movie_id"},
		{"movie_slug_aliases", "movie_slug_aliases_check1", "source_provider", "source_movie_id"},
	} {
		add(item.table, item.name, fmt.Sprintf("%s = 'cinemovida' AND cinemovida_identity_valid('movie',%s)", item.provider, item.id))
	}
	for _, change := range append([]providerConstraintChange(nil), changes...) {
		if (change.table == "theaters" || change.table == "showtimes") && change.cinemovidaAddition != "" {
			add("screening_history_"+change.table, change.name, change.cinemovidaAddition)
		}
	}
	for _, table := range []string{"showtimes", "screening_history_showtimes"} {
		found := false
		for i := range changes {
			if changes[i].table == table && changes[i].name == "showtimes_time_check" {
				changes[i].cinemovidaTiming = true
				found = true
			}
		}
		if !found {
			changes = append(changes, providerConstraintChange{table: table, name: "showtimes_time_check", cinemovidaTiming: true})
		}
		changes = append(changes, providerConstraintChange{table: table, name: "showtimes_cinemovida_shape_check", newCheck: true, addition: `provider <> 'cinemovida' OR (
cinemovida_identity_valid('movie',movie_provider_id)
AND theater_id = 'cinemovida-' || split_part(provider_showing_id,'-',1)
AND service_date = (start_time AT TIME ZONE 'Europe/Paris')::date
AND first_part_duration_minutes = 0
AND cinemovida_booking_valid(booking_url,split_part(provider_showing_id,'-',1),provider_showing_id))`})
	}
	for _, item := range []struct{ table, name, expression string }{
		{"movie_slug_aliases", "movie_slug_aliases_cinemovida_shape_check", "source_provider IS DISTINCT FROM 'cinemovida' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'cinemovida-film-' || source_movie_id)"},
		{"public_movie_sources", "public_movie_sources_cinemovida_slug_check", "source_provider <> 'cinemovida' OR source_slug = 'cinemovida-film-' || source_movie_id"},
		{"theater_locations", "theater_locations_cinemovida_identity_check", "provider <> 'cinemovida' OR cinemovida_identity_valid('theater',provider_theater_id)"},
		{"theater_images", "theater_images_cinemovida_identity_check", "provider <> 'cinemovida' OR cinemovida_identity_valid('theater',provider_theater_id)"},
	} {
		changes = append(changes, providerConstraintChange{table: item.table, name: item.name, addition: item.expression, newCheck: true})
	}
	return changes
}
