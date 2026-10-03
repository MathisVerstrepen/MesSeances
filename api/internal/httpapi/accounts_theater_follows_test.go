package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"messeances/api/internal/accounts"
)

const theaterFollowPath = "/api/v1/account/theater-follows"
const validTheaterFollow = `{"expected_username":"owner","expected_revision":"0","theater_id":"ugc-1","followed":"true"}`

func TestTheaterFollowStrictInput(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"valid", validTheaterFollow, 503},
		{"unfollow", strings.Replace(validTheaterFollow, `"true"`, `"false"`, 1), 503},
		{"max revision", strings.Replace(validTheaterFollow, `"0"`, `"9007199254740991"`, 1), 503},
		{"missing all", `{}`, 400},
		{"empty id", strings.Replace(validTheaterFollow, "ugc-1", "", 1), 400},
		{"invalid id", strings.Replace(validTheaterFollow, "ugc-1", "ugc/1", 1), 400},
		{"oversized id", strings.Replace(validTheaterFollow, "ugc-1", strings.Repeat("a", 129), 1), 400},
		{"empty revision", strings.Replace(validTheaterFollow, `"0"`, `""`, 1), 400},
		{"revision padding", strings.Replace(validTheaterFollow, `"0"`, `"00"`, 1), 400},
		{"revision overflow", strings.Replace(validTheaterFollow, `"0"`, `"9007199254740992"`, 1), 400},
		{"numeric revision", strings.Replace(validTheaterFollow, `"0"`, `0`, 1), 400},
		{"boolean", strings.Replace(validTheaterFollow, `"true"`, `true`, 1), 400},
		{"number", strings.Replace(validTheaterFollow, `"true"`, `1`, 1), 400},
		{"null", strings.Replace(validTheaterFollow, `"true"`, `null`, 1), 400},
		{"array", strings.Replace(validTheaterFollow, `"true"`, `[]`, 1), 400},
		{"object", strings.Replace(validTheaterFollow, `"true"`, `{}`, 1), 400},
		{"boolean casing", strings.Replace(validTheaterFollow, `"true"`, `"True"`, 1), 400},
		{"boolean whitespace", strings.Replace(validTheaterFollow, `"true"`, `"true "`, 1), 400},
		{"missing boolean", strings.Replace(validTheaterFollow, `,"followed":"true"`, "", 1), 400},
		{"missing username", strings.Replace(validTheaterFollow, `"expected_username":"owner",`, "", 1), 400},
		{"missing revision", strings.Replace(validTheaterFollow, `"expected_revision":"0",`, "", 1), 400},
		{"missing id", strings.Replace(validTheaterFollow, `"theater_id":"ugc-1",`, "", 1), 400},
		{"case key", strings.Replace(validTheaterFollow, "followed", "Followed", 1), 400},
		{"duplicate", strings.TrimSuffix(validTheaterFollow, "}") + `,"followed":"false"}`, 400},
		{"escaped duplicate", strings.TrimSuffix(validTheaterFollow, "}") + `,"\u0066ollowed":"false"}`, 400},
		{"extra", strings.TrimSuffix(validTheaterFollow, "}") + `,"extra":"x"}`, 400},
		{"trailing", validTheaterFollow + ` {}`, 400},
		{"invalid utf8", validTheaterFollow + "\xff", 400},
		{"surrogate", strings.Replace(validTheaterFollow, "ugc-1", `\ud800`, 1), 400},
		{"body bound", validTheaterFollow + strings.Repeat(" ", 8192), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, "POST", theaterFollowPath, tc.body))
			if w.Code != tc.status {
				t.Fatalf("status %d want %d", w.Code, tc.status)
			}
			assertAccountHeaders(t, w)
		})
	}
}

func TestTheaterFollowSecurityBoundary(t *testing.T) {
	for _, tc := range []struct {
		header, value string
		duplicate     bool
		status        int
	}{
		{"Origin", "", false, 403}, {"Origin", "https://evil.example", false, 403}, {"Origin", "https://messeances.fr", true, 403},
		{"X-Messeances-CSRF", "", false, 403}, {"X-Messeances-CSRF", "1", true, 403},
		{"Sec-Fetch-Site", "cross-site", false, 403}, {"Sec-Fetch-Site", "same-site", false, 403}, {"Sec-Fetch-Site", "same-origin", true, 403},
		{"Content-Type", "application/x-www-form-urlencoded", false, 400},
	} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
		r := theaterPreferenceRequest(t, "POST", theaterFollowPath, validTheaterFollow)
		if tc.duplicate {
			r.Header.Add(tc.header, tc.value)
		} else {
			r.Header.Set(tc.header, tc.value)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s status %d", tc.header, w.Code)
		}
		assertAccountHeaders(t, w)
	}
	for _, method := range []string{"GET", "POST"} {
		for _, suffix := range []string{"?", "?owner=other", "?limit=20"} {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, method, theaterFollowPath+suffix, validTheaterFollow))
			if w.Code != 400 {
				t.Fatal("follow query accepted")
			}
			assertAccountHeaders(t, w)
		}
		for _, cookie := range []string{"bad", strings.Repeat("A", 43) + "; " + accountCookieName + "=" + strings.Repeat("A", 43)} {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			r := theaterPreferenceRequest(t, method, theaterFollowPath, validTheaterFollow)
			r.Header.Set("Cookie", accountCookieName+"="+cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatal("invalid/duplicate cookie accepted")
			}
			assertAccountHeaders(t, w)
		}
	}
}

