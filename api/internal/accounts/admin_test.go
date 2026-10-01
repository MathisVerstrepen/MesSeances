package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestAdminAccountStatesAndMinimalDTO(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	username := "fixture_user"
	verified := now.Add(-time.Hour).In(time.FixedZone("fixture", 3600))
	for _, tc := range []struct {
		name     string
		verified *time.Time
		username *string
		age      time.Duration
		state    State
		password bool
		google   bool
	}{
		{"pending email", nil, nil, time.Hour, StatePendingEmail, true, false},
		{"pending username", &verified, nil, time.Hour, StatePendingUsername, false, true},
		{"before expiry", nil, nil, PendingLifetime - time.Nanosecond, StatePendingEmail, true, false},
		{"exact email expiry", nil, nil, PendingLifetime, "expired", true, false},
		{"exact username expiry", &verified, nil, PendingLifetime, "expired", false, true},
		{"complete never expires", &verified, &username, PendingLifetime * 2, StateComplete, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := account{email: "fixture@example.com", username: tc.username, verified: tc.verified, created: now.Add(-tc.age), google: tc.google}
			item := adminAccountView(a, tc.password, now)
			if item.State != tc.state || item.HasPassword != tc.password || item.GoogleLinked != tc.google {
				t.Fatalf("overview state/method mismatch: %+v", item)
			}
			if item.CreatedAt != a.created.UTC().Format(time.RFC3339Nano) || (item.EmailVerifiedAt == nil) != (tc.verified == nil) {
				t.Fatal("date/nullability mismatch")
			}
			if item.EmailVerifiedAt != nil && *item.EmailVerifiedAt != verified.UTC().Format(time.RFC3339Nano) {
				t.Fatal("verification date is not UTC")
			}
			encoded, err := json.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"email", "username", "state", "created_at", "email_verified_at", "has_password", "google_linked"} {
				if _, ok := fields[key]; !ok {
					t.Fatalf("missing DTO field %s", key)
				}
			}
			if len(fields) != 7 {
				t.Fatal("unexpected account fields escaped")
			}
		})
	}
}

type adminListBeginner struct {
	tx      *adminListTx
	err     error
	options pgx.TxOptions
	ctx     context.Context
	calls   int
}

func (b *adminListBeginner) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	b.options, b.ctx = options, ctx
	b.calls++
	return b.tx, b.err
}

type adminListTx struct {
	testTx
	total               int64
	countErr, queryErr  error
	rows                *adminListRows
	countSQL, pageSQL   string
	args                []any
	countCalls, queries int
}

func (tx *adminListTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.countSQL = sql
	tx.countCalls++
	return adminCountRow{total: tx.total, err: tx.countErr}
}

func (tx *adminListTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.pageSQL, tx.args = sql, args
	tx.queries++
	return tx.rows, tx.queryErr
}

type adminCountRow struct {
	total int64
	err   error
}

func (r adminCountRow) Scan(dest ...any) error {
	if r.err == nil {
		*dest[0].(*int64) = r.total
	}
	return r.err
}

type adminListRows struct {
	pgx.Rows
	accounts         []account
	passwords        []bool
	index            int
	scanErr, rowsErr error
	closed           bool
}

func (r *adminListRows) Next() bool { r.index++; return r.index <= len(r.accounts) }
func (r *adminListRows) Close()     { r.closed = true }
func (r *adminListRows) Err() error { return r.rowsErr }
func (r *adminListRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	a := r.accounts[r.index-1]
	*dest[0].(*string) = a.email
	*dest[1].(**string) = a.username
	*dest[2].(*time.Time) = a.created
	*dest[3].(**time.Time) = a.verified
	*dest[4].(*bool) = r.passwords[r.index-1]
	*dest[5].(*bool) = a.google
	return nil
}

