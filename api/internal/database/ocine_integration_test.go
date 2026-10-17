package database

import (
	"context"
	"fmt"
	"messeances/api/internal/schedule"
	"messeances/api/internal/synccontrol"
	"strings"
	"testing"
	"time"
)

func withOcineConstraintChanges(t *testing.T, changes []providerConstraintChange) []providerConstraintChange {
	t.Helper()
	add := func(table, name, expression string) {
		for i := range changes {
			if changes[i].table == table && changes[i].name == name {
				if changes[i].newCheck || changes[i].ocineAddition != "" {
					t.Fatal("unexpected 070 boundary", table, name)
				}
				changes[i].ocineAddition = expression
				return
			}
		}
		changes = append(changes, providerConstraintChange{table: table, name: name, ocineAddition: expression})
	}
	for _, table := range []string{"schedule_snapshot", "provider_snapshots", "theaters", "movies", "showtimes", "screening_history_providers", "theater_images", "cinema_activity_coverage"} {
		add(table, table+"_provider_check", "provider = 'ocine'")
	}
	for _, v := range []struct{ table, name, column string }{{"movie_matches", "movie_matches_source_provider_check", "source_provider"}, {"local_movie_groups", "local_movie_groups_primary_source_provider_check", "primary_source_provider"}, {"local_movie_group_members", "local_movie_group_members_source_provider_check", "source_provider"}, {"public_movies", "public_movies_identity_anchor_provider_check", "identity_anchor_provider"}, {"public_movie_sources", "public_movie_sources_source_provider_check", "source_provider"}, {"theater_locations", "theater_locations_provider_check", "provider"}, {"sync_runs", "sync_runs_target_check", "target"}, {"sync_schedules", "sync_schedules_target_check", "target"}} {
		add(v.table, v.name, v.column+" = 'ocine'")
	}
	add("theaters", "theaters_provider_identity_check", "provider = 'ocine' AND ocine_identity_valid('theater',provider_id)")
	add("theaters", "theaters_address_check", "provider = 'ocine'")
	add("theaters", "theaters_postal_code_check", "provider = 'ocine'")
	add("showtimes", "showtimes_provider_identity_check", "provider = 'ocine' AND ocine_identity_valid('showing',provider_showing_id) AND theater_id = 'ocine-' || split_part(provider_showing_id,'-',1)")
	for _, name := range []string{"showtimes_language_check", "showtimes_provider_version_check"} {
		add("showtimes", name, "provider = 'ocine' AND language = '' AND provider_version = '' AND booking_url = '' AND right(provider_showing_id,5) = '-past'")
	}
	add("showtimes", "showtimes_booking_url_check", "provider = 'ocine' AND booking_url = '' AND right(provider_showing_id,5) = '-past'")
	add("movie_slug_aliases", "movie_slug_aliases_check", "alias_kind = 'source' AND source_provider = 'ocine' AND source_movie_id IS NOT NULL")
	add("sync_runs", "sync_runs_trigger_occurrence_check", "trigger_source = 'scheduled' AND target = 'ocine' AND schedule_id IS NOT NULL AND schedule_id > 0 AND schedule_revision IS NOT NULL AND schedule_revision > 0 AND scheduled_for IS NOT NULL AND schedule_attempt IS NOT NULL AND schedule_attempt BETWEEN 0 AND 2")
	for _, v := range []struct{ table, name, provider, id string }{{"movies", "movies_provider_identity_check", "provider", "provider_id"}, {"movie_matches", "movie_matches_source_movie_id_check", "source_provider", "source_movie_id"}, {"local_movie_groups", "local_movie_groups_check", "primary_source_provider", "primary_source_movie_id"}, {"local_movie_group_members", "local_movie_group_members_check", "source_provider", "source_movie_id"}, {"public_movies", "public_movies_check1", "identity_anchor_provider", "identity_anchor_source_movie_id"}, {"public_movie_sources", "public_movie_sources_check", "source_provider", "source_movie_id"}, {"movie_slug_aliases", "movie_slug_aliases_check1", "source_provider", "source_movie_id"}} {
		add(v.table, v.name, fmt.Sprintf("%s = 'ocine' AND ocine_identity_valid('movie',%s)", v.provider, v.id))
	}
	for _, c := range append([]providerConstraintChange(nil), changes...) {
		if (c.table == "theaters" || c.table == "showtimes") && c.ocineAddition != "" {
			add("screening_history_"+c.table, c.name, c.ocineAddition)
		}
	}
	for _, table := range []string{"showtimes", "screening_history_showtimes"} {
		found := false
		for i := range changes {
			if changes[i].table == table && changes[i].name == "showtimes_time_check" {
				changes[i].ocineTiming = true
				found = true
			}
		}
		if !found {
			changes = append(changes, providerConstraintChange{table: table, name: "showtimes_time_check", ocineTiming: true})
		}
		changes = append(changes, providerConstraintChange{table: table, name: "showtimes_ocine_shape_check", newCheck: true, addition: "provider <> 'ocine' OR (first_part_duration_minutes = 0 AND ocine_showing_valid(provider_showing_id,substring(theater_id,7),movie_provider_id,start_time,end_time,service_date,language,provider_version,booking_url))"})
	}
	for _, v := range []struct{ table, name, expression string }{{"movie_slug_aliases", "movie_slug_aliases_ocine_shape_check", "source_provider IS DISTINCT FROM 'ocine' OR (alias_kind = 'source' AND source_movie_id IS NOT NULL AND slug = 'ocine-film-' || source_movie_id)"}, {"public_movie_sources", "public_movie_sources_ocine_shape_check", "source_provider <> 'ocine' OR (source_slug = 'ocine-film-' || source_movie_id AND ocine_poster_valid(coalesce(poster_url,'')))"}, {"movies", "movies_ocine_poster_check", "provider <> 'ocine' OR ocine_poster_valid(coalesce(poster_url,''))"}, {"theater_locations", "theater_locations_ocine_identity_check", "provider <> 'ocine' OR ocine_identity_valid('theater',provider_theater_id)"}, {"theater_images", "theater_images_ocine_identity_check", "provider <> 'ocine' OR ocine_identity_valid('theater',provider_theater_id)"}} {
		changes = append(changes, providerConstraintChange{table: v.table, name: v.name, newCheck: true, addition: v.expression})
	}
	return changes
}