func TestTheaterFollowUnavailableRoutes(t *testing.T) {
	for _, options := range []AccountOptions{{}, {Enabled: true}, {Enabled: true, Service: lifecycleHTTPOptions(t).Service, Origin: "http://evil.example"}, lifecycleHTTPOptions(t)} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: options})
		for _, method := range []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, theaterPreferenceRequest(t, method, theaterFollowPath, validTheaterFollow))
			want := 503
			if method != "GET" && method != "POST" {
				want = 405
			}
			if w.Code != want {
				t.Fatalf("%s status %d want %d", method, w.Code, want)
			}
			assertAccountHeaders(t, w)
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("private route exposed CORS")
			}
		}
	}
}

func TestTheaterFollowLimiter(t *testing.T) {
	h, err := newAccountHTTP(lifecycleHTTPOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h.theaters.now = func() time.Time { return now }
	for range 120 {
		if ok, _ := h.theaters.allow(unknownClientKey); !ok {
			t.Fatal("write capacity")
		}
	}
	w := httptest.NewRecorder()
	accountBoundary(h.mutation(h.saveTheaterFollow, h.theaters)).ServeHTTP(w, theaterPreferenceRequest(t, "POST", theaterFollowPath, validTheaterFollow))
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("follow limiter not shared")
	}
	assertAccountHeaders(t, w)
	w = httptest.NewRecorder()
	accountBoundary(h.mutation(h.saveTheaterPreferences, h.theaters)).ServeHTTP(w, theaterPreferenceRequest(t, "POST", theaterPreferencePath, emptyTheaterSelection))
	if w.Code != 429 {
		t.Fatal("selection uses different write bucket")
	}
	for _, limiter := range []*tokenBucketLimiter{h.login, h.send, h.step, h.activityReads} {
		if ok, _ := limiter.allow(unknownClientKey); !ok {
			t.Fatal("writes depleted unrelated bucket")
		}
	}
	now = now.Add(500 * time.Millisecond)
	if ok, _ := h.theaters.allow(unknownClientKey); !ok {
		t.Fatal("write refill")
	}
}

// A bounded, database-free authorization fixture proves registered routes use
// only account cookies and serialize committed snapshots without integration setup.
type followHTTPDatabase struct {
	now     time.Time
	pending bool
}

func (db followHTTPDatabase) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return &followHTTPTx{db: db}, nil
}

type followHTTPTx struct {
	pgx.Tx
	db followHTTPDatabase
}

func (*followHTTPTx) Commit(context.Context) error   { return nil }
func (*followHTTPTx) Rollback(context.Context) error { return nil }
func (*followHTTPTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type followHTTPRow struct {
	values []any
	err    error
}

func (row followHTTPRow) Scan(dst ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dst) != len(row.values) {
		return errors.New("fixture scan arity")
	}
	for i, value := range row.values {
		d := reflect.ValueOf(dst[i]).Elem()
		if value == nil {
			d.SetZero()
		} else {
			d.Set(reflect.ValueOf(value))
		}
	}
	return nil
}
func (tx *followHTTPTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	now := tx.db.now
	username, hash := "owner", "hash"
	scope := accounts.StateComplete
	verified := &now
	if tx.db.pending {
		verified = nil
		scope = accounts.StatePendingEmail
	}
	switch {
	case strings.HasPrefix(sql, "SELECT account_id"):
		return followHTTPRow{values: []any{int64(1)}}
	case strings.HasPrefix(sql, "SELECT id,email"):
		return followHTTPRow{values: []any{int64(1), "owner@example.com", verified, now.Add(-time.Hour), nil, int64(1), nil, nil, int64(0)}}
	case strings.HasPrefix(sql, "SELECT u.username"):
		return followHTTPRow{values: []any{&username, &hash, false, nil, false, nil}}
	case strings.HasPrefix(sql, "SELECT auth_revision"):
		return followHTTPRow{values: []any{int64(1), now, now.Add(time.Hour), now, scope}}
	case strings.Contains(sql, "FROM account_theater_follows"):
		return followHTTPRow{err: pgx.ErrNoRows}
	case sql == "SELECT transaction_timestamp()":
		return followHTTPRow{values: []any{now}}
	default:
		return followHTTPRow{err: errors.New("unexpected fixture query")}
	}
}
func followHTTPOptions(t *testing.T, pending bool) AccountOptions {
	t.Helper()
	now := time.Date(2026, 10, 3, 12, 0, 0, 123456000, time.UTC)
	s, err := accounts.NewService(accounts.NewPostgresStore(followHTTPDatabase{now: now, pending: pending}), accounts.ServiceOptions{Now: func() time.Time { return now }, Hasher: unavailableAccountHasher{}, Origin: "https://messeances.fr", AddressHMACKey: []byte(strings.Repeat("h", 32))})
	if err != nil {
		t.Fatal(err)
	}
	return AccountOptions{Enabled: true, Service: s, Origin: "https://messeances.fr"}
}

