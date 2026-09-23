package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accounts"
)

func TestAccountBrowserHarnessGuards(t *testing.T) {
	for _, origin := range []string{"https://messeances.fr", "http://0.0.0.0:13009", "http://127.0.0.1:13009/", "http://127.0.0.1:13009?", "http://user@127.0.0.1:13009", "http://127.0.0.1:0"} {
		if browserHarnessValidOrigin(origin) {
			t.Error("unsafe fixture origin accepted")
		}
	}
	if !browserHarnessValidOrigin(browserHarnessOrigin) {
		t.Fatal("default fixture origin rejected")
	}
	for _, dsn := range []string{"", "postgres://accountstest@127.0.0.1:5432/accountstest?sslmode=disable", "postgres://accountstest@localhost:55439/accountstest?sslmode=disable", "postgres://accountstest@127.0.0.1:55439/movieflow?sslmode=disable", "postgres://accountstest@127.0.0.1:55439/accountstest?sslmode=disable&options=x"} {
		if _, err := browserHarnessDatabaseConfig(dsn); err == nil {
			t.Error("unapproved fixture database accepted")
		}
	}
	h := &browserHarness{origin: browserHarnessOrigin, apiURL: "http://127.0.0.1:18089", handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })}
	for _, test := range []struct {
		name, host, peer, origin, path, header string
		want                                   int
	}{
		{"loopback", "127.0.0.1:18089", "127.0.0.1:1234", "", "/healthz", "", 204},
		{"proxy host", "127.0.0.1:13009", "127.0.0.1:1234", "", "/healthz", "", 204},
		{"remote", "127.0.0.1:18089", "192.0.2.1:1234", "", "/healthz", "", 403},
		{"rebound host", "attacker.test", "127.0.0.1:1234", "", "/healthz", "", 403},
		{"control header", "127.0.0.1:18089", "127.0.0.1:1234", "", "/api/__browser/mailbox", "", 403},
		{"foreign origin", "127.0.0.1:18089", "127.0.0.1:1234", "https://attacker.test", "/api/__browser/mailbox", "1", 403},
		{"non synthetic mailbox", "127.0.0.1:18089", "127.0.0.1:1234", "", "/api/__browser/mailbox?email=person@example.com", "1", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, h.apiURL+test.path, nil)
			r.Host, r.RemoteAddr = test.host, test.peer
			r.Header.Set("Origin", test.origin)
			r.Header.Set("X-Browser-Harness", test.header)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status=%d want=%d", w.Code, test.want)
			}
		})
	}
}

type browserProbe struct {
	t      *testing.T
	h      *browserHarness
	client *http.Client
}

func newBrowserProbe(t *testing.T) *browserProbe {
	t.Helper()
	h := newBrowserHarness(t, browserHarnessOrigin)
	h.serve(t, "0")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal("cookie jar unavailable")
	}
	return &browserProbe{t: t, h: h, client: &http.Client{Jar: jar, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type browserProbeResponse struct {
	header  http.Header
	cookies []*http.Cookie
}

func (p *browserProbe) request(method, path string, body any, status int, target any) browserProbeResponse {
	p.t.Helper()
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			p.t.Fatal("fixture request encoding failed")
		}
	}
	r, err := http.NewRequestWithContext(p.t.Context(), method, p.h.apiURL+path, bytes.NewReader(encoded))
	if err != nil {
		p.t.Fatal("fixture request construction failed")
	}
	r.Header.Set("Origin", p.h.origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Messeances-CSRF", "1")
	r.Header.Set("X-Browser-Harness", "1")
	response, err := p.client.Do(r)
	if err != nil {
		p.t.Fatal("fixture HTTP request failed") // Never print URLs carrying synthetic tokens.
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			p.t.Error("fixture response close failed")
		}
	}()
	if response.StatusCode != status {
		p.t.Fatalf("fixture HTTP status=%d want=%d", response.StatusCode, status)
	}
	if target != nil {
		if err = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(target); err != nil {
			p.t.Fatal("fixture response decoding failed")
		}
	}
	return browserProbeResponse{header: response.Header, cookies: response.Cookies()}
}

