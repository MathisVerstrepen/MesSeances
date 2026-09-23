package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"messeances/api/internal/accounts"
)

func TestGoogleCallbackQueryBounds(t *testing.T) {
	state, _, _ := accounts.NewToken(nil)
	for _, test := range []struct {
		query string
		valid bool
	}{
		{"state=" + state + "&code=synthetic&scope=openid+email&authuser=0&prompt=select_account", true},
		{"state=" + state + "&error=access_denied&error_description=private+provider+detail", true},
		{"state=" + state + "&code=one&code=two", false},
		{"state=" + state + "&code=one&state=" + state, false},
		{"state=" + state + "&code=one&return_url=https://evil.example", false},
		{"state=" + state + "&error=access_denied&code=one", false},
		{"state=invalid&code=one", false},
		{"state=" + state + "&code=" + strings.Repeat("x", 4097), false},
		{"state=" + state + "&code=one&scope=%zz", false},
	} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/google/callback?"+test.query, nil)
		_, _, ok := googleCallbackQuery(request)
		if ok != test.valid {
			t.Fatalf("callback validation mismatch (want %t)", test.valid)
		}
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
