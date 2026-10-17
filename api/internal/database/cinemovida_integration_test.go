package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/schedule"
)

func TestCinemovidaMigrationIdentityBookingAndUpgradeIntegration(t *testing.T) {
	for _, baseline := range []int64{0, 67, 68} {
		t.Run(map[int64]string{0: "fresh", 67: "upgrade067", 68: "upgrade068"}[baseline], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "cinemovida_migration_")
			var changes []providerConstraintChange
			var before string
			if baseline == 67 {
				installMigrationPrefix(t, ctx, pool, 67, "067_cineode_provider.sql")
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'ugc','10','ugc-film-10','Retained',90)`)
				changes = withCinemovidaConstraintChanges(t, nil)
				changes = withOcineConstraintChanges(t, changes)
				for i := range changes {
					if !changes[i].newCheck {
						if err := pool.QueryRow(ctx, `SELECT pg_get_expr(conbin,conrelid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, changes[i].table, changes[i].name).Scan(&changes[i].previous); err != nil {
							t.Fatal(err)
						}
					}
				}
				before = cinemovidaUnchangedConstraints(t, ctx, pool, providerConstraintKeys(changes))
			}
			if baseline == 68 {
				installMigrationPrefix(t, ctx, pool, 68, "068_cinemovida_provider.sql")
				changes = withOcineConstraintChanges(t, nil)
				for i := range changes {
					if !changes[i].newCheck {
						if err := pool.QueryRow(ctx, `SELECT pg_get_expr(conbin,conrelid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, changes[i].table, changes[i].name).Scan(&changes[i].previous); err != nil {
							t.Fatal(err)
						}
					}
				}
				mustCinevilleSQL(t, ctx, pool, `INSERT INTO movies(generation_id,provider,provider_id,slug,title,runtime_minutes) VALUES(1,'cinemovida','123','cinemovida-film-123','Retained Concorde',0)`)
				before = cinemovidaUnchangedConstraints(t, ctx, pool, providerConstraintKeys(changes))
				var accepted bool
				if err := pool.QueryRow(ctx, `SELECT cinemovida_booking_valid('https://achat.concordecinema.com/r/123','P0876',$1)`, schedule.CinemovidaShowingID("P0876", "source")).Scan(&accepted); err != nil || accepted {
					t.Fatal("068 unexpectedly accepts new route", err)
				}
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal(err)
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			if baseline == 67 {
				assertProviderConstraintChanges(t, ctx, pool, changes)
				if after := cinemovidaUnchangedConstraints(t, ctx, pool, providerConstraintKeys(changes)); after != before {
					t.Fatal("unrelated constraints or foreign keys changed")
				}
				var retained int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM movies WHERE provider='ugc' AND title='Retained' AND runtime_minutes=90`).Scan(&retained); err != nil || retained != 1 {
					t.Fatal("legacy data changed", err)
				}
			}
			if baseline == 68 {
				assertProviderConstraintChanges(t, ctx, pool, changes)
				if cinemovidaUnchangedConstraints(t, ctx, pool, providerConstraintKeys(changes)) != before {
					t.Fatal("069/070 changed unrelated CHECK definitions or foreign keys")
				}
				var retained int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM movies WHERE provider='cinemovida' AND title='Retained Concorde' AND runtime_minutes=0`).Scan(&retained); err != nil || retained != 1 {
					t.Fatal("068 data changed", err)
				}
			}
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_schedules WHERE target='cinemovida'`).Scan(&n); err != nil || n != 0 {
				t.Fatal("schedule seeded", err)
			}
			for _, kind := range []string{"theater", "movie", "showing"} {
				for _, value := range []string{"P0176", "P0218", "P7951", "P0876", "P9999", "p0176", "1", "01", "cLocal_1", "c" + strings.Repeat("a", 111), "c" + strings.Repeat("a", 112), "P0176-" + strings.Repeat("a", 64), "P9999-" + strings.Repeat("a", 64)} {
					var got bool
					if err := pool.QueryRow(ctx, `SELECT cinemovida_identity_valid($1,$2)`, kind, value).Scan(&got); err != nil || got != schedule.ValidCinemovidaIdentity(kind, value) {
						t.Fatal("identity parity", kind, value, err)
					}
				}
			}
			for _, theater := range []string{"P0176", "P0218", "P7951", "P0876", "P9999"} {
				showing := schedule.CinemovidaShowingID(theater, "source")
				valid := "https://" + schedule.CinemovidaBookingHost(theater) + "/reserver/r/123"
				short := "https://" + schedule.CinemovidaBookingHost(theater) + "/r/123"
				for _, raw := range []string{valid, valid + "?x=1", valid + "#", strings.Replace(valid, "https:", "http:", 1), strings.Replace(valid, "/r/123", "/r/0123", 1), "https://achat.cinema-castillet.com/reserver/r/123", "https://relay.mvtx.us/ticketing/dbz", "https://achat.cinema-castillet.com:443/reserver/r/123", "https://achat.cinema-castillet.com/reserver/%72/123", short, short + "?", short + "#", short + "\n", " " + short, strings.Replace(short, "https:", "http:", 1), strings.Replace(short, "/123", "/0123", 1), strings.Replace(short, "/123", "/0", 1), strings.Replace(short, "/123", "/%31", 1), strings.Replace(short, "/123", "/../123", 1), strings.Replace(short, ".com/", ".com:443/", 1), strings.Replace(short, "https://", "https://user@", 1), "https://achat.concordecinema.com/r/123"} {
					var got bool
					want := schedule.ValidCinemovidaBookingURL(raw, theater) && schedule.ValidCinemovidaIdentity("showing", showing)
					if err := pool.QueryRow(ctx, `SELECT cinemovida_booking_valid($1,$2,$3)`, raw, theater, showing).Scan(&got); err != nil || got != want {
						t.Fatal("booking parity", theater, raw, err)
					}
				}
				var got bool
				if err := pool.QueryRow(ctx, `SELECT cinemovida_booking_valid($1,$2,$3)`, valid, theater, schedule.CinemovidaShowingID("P9999", "source")).Scan(&got); err != nil || got {
					t.Fatal("cross-theater SQL accepted", err)
				}
			}
		})
	}
}

func cinemovidaUnchangedConstraints(t *testing.T, ctx context.Context, pool *pgxpool.Pool, exclude []string) string {
	t.Helper()
	var result string
	if err := pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_array(c.relname,k.conname,pg_get_constraintdef(k.oid),k.convalidated) ORDER BY c.relname,k.conname),'[]'::jsonb)::text FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND NOT (c.relname||'.'||k.conname=ANY($1::text[]))`, exclude).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
