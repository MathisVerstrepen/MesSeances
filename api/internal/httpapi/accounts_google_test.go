package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"messeances/api/internal/accountmail"
	"messeances/api/internal/accounts"
)

func TestGoogleCallbackQueryBounds(t *testing.T) {
	state := strings.Repeat("A", 43)
	base := "state=" + state + "&iss=https%3A%2F%2Faccounts.google.com"
	for _, test := range []struct {
		name  string
		query string
		valid bool
	}{
		{"provider success", base + "&code=synthetic&scope=openid+email&authuser=0&prompt=select_account", true},
		{"provider denial", base + "&error=access_denied&error_description=private+provider+detail", true},
		{"duplicate code", base + "&code=one&code=two", false},
		{"duplicate state", base + "&code=one&state=" + state, false},
		{"unknown parameter", base + "&code=one&return_url=https://evil.example", false},
		{"code and error", base + "&error=access_denied&code=one", false},
		{"invalid state", strings.Replace(base, state, "invalid", 1) + "&code=one", false},
		{"missing state", "iss=https%3A%2F%2Faccounts.google.com&code=one", false},
		{"missing code and error", base, false},
		{"empty code", base + "&code=", false},
		{"code at bound", base + "&code=" + strings.Repeat("x", 4096), true},
		{"code over bound", base + "&code=" + strings.Repeat("x", 4097), false},
		{"query at bound", base + "&code=" + strings.Repeat("x", 4096) + "&scope=" + strings.Repeat("x", 8192-len(base)-len("&code=&scope=")-4096), true},
		{"query over bound", base + "&code=" + strings.Repeat("x", 4096) + "&scope=" + strings.Repeat("x", 8193-len(base)-len("&code=&scope=")-4096), false},
		{"malformed escape", base + "&code=one&scope=%zz", false},
		{"malformed separator", base + "&code=one;scope=openid", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/google/callback?"+test.query, nil)
			gotState, gotCode, ok := googleCallbackQuery(request)
			if ok != test.valid {
				t.Fatalf("callback validation mismatch (want %t)", test.valid)
			}
			if ok && (gotState != state || gotCode != request.URL.Query().Get("code")) {
				t.Fatal("callback state or code changed")
			}
		})
	}
}

func TestGoogleCallbackIssuer(t *testing.T) {
	for _, test := range []struct {
		name, issuer string
		valid        bool
	}{
		{"exact", "&iss=https%3A%2F%2Faccounts.google.com", true},
		{"missing", "", false},
		{"empty", "&iss=", false},
		{"bare", "&iss", false},
		{"wrong host", "&iss=https%3A%2F%2Fevil.example", false},
		{"host suffix", "&iss=https%3A%2F%2Faccounts.google.com.evil.example", false},
		{"legacy token issuer", "&iss=accounts.google.com", false},
		{"http", "&iss=http%3A%2F%2Faccounts.google.com", false},
		{"case", "&iss=https%3A%2F%2FACCOUNTS.GOOGLE.COM", false},
		{"trailing slash", "&iss=https%3A%2F%2Faccounts.google.com%2F", false},
		{"whitespace", "&iss=+https%3A%2F%2Faccounts.google.com", false},
		{"double encoded", "&iss=https%253A%252F%252Faccounts.google.com", false},
		{"duplicate", "&iss=https%3A%2F%2Faccounts.google.com&iss=https%3A%2F%2Faccounts.google.com", false},
		{"conflicting duplicate", "&iss=https%3A%2F%2Faccounts.google.com&iss=https%3A%2F%2Fevil.example", false},
		{"encoded duplicate key", "&iss=https%3A%2F%2Faccounts.google.com&%69ss=https%3A%2F%2Faccounts.google.com", false},
	} {
		for _, outcome := range []string{"code=synthetic", "error=access_denied"} {
			t.Run(test.name+"/"+outcome, func(t *testing.T) {
				query := "state=" + strings.Repeat("A", 43) + "&" + outcome + test.issuer
				request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/google/callback?"+query, nil)
				_, _, ok := googleCallbackQuery(request)
				if ok != test.valid {
					t.Fatalf("issuer validation mismatch (want %t)", test.valid)
				}
			})
		}
	}
}

type googleCallbackStoreProbe struct{ calls int }

func (p *googleCallbackStoreProbe) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	p.calls++
	return nil, accounts.ErrUnavailable
}