func (p *browserProbe) mail(email, purpose string) browserMail {
	p.t.Helper()
	var response struct {
		Messages []browserMail `json:"messages"`
	}
	r := p.request(http.MethodGet, "/api/__browser/mailbox?email="+url.QueryEscape(email), nil, 200, &response)
	if r.header.Get("Cache-Control") != "no-store" {
		p.t.Fatal("synthetic mailbox must not cache")
	}
	for _, mail := range response.Messages {
		if mail.Purpose == purpose && mail.To == email {
			if _, err := accounts.TokenDigest(mail.Token); err != nil || !strings.HasPrefix(mail.Link, p.h.origin+"/") || strings.Contains(mail.Link, "?token=") {
				p.t.Fatal("invalid synthetic mail link")
			}
			return mail
		}
	}
	p.t.Fatal("synthetic mail missing")
	return browserMail{}
}

func (p *browserProbe) state(want accounts.State) {
	p.t.Helper()
	var session accounts.SessionView
	r := p.request(http.MethodGet, "/api/v1/auth/session", nil, 200, &session)
	if !session.Enabled || session.State != want || r.header.Get("Cache-Control") != "no-store" {
		p.t.Fatal("unexpected account session state or cache policy")
	}
}

func (p *browserProbe) grant(password, action, target string) string {
	p.t.Helper()
	var result struct {
		Grant string `json:"grant"`
	}
	p.request(http.MethodPost, "/api/v1/account/reauth/password", map[string]string{"password": password, "action": action, "target": target}, 200, &result)
	if _, err := accounts.TokenDigest(result.Grant); err != nil {
		p.t.Fatal("invalid fixture action grant")
	}
	return result.Grant
}

func (p *browserProbe) google(input map[string]string, identity, destination string) browserProbeResponse {
	p.t.Helper()
	var start struct {
		URL string `json:"authorization_url"`
	}
	p.request(http.MethodPost, "/api/v1/auth/google/start", input, 200, &start)
	u, err := url.Parse(start.URL)
	if err != nil || u.Scheme+"://"+u.Host != p.h.origin || u.Path != "/api/__browser/google/authorize" {
		p.t.Fatal("fixture attempted nonlocal Google authorization")
	}
	q := u.Query()
	q.Set("identity", identity)
	u.RawQuery = q.Encode()
	response := p.request(http.MethodGet, u.RequestURI(), nil, 303, nil)
	callback, err := url.Parse(response.header.Get("Location"))
	if err != nil || callback.Scheme+"://"+callback.Host != p.h.origin || callback.Path != "/api/v1/auth/google/callback" {
		p.t.Fatal("fixture attempted nonlocal callback")
	}
	response = p.request(http.MethodGet, callback.RequestURI(), nil, 303, nil)
	if response.header.Get("Location") != destination {
		p.t.Fatal("unexpected synthetic Google callback destination")
	}
	return response
}

