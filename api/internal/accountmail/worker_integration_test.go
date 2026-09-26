package accountmail

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/database"
)

func mailTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL required for isolated mail integration")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	hosts := []string{cfg.ConnConfig.Host}
	for _, fallback := range cfg.ConnConfig.Fallbacks {
		hosts = append(hosts, fallback.Host)
	}
	for _, host := range hosts {
		if host != "localhost" && !net.ParseIP(host).IsLoopback() {
			t.Fatal("loopback database required")
		}
	}
	ctx := context.Background()
	admin, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatal(err)
	}
	var random [10]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("account_mail_%x", random)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error("schema cleanup failed")
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
	if err = database.RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

type testSender struct {
	calls    int
	err      error
	messages []Message
}

func (s *testSender) Send(_ context.Context, m Message) error {
	s.calls++
	s.messages = append(s.messages, m)
	return s.err
}

type mailFixture struct {
	pool   *pgxpool.Pool
	worker *Worker
	sender *testSender
	now    time.Time
	id     int64
	token  []byte
}

var retryDelays = [...]time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 4 * time.Hour}

func newMailFixture(t *testing.T) *mailFixture {
	t.Helper()
	pool := mailTestPool(t)
	cipher, err := NewCipher("test", []byte(strings.Repeat("k", 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := &mailFixture{pool: pool, now: time.Now().UTC(), sender: &testSender{}}
	f.worker = &Worker{Pool: pool, Cipher: cipher, Sender: f.sender, Address: testAddress, Now: func() time.Time { return f.now }}
	return f
}
func (f *mailFixture) enqueue(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	f.token = make([]byte, 32)
	if _, err = rand.Read(f.token); err != nil {
		t.Fatal(err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO accounts(email,created_at,pending_kind) VALUES('person@example.com',$1,'email') RETURNING id`, f.now).Scan(&f.id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO account_tokens(token_digest,account_id,auth_revision,purpose,created_at,expires_at) VALUES($1,$2,1,'verification',$3,$4)`, f.token, f.id, f.now, f.now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	address, _ := testAddress("person@example.com")
	job := Job{EventDigest: f.token, TokenDigest: f.token, AddressDigest: address, AccountID: f.id, Revision: 1, Purpose: "verification", CreatedAt: f.now, ExpiresAt: f.now.Add(24 * time.Hour), Message: Message{Recipient: "person@example.com", Subject: "verification", Text: "https://example.com/verification#token=secret", HTML: "<p>safe</p>"}}
	if err = (&Outbox{Cipher: f.worker.Cipher}).Enqueue(ctx, tx, job); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
func (f *mailFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *mailFixture) terminal(t *testing.T, want string, attempts int) {
	t.Helper()
	var state string
	var count int
	var erased bool
	err := f.pool.QueryRow(context.Background(), `SELECT state,attempts,payload_ciphertext IS NULL AND payload_nonce IS NULL AND payload_key_id IS NULL AND lease_digest IS NULL AND account_id IS NULL AND token_digest IS NULL FROM account_mail_outbox`).Scan(&state, &count, &erased)
	if err != nil || state != want || count != attempts || !erased {
		t.Fatalf("terminal state=%s attempts=%d erased=%t err=%v", state, count, erased, err)
	}
}
func TestDispatchIntegration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*mailFixture, *testing.T)
		sent   bool
	}{
		{"accepted", nil, true},
		{"revoked", func(f *mailFixture, t *testing.T) { f.exec(t, `UPDATE accounts SET auth_revision=2`) }, false},
		{"consumed", func(f *mailFixture, t *testing.T) { f.exec(t, `UPDATE account_tokens SET consumed_at=$1`, f.now) }, false},
		{"suppressed", func(f *mailFixture, t *testing.T) {
			d, _ := testAddress("person@example.com")
			fb := testFeedback()
			fb.Pool = f.pool
			if err := fb.apply(context.Background(), []suppression{{digest: d, reason: "complaint"}}); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"expired", func(f *mailFixture, t *testing.T) { f.now = f.now.Add(24 * time.Hour) }, false},
		{"corrupt", func(f *mailFixture, t *testing.T) {
			f.exec(t, `UPDATE account_mail_outbox SET payload_ciphertext=decode(repeat('aa',32),'hex')`)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMailFixture(t)
			f.enqueue(t)
			if tc.mutate != nil {
				tc.mutate(f, t)
			}
			if _, err := f.worker.step(context.Background()); err != nil {
				t.Fatal(err)
			}
			if (f.sender.calls == 1) != tc.sent {
				t.Fatal("send recheck failed")
			}
			state := "failed"
			if tc.sent {
				state = "sent"
			}
			attempts := 1
			if tc.name == "expired" {
				attempts = 0
			}
			f.terminal(t, state, attempts)
		})
	}
}
func TestCrashRetryAndLeaseIntegration(t *testing.T) {
	f := newMailFixture(t)
	f.enqueue(t)
	ctx := context.Background()
	d, err := f.worker.claim(ctx)
	if err != nil || d.id == 0 {
		t.Fatal("claim failed")
	}
	// Simulate SES acceptance followed by process crash before durable finish.
	plain, err := f.worker.Cipher.Open(OutboxBinding(d.event, d.purpose), d.envelope)
	if err != nil {
		t.Fatal(err)
	}
	var m Message
	if json.Unmarshal(plain, &m) != nil {
		t.Fatal("payload")
	}
	if err = f.sender.Send(ctx, m); err != nil {
		t.Fatal(err)
	}
	if _, err = f.worker.step(ctx); err != nil || f.sender.calls != 1 {
		t.Fatal("live lease sent twice")
	}
	f.now = f.now.Add(time.Minute)
	if _, err = f.worker.step(ctx); err != nil {
		t.Fatal(err)
	}
	f.terminal(t, "sent", 2)
	if f.sender.calls != 2 || f.sender.messages[0] != f.sender.messages[1] {
		t.Fatal("retry minted different link")
	}
	if err = f.worker.finish(ctx, d, "failed"); err != nil {
		t.Fatal(err)
	}
	f.terminal(t, "sent", 2)
}
func TestRetryCeilingIntegration(t *testing.T) {
	f := newMailFixture(t)
	f.enqueue(t)
	f.sender.err = ErrTransient
	for i := 0; i < 6; i++ {
		if _, err := f.worker.step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if f.sender.calls != i+1 {
			t.Fatal("attempt not sent")
		}
		if i < 5 {
			if _, err := f.worker.step(context.Background()); err != nil || f.sender.calls != i+1 {
				t.Fatal("backoff ignored")
			}
			f.now = f.now.Add(retryDelays[i])
		}
	}
	f.terminal(t, "failed", 6)
	f.now = f.now.Add(time.Hour)
	if _, err := f.worker.step(context.Background()); err != nil || f.sender.calls != 6 {
		t.Fatal("retry ceiling bypass")
	}
}
func TestClaimCrashCeilingIntegration(t *testing.T) {
	f := newMailFixture(t)
	f.enqueue(t)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		d, err := f.worker.claim(ctx)
		if err != nil || d.id == 0 {
			t.Fatal("claim")
		}
		delay := time.Minute
		if i < 5 {
			delay = retryDelays[i]
		}
		f.now = f.now.Add(delay)
	}
	if _, err := f.worker.step(ctx); err != nil {
		t.Fatal(err)
	}
	f.terminal(t, "failed", 6)
	if f.sender.calls != 0 {
		t.Fatal("exhausted crashed claim sent")
	}
}
func TestConcurrentClaimAndRecheckIntegration(t *testing.T) {
	f := newMailFixture(t)
	f.enqueue(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	claims := make(chan delivery, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := f.worker.claim(ctx)
			if err != nil {
				t.Error(err)
			}
			claims <- d
		}()
	}
	wg.Wait()
	close(claims)
	var owned delivery
	count := 0
	for d := range claims {
		if d.id != 0 {
			count++
			owned = d
		}
	}
	if count != 1 {
		t.Fatal("duplicate claim")
	}
	f.exec(t, `UPDATE accounts SET auth_revision=2`)
	address, _ := testAddress("person@example.com")
	if ready, err := f.worker.ready(ctx, owned, address); err != nil || ready {
		t.Fatal("post-claim revocation ignored")
	}
}
func TestFeedbackSuppressionBeforeDeleteIntegration(t *testing.T) {
	f := newMailFixture(t)
	f.enqueue(t)
	fb := testFeedback()
	fb.Pool = f.pool
	q := validQueue()
	q.messages = []types.Message{{Body: aws.String(feedbackBody(t, func(e *feedbackEvent) { e.EventType = "Complaint"; e.Complaint.Recipients = e.Bounce.Recipients })), ReceiptHandle: aws.String("receipt")}}
	fb.Client = q
	q.deleteErr = ErrUnavailable
	if _, _, err := fb.poll(context.Background()); err == nil {
		t.Fatal("delete failure hidden")
	}
	var reason string
	if err := f.pool.QueryRow(context.Background(), `SELECT reason FROM account_mail_suppressions`).Scan(&reason); err != nil || reason != "complaint" {
		t.Fatal("suppression not committed first")
	}
	q.deleteErr = nil
	q.messages[0].Body = aws.String(feedbackBody(t, nil))
	if _, _, err := fb.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT reason FROM account_mail_suppressions`).Scan(&reason); err != nil || reason != "complaint" {
		t.Fatal("out-of-order bounce weakened complaint")
	}
	if _, err := f.worker.step(context.Background()); err != nil || f.sender.calls != 0 {
		t.Fatal("queued send bypassed suppression")
	}
}
func TestWorkerGracefulShutdownIntegration(t *testing.T) {
	f := newMailFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.worker.Run(ctx, slog.New(slog.NewTextHandler(t.Output(), nil))); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestSupersededAndDeletedMailIntegration(t *testing.T) {
	for _, sql := range []string{`DELETE FROM account_tokens`, `DELETE FROM accounts`} {
		f := newMailFixture(t)
		f.enqueue(t)
		ctx := context.Background()
		d, err := f.worker.claim(ctx)
		if err != nil || d.id == 0 {
			t.Fatal("claim failed")
		}
		f.exec(t, sql)
		address, _ := testAddress("person@example.com")
		if ready, err := f.worker.ready(ctx, d, address); err != nil || ready {
			t.Fatal("deleted authority was sendable")
		}
		var count int
		if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_mail_outbox`).Scan(&count); err != nil || count != 0 {
			t.Fatal("encrypted payload retained")
		}
	}
}

type timedSender struct{ sent chan time.Time }

func (s *timedSender) Send(ctx context.Context, _ Message) error {
	select {
	case s.sent <- time.Now():
		return nil
	case <-ctx.Done():
		return ErrTransient
	}
}

func TestWorkerLeaderAndRateIntegration(t *testing.T) {
	f := newMailFixture(t)
	f.enqueue(t)
	ctx := context.Background()
	// A second logical event, same valid authority, lets two contenders attempt work.
	f.exec(t, `INSERT INTO account_mail_outbox(event_digest,account_id,token_digest,auth_revision,purpose,payload_key_id,payload_nonce,payload_ciphertext,created_at,expires_at,next_attempt_at) SELECT decode(repeat('ff',32),'hex'),account_id,token_digest,auth_revision,purpose,payload_key_id,payload_nonce,payload_ciphertext,created_at,expires_at,next_attempt_at FROM account_mail_outbox`)
	// Re-encrypt with its own event binding instead of copying authenticated data.
	var original Envelope
	if err := f.pool.QueryRow(ctx, `SELECT payload_key_id,payload_nonce,payload_ciphertext FROM account_mail_outbox WHERE event_digest=$1`, f.token).Scan(&original.KeyID, &original.Nonce, &original.Ciphertext); err != nil {
		t.Fatal(err)
	}
	plain, err := f.worker.Cipher.Open(OutboxBinding(f.token, "verification"), original)
	if err != nil {
		t.Fatal(err)
	}
	event := make([]byte, 32)
	for i := range event {
		event[i] = 255
	}
	env, err := f.worker.Cipher.Seal(OutboxBinding(event, "verification"), plain)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE account_mail_outbox SET payload_nonce=$1,payload_ciphertext=$2 WHERE event_digest=$3`, env.Nonce, env.Ciphertext, event)
	sender := &timedSender{sent: make(chan time.Time, 2)}
	f.worker.Sender = sender
	other := *f.worker
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	logger := slog.New(slog.NewTextHandler(t.Output(), nil))
	for _, w := range []*Worker{f.worker, &other} {
		wg.Add(1)
		go func() { defer wg.Done(); w.Run(runCtx, logger) }()
	}
	var first, second time.Time
	select {
	case first = <-sender.sent:
	case <-time.After(5 * time.Second):
		cancel()
		wg.Wait()
		t.Fatal("first send missing")
	}
	select {
	case second = <-sender.sent:
	case <-time.After(5 * time.Second):
		cancel()
		wg.Wait()
		t.Fatal("second send missing")
	}
	cancel()
	wg.Wait()
	if second.Sub(first) < time.Second {
		t.Fatal("replicas exceeded one send per second")
	}
}
