package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"messeances/api/internal/accounts"
	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/enrichment"
	"messeances/api/internal/httpapi"
)

func TestRuntimeWiresAccountGateWithoutProviders(t *testing.T) {
	var cfg runtimeconfig.Config
	cfg.Server.Origin = "http://localhost:3000"
	for _, enabled := range []bool{false, true} {
		cfg.Accounts.Enabled = enabled
		handler := newAPIHandler(nil, cfg, httpapi.AdminOptions{}, nil, nil, nil, httpapi.ReadinessOptions{}, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/auth/session", nil))
		want := http.StatusOK
		if enabled {
			want = http.StatusServiceUnavailable
		}
		if response.Code != want {
			t.Fatalf("enabled=%t status=%d want=%d", enabled, response.Code, want)
		}
		health := httptest.NewRecorder()
		handler.ServeHTTP(health, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
		if health.Code != http.StatusOK {
			t.Fatal("account gate affected health")
		}
	}
}

type runtimeAdminAccounts struct{ calls int }

func (s *runtimeAdminAccounts) List(_ context.Context, query accounts.AdminAccountsQuery) (accounts.AdminAccountsPage, error) {
	s.calls++
	return accounts.AdminAccountsPage{Items: []accounts.AdminAccount{}, Total: 0, Limit: query.Limit, Offset: query.Offset}, nil
}

func TestRuntimeWiresAdminAccountsIndependentlyOfProviders(t *testing.T) {
	var cfg runtimeconfig.Config
	cfg.Server.Origin = "http://localhost:3000"
	list := &runtimeAdminAccounts{}
	options := httpapi.AdminOptions{
		Password: "fixture-password", SessionSecret: "fixture-secret",
		Reviews: enrichment.NewReviewService(nil, nil, nil), Accounts: list,
	}
	for _, enabled := range []bool{false, true} {
		cfg.Accounts.Enabled = enabled
		handler := newAPIHandler(nil, cfg, options, nil, nil, nil, httpapi.ReadinessOptions{}, nil)
		login := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/login", strings.NewReader(`{"password":"fixture-password"}`))
		login.Header.Set("Origin", cfg.Server.Origin)
		login.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, login)
		if response.Code != http.StatusOK || len(response.Result().Cookies()) != 1 {
			t.Fatalf("runtime admin login failed: %d", response.Code)
		}
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/accounts", nil)
		request.AddCookie(response.Result().Cookies()[0])
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"items":[],"total":0,"limit":50,"offset":0}` {
			t.Fatalf("account providers affected admin listing: enabled=%t status=%d body=%s", enabled, response.Code, response.Body.String())
		}
	}
	if list.calls != 2 {
		t.Fatal("runtime admin listing dependency lost")
	}
}