func TestAccountBrowserHarnessSanityIntegration(t *testing.T) {
	t.Run("email lifecycle and settings", func(t *testing.T) {
		p := newBrowserProbe(t)
		const email = "browser-sanity@example.test"
		const changedEmail = "browser-changed@example.test"
		const password = "synthetic browser initial passphrase"
		const resetPassword = "synthetic browser reset passphrase"
		const changedPassword = "synthetic browser changed passphrase"
		p.request(http.MethodGet, "/healthz", nil, 200, nil)
		p.request(http.MethodGet, "/api/v1/theaters", nil, 200, nil)
		p.request(http.MethodGet, "/api/v1/movies", nil, 200, nil)
		p.state(accounts.StateAnonymous)
		p.request(http.MethodPost, "/api/v1/auth/register", accountCredentials{Email: email, Password: password}, 202, nil)
		mail := p.mail(email, string(accounts.TokenVerification))
		r := p.request(http.MethodPost, "/api/v1/auth/verification/confirm", accountVerification{Token: mail.Token}, 200, nil)
		cookies := r.cookies
		if len(cookies) != 2 || cookies[0].Name != accountDevCookieName || !cookies[0].HttpOnly || cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" || cookies[0].Domain != "" || cookies[0].MaxAge <= 0 || cookies[1].Name != accountDevRegistrationCookieName || cookies[1].MaxAge != -1 {
			t.Fatal("unexpected browser development cookie attributes")
		}
		p.state(accounts.StatePendingUsername)
		p.request(http.MethodPost, "/api/v1/account/username", accountUsername{Username: "browser_sanity"}, 200, nil)
		p.state(accounts.StateComplete)
		p.request(http.MethodPost, "/api/v1/auth/logout", struct{}{}, 204, nil)
		p.state(accounts.StateAnonymous)
		p.request(http.MethodPost, "/api/v1/auth/login", accountCredentials{Email: email, Password: password}, 200, nil)
		p.request(http.MethodPost, "/api/v1/auth/password/reset/request", accountEmail{Email: email}, 202, nil)
		mail = p.mail(email, string(accounts.TokenPasswordReset))
		p.request(http.MethodPost, "/api/v1/auth/password/reset/confirm", accountConfirmation{Token: mail.Token, Password: resetPassword}, 204, nil)
		p.state(accounts.StateAnonymous)
		p.request(http.MethodPost, "/api/v1/auth/login", accountCredentials{Email: email, Password: resetPassword}, 200, nil)
		grant := p.grant(resetPassword, "password_change", "")
		p.request(http.MethodPost, "/api/v1/account/password", accountPassword{Password: changedPassword, Grant: grant}, 204, nil)
		grant = p.grant(changedPassword, "google_link", "")
		p.google(map[string]string{"mode": "link", "grant": grant}, "link", "/compte")
		var details accounts.AccountDetails
		p.request(http.MethodGet, "/api/v1/account", nil, 200, &details)
		if !details.GoogleLinked || !details.HasPassword {
			t.Fatal("synthetic Google link did not preserve password")
		}
		grant = p.grant(changedPassword, "google_unlink", "")
		p.request(http.MethodPost, "/api/v1/account/google/unlink", map[string]string{"grant": grant}, 204, nil)
		grant = p.grant(changedPassword, "email_change", changedEmail)
		p.request(http.MethodPost, "/api/v1/account/email/request", accountEmailRequest{Email: changedEmail, Grant: grant}, 202, nil)
		mail = p.mail(changedEmail, string(accounts.TokenEmailChange))
		grant = p.grant(changedPassword, "email_change", changedEmail)
		p.request(http.MethodPost, "/api/v1/account/email/confirm", accountEmailConfirm{Token: mail.Token, Grant: grant}, 204, nil)
		p.state(accounts.StateAnonymous)
		p.request(http.MethodPost, "/api/v1/auth/login", accountCredentials{Email: changedEmail, Password: changedPassword}, 200, nil)
		grant = p.grant(changedPassword, "delete_account", "")
		p.request(http.MethodDelete, "/api/v1/account", map[string]string{"grant": grant, "confirmation": "SUPPRIMER"}, 204, nil)
		p.state(accounts.StateAnonymous)
		var reserved bool
		if err := p.h.pool.QueryRow(t.Context(), `SELECT account_id IS NULL FROM account_username_claims WHERE username='browser_sanity'`).Scan(&reserved); err != nil || !reserved {
			t.Fatal("deleted fixture username was not permanently reserved")
		}
		p.request(http.MethodPost, "/api/__browser/shutdown", struct{}{}, 204, nil)
		select {
		case <-p.h.stop:
		default:
			t.Fatal("fixture shutdown not signaled")
		}
	})
	t.Run("Google fixtures", func(t *testing.T) {
		p := newBrowserProbe(t)
		p.google(map[string]string{"mode": "login"}, "denied", "/connexion?error=google_failed")
		p.state(accounts.StateAnonymous)
		p.google(map[string]string{"mode": "login"}, "unverified", "/verification")
		p.state(accounts.StatePendingEmail)
		mail := p.mail("google-unverified@example.test", string(accounts.TokenVerification))
		p.request(http.MethodPost, "/api/v1/auth/verification/confirm", map[string]string{"token": mail.Token}, 200, nil)
		p.state(accounts.StatePendingUsername)
		p.request(http.MethodPost, "/api/v1/auth/logout", struct{}{}, 204, nil)
		p.google(map[string]string{"mode": "login"}, "verified", "/finaliser")
		p.request(http.MethodPost, "/api/v1/account/username", accountUsername{Username: "browser_google"}, 200, nil)
		p.google(map[string]string{"mode": "reauth", "action": "password_add"}, "verified", "/compte/confirmer-identite")
		p.request(http.MethodPost, "/api/v1/account/reauth/email/request", struct{}{}, 202, nil)
		mail = p.mail("google-verified@example.test", string(accounts.TokenEmailStepUp))
		var proof struct {
			Grant string `json:"grant"`
		}
		p.request(http.MethodPost, "/api/v1/account/reauth/email/confirm", map[string]string{"token": mail.Token, "action": "password_add"}, 200, &proof)
		p.request(http.MethodPost, "/api/v1/account/password", accountPassword{Password: "synthetic Google added passphrase", Grant: proof.Grant}, 204, nil)
		p.state(accounts.StateComplete)
	})
}

func (p *browserProbe) otherBrowser(t *testing.T) *browserProbe {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &browserProbe{t: t, h: p.h, client: &http.Client{Jar: jar, Timeout: 15 * time.Second}}
}

