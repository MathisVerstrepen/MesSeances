package database

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func localAccountMigrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL is not set; account migration integration unavailable")
	}
	config, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid local test database configuration")
	}
	host := config.ConnConfig.Host
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		t.Fatal("account migration tests require an explicit loopback database")
	}
	for _, fallback := range config.ConnConfig.Fallbacks {
		if fallback.Host != "localhost" && !net.ParseIP(fallback.Host).IsLoopback() {
			t.Fatal("account migration tests reject nonlocal fallback")
		}
	}
	pool, schema := newMigrationTestPool(t, ctx, "movieflow_accounts_test_")
	var current string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&current); err != nil || current != schema {
		t.Fatal("account test schema isolation failed")
	}
	return pool
}

func TestAccountsMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "upgrade_from_042"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			pool := localAccountMigrationPool(t, ctx)
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 42, "042_query_only_vof_language.sql")
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatalf("account migration failed: %v", err)
			}
			if err := RunMigrations(ctx, pool); err != nil {
				t.Fatal("account migration reapply failed")
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			assertAccountSchema(t, ctx, pool)
		})
	}
}

func assertAccountSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var columns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='account_username_claims'`).Scan(&columns); err != nil || columns != 2 {
		t.Fatal("username tombstone has extra identity fields")
	}
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO accounts(email,created_at,email_verified_at,verification_source,pending_kind) VALUES ('alice@example.com',now(),now(),'email',NULL) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO account_username_claims VALUES ('alice_123',$1)`,
		`INSERT INTO account_passwords VALUES ($1,'$argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',false)`,
		`INSERT INTO account_google_identities VALUES ($1,'https://accounts.google.com','Subject-1','alice@example.com',true)`,
		`INSERT INTO account_sessions VALUES (decode(repeat('11',32),'hex'),$1,1,now(),now()+interval '720 hours',now(),'complete')`,
		`INSERT INTO account_tokens(token_digest,account_id,auth_revision,purpose,session_digest,created_at,expires_at) VALUES (decode(repeat('22',32),'hex'),$1,1,'verification',decode(repeat('11',32),'hex'),now(),now()+interval '1 hour')`,
		`INSERT INTO account_oauth_flows(state_digest,browser_digest,nonce,verifier_key_id,verifier_nonce,verifier_ciphertext,mode,account_id,session_digest,auth_revision,action,action_digest,created_at,expires_at) VALUES (decode(repeat('33',32),'hex'),decode(repeat('44',32),'hex'),repeat('A',43),'key-1',decode(repeat('55',12),'hex'),decode(repeat('66',59),'hex'),'reauth',$1,decode(repeat('11',32),'hex'),1,'delete_account',decode(repeat('77',32),'hex'),now(),now()+interval '10 minutes')`,
		`INSERT INTO account_mail_outbox(event_digest,account_id,token_digest,auth_revision,purpose,payload_key_id,payload_nonce,payload_ciphertext,created_at,expires_at,next_attempt_at) VALUES (decode(repeat('88',32),'hex'),$1,decode(repeat('22',32),'hex'),1,'verification','key-1',decode(repeat('55',12),'hex'),decode(repeat('99',32),'hex'),now(),now()+interval '1 hour',now())`,
	} {
		if _, err := pool.Exec(ctx, sql, id); err != nil {
			t.Fatalf("account child fixture failed: %v", err)
		}
	}
	for _, sql := range []string{
		`UPDATE accounts SET auth_revision=0`,
		`UPDATE accounts SET email='Alice@example.com'`,
		`UPDATE account_username_claims SET username='Alice'`,
		`UPDATE account_username_claims SET username='1alice'`,
		`UPDATE account_sessions SET token_digest=decode('11','hex')`,
		`UPDATE account_sessions SET expires_at=created_at+interval '744 hours'`,
		`UPDATE account_oauth_flows SET mode='login'`,
		`UPDATE account_oauth_flows SET session_digest=NULL`,
		`UPDATE account_mail_outbox SET state='sent',finished_at=now()`,
		`UPDATE account_tokens SET purpose='reauth_grant'`,
	} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Fatalf("invalid account row accepted: %s", sql)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO account_mail_suppressions VALUES (decode(repeat('aa',32),'hex'),'complaint',now(),now(),now()+interval '4320 hours')`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err := tx.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var attached bool
	if err := pool.QueryRow(ctx, `SELECT account_id=$1 FROM account_username_claims WHERE username='alice_123'`, id).Scan(&attached); err != nil || !attached {
		t.Fatal("rollback lost username ownership")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"accounts", "account_passwords", "account_google_identities", "account_sessions", "account_tokens", "account_oauth_flows", "account_mail_outbox"} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil || count != 0 {
			t.Fatalf("deletion did not purge %s: %d/%v", table, count, err)
		}
	}
	var unlinked bool
	if err := pool.QueryRow(ctx, `SELECT account_id IS NULL FROM account_username_claims WHERE username='alice_123'`).Scan(&unlinked); err != nil || !unlinked {
		t.Fatal("permanent reservation missing")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO account_username_claims(username) VALUES ('alice_123')`); !uniqueViolation(err) {
		t.Fatal("deleted username reusable")
	}
	if err := pool.QueryRow(ctx, `INSERT INTO accounts(email,created_at,pending_kind) VALUES ('alice@example.com',now(),'google') RETURNING id`).Scan(&id); err != nil {
		t.Fatal("deleted email not reusable")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM account_mail_suppressions`).Scan(&count); err != nil || count != 1 {
		t.Fatal("suppression wrongly deleted")
	}
}

func TestAccountsMigrationRollbackIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := localAccountMigrationPool(t, ctx)
	migrations := installMigrationPrefix(t, ctx, pool, 42, "042_query_only_vof_language.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE account_tokens (collision boolean)`); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, pool); err == nil {
		t.Fatal("conflicting migration succeeded")
	}
	assertCompleteMigrationHistory(t, ctx, pool, migrations[:42])
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('accounts') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatal("failed migration left partial account schema")
	}
}

func TestAccountsUsernameUniquenessIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := localAccountMigrationPool(t, ctx)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var ids [2]int64
	for i, email := range []string{"first@example.com", "second@example.com"} {
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(email,created_at,email_verified_at,verification_source,pending_kind) VALUES ($1,now(),now(),'email','email') RETURNING id`, email).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range ids {
		go func() {
			<-start
			_, err := pool.Exec(ctx, `INSERT INTO account_username_claims VALUES ('contested_name',$1)`, id)
			results <- err
		}()
	}
	close(start)
	wins, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			wins++
		} else if uniqueViolation(err) {
			conflicts++
		} else {
			t.Fatalf("unexpected claim error: %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
