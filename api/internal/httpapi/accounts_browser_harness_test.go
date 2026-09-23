package httpapi

// This entire fixture is test-only. It never loads deploy/.env, constructs a
// provider client, sends mail, or exposes a production fake-provider switch.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/accountmail"
	"messeances/api/internal/accounts"
	"messeances/api/internal/database"
	"messeances/api/internal/schedule"
)

const browserHarnessOrigin = "http://127.0.0.1:13009"
const browserHarnessAdminPassword = "browser-only synthetic admin password"

type browserHarness struct {
	pool     *pgxpool.Pool
	cipher   *accountmail.Cipher
	google   *browserGoogle
	origin   string
	apiURL   string
	handler  http.Handler
	stop     chan struct{}
	stopOnce sync.Once
}

// TestAccountBrowserHarness is a deliberately blocking opt-in test server.
// Run only this exact test with -timeout=0. The shutdown endpoint or SIGINT/
// SIGTERM drains HTTP requests before closing the pool and dropping its schema.
func TestAccountBrowserHarness(t *testing.T) {
	if os.Getenv("ACCOUNT_BROWSER_HARNESS") != "1" {
		t.Skip("explicit ACCOUNT_BROWSER_HARNESS=1 required")
	}
	origin := os.Getenv("ACCOUNT_BROWSER_ORIGIN")
	if origin == "" {
		origin = browserHarnessOrigin
	}
	port := os.Getenv("ACCOUNT_BROWSER_PORT")
	if port == "" {
		port = "18089"
	}
	if !browserHarnessPort(port) {
		t.Fatal("browser harness requires a valid loopback port")
	}
	h := newBrowserHarness(t, origin)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	h.serve(t, port)
	t.Logf("ACCOUNT_BROWSER_HARNESS_READY api=%s origin=%s", h.apiURL, h.origin)
	select {
	case <-ctx.Done():
	case <-h.stop:
	}
}

func browserHarnessPort(raw string) bool {
	n, err := strconv.Atoi(raw)
	return err == nil && n > 0 && n <= 65535 && strconv.Itoa(n) == raw
}

func browserHarnessValidOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "http" && u.Hostname() == "127.0.0.1" && browserHarnessPort(u.Port()) && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.String() == raw
}

