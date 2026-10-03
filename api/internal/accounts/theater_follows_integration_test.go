package accounts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/database"
)

func TestTheaterFollowPersistenceIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "one@example.com", "follow_one")
	two := f.complete(t, "two@example.com", "follow_two")
	var initialAuthRevision int64
	if err := f.pool.QueryRow(ctx, `SELECT auth_revision FROM accounts WHERE email='one@example.com'`).Scan(&initialAuthRevision); err != nil {
		t.Fatal(err)
	}
	assertView := func(raw, username, revision string, ids []string) {
		t.Helper()
		v, err := f.service.TheaterFollows(ctx, raw)
		if err != nil || v.Username != username || v.Revision != revision || v.TheaterIDs == nil || !slices.Equal(v.TheaterIDs, ids) {
			t.Fatalf("view %+v: %v", v, err)
		}
	}
	assertView(one.Cookie.Token, "follow_one", "0", []string{})
	v, err := f.service.SaveTheaterFollow(ctx, one.Cookie.Token, "follow_one", "0", "ugc-1", false)
	if err != nil || v.Revision != "0" {
		t.Fatal("absent remove initialized state", err)
	}
	var rows int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_theater_follows`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("empty read/remove wrote state")
	}
	if _, err = f.service.SaveTheaterFollow(ctx, two.Cookie.Token, "follow_one", "0", "ugc-1", true); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("client chose owner")
	}
	for _, step := range []struct {
		expected, id, revision string
		followed               bool
	}{
		{"0", "unknown-z", "1", true}, {"1", "ugc-1", "2", true}, {"2", "ugc-1", "2", true},
		{"2", "ugc-1", "3", false}, {"3", "unknown-z", "4", false}, {"4", "ugc-1", "4", false},
	} {
		v, err = f.service.SaveTheaterFollow(ctx, one.Cookie.Token, "follow_one", step.expected, step.id, step.followed)
		if err != nil || v.Revision != step.revision {
			t.Fatalf("mutation %+v: %v", v, err)
		}
		if step.revision == "2" && !slices.Equal(v.TheaterIDs, []string{"ugc-1", "unknown-z"}) {
			t.Fatal("IDs not canonical or absent inventory dropped")
		}
	}
	if _, err = f.service.SaveTheaterFollow(ctx, one.Cookie.Token, "follow_one", "3", "ugc-1", false); !errors.Is(err, ErrTheaterFollowsChanged) {
		t.Fatal("stale no-op succeeded")
	}
	assertView(one.Cookie.Token, "follow_one", "4", []string{})
	assertView(two.Cookie.Token, "follow_two", "0", []string{})
	// Membership is separate from both existing resources and account authority.
	var revision int64
	if err = f.pool.QueryRow(ctx, `SELECT auth_revision FROM accounts WHERE email='one@example.com'`).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != initialAuthRevision {
		t.Fatalf("follow changed auth revision: before=%d after=%d", initialAuthRevision, revision)
	}
	if err = f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM account_theater_preferences)+(SELECT count(*) FROM account_watchlist_state)+(SELECT count(*) FROM account_watchlist_items)`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("follow wrote selection/watchlist")
	}
	login, err := f.service.Login(ctx, "one@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	assertView(login.Cookie.Token, "follow_one", "4", []string{})
	if err = f.service.Logout(ctx, one.Cookie.Token, false); err != nil {
		t.Fatal(err)
	}
	assertView(login.Cookie.Token, "follow_one", "4", []string{})
}

func TestTheaterFollowSchemaIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	a := f.complete(t, "schema@example.com", "follow_schema")
	if _, err := f.service.SaveTheaterFollow(t.Context(), a.Cookie.Token, "follow_schema", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	var before, after time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT applied_at FROM movieflow_schema_migrations WHERE version=59 AND name='059_account_theater_follows.sql'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := database.RunMigrations(t.Context(), f.pool); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(t.Context(), `SELECT applied_at FROM movieflow_schema_migrations WHERE version=59`).Scan(&after); err != nil || !before.Equal(after) {
		t.Fatal("migration reapplied", err)
	}
	for _, statement := range []string{
		`UPDATE account_theater_follows SET revision=0`,
		`UPDATE account_theater_follows SET revision=9007199254740992`,
		`UPDATE account_theater_follows SET theater_ids=NULL`,
		`UPDATE account_theater_follows SET theater_ids=ARRAY['']`,
		`UPDATE account_theater_follows SET theater_ids=ARRAY['ugc/1']`,
		`UPDATE account_theater_follows SET theater_ids=ARRAY['ugc-1',NULL]`,
		`UPDATE account_theater_follows SET theater_ids=ARRAY['ugc-1','ugc-1']`,
		`UPDATE account_theater_follows SET theater_ids='[0:0]={ugc-1}'::text[]`,
		`UPDATE account_theater_follows SET theater_ids=ARRAY[['ugc-1','ugc-2']]`,
		`UPDATE account_theater_follows SET theater_ids=ARRAY(SELECT 'unknown-'||n FROM generate_series(1,4097) n)`,
		`INSERT INTO account_theater_follows VALUES(9223372036854775807,1,'{}')`,
		`INSERT INTO account_theater_follows SELECT * FROM account_theater_follows`,
	} {
		if _, err := f.pool.Exec(t.Context(), statement); err == nil {
			t.Fatal("invalid private state accepted", statement)
		}
	}
	v, err := f.service.TheaterFollows(t.Context(), a.Cookie.Token)
	if err != nil || v.Revision != "1" || !slices.Equal(v.TheaterIDs, []string{"ugc-1"}) {
		t.Fatal("constraint/reapply changed state", v, err)
	}
}

