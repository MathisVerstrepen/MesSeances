package accounts

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
)

func TestTheaterPreferencesMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprintf("upgrade_from_048_%t", upgrade), func(t *testing.T) {
			migrate := database.RunMigrations
			if upgrade {
				migrate = theaterPreferencesMigrationPrefix
			}
			f := newLifecycleFixtureWithSchema(t, migrate)
			ctx := t.Context()
			owner := f.complete(t, "schema@example.com", "theater_schema")
			if err := database.RunMigrations(ctx, f.pool); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_theater_preferences`).Scan(&count); err != nil || count != 0 {
				t.Fatal("migration backfilled preferences")
			}
			var id int64
			if err := f.pool.QueryRow(ctx, `SELECT id FROM accounts WHERE email='schema@example.com'`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{
				`NULL`, `ARRAY[NULL]::text[]`, `ARRAY['']`, `ARRAY['ugc-1','ugc-1']`, `ARRAY['ugc-1','ugc-1 ']`,
				`ARRAY['é']`, `ARRAY[E'ugc-1\n']`, `ARRAY[repeat('a',129)]`, `ARRAY[['ugc-1','ugc-2']]`,
				`'[0:0]={ugc-1}'::text[]`, `ARRAY(SELECT 'ugc-' || n FROM generate_series(1,4097) n)`,
			} {
				if _, err := f.pool.Exec(ctx, `INSERT INTO account_theater_preferences VALUES($1,1,`+value+`)`, id); err == nil {
					t.Fatalf("schema accepted invalid array %s", value)
				}
			}
			for _, revision := range []int64{0, -1, maxTheaterPreferenceRevision + 1} {
				if _, err := f.pool.Exec(ctx, `INSERT INTO account_theater_preferences VALUES($1,$2,'{}')`, id, revision); err == nil {
					t.Fatal("schema accepted invalid revision")
				}
			}
			if _, err := f.pool.Exec(ctx, `INSERT INTO account_theater_preferences VALUES($1,1,ARRAY(SELECT lpad(n::text,128,'0') FROM generate_series(1,4096) n))`, id); err != nil {
				t.Fatal("schema rejected maximum list", err)
			}
			view, err := f.service.TheaterPreferences(ctx, owner.Cookie.Token)
			if err != nil || len(view.TheaterIDs) != 4096 {
				t.Fatal("maximum list read failed")
			}
			// No live-catalog foreign key: both known and absent durable IDs
			// survive replacing and then pruning every schedule generation.
			if _, err = f.pool.Exec(ctx, `UPDATE account_theater_preferences SET theater_ids=ARRAY['ugc-1','unknown-z'] WHERE account_id=$1`, id); err != nil {
				t.Fatal(err)
			}
			for _, generation := range []int{1, 2} {
				if _, err = f.pool.Exec(ctx, `INSERT INTO theaters(generation_id,id,provider,provider_id,slug,name,address,city,postal_code) VALUES($1,'ugc-1','ugc','1','ugc-1','Cinema','Street','Paris','75001')`, generation); err != nil {
					t.Fatal(err)
				}
				if _, err = f.pool.Exec(ctx, `DELETE FROM theaters WHERE generation_id<$1`, generation); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = f.pool.Exec(ctx, `DELETE FROM theaters`); err != nil {
				t.Fatal(err)
			}
			view, err = f.service.TheaterPreferences(ctx, owner.Cookie.Token)
			if err != nil || strings.Join(view.TheaterIDs, ",") != "ugc-1,unknown-z" || view.Revision != "1" {
				t.Fatal("catalog pruning affected durable IDs")
			}
			if _, err = f.pool.Exec(ctx, `UPDATE account_theater_preferences SET theater_ids='{}' WHERE account_id=$1`, id); err != nil {
				t.Fatal("schema rejected empty selection", err)
			}
			if _, err = f.pool.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
			if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_theater_preferences`).Scan(&count); err != nil || count != 0 {
				t.Fatal("account deletion left preferences")
			}
		})
	}
}

// Install the historical prefix in this fixture's isolated schema. Runtime
// migrations then perform the real upgrade and validate the recorded history.
func theaterPreferencesMigrationPrefix(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `CREATE TABLE movieflow_schema_migrations(version bigint PRIMARY KEY,name text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := os.ReadDir("../database/migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") || name >= "049" {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return err
		}
		body, err := os.ReadFile("../database/migrations/" + name)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(body), pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf("install prefix migration %s: %w", name, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO movieflow_schema_migrations(version,name) VALUES($1,$2)`, version, name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