func browserHarnessDatabaseConfig(raw string) (*pgxpool.Config, error) {
	// Do not inherit PG* defaults or accept a DSN for any other database, even
	// another loopback database. Only the explicitly approved disposable target.
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "postgres" || u.Host != "127.0.0.1:55439" || u.Path != "/accountstest" || u.User == nil || u.User.Username() != "accountstest" || u.RawQuery != "sslmode=disable" || u.Fragment != "" {
		return nil, errors.New("browser harness requires approved disposable database")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55439 || cfg.ConnConfig.Database != "accountstest" || cfg.ConnConfig.User != "accountstest" || len(cfg.ConnConfig.Fallbacks) != 0 {
		return nil, errors.New("invalid browser harness database configuration")
	}
	cfg.ConnConfig.RuntimeParams = map[string]string{"application_name": "account_browser_harness"}
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.MaxConns = 8
	return cfg, nil
}

func newBrowserHarness(t *testing.T, origin string) *browserHarness {
	t.Helper()
	if os.Getenv("ACCOUNT_BROWSER_HARNESS") != "1" {
		t.Skip("explicit ACCOUNT_BROWSER_HARNESS=1 required")
	}
	if !browserHarnessValidOrigin(origin) {
		t.Fatal("browser harness origin must be an explicit IPv4 loopback HTTP origin")
	}
	cfg, err := browserHarnessDatabaseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	admin, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatal("browser harness database initialization failed")
	}
	_, digest, err := accounts.NewToken(nil)
	if err != nil {
		admin.Close()
		t.Fatal("browser harness entropy unavailable")
	}
	schema := fmt.Sprintf("account_browser_%x", digest[:12])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatal("browser harness schema creation failed")
	}
	t.Cleanup(func() {
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error("browser harness isolated schema cleanup failed")
		}
	})
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("browser harness pool initialization failed")
	}
	t.Cleanup(pool.Close)
	var actual string
	if err = pool.QueryRow(ctx, "SELECT current_schema()").Scan(&actual); err != nil || actual != schema {
		t.Fatal("browser harness schema isolation failed")
	}
	if err = database.RunMigrations(ctx, pool); err != nil {
		t.Fatal("browser harness migration failed")
	}
	key, hmacKey := make([]byte, 32), make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal("browser harness entropy unavailable")
	}
	if _, err = rand.Read(hmacKey); err != nil {
		t.Fatal("browser harness entropy unavailable")
	}
	cipher, err := accountmail.NewCipher("browser-ephemeral", key, nil)
	if err != nil {
		t.Fatal("browser harness cipher unavailable")
	}
	hasher, err := accounts.NewArgonHasher(nil)
	if err != nil {
		t.Fatal("browser harness hasher unavailable")
	}
	h := &browserHarness{pool: pool, cipher: cipher, origin: origin, stop: make(chan struct{})}
	h.google = &browserGoogle{origin: origin, flows: make(map[string]browserGoogleFlow)}
	service, err := accounts.NewService(accounts.NewPostgresStore(pool), accounts.ServiceOptions{
		Hasher: hasher, Mail: &accountmail.Outbox{Cipher: cipher}, Origin: origin,
		AddressHMACKey: hmacKey, Google: h.google, FlowCipher: cipher,
	})
	if err != nil {
		t.Fatal("browser harness account service unavailable")
	}
	// Reuse the in-memory catalog fixture, stripping all external assets and
	// booking links. Actual catalog handlers remain available to public pages.
	data := fixtureDataset(t)
	day := time.Now().UTC().Format(time.DateOnly)
	data.GeneratedAt, data.UpcomingCompletedAt = time.Now().UTC(), time.Now().UTC()
	data.Window = schedule.Window{From: day, Through: day}
	for i := range data.Theaters {
		data.Theaters[i].AvailableDates = []string{day}
	}
	for i := range data.Showtimes {
		s := &data.Showtimes[i]
		s.ServiceDate = day
		s.StartTime = time.Now().UTC().Add(time.Duration(i+1) * time.Hour)
		s.EndTime = s.StartTime.Add(100 * time.Minute)
		s.Movie.PosterURL, s.BookingURL, s.Movie.Enrichment = "", "", nil
	}
	catalog, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data)}, schedule.ServiceOptions{DefaultCity: "Lille"})
	if err != nil {
		t.Fatal("browser harness catalog unavailable")
	}
	h.handler = NewHandlerWithOptions(catalog, origin, HandlerOptions{
		Accounts: AccountOptions{Enabled: true, Service: service, Origin: origin},
		Admin:    AdminOptions{Password: browserHarnessAdminPassword, SessionSecret: string(hmacKey)},
	})
	return h
}

func (h *browserHarness) serve(t *testing.T, port string) {
	t.Helper()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp4", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal("browser harness loopback listener unavailable")
	}
	h.apiURL = "http://" + listener.Addr().String()
	server := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error("browser harness HTTP drain failed")
			if err := server.Close(); err != nil {
				t.Error("browser harness HTTP close failed")
			}
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			t.Error("browser harness HTTP server failed")
		}
	})
}

func (h *browserHarness) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Host validation prevents DNS rebinding; no forwarded peer headers are
	// trusted. No CORS on controls, and no request/body logging anywhere here.
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(peer).IsLoopback() || ("http://"+r.Host != h.apiURL && "http://"+r.Host != h.origin) {
		http.Error(w, "loopback fixture only", http.StatusForbidden)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/__browser/") {
		h.handler.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if origin := r.Header.Get("Origin"); (origin != "" && origin != h.origin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "fixture origin rejected", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/api/__browser/google/authorize" && r.Method == http.MethodGet {
		h.google.authorize(w, r)
		return
	}
	if r.Header.Get("X-Browser-Harness") != "1" {
		http.Error(w, "fixture control header required", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/api/__browser/mailbox" && r.Method == http.MethodGet:
		h.mailbox(w, r)
	case r.URL.Path == "/api/__browser/shutdown" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusNoContent)
		h.stopOnce.Do(func() { close(h.stop) })
	default:
		http.NotFound(w, r)
	}
}

type browserMail struct {
	ID      int64  `json:"id"`
	Purpose string `json:"purpose"`
	To      string `json:"to"`
	Link    string `json:"link"`
	Token   string `json:"token"`
}

