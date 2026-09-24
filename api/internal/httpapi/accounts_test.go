package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"messeances/api/internal/accounts"
)

func TestDisabledAccountSessionIndependentOfReadinessAndAuth(t *testing.T) {
	handler := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{InternalSharedSecret: strings.Repeat("a", 64)})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/session", nil)
	request.Header.Set("Cookie", "messeances_admin_session=synthetic; __Host-messeances_session=synthetic")
	request.Header.Set("X-Messeances-Internal-Token", strings.Repeat("a", 64))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var got accounts.SessionView
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.State != accounts.StateAnonymous || got.Account != nil {
		t.Fatalf("session=%+v", got)
	}
	if response.Header().Get("Set-Cookie") != "" {
		t.Fatal("disabled accounts mutated cookies")
	}
	assertAccountHeaders(t, response)
}

func TestAccountRoutesFailClosed(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		handler := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: AccountOptions{Enabled: enabled}})
		for _, tc := range []struct{ method, path string }{
			{http.MethodPost, "/api/v1/auth/register"}, {http.MethodPost, "/api/v1/auth/login"},
			{http.MethodPost, "/api/v1/auth/logout"}, {http.MethodPost, "/api/v1/auth/google/start"},
			{http.MethodGet, "/api/v1/auth/google/callback?code=synthetic-secret&state=synthetic-state"},
			{http.MethodGet, "/api/v1/account"}, {http.MethodDelete, "/api/v1/account"},
			{http.MethodPost, "/api/v1/account/avatar"}, {http.MethodDelete, "/api/v1/account/avatar"}, {http.MethodGet, "/api/v1/account/avatar/1"},
		} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, strings.NewReader(`{}`)))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("%s status=%d", tc.path, response.Code)
			}
			if strings.Contains(response.Body.String(), "synthetic") || response.Header().Get("Set-Cookie") != "" {
				t.Fatal("request secret leaked or cookie set")
			}
			assertAccountHeaders(t, response)
		}
		if enabled {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/session", nil))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatal("enabled but uninstalled service reported anonymous success")
			}
		}
	}
}

func TestAccountBoundaryNoCORSIncludingErrors(t *testing.T) {
	handler := NewHandler(nil, "https://messeances.fr")
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodOptions, "/api/v1/auth/login", http.StatusMethodNotAllowed},
		{http.MethodPut, "/api/v1/account", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/account/missing", http.StatusNotFound},
		{http.MethodGet, "/api/v1/auth/session?extra=1", http.StatusBadRequest},
	} {
		request := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
		request.Header.Set("Origin", "https://messeances.fr")
		request.Header.Set("Access-Control-Request-Method", "POST")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s status=%d want=%d", tc.path, response.Code, tc.status)
		}
		if response.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("account CORS enabled")
		}
		assertAccountHeaders(t, response)
	}
	// Existing non-account CORS behavior stays untouched.
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	request.Header.Set("Origin", "https://messeances.fr")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Access-Control-Allow-Origin") != "https://messeances.fr" {
		t.Fatal("public regression")
	}
}

func assertAccountHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(response.Header().Get("Vary"), "Cookie") {
		t.Fatalf("missing private headers: %v", response.Header())
	}
}