func TestAccountVerificationBrowserContractIntegration(t *testing.T) {
	p := newBrowserProbe(t)
	const email = "browser-binding@example.test"
	const password = "unique-ten"
	registration := p.request(http.MethodPost, "/api/v1/auth/register", accountCredentials{Email: email, Password: password}, 202, nil)
	if len(registration.cookies) != 1 {
		t.Fatal("registration must always issue exactly one independent proof cookie")
	}
	cookie := registration.cookies[0]
	if cookie.Name != accountDevRegistrationCookieName || !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge <= 0 || cookie.MaxAge > 604800 {
		t.Fatal("unsafe registration cookie")
	}
	p.state(accounts.StateAnonymous)
	mail := p.mail(email, string(accounts.TokenVerification))
	// Visiting a link cannot activate credentials, including a scanner with the
	// original cookie. Only explicit token-only POST reaches confirmation.
	p.request(http.MethodGet, "/api/v1/auth/verification/confirm", nil, 405, nil)
	p.request(http.MethodPost, "/api/v1/auth/verification/confirm", accountConfirmation{Token: mail.Token, Password: password}, 400, nil)
	other := p.otherBrowser(t)
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	other.request(http.MethodPost, "/api/v1/auth/verification/confirm", accountVerification{Token: mail.Token}, 403, &failure)
	if failure.Error.Code != "verification_browser_required" {
		t.Fatal("wrong browser must receive original-browser recovery error")
	}
	// Victim knowing an attacker-supplied tentative password still cannot verify
	// that attacker's attempt using an ordinary login session.
	other.request(http.MethodPost, "/api/v1/auth/login", accountCredentials{Email: email, Password: password}, 200, nil)
	other.state(accounts.StatePendingEmail)
	other.request(http.MethodPost, "/api/v1/auth/verification/confirm", accountVerification{Token: mail.Token}, 403, &failure)
	if failure.Error.Code != "verification_browser_required" {
		t.Fatal("pending login substituted for original registration proof")
	}
	// Session rotation in the real registration browser leaves attempt proof intact.
	p.request(http.MethodPost, "/api/v1/auth/login", accountCredentials{Email: email, Password: password}, 200, nil)
	p.request(http.MethodPost, "/api/v1/auth/login", accountCredentials{Email: email, Password: password}, 200, nil)
	p.request(http.MethodPost, "/api/v1/auth/verification/confirm", accountVerification{Token: mail.Token}, 200, nil)
	p.state(accounts.StatePendingUsername)
	other.state(accounts.StateAnonymous)
	p.request(http.MethodPost, "/api/v1/auth/verification/confirm", accountVerification{Token: mail.Token}, 400, nil)
	p.request(http.MethodPost, "/api/v1/auth/login", accountCredentials{Email: email, Password: password}, 200, nil)
}

func TestAccountRegistrationNonenumerationIntegration(t *testing.T) {
	for _, state := range []string{"new", "pending", "verified", "google"} {
		t.Run(state, func(t *testing.T) {
			p := newBrowserProbe(t)
			email := "browser-enumeration@example.test"
			if state == "google" {
				email = "google-unverified@example.test"
				p.google(map[string]string{"mode": "login"}, "unverified", "/verification")
			} else if state != "new" {
				p.request(http.MethodPost, "/api/v1/auth/register", accountCredentials{Email: email, Password: "unique-ten"}, 202, nil)
				if state == "verified" {
					mail := p.mail(email, string(accounts.TokenVerification))
					p.request(http.MethodPost, "/api/v1/auth/verification/confirm", accountVerification{Token: mail.Token}, 200, nil)
				}
			}
			// Clear only this fixture's isolated quota rows to compare identical public
			// requests without waiting a minute. No real database/schema is touched.
			if _, err := p.h.pool.Exec(t.Context(), `DELETE FROM account_rate_limits`); err != nil {
				t.Fatal(err)
			}
			result := p.request(http.MethodPost, "/api/v1/auth/register", accountCredentials{Email: email, Password: "unique-ten"}, 202, nil)
			if len(result.cookies) != 1 || result.cookies[0].Name != accountDevRegistrationCookieName || len(result.cookies[0].Value) != 43 || result.cookies[0].MaxAge < 604790 || result.header.Get("Content-Length") != "0" || result.header.Get("Cache-Control") != "no-store" {
				t.Fatal("existence-dependent registration cookie/response shape")
			}
		})
	}
}
