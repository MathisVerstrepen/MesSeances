package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accounts"
	"messeances/api/internal/enrichment"
)

type adminAccountsStub struct {
	page  accounts.AdminAccountsPage
	err   error
	query accounts.AdminAccountsQuery
	calls int
}

func (s *adminAccountsStub) List(_ context.Context, query accounts.AdminAccountsQuery) (accounts.AdminAccountsPage, error) {
	s.query = query
	s.calls++
	page := s.page
	page.Limit, page.Offset = query.Limit, query.Offset
	return page, s.err
}

func adminAccountsTestHandler(t *testing.T, list AdminAccountsLister, now func() time.Time) http.Handler {
	t.Helper()
	return NewHandlerWithOptions(nil, "http://localhost:3000", HandlerOptions{
		// Public account providers deliberately absent and feature disabled.
		Admin: AdminOptions{Password: "fixture-password", SessionSecret: "fixture-secret", Reviews: enrichment.NewReviewService(adminReviewStore{}, nil, now), Accounts: list, Now: now},
	})
}

func assertAdminAccountsPrivacy(t *testing.T, headers http.Header) {
	t.Helper()
	for key, want := range map[string]string{"Cache-Control": "no-store", "Referrer-Policy": "no-referrer", "X-Robots-Tag": "noindex, nofollow"} {
		if headers.Get(key) != want {
			t.Fatalf("privacy header %s=%q want=%q", key, headers.Get(key), want)
		}
	}
}