func TestTheaterFollowCapacityAndExhaustionIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	a := f.complete(t, "cap@example.com", "follow_cap")
	ids := make([]string, maxTheaterPreferences)
	for i := range ids {
		ids[i] = fmt.Sprintf("unknown-%04d", i)
	}
	ctx := t.Context()
	storedIDs := slices.Clone(ids)
	slices.Reverse(storedIDs)
	if _, err := f.pool.Exec(ctx, `INSERT INTO account_theater_follows(account_id,revision,theater_ids) SELECT id,1,$1 FROM accounts WHERE email='cap@example.com'`, storedIDs); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "follow_cap", "0", "extra", true); !errors.Is(err, ErrTheaterFollowsChanged) {
		t.Fatal("capacity before CAS")
	}
	if _, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "follow_cap", "1", "extra", true); !errors.Is(err, ErrTheaterFollowLimit) {
		t.Fatal("capacity not enforced")
	}
	if v, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "follow_cap", "1", ids[0], true); err != nil || v.Revision != "1" {
		t.Fatal("capacity no-op rejected")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE account_theater_follows SET revision=$1`, maxTheaterPreferenceRevision); err != nil {
		t.Fatal(err)
	}
	maxRevision := strconv.FormatInt(maxTheaterPreferenceRevision, 10)
	if v, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "follow_cap", maxRevision, ids[0], true); err != nil || v.Revision != maxRevision {
		t.Fatal("max-revision no-op rejected")
	}
	if _, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "follow_cap", maxRevision, ids[0], false); !errors.Is(err, ErrUnavailable) {
		t.Fatal("revision exhausted write succeeded")
	}
	v, err := f.service.TheaterFollows(ctx, a.Cookie.Token)
	if err != nil || v.Revision != maxRevision || !slices.Equal(v.TheaterIDs, ids) {
		t.Fatal("exhaustion mutated state")
	}
}

func TestTheaterFollowConcurrentCASIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	one := f.complete(t, "cas@example.com", "follow_cas")
	two, err := f.service.Login(t.Context(), "cas@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"0", "1"} {
		start := make(chan struct{})
		done := make(chan error, 2)
		for i, raw := range []string{one.Cookie.Token, two.Cookie.Token} {
			go func() {
				<-start
				_, err := f.service.SaveTheaterFollow(t.Context(), raw, "follow_cas", revision, "unknown-"+revision+"-"+strconv.Itoa(i), true)
				done <- err
			}()
		}
		close(start)
		success, conflict := 0, 0
		for range 2 {
			err := <-done
			switch {
			case err == nil:
				success++
			case errors.Is(err, ErrTheaterFollowsChanged):
				conflict++
			default:
				t.Fatal(err)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatal("CAS winners", success, conflict)
		}
		a, err := f.service.TheaterFollows(t.Context(), one.Cookie.Token)
		if err != nil {
			t.Fatal(err)
		}
		b, err := f.service.TheaterFollows(t.Context(), two.Cookie.Token)
		if err != nil || a.Revision != b.Revision || !slices.Equal(a.TheaterIDs, b.TheaterIDs) {
			t.Fatal("sessions differ")
		}
	}
}

func TestTheaterFollowRollbackIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	a := f.complete(t, "rollback@example.com", "follow_rollback")
	ctx := t.Context()
	if _, err := f.pool.Exec(ctx, `ALTER TABLE account_theater_follows ADD CONSTRAINT fixture_reject_follow CHECK (NOT ('reject-me'=ANY(theater_ids)))`); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"0", "1"} {
		digest, _ := TokenDigest(a.Cookie.Token)
		var before, after time.Time
		if err := f.pool.QueryRow(ctx, `SELECT last_seen_at FROM account_sessions WHERE token_digest=$1`, digest[:]).Scan(&before); err != nil {
			t.Fatal(err)
		}
		f.advance(time.Minute)
		v, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "follow_rollback", revision, "reject-me", true)
		if !errors.Is(err, ErrUnavailable) || v.Username != "" || v.TheaterIDs != nil {
			t.Fatal("failed commit leaked data")
		}
		if err := f.pool.QueryRow(ctx, `SELECT last_seen_at FROM account_sessions WHERE token_digest=$1`, digest[:]).Scan(&after); err != nil || !before.Equal(after) {
			t.Fatal("failed write retained session touch")
		}
		v, err = f.service.TheaterFollows(ctx, a.Cookie.Token)
		if err != nil || v.Revision != revision || len(v.TheaterIDs) != 0 {
			t.Fatal("rollback lost state")
		}
		if revision == "0" {
			if _, err = f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "follow_rollback", "0", "unknown", true); err != nil {
				t.Fatal(err)
			}
			if _, err = f.pool.Exec(ctx, `UPDATE account_theater_follows SET theater_ids='{}'`); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestTheaterFollowAuthorizationIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	assertDenied := func(raw string, want error) {
		t.Helper()
		if _, err := f.service.TheaterFollows(t.Context(), raw); !errors.Is(err, want) {
			t.Fatalf("read %v want %v", err, want)
		}
		if _, err := f.service.SaveTheaterFollow(t.Context(), raw, "follow_auth", "0", "ugc-1", true); !errors.Is(err, want) {
			t.Fatalf("write %v want %v", err, want)
		}
	}
	assertDenied("", ErrUnauthorized)
	f.register(t, "pending@example.com", testPassword)
	pending, err := f.service.Login(t.Context(), "pending@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	assertDenied(pending.Cookie.Token, ErrPending)
	assertDenied(f.pending(t, "username@example.com").Cookie.Token, ErrPending)
	a := f.complete(t, "auth@example.com", "follow_auth")
	if err := f.service.Logout(t.Context(), a.Cookie.Token, true); err != nil {
		t.Fatal(err)
	}
	assertDenied(a.Cookie.Token, ErrUnauthorized)
	for _, expiry := range []time.Duration{SessionIdleLifetime, SessionLifetime} {
		a, err = f.service.Login(t.Context(), "auth@example.com", testPassword, "")
		if err != nil {
			t.Fatal(err)
		}
		f.advance(expiry)
		assertDenied(a.Cookie.Token, ErrUnauthorized)
	}
}

func TestTheaterFollowRevocationAndDeletionRaceIntegration(t *testing.T) {
	requireFollowDatabase(t)
	for _, operation := range []string{"revoke", "delete"} {
		t.Run(operation, func(t *testing.T) {
			f := newLifecycleFixture(t)
			a := f.complete(t, "race@example.com", "follow_race")
			if _, err := f.service.SaveTheaterFollow(t.Context(), a.Cookie.Token, "follow_race", "0", "ugc-1", true); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tx, err := f.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			var id int64
			if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE email='race@example.com' FOR UPDATE`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "follow_race", "1", "ugc-2", true)
				done <- err
			}()
			if operation == "delete" {
				_, err = tx.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id)
			} else {
				err = revoke(ctx, tx, &account{id: id, revision: 1})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err = <-done; !errors.Is(err, ErrUnauthorized) {
				t.Fatal("stale write accepted", err)
			}
			var revision int64
			err = f.pool.QueryRow(ctx, `SELECT revision FROM account_theater_follows WHERE account_id=$1`, id).Scan(&revision)
			if operation == "delete" {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatal("cascade missing")
				}
			} else if err != nil || revision != 1 {
				t.Fatal("revoked write mutated state")
			}
		})
	}
}

func requireFollowDatabase(t *testing.T) {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL is required for integration tests")
	}
}
