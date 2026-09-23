package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accounts"
)

type unavailableAccountHasher struct{}

func (unavailableAccountHasher) Hash(context.Context, string) (string, error) {
	return "", accounts.ErrUnavailable
}
func (unavailableAccountHasher) Verify(context.Context, string, string) (bool, bool, error) {
	return false, false, accounts.ErrUnavailable
}
func (unavailableAccountHasher) Dummy(context.Context, string) error { return accounts.ErrUnavailable }

func lifecycleHTTPOptions(t *testing.T) AccountOptions {
	t.Helper()
	service, err := accounts.NewService(accounts.NewPostgresStore(nil), accounts.ServiceOptions{Hasher: unavailableAccountHasher{}, Origin: "https://messeances.fr", AddressHMACKey: []byte(strings.Repeat("h", 32))})
	if err != nil {
		t.Fatal(err)
	}
	return AccountOptions{Enabled: true, Service: service, Origin: "https://messeances.fr"}
}

func TestAccountMutationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, header, value string
		status              int
	}{{"valid", "", "", 503}, {"missing_origin", "Origin", "", 403}, {"foreign_origin", "Origin", "https://evil.example", 403}, {"null_origin", "Origin", "null", 403}, {"missing_csrf", "X-Messeances-CSRF", "", 403}, {"cross_site", "Sec-Fetch-Site", "cross-site", 403}, {"same_site", "Sec-Fetch-Site", "same-site", 403}, {"form", "Content-Type", "application/x-www-form-urlencoded", 400}, {"wrong_charset", "Content-Type", "application/json; charset=iso-8859-1", 400}} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"email":"a@example.com","password":"a deliberately unique passphrase"}`))
			r.RemoteAddr = "203.0.113.5:1234"
			r.Header.Set("Origin", "https://messeances.fr")
			r.Header.Set("X-Messeances-CSRF", "1")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			if tc.header != "" {
				if tc.value == "" {
					r.Header.Del(tc.header)
				} else {
					r.Header.Set(tc.header, tc.value)
				}
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "" || len(w.Result().Cookies()) != 0 {
				t.Fatal("account error policy bypass")
			}
		})
	}
}

func TestAccountStrictJSON(t *testing.T) {
	for _, body := range []string{`{"email":"a","email":"b","password":"x"}`, `{"email":"a","\u0065mail":"b"}`, `{"Email":"a"}`, `{"email":null}`, `{"email":3}`, `{"email":{}}`, `{"email":[]}`, `{"extra":"x"}`, `{} {}`, `[]`, `null`, ``, "{\"email\":\"\xff\"}", `{"email":"` + strings.Repeat("x", 8192) + `"}`} {
		t.Run(body[:min(len(body), 35)], func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), "POST", "/", strings.NewReader(body))
			w := httptest.NewRecorder()
			var input accountCredentials
			if accountJSON(w, r, &input) || w.Code != 400 {
				t.Fatalf("accepted invalid JSON: %q", body)
			}
		})
	}
	for _, body := range []string{`{"email":"a@example.com","password":"spaces stay here"}`, `{ "email" : "a@example.com" }`} {
		r := httptest.NewRequestWithContext(t.Context(), "POST", "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		var input accountCredentials
		if !accountJSON(w, r, &input) {
			t.Fatal("rejected valid JSON")
		}
	}
}

func TestAccountCookies(t *testing.T) {
	h, err := newAccountHTTP(lifecycleHTTPOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := accounts.NewToken(nil)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: accountCookieName, Value: raw})
	r.AddCookie(&http.Cookie{Name: accountCookieName, Value: raw})
	if _, err = h.cookie(r); err == nil {
		t.Fatal("duplicate cookie accepted")
	}
	w := httptest.NewRecorder()
	h.setCookie(w, accounts.Cookie{Token: raw, ExpiresAt: time.Now().Add(time.Hour)})
	c := w.Result().Cookies()[0]
	if c.Name != accountCookieName || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode || c.MaxAge > 3600 {
		t.Fatal("unsafe production cookie")
	}
	w = httptest.NewRecorder()
	h.setNamedCookie(w, h.registrationCookieName(), accounts.Cookie{Token: raw, ExpiresAt: time.Now().Add(accounts.PendingLifetime)})
	c = w.Result().Cookies()[0]
	if c.Name != accountRegistrationCookieName || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode || c.MaxAge > 604800 {
		t.Fatal("unsafe production registration cookie")
	}
	r = httptest.NewRequestWithContext(t.Context(), "POST", "/", nil)
	r.AddCookie(&http.Cookie{Name: accountRegistrationCookieName, Value: raw})
	if h.registrationCookie(r) != raw {
		t.Fatal("registration cookie missing")
	}
	r.AddCookie(&http.Cookie{Name: accountRegistrationCookieName, Value: raw})
	if h.registrationCookie(r) != "" {
		t.Fatal("duplicate registration cookie accepted")
	}
	options := lifecycleHTTPOptions(t)
	options.Origin = "http://localhost:3000"
	local, err := newAccountHTTP(options)
	if err != nil {
		t.Fatal(err)
	}
	if local.secure || local.cookieName != accountDevCookieName || local.registrationCookieName() != accountDevRegistrationCookieName {
		t.Fatal("bad loopback cookie")
	}
	options.Origin = "http://evil.example"
	if _, err = newAccountHTTP(options); err == nil {
		t.Fatal("nonlocal insecure origin accepted")
	}
}

func TestVerificationOnlyAcceptsToken(t *testing.T) {
	h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
	for _, body := range []string{`{"token":"x","password":"a deliberately unique passphrase"}`, `{"token":"x","password":""}`, `{"token":"x","method":"google"}`} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/verification/confirm", strings.NewReader(body))
		r.Header.Set("Origin", "https://messeances.fr")
		r.Header.Set("X-Messeances-CSRF", "1")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"invalid_input"`) || len(w.Result().Cookies()) != 0 {
			t.Fatal("verification accepted client method or replacement password")
		}
	}
	w := httptest.NewRecorder()
	accountError(w, accounts.ErrVerificationBrowser)
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"verification_browser_required"`) {
		t.Fatal("missing original browser recovery contract")
	}
}

func TestAccountJSONDoesNotReplacePasswordCodepoints(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{{`"\ud800"`, false}, {`"\udc00"`, false}, {`"\ud800x"`, false}, {`"\ud800\u1234"`, false}, {`"\ud83d\ude00"`, true}, {`"\\ud800"`, true}, {`"\ufffd"`, true}} {
		r := httptest.NewRequestWithContext(t.Context(), "POST", "/", strings.NewReader(`{"password":`+tc.value+`}`))
		w := httptest.NewRecorder()
		var input accountCredentials
		if got := accountJSON(w, r, &input); got != tc.valid {
			t.Fatalf("%s: valid=%v", tc.value, got)
		}
	}
}

func TestAccountDatabaseOutageIsNotAnonymous(t *testing.T) {
	h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
	for _, path := range []string{"/api/v1/auth/session", "/api/v1/account"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", path, nil))
		if w.Code != 503 {
			t.Fatalf("outage became success: %d", w.Code)
		}
	}
}

func TestAccountIPLimitDoesNotTrustForgedForwarding(t *testing.T) {
	h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
	for i := 0; i < 11; i++ {
		r := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/auth/register", strings.NewReader(`{"email":"a@example.com","password":"a deliberate unique passphrase"}`))
		r.RemoteAddr = "203.0.113.5:1234"
		r.Header.Set("X-Forwarded-For", time.Now().String())
		r.Header.Set("Origin", "https://messeances.fr")
		r.Header.Set("X-Messeances-CSRF", "1")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i == 10 && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatal("forged forwarding bypassed bounded IP quota")
		}
	}
}
