package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/accountmail"
	"messeances/api/internal/database"
)

const testPassword = "a deliberate unique passphrase"
const changedPassword = "another deliberately unique passphrase"

// The injected test hasher tests lifecycle authority without spending Argon2
// capacity on each race fixture. Real Argon2 parameters have separate tests.
type lifecycleHasher struct {
	verified chan struct{}
	resume   chan struct{}
	dummy    atomic.Int32
}

func (*lifecycleHasher) Hash(_ context.Context, p string) (string, error) {
	sum := sha256.Sum256([]byte(p))
	return "$argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}
func (h *lifecycleHasher) Verify(ctx context.Context, p, hash string) (bool, bool, error) {
	value, _ := h.Hash(ctx, p)
	if h.verified != nil {
		close(h.verified)
		select {
		case <-h.resume:
		case <-ctx.Done():
			return false, false, ctx.Err()
		}
	}
	return value == hash, false, nil
}
func (h *lifecycleHasher) Dummy(context.Context, string) error { h.dummy.Add(1); return nil }

type lifecycleFixture struct {
	service *Service
	pool    *pgxpool.Pool
	cipher  *accountmail.Cipher
	clock   atomic.Int64
	hasher  *lifecycleHasher
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	return newLifecycleFixtureWithSchema(t, database.RunMigrations)
}

func newLifecycleFixtureWithSchema(t *testing.T, migrate func(context.Context, *pgxpool.Pool) error) *lifecycleFixture {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL not set; isolated account lifecycle unavailable")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	for _, host := range append([]string{cfg.ConnConfig.Host}, fallbackHosts(cfg)...) {
		if host != "localhost" && !net.ParseIP(host).IsLoopback() {
			t.Fatal("account tests require loopback database")
		}
	}
	ctx := context.Background()
	admin, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatal(err)
	}
	_, digest, err := NewToken(nil)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("account_lifecycle_%x", digest[:10])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error("isolated schema cleanup failed")
		}
	})
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var actual string
	if err = pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&actual); err != nil || actual != schema {
		t.Fatal("schema isolation failed")
	}
	if err = migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cipher, err := accountmail.NewCipher("test-key", []byte(strings.Repeat("k", 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &lifecycleFixture{pool: pool, cipher: cipher, hasher: &lifecycleHasher{}}
	f.clock.Store(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC).UnixNano())
	f.service, err = NewService(NewPostgresStore(pool), ServiceOptions{Now: f.now, Hasher: f.hasher, Mail: &accountmail.Outbox{Cipher: cipher}, Origin: "https://messeances.fr", AddressHMACKey: []byte(strings.Repeat("h", 32))})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func fallbackHosts(cfg *pgxpool.Config) []string {
	var hosts []string
	for _, fallback := range cfg.ConnConfig.Fallbacks {
		hosts = append(hosts, fallback.Host)
	}
	return hosts
}
func (f *lifecycleFixture) now() time.Time          { return time.Unix(0, f.clock.Load()).UTC() }
func (f *lifecycleFixture) advance(d time.Duration) { f.clock.Add(int64(d)) }
func (f *lifecycleFixture) token(t *testing.T, email string, purpose TokenPurpose) string {
	t.Helper()
	var event []byte
	var env accountmail.Envelope
	err := f.pool.QueryRow(context.Background(), `SELECT o.event_digest,o.payload_key_id,o.payload_nonce,o.payload_ciphertext FROM account_mail_outbox o JOIN accounts a ON a.id=o.account_id WHERE a.email=$1 AND o.purpose=$2 AND o.state='pending' ORDER BY o.id DESC LIMIT 1`, email, purpose).Scan(&event, &env.KeyID, &env.Nonce, &env.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := f.cipher.Open(accountmail.OutboxBinding(event, string(purpose)), env)
	if err != nil {
		t.Fatal(err)
	}
	var message accountmail.Message
	if err = json.Unmarshal(plain, &message); err != nil {
		t.Fatal(err)
	}
	_, raw, ok := strings.Cut(message.Text, "#token=")
	if !ok || strings.Contains(message.Text, "?token=") {
		t.Fatal("mail link must use fragment")
	}
	if _, err = TokenDigest(raw); err != nil {
		t.Fatal("invalid mail token")
	}
	return raw
}
func (f *lifecycleFixture) pending(t *testing.T, email string) SessionResult {
	t.Helper()
	ctx := context.Background()
	browser, err := f.service.Register(ctx, email, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.service.ConfirmVerification(ctx, f.token(t, email, TokenVerification), browser.Token, "")
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func (f *lifecycleFixture) complete(t *testing.T, email, name string) SessionResult {
	t.Helper()
	p := f.pending(t, email)
	result, err := f.service.Username(context.Background(), p.Cookie.Token, name)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func assertAnonymous(t *testing.T, s *Service, raw string) {
	t.Helper()
	view, err := s.Session(context.Background(), raw)
	if err != nil || view.State != StateAnonymous {
		t.Fatalf("expected anonymous, got %s: %v", view.State, err)
	}
}

func TestEmailLifecycleIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	email := "owner@example.com"
	attacker, err := f.service.Register(ctx, email, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	first := f.token(t, email, TokenVerification)
	pending, err := f.service.Login(ctx, email, testPassword, "")
	if err != nil || pending.View.State != StatePendingEmail {
		t.Fatalf("pending login: %v", err)
	}
	if _, err = f.service.Details(ctx, pending.Cookie.Token); !errors.Is(err, ErrPending) {
		t.Fatal("pending accessed settings")
	}
	f.advance(time.Minute)
	victim, err := f.service.Register(ctx, email, changedPassword)
	if err != nil {
		t.Fatal(err)
	}
	second := f.token(t, email, TokenVerification)
	if first == second {
		t.Fatal("resend retained token")
	}
	if _, err = f.service.ConfirmVerification(ctx, first, attacker.Token, pending.Cookie.Token); !errors.Is(err, ErrInvalidLink) {
		t.Fatal("superseded verification accepted")
	}
	if _, err = f.service.ConfirmVerification(ctx, second, attacker.Token, pending.Cookie.Token); !errors.Is(err, ErrVerificationBrowser) {
		t.Fatal("old attempt adopted victim password")
	}
	verified, err := f.service.ConfirmVerification(ctx, second, victim.Token, "")
	if err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, f.service, pending.Cookie.Token)
	if _, err = f.service.Login(ctx, email, testPassword, ""); !errors.Is(err, ErrCredentials) {
		t.Fatal("tentative attacker password activated")
	}
	if _, err = f.service.ConfirmVerification(ctx, second, victim.Token, ""); !errors.Is(err, ErrInvalidLink) {
		t.Fatal("verification replay accepted")
	}
	if _, err = f.service.Username(ctx, verified.Cookie.Token, "Admin"); !errors.Is(err, ErrUsernameUnavailable) {
		t.Fatalf("reserved username: %v", err)
	}
	complete, err := f.service.Username(ctx, verified.Cookie.Token, "Alice_123")
	if err != nil || complete.View.State != StateComplete || *complete.View.Account.Username != "alice_123" {
		t.Fatalf("completion: %v", err)
	}
	assertAnonymous(t, f.service, verified.Cookie.Token)
	if _, err = f.service.Username(ctx, complete.Cookie.Token, "alice_other"); !errors.Is(err, ErrPending) {
		t.Fatal("username rename accepted")
	}
	other, err := f.service.Login(ctx, email, changedPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Logout(ctx, complete.Cookie.Token, true); err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, f.service, other.Cookie.Token)
	assertAnonymous(t, f.service, complete.Cookie.Token)
}

func TestAccountDeadlineIntegration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		deadline time.Duration
		complete bool
		touch    bool
	}{{"pending_exact", PendingLifetime, false, false}, {"idle_exact", SessionIdleLifetime, true, false}, {"absolute_exact", SessionLifetime, true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLifecycleFixture(t)
			ctx := context.Background()
			var session SessionResult
			if tc.complete {
				session = f.complete(t, "deadline@example.com", "deadline_user")
			} else {
				session = f.pending(t, "deadline@example.com")
			}
			if tc.touch {
				for i := 0; i < 5; i++ {
					f.advance(6*24*time.Hour - time.Second)
					if _, err := f.service.Session(ctx, session.Cookie.Token); err != nil {
						t.Fatal(err)
					}
				}
				f.advance(4 * time.Second)
			} else {
				f.advance(tc.deadline - time.Second)
			}
			if tc.name == "idle_exact" {
				// Session() is itself activity. Do not refresh last_seen_at one
				// second before asserting the original idle deadline.
				f.advance(time.Second)
				assertAnonymous(t, f.service, session.Cookie.Token)
				return
			}
			view, err := f.service.Session(ctx, session.Cookie.Token)
			if err != nil || view.State == StateAnonymous {
				t.Fatal("expired before deadline")
			}
			f.advance(time.Second)
			assertAnonymous(t, f.service, session.Cookie.Token)
			if !tc.complete {
				if _, err = f.service.Login(ctx, "deadline@example.com", testPassword, ""); !errors.Is(err, ErrCredentials) {
					t.Fatal("expired pending login accepted")
				}
				if f.hasher.dummy.Load() != 1 {
					t.Fatal("expired pending did not dummy hash")
				}
				if _, err = f.service.Register(ctx, "deadline@example.com", changedPassword); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestVerificationAndUsernameRaceIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	browser, err := f.service.Register(ctx, "race@example.com", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	token := f.token(t, "race@example.com", TokenVerification)
	results := make(chan SessionResult, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			result, err := f.service.ConfirmVerification(ctx, token, browser.Token, "")
			results <- result
			failures <- err
		}()
	}
	wins := 0
	for i := 0; i < 2; i++ {
		<-results
		err := <-failures
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrInvalidLink) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("verification race winners", wins)
	}
	a := f.pending(t, "a@example.com")
	b := f.pending(t, "b@example.com")
	for _, raw := range []string{a.Cookie.Token, b.Cookie.Token} {
		go func() { _, err := f.service.Username(ctx, raw, "one_claim"); failures <- err }()
	}
	wins = 0
	for i := 0; i < 2; i++ {
		err := <-failures
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrUsernameUnavailable) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("username race winners", wins)
	}
}

func TestPasswordRecoveryAndStepUpIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	a := f.complete(t, "secure@example.com", "secure_user")
	other, err := f.service.Login(ctx, "secure@example.com", testPassword, "")
	if err != nil {
		t.Fatal(err)
	}
	proof, err := f.service.ReauthPassword(ctx, a.Cookie.Token, testPassword, ActionPasswordChange, "")
	if err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, f.service, a.Cookie.Token)
	if err = f.service.RequestEmailChange(ctx, proof.Cookie.Token, "new@example.com", proof.Grant); !errors.Is(err, ErrRecentAuth) {
		t.Fatal("wrong action grant accepted")
	}
	changed, err := f.service.ChangePassword(ctx, proof.Cookie.Token, changedPassword, proof.Grant)
	if err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, f.service, other.Cookie.Token)
	assertAnonymous(t, f.service, proof.Cookie.Token)
	if _, err = f.service.ChangePassword(ctx, changed.Token, testPassword, proof.Grant); !errors.Is(err, ErrRecentAuth) {
		t.Fatal("grant replay accepted")
	}
	if err = f.service.RequestReset(ctx, "secure@example.com"); err != nil {
		t.Fatal(err)
	}
	token := f.token(t, "secure@example.com", TokenPasswordReset)
	if _, err = f.service.ConfirmVerification(ctx, token, "", ""); !errors.Is(err, ErrInvalidLink) {
		t.Fatal("wrong purpose accepted")
	}
	errorsCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errorsCh <- f.service.ConfirmReset(ctx, token, testPassword) }()
	}
	wins := 0
	for i := 0; i < 2; i++ {
		err = <-errorsCh
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrInvalidLink) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("reset race winners", wins)
	}
	assertAnonymous(t, f.service, changed.Token)
	if _, err = f.service.Login(ctx, "secure@example.com", testPassword, ""); err != nil {
		t.Fatal(err)
	}
}

func TestLoginVersusRevocationIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	a := f.complete(t, "raceauth@example.com", "raceauth_user")
	f.hasher.verified = make(chan struct{})
	f.hasher.resume = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := f.service.Login(ctx, "raceauth@example.com", testPassword, ""); done <- err }()
	<-f.hasher.verified
	if err := f.service.Logout(ctx, a.Cookie.Token, true); err != nil {
		t.Fatal(err)
	}
	close(f.hasher.resume)
	if err := <-done; !errors.Is(err, ErrCredentials) {
		t.Fatalf("racing login survived revocation: %v", err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("surviving session")
	}
}

func TestEmailChangeIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	a := f.complete(t, "old@example.com", "email_user")
	proof, err := f.service.ReauthPassword(ctx, a.Cookie.Token, testPassword, ActionEmailChange, "NEW@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.RequestEmailChange(ctx, proof.Cookie.Token, "different@example.com", proof.Grant); !errors.Is(err, ErrRecentAuth) {
		t.Fatal("wrong target accepted")
	}
	f.advance(time.Minute)
	if err = f.service.RequestEmailChange(ctx, proof.Cookie.Token, "new@example.com", proof.Grant); err != nil {
		t.Fatal(err)
	}
	token := f.token(t, "old@example.com", TokenEmailChange)
	details, err := f.service.Details(ctx, proof.Cookie.Token)
	if err != nil || details.PendingEmail == nil || *details.PendingEmail != "new@example.com" || details.Email != "old@example.com" {
		t.Fatal("pending target did not preserve old email")
	}
	proof, err = f.service.ReauthPassword(ctx, proof.Cookie.Token, testPassword, ActionEmailChange, "new@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.ConfirmEmailChange(ctx, proof.Cookie.Token, token, proof.Grant); err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, f.service, proof.Cookie.Token)
	if _, err = f.service.Login(ctx, "old@example.com", testPassword, ""); !errors.Is(err, ErrCredentials) {
		t.Fatal("old login address survives")
	}
	if _, err = f.service.Login(ctx, "new@example.com", testPassword, ""); err != nil {
		t.Fatal(err)
	}
}

func TestEmailTargetConflictRaceIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	emails := []string{"first@example.com", "second@example.com"}
	proofs := make([]GrantResult, 2)
	tokens := make([]string, 2)
	for i, email := range emails {
		a := f.complete(t, email, fmt.Sprintf("target_user_%d", i))
		proof, err := f.service.ReauthPassword(ctx, a.Cookie.Token, testPassword, ActionEmailChange, "target@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if err = f.service.RequestEmailChange(ctx, proof.Cookie.Token, "target@example.com", proof.Grant); err != nil {
			t.Fatal(err)
		}
		tokens[i] = f.token(t, email, TokenEmailChange)
		proofs[i], err = f.service.ReauthPassword(ctx, proof.Cookie.Token, testPassword, ActionEmailChange, "target@example.com")
		if err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 2)
	for i := range proofs {
		go func() { done <- f.service.ConfirmEmailChange(ctx, proofs[i].Cookie.Token, tokens[i], proofs[i].Grant) }()
	}
	wins := 0
	for i := 0; i < 2; i++ {
		err := <-done
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrEmailUnavailable) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("email conflict winners", wins)
	}
}

func TestDurableQuotaAndSuppressionIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	if err := f.service.RequestReset(ctx, "absent@example.com"); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(NewPostgresStore(f.pool), ServiceOptions{Now: f.now, Hasher: f.hasher, Mail: &accountmail.Outbox{Cipher: f.cipher}, Origin: "https://messeances.fr", AddressHMACKey: []byte(strings.Repeat("h", 32))})
	if err != nil {
		t.Fatal(err)
	}
	var limit *RateLimitError
	if err = restarted.RequestReset(ctx, "absent@example.com"); !errors.As(err, &limit) || limit.RetryAfter < 60 {
		t.Fatal("restart lost cooldown")
	}
	address, err := AddressKey([]byte(strings.Repeat("h", 32)), "suppression", "suppressed@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO account_mail_suppressions VALUES($1,'complaint',$2,$2,$3)`, address[:], f.now(), f.now().Add(180*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Register(ctx, "suppressed@example.com", testPassword); err != nil {
		t.Fatal("suppressed signup must be accepted", err)
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_mail_outbox`).Scan(&count); err != nil || count != 0 {
		t.Fatal("suppressed mail enqueued")
	}
}

type failingAccountMail struct{}

func (failingAccountMail) Enqueue(context.Context, pgx.Tx, accountmail.Job) error {
	return accountmail.ErrUnavailable
}

type fullAccountMail struct{}

func (fullAccountMail) Enqueue(context.Context, pgx.Tx, accountmail.Job) error {
	return accountmail.ErrQueueFull
}

func TestMailCapacityAdmissionIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	// Simulate capacity becoming full only after prelookup admission. The
	// public response stays generic, and the whole registration is rolled back.
	f.service.mail = fullAccountMail{}
	if _, err := f.service.Register(ctx, "capacityrace@example.com", testPassword); err != nil {
		t.Fatal("capacity race exposed eligibility", err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("capacity race committed partial registration")
	}
	if err := f.service.RequestReset(ctx, "absentcapacity@example.com"); err != nil {
		t.Fatal(err)
	}
	f.service.mail = &accountmail.Outbox{Cipher: f.cipher}
	f.complete(t, "existingcapacity@example.com", "capacity_user")
	if _, err := f.pool.Exec(ctx, `INSERT INTO account_mail_outbox(event_digest,purpose,payload_key_id,payload_nonce,payload_ciphertext,created_at,expires_at,next_attempt_at) SELECT decode(lpad(to_hex(n),64,'0'),'hex'),'security_notification','test-key',decode(repeat('11',12),'hex'),decode(repeat('22',17),'hex'),$1::timestamptz,$1::timestamptz+interval '1 hour',$1::timestamptz FROM generate_series(1,10000) AS n`, f.now()); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"existingcapacity@example.com", "absentfull@example.com"} {
		if err := f.service.RequestReset(ctx, email); !errors.Is(err, ErrUnavailable) {
			t.Fatal("full queue did not uniformly reject prelookup", err)
		}
	}
	var id, revision int64
	if err := f.pool.QueryRow(ctx, `SELECT id,auth_revision FROM accounts WHERE email='existingcapacity@example.com'`).Scan(&id, &revision); err != nil {
		t.Fatal(err)
	}
	_, digest, err := NewToken(nil)
	if err != nil {
		t.Fatal(err)
	}
	address, err := AddressKey(f.service.hmacKey, "suppression", "existingcapacity@example.com")
	if err != nil {
		t.Fatal(err)
	}
	err = f.service.store.withTransaction(ctx, func(tx pgx.Tx) error {
		return (&accountmail.Outbox{Cipher: f.cipher}).Enqueue(ctx, tx, accountmail.Job{EventDigest: digest[:], AccountID: id, Revision: revision, Purpose: "security_notification", AddressDigest: address[:], CreatedAt: f.now(), ExpiresAt: f.now().Add(time.Hour), Message: accountmail.Message{Recipient: "existingcapacity@example.com", Text: "test"}})
	})
	if !errors.Is(err, accountmail.ErrQueueFull) {
		t.Fatal("outbox final admission failed", err)
	}
}

func TestTokenDeadlinesAndRollbackIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	browser, err := f.service.Register(ctx, "expiredlink@example.com", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	token := f.token(t, "expiredlink@example.com", TokenVerification)
	f.advance(VerificationLifetime)
	if _, err := f.service.ConfirmVerification(ctx, token, browser.Token, ""); !errors.Is(err, ErrInvalidLink) {
		t.Fatal("verification accepted at exact deadline")
	}
	a := f.complete(t, "boundaries@example.com", "boundary_user")
	proof, err := f.service.ReauthPassword(ctx, a.Cookie.Token, testPassword, ActionPasswordChange, "")
	if err != nil {
		t.Fatal(err)
	}
	f.advance(GrantLifetime)
	if _, err = f.service.ChangePassword(ctx, proof.Cookie.Token, changedPassword, proof.Grant); !errors.Is(err, ErrRecentAuth) {
		t.Fatal("grant accepted at exact deadline")
	}
	proof, err = f.service.ReauthPassword(ctx, proof.Cookie.Token, testPassword, ActionPasswordChange, "")
	if err != nil {
		t.Fatal(err)
	}
	f.service.mail = failingAccountMail{}
	if _, err = f.service.ChangePassword(ctx, proof.Cookie.Token, changedPassword, proof.Grant); !errors.Is(err, ErrUnavailable) {
		t.Fatal("failed mail enqueue did not roll back")
	}
	view, err := f.service.Session(ctx, proof.Cookie.Token)
	if err != nil || view.State != StateComplete {
		t.Fatal("rollback lost original session")
	}
	f.service.mail = &accountmail.Outbox{Cipher: f.cipher}
	changed, err := f.service.ChangePassword(ctx, proof.Cookie.Token, changedPassword, proof.Grant)
	if err != nil {
		t.Fatal("rollback consumed grant", err)
	}
	if err = f.service.RequestReset(ctx, "boundaries@example.com"); err != nil {
		t.Fatal(err)
	}
	reset := f.token(t, "boundaries@example.com", TokenPasswordReset)
	f.advance(RecoveryLifetime)
	if err = f.service.ConfirmReset(ctx, reset, testPassword); !errors.Is(err, ErrInvalidLink) {
		t.Fatal("reset accepted at exact deadline")
	}
	view, err = f.service.Session(ctx, changed.Token)
	if err != nil || view.State != StateComplete {
		t.Fatal("expired reset revoked session")
	}
}

func TestGoogleOnlyRecoveryRefusesPasswordIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	a := f.complete(t, "googleonly@example.com", "googleonly_user")
	if _, err := f.pool.Exec(ctx, `INSERT INTO account_google_identities(account_id,issuer,subject,observed_email,observed_email_verified) SELECT id,'https://accounts.google.com','test-subject','provider@example.com',true FROM accounts WHERE email='googleonly@example.com'; DELETE FROM account_passwords`); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RequestReset(ctx, "googleonly@example.com"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_tokens WHERE purpose='password_reset'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("Google-only reset minted authority")
	}
	if _, err := f.service.Login(ctx, "googleonly@example.com", testPassword, ""); !errors.Is(err, ErrCredentials) {
		t.Fatal("Google-only password login accepted")
	}
	if _, err := f.service.ReauthPassword(ctx, a.Cookie.Token, testPassword, ActionPasswordAdd, ""); !errors.Is(err, ErrRecentAuth) {
		t.Fatal("Google-only password step-up accepted")
	}
	if f.hasher.dummy.Load() != 2 {
		t.Fatal("wrong-method dummy hashing missing")
	}
	details, err := f.service.Details(ctx, a.Cookie.Token)
	if err != nil || details.GoogleEmail == nil || *details.GoogleEmail != "provider@example.com" || details.Email != "googleonly@example.com" {
		t.Fatal("Google identity email confused with account email")
	}
}

func TestResetRacingLoginIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	f.complete(t, "resetlogin@example.com", "resetlogin_user")
	if err := f.service.RequestReset(ctx, "resetlogin@example.com"); err != nil {
		t.Fatal(err)
	}
	reset := f.token(t, "resetlogin@example.com", TokenPasswordReset)
	f.hasher.verified = make(chan struct{})
	f.hasher.resume = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := f.service.Login(ctx, "resetlogin@example.com", testPassword, ""); done <- err }()
	<-f.hasher.verified
	if err := f.service.ConfirmReset(ctx, reset, changedPassword); err != nil {
		t.Fatal(err)
	}
	close(f.hasher.resume)
	if err := <-done; !errors.Is(err, ErrCredentials) {
		t.Fatal("racing login survived reset", err)
	}
}

func TestEmailCancelAndBrowserAccountSwitchIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	a := f.complete(t, "cancel@example.com", "cancel_user")
	f.complete(t, "switch@example.com", "switch_user")
	proof, err := f.service.ReauthPassword(ctx, a.Cookie.Token, testPassword, ActionEmailChange, "canceled@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.RequestEmailChange(ctx, proof.Cookie.Token, "canceled@example.com", proof.Grant); err != nil {
		t.Fatal(err)
	}
	token := f.token(t, "cancel@example.com", TokenEmailChange)
	if err = f.service.CancelEmailChange(ctx, proof.Cookie.Token); err != nil {
		t.Fatal(err)
	}
	proof, err = f.service.ReauthPassword(ctx, proof.Cookie.Token, testPassword, ActionEmailChange, "canceled@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.ConfirmEmailChange(ctx, proof.Cookie.Token, token, proof.Grant); !errors.Is(err, ErrInvalidLink) {
		t.Fatal("canceled email token accepted")
	}
	if _, err = f.service.Login(ctx, "switch@example.com", testPassword, proof.Cookie.Token); err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, f.service, proof.Cookie.Token)
}