func TestGoogleCallbackServiceReachability(t *testing.T) {
	const issuer = "&iss=https%3A%2F%2Faccounts.google.com"
	for _, test := range []struct {
		name, query string
		cookies     int
		calls       int
	}{
		{"success reaches service", "&code=synthetic&scope=openid+email&authuser=0&prompt=select_account" + issuer, 1, 1},
		{"denial reaches service", "&error=access_denied&error_description=private-detail" + issuer, 1, 1},
		{"missing issuer", "&code=synthetic", 1, 0},
		{"wrong issuer", "&code=synthetic&iss=https%3A%2F%2Fevil.example", 1, 0},
		{"duplicate issuer", "&code=synthetic" + issuer + issuer, 1, 0},
		{"missing flow cookie", "&code=synthetic" + issuer, 0, 0},
		{"duplicate flow cookie", "&code=synthetic" + issuer, 2, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &googleCallbackStoreProbe{}
			cipher, err := accountmail.NewCipher("synthetic", []byte(strings.Repeat("k", 32)), nil)
			if err != nil {
				t.Fatal(err)
			}
			service, err := accounts.NewService(accounts.NewPostgresStore(probe), accounts.ServiceOptions{
				Hasher: unavailableAccountHasher{}, Origin: "https://messeances.fr",
				AddressHMACKey: []byte(strings.Repeat("h", 32)), Google: &browserGoogle{}, FlowCipher: cipher,
			})
			if err != nil {
				t.Fatal(err)
			}
			h, err := newAccountHTTP(AccountOptions{Origin: "https://messeances.fr", Service: service})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/google/callback?state="+strings.Repeat("A", 43)+test.query, nil)
			for range test.cookies {
				request.AddCookie(&http.Cookie{Name: h.googleCookieName(), Value: strings.Repeat("B", 42) + "A"})
			}
			response := httptest.NewRecorder()
			accountBoundary(http.HandlerFunc(h.googleCallback)).ServeHTTP(response, request)
			if probe.calls != test.calls {
				t.Fatalf("service transaction attempts = %d, want %d", probe.calls, test.calls)
			}
			// The probe stops before any database or provider I/O. Its service error
			// must use the same private failure response as query/cookie rejection.
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/connexion?error=google_failed" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("unsafe callback failure response")
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != h.googleCookieName() || cookies[0].MaxAge != -1 {
				t.Fatal("callback must clear only the flow cookie")
			}
		})
	}
}

func TestGoogleCallbackBrowserFixture(t *testing.T) {
	for _, identity := range []string{"verified", "denied"} {
		t.Run(identity, func(t *testing.T) {
			state := strings.Repeat("A", 43)
			provider := &browserGoogle{flows: map[string]browserGoogleFlow{state: {expires: time.Now().Add(time.Minute)}}}
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/__browser/google/authorize?state="+state+"&identity="+identity, nil)
			response := httptest.NewRecorder()
			provider.authorize(response, request)
			callback := httptest.NewRequestWithContext(t.Context(), http.MethodGet, response.Header().Get("Location"), nil)
			gotState, code, ok := googleCallbackQuery(callback)
			if response.Code != http.StatusSeeOther || !ok || gotState != state || (code == "") != (identity == "denied") || callback.URL.Query().Get("iss") != "https://accounts.google.com" {
				t.Fatal("synthetic callback differs from documented Google response")
			}
		})
	}
}

