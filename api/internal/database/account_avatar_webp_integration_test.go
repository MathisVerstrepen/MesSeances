package database

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestAccountAvatarWebPConstraintIntegration(t *testing.T) {
	for _, scenario := range []string{"fresh", "validated_047", "unvalidated_047_webp", "unvalidated_047_png"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "movieflow_avatar_webp_test_")
			migrations := mustEmbeddedMigrations(t)
			const webpPath = "11111111111111111111111111111111.webp"
			const pngPath = "11111111111111111111111111111111.png"
			const insert = `INSERT INTO accounts(email,created_at,pending_kind,avatar_path,avatar_source,avatar_revision)
				VALUES('avatar@example.com',now(),'google',$1,'upload',3)`
			pending := scenario == "unvalidated_047_webp" || scenario == "unvalidated_047_png"
			if pending {
				installMigrationPrefix(t, ctx, pool, 46, "046_account_avatars.sql")
				if _, err := pool.Exec(ctx, insert, pngPath); err != nil {
					t.Fatal(err)
				}
				migration := requireMigrationPrefix(t, migrations, 47, "047_account_avatar_webp.sql")[46]
				if _, err := pool.Exec(ctx, migration.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `INSERT INTO movieflow_schema_migrations(version,name) VALUES($1,$2)`, migration.version, migration.name); err != nil {
					t.Fatal(err)
				}
				if scenario == "unvalidated_047_webp" {
					if _, err := pool.Exec(ctx, `UPDATE accounts SET avatar_path=$1`, webpPath); err != nil {
						t.Fatal(err)
					}
				}
			} else if scenario == "validated_047" {
				installMigrationPrefix(t, ctx, pool, 47, "047_account_avatar_webp.sql")
				if _, err := pool.Exec(ctx, insert, webpPath); err != nil {
					t.Fatal(err)
				}
			}
			const stateSQL = `SELECT convalidated,conenforced FROM pg_constraint
				WHERE conrelid='accounts'::regclass AND conname='accounts_avatar_webp_check'`
			var before string
			if scenario != "fresh" {
				var validated, enforced bool
				if err := pool.QueryRow(ctx, stateSQL).Scan(&validated, &enforced); err != nil || validated == pending || !enforced {
					t.Fatalf("initial constraint validated=%v enforced=%v err=%v", validated, enforced, err)
				}
				if err := pool.QueryRow(ctx, `SELECT row_to_json(accounts)::text FROM accounts`).Scan(&before); err != nil {
					t.Fatal(err)
				}
			}
			err := RunMigrations(ctx, pool)
			invalid := scenario == "unvalidated_047_png"
			if (err != nil) != invalid {
				t.Fatalf("migration error=%v; invalid data=%v", err, invalid)
			}
			var validated, enforced bool
			if err := pool.QueryRow(ctx, stateSQL).Scan(&validated, &enforced); err != nil || validated == invalid || !enforced {
				t.Fatalf("final constraint validated=%v enforced=%v err=%v", validated, enforced, err)
			}
			if scenario != "fresh" {
				var after string
				if err := pool.QueryRow(ctx, `SELECT row_to_json(accounts)::text FROM accounts`).Scan(&after); err != nil || after != before {
					t.Fatalf("migration changed account data: err=%v", err)
				}
			}
			if invalid {
				assertCompleteMigrationHistory(t, ctx, pool, migrations[:47])
				return
			}
			assertCompleteMigrationHistory(t, ctx, pool, migrations)
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatalf("repeat migration: %v", err)
			}
			if scenario == "fresh" {
				if _, err := pool.Exec(ctx, insert, webpPath); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(ctx, `UPDATE accounts SET avatar_path=$1`, pngPath); err == nil {
				t.Fatal("PNG reference accepted")
			}
		})
	}
}
