package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	runtimeconfig "messeances/api/internal/config"
	"messeances/api/internal/httpapi"
)

func TestRuntimeWiresAccountGateWithoutProviders(t *testing.T) {
	var cfg runtimeconfig.Config
	cfg.Server.Origin = "http://localhost:3000"
	for _, enabled := range []bool{false, true} {
		cfg.Accounts.Enabled = enabled
		handler := newAPIHandler(nil, cfg, httpapi.AdminOptions{}, nil, nil, httpapi.ReadinessOptions{}, nil)
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
