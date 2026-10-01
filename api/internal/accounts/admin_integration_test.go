package accounts

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// These tests use the existing isolated loopback-schema harness. Run only with
// an explicitly approved disposable TEST_DATABASE_URL.
func TestAdminAccountsListingIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	now := f.now()
	created := now.Add(-time.Hour)
	verified := created.Add(time.Minute)
	var completeID, googleID int64
	for _, tc := range []struct {
		email    string
		created  time.Time
		verified *time.Time
		source   *string
		pending  *string
		id       *int64
	}{
		{"complete@example.com", created, &verified, stringPointer("email"), nil, &completeID},
		{"google@example.com", created, &verified, stringPointer("google"), stringPointer("google"), &googleID},
		{"pending@example.com", created, nil, nil, stringPointer("email"), nil},
		{"expired@example.com", now.Add(-PendingLifetime), nil, nil, stringPointer("email"), nil},
	} {
		var id int64
		if err := f.pool.QueryRow(ctx, `INSERT INTO accounts(email,created_at,email_verified_at,verification_source,pending_kind) VALUES($1,$2,$3,$4,$5) RETURNING id`, tc.email, tc.created, tc.verified, tc.source, tc.pending).Scan(&id); err != nil {
			t.Fatal("account fixture insert failed")
		}
		if tc.id != nil {
			*tc.id = id
		}
	}
	hash, err := f.hasher.Hash(ctx, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO account_passwords(account_id,encoded_hash,tentative) VALUES($1,$2,false)`, completeID, hash); err != nil {
		t.Fatal("password fixture insert failed")
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO account_google_identities(account_id,issuer,subject,observed_email_verified) VALUES($1,'https://accounts.google.com','fixture-subject',true)`, googleID); err != nil {
		t.Fatal("google fixture insert failed")
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO account_username_claims(username,account_id) VALUES('fixture_user',$1),('deleted_user',NULL)`, completeID); err != nil {
		t.Fatal("username fixture insert failed")
	}
	service := NewAdminService(NewPostgresStore(f.pool), f.now)
	first, err := service.List(ctx, AdminAccountsQuery{Limit: 2})
	if err != nil || first.Total != 4 || len(first.Items) != 2 || first.Items[0].Email != "pending@example.com" || first.Items[0].State != StatePendingEmail || first.Items[1].Email != "google@example.com" || first.Items[1].State != StatePendingUsername || !first.Items[1].GoogleLinked || first.Items[1].HasPassword {
		t.Fatalf("first page/order/capabilities mismatch: %+v %v", first, err)
	}
	second, err := service.List(ctx, AdminAccountsQuery{Limit: 2, Offset: 2})
	if err != nil || second.Total != 4 || len(second.Items) != 2 || second.Items[0].Email != "complete@example.com" || second.Items[0].State != StateComplete || !second.Items[0].HasPassword || second.Items[0].GoogleLinked || second.Items[0].Username == nil || *second.Items[0].Username != "fixture_user" || second.Items[1].Email != "expired@example.com" || second.Items[1].State != "expired" {
		t.Fatalf("second page/expired inclusion mismatch: %+v %v", second, err)
	}
	empty, err := service.List(ctx, AdminAccountsQuery{Limit: 2, Offset: 4})
	if err != nil || empty.Total != 4 || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("past-last page mismatch: %+v %v", empty, err)
	}
	// A registration committed after count must not enter the page snapshot.
	db := adminSnapshotBeginner{TransactionBeginner: f.pool, afterCount: func() {
		if _, err := f.pool.Exec(ctx, `INSERT INTO accounts(email,created_at,pending_kind) VALUES('concurrent@example.com',$1,'email')`, now); err != nil {
			t.Fatal("concurrent fixture insert failed")
		}
	}}
	coherent, err := NewAdminService(NewPostgresStore(db), f.now).List(ctx, AdminAccountsQuery{Limit: 100})
	if err != nil || coherent.Total != 4 || len(coherent.Items) != 4 || coherent.Items[0].Email != "pending@example.com" {
		t.Fatalf("count/page snapshot inconsistent: %+v %v", coherent, err)
	}
	next, err := service.List(ctx, AdminAccountsQuery{Limit: 100})
	if err != nil || next.Total != 5 || len(next.Items) != 5 || next.Items[0].Email != "concurrent@example.com" {
		t.Fatalf("next snapshot missed committed registration: %+v %v", next, err)
	}
}

func stringPointer(value string) *string { return &value }

type adminSnapshotBeginner struct {
	TransactionBeginner
	afterCount func()
}

func (b adminSnapshotBeginner) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.TransactionBeginner.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return adminSnapshotTx{Tx: tx, afterCount: b.afterCount}, nil
}

type adminSnapshotTx struct {
	pgx.Tx
	afterCount func()
}

func (tx adminSnapshotTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return adminSnapshotRow{Row: tx.Tx.QueryRow(ctx, sql, args...), afterCount: tx.afterCount}
}

type adminSnapshotRow struct {
	pgx.Row
	afterCount func()
}

func (r adminSnapshotRow) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	r.afterCount()
	return nil
}