func TestOcineMigrationUpgradeAndIdentityIntegration(t *testing.T) {
	for _, baseline := range []int64{0, 69} {
		t.Run(fmt.Sprint(baseline), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "ocine_migration_")
			var changes []providerConstraintChange
			var before string
			if baseline == 69 {
				installMigrationPrefix(t, ctx, pool, 69, "069_cinemovida_concorde_booking.sql")
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Retained',90)`)
				changes = withOcineConstraintChanges(t, nil)
				for i := range changes {
					if !changes[i].newCheck {
						if err := pool.QueryRow(ctx, `SELECT pg_get_expr(conbin,conrelid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, changes[i].table, changes[i].name).Scan(&changes[i].previous); err != nil {
							t.Fatal(err)
						}
					}
				}
				before = cinemovidaUnchangedConstraints(t, ctx, pool, providerConstraintKeys(changes))
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			if baseline == 69 {
				assertProviderConstraintChanges(t, ctx, pool, changes)
				if after := cinemovidaUnchangedConstraints(t, ctx, pool, providerConstraintKeys(changes)); after != before {
					t.Fatal("unrelated predicates/foreign keys changed")
				}
				var n int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM movies WHERE provider='ugc' AND title='Retained' AND runtime_minutes=90`).Scan(&n); err != nil || n != 1 {
					t.Fatal("legacy data changed", err)
				}
			}
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedules WHERE target='ocine'`).Scan(&n); err != nil || n != 0 {
				t.Fatal("schedule seeded", err)
			}
			started := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
			runs := synccontrol.NewPostgresRunStore(pool)
			created, err := runs.Create(ctx, synccontrol.Status{Target: synccontrol.TargetOcine, State: synccontrol.StateRunning, Trigger: synccontrol.TriggerScheduled, StartedAt: started, From: "2026-10-17", Through: "2026-10-17", Occurrence: &synccontrol.Occurrence{ScheduleID: 42, Provider: synccontrol.TargetOcine, Revision: 2, ScheduledFor: started, Attempt: 1}, Providers: map[string]synccontrol.ProviderStatus{"ocine": {State: synccontrol.ProviderRunning}}})
			if err != nil {
				t.Fatal("scheduled run creation", err)
			}
			snapshot, err := runs.Snapshot(ctx)
			if err != nil || snapshot.Job == nil || snapshot.Job.ID != created.ID || snapshot.Job.Target != synccontrol.TargetOcine || snapshot.Job.Occurrence == nil || snapshot.Job.Occurrence.ScheduleID != 42 || snapshot.Job.Occurrence.Attempt != 1 || snapshot.Job.Providers["ocine"].State != synccontrol.ProviderRunning {
				t.Fatal("scheduled run round-trip", err)
			}
			for _, kind := range []string{"theater", "movie", "showing"} {
				for _, value := range []string{"309", "310", "461", "999", "01", "atelier-local", "atelier--local", "309-123-1792231200-4", "310-atelier-local-1792231200-past", "309-123-9223372036854775808-4", strings.Repeat("a", 118), "309-" + strings.Repeat("a", 90) + "-1792231200-past"} {
					var got bool
					if err := pool.QueryRow(ctx, `SELECT ocine_identity_valid($1,$2)`, kind, value).Scan(&got); err != nil || got != schedule.ValidOcineIdentity(kind, value) {
						t.Fatal("identity parity", kind, value, err)
					}
				}
			}
		})
	}
}