func TestTheaterFollowAccountCookieAndSnapshot(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		for _, cookie := range []string{"", "messeances_admin=anything", "internal_account=anything", "other=" + strings.Repeat("A", 43)} {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, false)})
			r := theaterPreferenceRequest(t, method, theaterFollowPath, validTheaterFollow)
			r.Header.Set("Cookie", cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatal("nonaccount identity authorized", w.Code)
			}
			assertAccountHeaders(t, w)
		}
		for _, pending := range []bool{false, true} {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, pending)})
			r := theaterPreferenceRequest(t, method, theaterFollowPath, strings.Replace(validTheaterFollow, `"true"`, `"false"`, 1))
			r.Header.Set("Cookie", accountCookieName+"="+strings.Repeat("A", 43))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			assertAccountHeaders(t, w)
			if pending {
				if w.Code != 403 || !strings.Contains(w.Body.String(), "onboarding_required") {
					t.Fatal("pending authorized")
				}
				continue
			}
			if w.Code != 200 || w.Body.String() != "{\"username\":\"owner\",\"revision\":\"0\",\"theater_ids\":[]}\n" {
				t.Fatal("snapshot serialization", w.Code, w.Body.String())
			}
		}
	}
	// Both mutation fences precede no-op serialization.
	for _, tc := range []struct {
		body   string
		status int
		code   string
	}{
		{strings.Replace(validTheaterFollow, "owner", "foreign", 1), 401, "authentication_required"},
		{strings.Replace(validTheaterFollow, `"0"`, `"1"`, 1), 409, "theater_follows_changed"},
	} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: followHTTPOptions(t, false)})
		r := theaterPreferenceRequest(t, "POST", theaterFollowPath, tc.body)
		r.Header.Set("Cookie", accountCookieName+"="+strings.Repeat("A", 43))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
			t.Fatal("fence contract")
		}
	}
}

func TestTheaterFollowErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err           error
		status        int
		code, message string
	}{
		{accounts.ErrInvalidInput, 400, "invalid_input", "Requête invalide."},
		{accounts.ErrUnauthorized, 401, "authentication_required", "Connectez-vous pour continuer."},
		{accounts.ErrPending, 403, "onboarding_required", "Terminez votre inscription."},
		{accounts.ErrTheaterFollowsChanged, 409, "theater_follows_changed", "Vos cinémas suivis ont changé. Vérifiez leur état avant de recommencer."},
		{accounts.ErrTheaterFollowLimit, 409, "theater_follow_limit_reached", "Vous suivez déjà le nombre maximal de cinémas. Arrêtez de suivre un cinéma avant de réessayer."},
		{&accounts.RateLimitError{RetryAfter: 3}, 429, "rate_limited", "Trop de requêtes. Réessayez plus tard."},
		{accounts.ErrUnavailable, 503, "accounts_unavailable", ""},
		{errors.New("secret SQL details"), 503, "accounts_unavailable", ""},
	} {
		w := httptest.NewRecorder()
		accountBoundary(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { accountError(w, tc.err) })).ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", theaterFollowPath, nil))
		var envelope struct {
			Error struct{ Code, Message string }
		}
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || w.Code != tc.status || envelope.Error.Code != tc.code || (tc.message != "" && envelope.Error.Message != tc.message) {
			t.Fatal("error mapping", w.Body.String())
		}
		if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "theater_ids") || strings.Contains(w.Body.String(), "username") {
			t.Fatal("unsafe error")
		}
		assertAccountHeaders(t, w)
		if tc.status == 429 && w.Header().Get("Retry-After") != "3" {
			t.Fatal("retry header")
		}
	}
}