func TestGoogleCallbackFailureSafeRedirect(t *testing.T) {
	h, err := newAccountHTTP(AccountOptions{Origin: "https://messeances.fr"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/google/callback?code=private-code&state=private-state&error_description=private-error", nil)
	response := httptest.NewRecorder()
	accountBoundary(http.HandlerFunc(h.googleCallback)).ServeHTTP(response, request)
	if response.Code != 303 || response.Header().Get("Location") != "/connexion?error=google_failed" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unsafe callback response")
	}
	for key, values := range response.Header() {
		for _, value := range values {
			if strings.Contains(value, "private-") {
				t.Fatalf("callback input leaked in %s", key)
			}
		}
	}
	if strings.Contains(response.Body.String(), "private-") {
		t.Fatal("callback input leaked")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-messeances_google" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Path != "/" || cookies[0].Domain != "" || cookies[0].MaxAge != -1 {
		t.Fatal("unsafe flow cookie")
	}
	u, _ := url.Parse(response.Header().Get("Location"))
	if u.Host != "" || u.Query().Get("error") != "google_failed" {
		t.Fatal("unsafe fixed destination")
	}
}

func TestGoogleActionStrictJSON(t *testing.T) {
	for _, body := range []string{`{"mode":"login","target":null}`, `{"mode":"login","mode":"link"}`, `{"mode":"reauth","return_url":"/evil"}`, `{"mode":"login"} {}`, `{"mode":true}`} {
		response := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/google/start", strings.NewReader(body))
		var input accounts.GoogleStart
		if accountJSON(response, request, &input) || response.Code != 400 {
			t.Fatal("unsafe Google input accepted")
		}
	}
}

func TestGoogleCallbackExistingEmailIntegration(t *testing.T) {
	for _, test := range []struct {
		identity, destination string
	}{
		{"verified", "/connexion?error=google_email_in_use"},
		{"unverified", "/connexion?error=google_failed"},
	} {
		t.Run(test.identity, func(t *testing.T) {
			p := newBrowserProbe(t)
			email := "google-" + test.identity + "@example.test"
			const password = "synthetic collision passphrase"
			p.request(http.MethodPost, "/api/v1/auth/register", accountCredentials{Email: email, Password: password}, 202, nil)
			mail := p.mail(email, string(accounts.TokenVerification))
			p.request(http.MethodPost, "/api/v1/auth/verification/confirm", accountVerification{Token: mail.Token}, 200, nil)
			p.request(http.MethodPost, "/api/v1/account/username", accountUsername{Username: "collision_owner"}, 200, nil)
			p.request(http.MethodPost, "/api/v1/auth/logout", struct{}{}, 204, nil)
			response := p.google(map[string]string{"mode": "login"}, test.identity, test.destination)
			assertGoogleFailureCookies(t, response)
			p.state(accounts.StateAnonymous)
			var identities, sessions int
			if err := p.h.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM account_google_identities), (SELECT count(*) FROM account_sessions)`).Scan(&identities, &sessions); err != nil || identities != 0 || sessions != 0 {
				t.Fatal("collision created identity or session")
			}
			p.request(http.MethodPost, "/api/v1/auth/login", accountCredentials{Email: email, Password: password}, 200, nil)
			// Even an existing session does not make login an implicit link, and
			// failure must neither replace nor clear that unrelated session cookie.
			response = p.google(map[string]string{"mode": "login"}, test.identity, test.destination)
			assertGoogleFailureCookies(t, response)
			p.state(accounts.StateComplete)
			grant := p.grant(password, "google_link", "")
			p.google(map[string]string{"mode": "link", "grant": grant}, test.identity, "/compte")
			p.request(http.MethodPost, "/api/v1/auth/logout", struct{}{}, 204, nil)
			p.google(map[string]string{"mode": "login"}, test.identity, "/compte")
			var details accounts.AccountDetails
			p.request(http.MethodGet, "/api/v1/account", nil, 200, &details)
			if details.Email != email || !details.HasPassword || !details.GoogleLinked || details.Username == nil || *details.Username != "collision_owner" {
				t.Fatal("explicit linking did not retain the existing account")
			}
		})
	}
}

func TestGoogleCallbackLinkConflictIntegration(t *testing.T) {
	p := newBrowserProbe(t)
	p.google(map[string]string{"mode": "login"}, "verified", "/finaliser")
	p.request(http.MethodPost, "/api/v1/account/username", accountUsername{Username: "subject_owner"}, 200, nil)
	p.request(http.MethodPost, "/api/v1/auth/logout", struct{}{}, 204, nil)
	const email = "browser-link-conflict@example.test"
	const password = "synthetic conflict passphrase"
	p.request(http.MethodPost, "/api/v1/auth/register", accountCredentials{Email: email, Password: password}, 202, nil)
	mail := p.mail(email, string(accounts.TokenVerification))
	p.request(http.MethodPost, "/api/v1/auth/verification/confirm", accountVerification{Token: mail.Token}, 200, nil)
	p.request(http.MethodPost, "/api/v1/account/username", accountUsername{Username: "link_requester"}, 200, nil)
	grant := p.grant(password, "google_link", "")
	response := p.google(map[string]string{"mode": "link", "grant": grant}, "verified", "/connexion?error=google_failed")
	assertGoogleFailureCookies(t, response)
	var details accounts.AccountDetails
	p.request(http.MethodGet, "/api/v1/account", nil, 200, &details)
	if details.Email != email || details.GoogleLinked {
		t.Fatal("failed link changed identity owner or cleared session")
	}
}

func assertGoogleFailureCookies(t *testing.T, response browserProbeResponse) {
	t.Helper()
	if response.header.Get("Cache-Control") != "no-store" || response.header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("callback failure lost privacy headers")
	}
	if len(response.cookies) != 1 || response.cookies[0].Name != "messeances_google_dev" || response.cookies[0].MaxAge != -1 || !response.cookies[0].HttpOnly || response.cookies[0].Path != "/" || response.cookies[0].Domain != "" || response.cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("callback failure must clear only the flow cookie")
	}
}