func TestAdminAccountsRequiresOnlyAdminAuthority(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	stub := &adminAccountsStub{}
	handler := adminAccountsTestHandler(t, stub, func() time.Time { return now })
	adminCookie := loginAdmin(t, handler, "fixture-password")
	for _, cookie := range []*http.Cookie{
		nil,
		{Name: adminCookieName, Value: "invalid"},
		{Name: accountCookieName, Value: adminCookie.Value},
		{Name: accountDevCookieName, Value: adminCookie.Value},
	} {
		response := adminRequest(handler, http.MethodGet, "/api/v1/admin/accounts", "", "", cookie)
		assertAPIError(t, response, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
		assertAdminAccountsPrivacy(t, response.Header())
	}
	if stub.calls != 0 {
		t.Fatal("unauthorized request reached listing")
	}
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/accounts", "", "", adminCookie)
	if response.Code != http.StatusOK || stub.calls != 1 || stub.query != (accounts.AdminAccountsQuery{Limit: 50}) {
		t.Fatalf("admin read failed while accounts disabled: %d %s", response.Code, response.Body.String())
	}
	assertAdminAccountsPrivacy(t, response.Header())
	now = now.Add(adminSessionTTL)
	response = adminRequest(handler, http.MethodGet, "/api/v1/admin/accounts", "", "", adminCookie)
	assertAPIError(t, response, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	if stub.calls != 1 {
		t.Fatal("expired session reached listing")
	}
	unconfigured := NewHandler(nil, "http://localhost:3000")
	response = adminRequest(unconfigured, http.MethodGet, "/api/v1/admin/accounts", "", "", adminCookie)
	assertAPIError(t, response, http.StatusServiceUnavailable, "admin_unavailable", "Service administrateur indisponible.")
	assertAdminAccountsPrivacy(t, response.Header())
}

func TestAdminAccountsStrictQuery(t *testing.T) {
	stub := &adminAccountsStub{}
	handler := adminAccountsTestHandler(t, stub, nil)
	cookie := loginAdmin(t, handler, "fixture-password")
	for _, tc := range []struct {
		query string
		want  accounts.AdminAccountsQuery
	}{
		{"", accounts.AdminAccountsQuery{Limit: 50}},
		{"limit=1&offset=0", accounts.AdminAccountsQuery{Limit: 1}},
		{"limit=100&offset=2147483647", accounts.AdminAccountsQuery{Limit: 100, Offset: 2147483647}},
		{"offset=50", accounts.AdminAccountsQuery{Limit: 50, Offset: 50}},
	} {
		response := adminRequest(handler, http.MethodGet, "/api/v1/admin/accounts?"+tc.query, "", "", cookie)
		if response.Code != http.StatusOK || stub.query != tc.want {
			t.Fatalf("query=%s status=%d forwarded=%+v", tc.query, response.Code, stub.query)
		}
	}
	for _, query := range []string{
		"limit=0", "limit=-1", "limit=101", "limit=", "limit=%20", "limit=1.5", "limit=x",
		"limit=2147483648", "offset=-1", "offset=2147483648", "offset=99999999999999999999999999999", "offset=",
		"limit=1&limit=2", "offset=0&offset=1", "limit=50&offset=0&email=fixture%40example.com",
		"unknown=1", "Limit=50", "offset=%ZZ", "limit=50;offset=0", "=1",
	} {
		calls := stub.calls
		response := adminRequest(handler, http.MethodGet, "/api/v1/admin/accounts?"+query, "", "", cookie)
		assertAPIError(t, response, http.StatusBadRequest, "invalid_query", "Pagination des comptes invalide.")
		assertAdminAccountsPrivacy(t, response.Header())
		if stub.calls != calls {
			t.Fatalf("invalid query reached listing: %s", query)
		}
	}
}

func TestAdminAccountsResponseContract(t *testing.T) {
	username := "fixture_user"
	verified := "2026-10-01T12:00:00Z"
	stub := &adminAccountsStub{page: accounts.AdminAccountsPage{Total: 200, Items: []accounts.AdminAccount{{
		Email: "fixture@example.com", Username: &username, State: accounts.StateComplete,
		CreatedAt: "2026-09-01T12:00:00Z", EmailVerifiedAt: &verified, HasPassword: true, GoogleLinked: true,
	}}}}
	handler := adminAccountsTestHandler(t, stub, nil)
	cookie := loginAdmin(t, handler, "fixture-password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/accounts?limit=50&offset=100", "", "", cookie)
	want := `{"items":[{"email":"fixture@example.com","username":"fixture_user","state":"complete","created_at":"2026-09-01T12:00:00Z","email_verified_at":"2026-10-01T12:00:00Z","has_password":true,"google_linked":true}],"total":200,"limit":50,"offset":100}`
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != want {
		t.Fatalf("response contract mismatch: %d %s", response.Code, response.Body.String())
	}
	stub.page.Items = nil
	response = adminRequest(handler, http.MethodGet, "/api/v1/admin/accounts?offset=200", "", "", cookie)
	if strings.TrimSpace(response.Body.String()) != `{"items":[],"total":200,"limit":50,"offset":200}` {
		t.Fatalf("empty page lost total or array: %s", response.Body.String())
	}
	stub.page.Items = []accounts.AdminAccount{{Email: "pending@example.com", State: accounts.StatePendingEmail, CreatedAt: "2026-10-01T12:00:00Z"}}
	response = adminRequest(handler, http.MethodGet, "/api/v1/admin/accounts", "", "", cookie)
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || len(page.Items[0]) != 7 || page.Items[0]["username"] != nil || page.Items[0]["email_verified_at"] != nil {
		t.Fatalf("minimal nullable DTO failed: %s %v", response.Body.String(), err)
	}
}

func TestAdminAccountsUnavailableAndReadOnly(t *testing.T) {
	for _, stub := range []*adminAccountsStub{nil, {err: errors.New("SQL fixture@example.com token-secret")}, {err: accounts.ErrInvalidInput}} {
		var list AdminAccountsLister
		if stub != nil {
			list = stub
		}
		handler := adminAccountsTestHandler(t, list, nil)
		cookie := loginAdmin(t, handler, "fixture-password")
		response := adminRequest(handler, http.MethodGet, "/api/v1/admin/accounts", "", "", cookie)
		if stub != nil && errors.Is(stub.err, accounts.ErrInvalidInput) {
			assertAPIError(t, response, http.StatusBadRequest, "invalid_query", "Pagination des comptes invalide.")
		} else {
			assertAPIError(t, response, http.StatusServiceUnavailable, "admin_accounts_unavailable", "Les comptes sont indisponibles.")
		}
		assertAdminAccountsPrivacy(t, response.Header())
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			response := adminRequest(handler, method, "/api/v1/admin/accounts", "", "http://localhost:3000", cookie)
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("mutation route registered: method=%s status=%d", method, response.Code)
			}
		}
		if stub != nil && stub.calls != 1 {
			t.Fatal("unsupported method reached listing")
		}
	}
}