func (h *browserHarness) mailbox(w http.ResponseWriter, r *http.Request) {
	email, err := accounts.NormalizeEmail(r.URL.Query().Get("email"))
	if err != nil || !strings.HasSuffix(email, ".test") {
		http.Error(w, "synthetic .test mailbox required", http.StatusBadRequest)
		return
	}
	// Read only committed outbox rows. Unlike an enqueue spy, this cannot expose
	// mail from a rolled-back transaction. No delivery worker or AWS client exists.
	rows, err := h.pool.Query(r.Context(), `SELECT id,event_digest,purpose,payload_key_id,payload_nonce,payload_ciphertext FROM account_mail_outbox WHERE state='pending' AND expires_at>now() ORDER BY id DESC LIMIT 1000`)
	if err != nil {
		http.Error(w, "fixture mailbox unavailable", http.StatusServiceUnavailable)
		return
	}
	defer rows.Close()
	messages := []browserMail{}
	for rows.Next() {
		var mail browserMail
		var event []byte
		var envelope accountmail.Envelope
		if err = rows.Scan(&mail.ID, &event, &mail.Purpose, &envelope.KeyID, &envelope.Nonce, &envelope.Ciphertext); err != nil {
			break
		}
		var plain []byte
		plain, err = h.cipher.Open(accountmail.OutboxBinding(event, mail.Purpose), envelope)
		if err != nil {
			break
		}
		var message accountmail.Message
		if err = json.Unmarshal(plain, &message); err != nil {
			break
		}
		if message.Recipient != email {
			continue
		}
		mail.To = message.Recipient
		for _, field := range strings.Fields(message.Text) {
			if strings.HasPrefix(field, h.origin+"/") && strings.Contains(field, "#token=") {
				mail.Link = field
				_, mail.Token, _ = strings.Cut(field, "#token=")
			}
		}
		messages = append(messages, mail)
	}
	if err != nil || rows.Err() != nil {
		http.Error(w, "fixture mailbox unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Messages []browserMail `json:"messages"`
	}{messages})
}

type browserGoogleFlow struct {
	nonce, verifier string
	code            string
	identity        accounts.GoogleIdentity
	expires         time.Time
}

type browserGoogle struct {
	origin string
	mu     sync.Mutex // protects flows across start, local authorize and callback
	flows  map[string]browserGoogleFlow
}

func (g *browserGoogle) AuthorizationURL(state, nonce, verifier string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for state, flow := range g.flows {
		if !time.Now().Before(flow.expires) {
			delete(g.flows, state)
		}
	}
	if len(g.flows) >= 1000 {
		return "", accounts.ErrUnavailable
	}
	g.flows[state] = browserGoogleFlow{nonce: nonce, verifier: verifier, expires: time.Now().Add(accounts.OAuthLifetime)}
	// Deliberately NOT an accounts.google.com URL: even an uninstrumented browser
	// cannot contact Google. Production frontend allowlisting stays unchanged.
	return g.origin + "/api/__browser/google/authorize?state=" + url.QueryEscape(state), nil
}

func (g *browserGoogle) authorize(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	state := r.URL.Query().Get("state")
	flow, ok := g.flows[state]
	if !ok || !time.Now().Before(flow.expires) || flow.code != "" {
		http.Error(w, "invalid synthetic flow", http.StatusBadRequest)
		return
	}
	identity := r.URL.Query().Get("identity")
	if identity == "" {
		identity = "verified"
	}
	if identity == "denied" {
		delete(g.flows, state)
		http.Redirect(w, r, g.origin+"/api/v1/auth/google/callback?error=access_denied&state="+url.QueryEscape(state), http.StatusSeeOther)
		return
	}
	if identity != "verified" && identity != "unverified" && identity != "link" {
		http.Error(w, "unknown synthetic identity", http.StatusBadRequest)
		return
	}
	code, _, err := accounts.NewToken(nil)
	if err != nil {
		http.Error(w, "synthetic provider unavailable", http.StatusServiceUnavailable)
		return
	}
	flow.code = code
	flow.identity = accounts.GoogleIdentity{Subject: "browser-only-" + identity, Email: "google-" + identity + "@example.test", EmailVerified: identity != "unverified"}
	g.flows[state] = flow
	http.Redirect(w, r, g.origin+"/api/v1/auth/google/callback?state="+url.QueryEscape(state)+"&code="+url.QueryEscape(code), http.StatusSeeOther)
}

func (g *browserGoogle) Exchange(_ context.Context, code, verifier, nonce string) (accounts.GoogleIdentity, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for state, flow := range g.flows {
		if code != "" && flow.code == code {
			delete(g.flows, state)
			if time.Now().Before(flow.expires) && flow.verifier == verifier && flow.nonce == nonce {
				return flow.identity, nil
			}
			break
		}
	}
	return accounts.GoogleIdentity{}, accounts.ErrInvalidLink
}
