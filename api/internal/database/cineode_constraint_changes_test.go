package database

import (
	"fmt"
	"testing"
)

// Independently specify the full 067 delta, retaining every legacy predicate.
func withCineodeConstraintChanges(t *testing.T, changes []providerConstraintChange) []providerConstraintChange {
	t.Helper()
	add := func(table, name, expression string) {
		for i := range changes {
			if changes[i].table == table && changes[i].name == name {
				if changes[i].newCheck || changes[i].cineodeAddition != "" {
					t.Fatal("unexpected 067 boundary", table, name)
				}
				changes[i].cineodeAddition = expression
				return
			}
		}
		changes = append(changes, providerConstraintChange{table: table, name: name, cineodeAddition: expression})
	}
	for _, table := range []string{"schedule_snapshot", "provider_snapshots", "theaters", "movies", "showtimes", "screening_history_providers", "theater_images", "cinema_activity_coverage"} {
		add(table, table+"_provider_check", "provider = 'cineode'")
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
		add(item.table, item.name, item.column+" = 'cineode'")
	}
	add("theaters", "theaters_provider_identity_check", "provider = 'cineode' AND cineode_identity_valid('theater',provider_id)")
	add("theaters", "theaters_address_check", "provider = 'cineode'")
	add("theaters", "theaters_postal_code_check", "provider = 'cineode'")
	add("showtimes", "showtimes_provider_identity_check", "provider = 'cineode' AND cineode_identity_valid('showing',provider_showing_id) AND theater_id = 'cineode-' || split_part(provider_showing_id,'-',1)")
	add("movie_slug_aliases", "movie_slug_aliases_check", "alias_kind = 'source' AND source_provider = 'cineode' AND source_movie_id IS NOT NULL")
	add("sync_runs", "sync_runs_trigger_occurrence_check", "trigger_source = 'scheduled' AND target = 'cineode' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2")
	for _, item := range []struct{ table, name, provider, id string }{
		{"movies", "movies_provider_identity_check", "provider", "provider_id"},
		{"movie_matches", "movie_matches_source_movie_id_check", "source_provider", "source_movie_id"},
		{"local_movie_groups", "local_movie_groups_check", "primary_source_provider", "primary_source_movie_id"},
		{"local_movie_group_members", "local_movie_group_members_check", "source_provider", "source_movie_id"},
		{"public_movies", "public_movies_check1", "identity_anchor_provider", "identity_anchor_source_movie_id"},
		{"public_movie_sources", "public_movie_sources_check", "source_provider", "source_movie_id"},
		{"movie_slug_aliases", "movie_slug_aliases_check1", "source_provider", "source_movie_id"},
	} {
		add(item.table, item.name, fmt.Sprintf("%s = 'cineode' AND cineode_identity_valid('movie',%s)", item.provider, item.id))
	}
	for _, change := range append([]providerConstraintChange(nil), changes...) {
		if (change.table == "theaters" || change.table == "showtimes") && change.cineodeAddition != "" {
			add("screening_history_"+change.table, change.name, change.cineodeAddition)
		}
	}
	for _, table := range []string{"showtimes", "screening_history_showtimes"} {
		changes = append(changes, providerConstraintChange{table: table, name: "showtimes_cineode_shape_check", newCheck: true, addition: `provider <> 'cineode' OR (
cineode_identity_valid('movie',movie_provider_id)
AND provider_showing_id = substring(theater_id,9) || '-' || movie_provider_id || '-' || trunc(extract(epoch from start_time))::text
AND start_time = date_trunc('second',start_time)
AND service_date = (start_time AT TIME ZONE 'Europe/Paris')::date
AND booking_url = 'https://www.cineode.fr/' || cineode_slug(substring(theater_id,9)) || '/seances/'
AND ((language = 'VF' AND provider_version = 'VF') OR (language = 'VOSTFR' AND provider_version = 'VOSTF'))
AND room = '' AND first_part_duration_minutes = 0)`})
	}
	for _, item := range []struct{ table, name, expression string }{
		{"movie_slug_aliases", "movie_slug_aliases_cineode_shape_check", "source_provider IS DISTINCT FROM 'cineode' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'cineode-film-' || source_movie_id)"},
		{"public_movie_sources", "public_movie_sources_cineode_shape_check", "source_provider <> 'cineode' OR (source_slug = 'cineode-film-' || source_movie_id AND cineode_poster_valid(coalesce(poster_url,'')))"},
		{"movies", "movies_cineode_poster_check", "provider <> 'cineode' OR cineode_poster_valid(coalesce(poster_url,''))"},
		{"theater_locations", "theater_locations_cineode_identity_check", "provider <> 'cineode' OR cineode_identity_valid('theater',provider_theater_id)"},
		{"theater_images", "theater_images_cineode_identity_check", "provider <> 'cineode' OR cineode_identity_valid('theater',provider_theater_id)"},
	} {
		changes = append(changes, providerConstraintChange{table: item.table, name: item.name, addition: item.expression, newCheck: true})
	}
	return changes
}