func TestAdminAccountsReadBoundary(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "page", true: "past last page"}[empty], func(t *testing.T) {
			rows := &adminListRows{}
			if !empty {
				rows.accounts = []account{{email: "fixture@example.com", created: now.Add(-PendingLifetime)}}
				rows.passwords = []bool{true}
			}
			tx := &adminListTx{total: 200, rows: rows}
			db := &adminListBeginner{tx: tx}
			query := AdminAccountsQuery{Limit: 50, Offset: 150}
			page, err := NewAdminService(NewPostgresStore(db), func() time.Time { return now }).List(t.Context(), query)
			if err != nil || page.Total != 200 || page.Limit != 50 || page.Offset != 150 || page.Items == nil {
				t.Fatalf("page=%+v err=%v", page, err)
			}
			if !empty && (len(page.Items) != 1 || page.Items[0].State != "expired" || !page.Items[0].HasPassword) {
				t.Fatal("stored expired account omitted or capability changed")
			}
			if db.options.IsoLevel != pgx.RepeatableRead || db.options.AccessMode != pgx.ReadOnly || db.calls != 1 {
				t.Fatal("read is not one read-only coherent snapshot")
			}
			if deadline, ok := db.ctx.Deadline(); !ok || time.Until(deadline) > adminAccountsTimeout {
				t.Fatal("listing has no bounded deadline")
			}
			if tx.countSQL != "SELECT count(*) FROM accounts" || tx.countCalls != 1 || tx.queries != 1 || tx.pageSQL != adminAccountsSQL || !reflect.DeepEqual(tx.args, []any{50, 150}) {
				t.Fatal("bounded count/page query mismatch")
			}
			if !tx.committed || !tx.rolledBack || !tx.rollbackContextActive || !rows.closed {
				t.Fatal("read transaction/resources not released")
			}
		})
	}
	if !strings.Contains(adminAccountsSQL, "ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2") || !strings.Contains(adminAccountsSQL, "ORDER BY a.created_at DESC,a.id DESC") {
		t.Fatal("deterministic bounded ordering missing")
	}
}

func TestAdminAccountsFailureSanitization(t *testing.T) {
	private := errors.New("SQL fixture@example.com credential detail")
	for _, phase := range []string{"begin", "count", "query", "scan", "rows", "commit"} {
		t.Run(phase, func(t *testing.T) {
			rows := &adminListRows{accounts: []account{{created: time.Now()}}, passwords: []bool{false}}
			tx := &adminListTx{total: 1, rows: rows}
			db := &adminListBeginner{tx: tx}
			switch phase {
			case "begin":
				db.err = private
			case "count":
				tx.countErr = private
			case "query":
				tx.queryErr = private
			case "scan":
				rows.scanErr = private
			case "rows":
				rows.rowsErr = private
			case "commit":
				tx.commitErr = private
			}
			page, err := NewAdminService(NewPostgresStore(db), nil).List(t.Context(), AdminAccountsQuery{Limit: 50})
			if !errors.Is(err, ErrUnavailable) || page.Items != nil || page.Total != 0 || strings.Contains(err.Error(), "fixture@example.com") {
				t.Fatalf("private/partial failure leaked: page=%+v error=%v", page, err)
			}
			if phase != "begin" && (!tx.rolledBack || !tx.rollbackContextActive) {
				t.Fatal("failed transaction not rolled back")
			}
		})
	}
}

func TestAdminAccountsQueryAndAvailability(t *testing.T) {
	db := &adminListBeginner{}
	service := NewAdminService(NewPostgresStore(db), nil)
	for _, query := range []AdminAccountsQuery{{}, {Limit: -1}, {Limit: 101}, {Limit: 50, Offset: -1}, {Limit: 50, Offset: math.MaxInt32 + 1}} {
		if _, err := service.List(t.Context(), query); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("query=%+v error=%v", query, err)
		}
	}
	if db.calls != 0 {
		t.Fatal("invalid query reached database")
	}
	for _, service := range []*AdminService{nil, NewAdminService(nil, nil), NewAdminService(NewPostgresStore(nil), nil)} {
		if _, err := service.List(t.Context(), AdminAccountsQuery{Limit: 50}); !errors.Is(err, ErrUnavailable) {
			t.Fatal("missing store did not fail closed")
		}
	}
}
