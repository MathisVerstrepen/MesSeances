package observability

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestHTTPMiddlewareUsesBoundedLabelsAndRedactsRequestData(t *testing.T) {
	secret := "synthetic-secret"
	var logs bytes.Buffer
	metrics := NewMetrics()
	router := chi.NewRouter()
	router.Use(HTTPMiddleware(NewLogger(&logs), metrics))
	router.Get("/items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/items/"+secret+"?token="+secret, nil)
	request.Header.Set("Authorization", "Bearer "+secret)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || strings.Contains(logs.String(), secret) || !strings.Contains(logs.String(), `"route":"/items/{id}"`) {
		t.Fatalf("status=%d logs=%q", response.Code, logs.String())
	}

	scrape := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := scrape.Body.String()
	if !strings.Contains(body, `messeances_http_requests_total{method="GET",route="/items/{id}",status="204"} 1`) || strings.Contains(body, secret) {
		t.Fatalf("metrics=%q", body)
	}
}

func TestHTTPMiddlewareNormalizesUnknownMethodAndUnmatchedRoute(t *testing.T) {
	metrics := NewMetrics()
	router := chi.NewRouter()
	router.Use(HTTPMiddleware(NewLogger(&bytes.Buffer{}), metrics))
	router.Get("/known", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/missing", nil))
	scrape := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(scrape.Body.String(), `messeances_http_requests_total{method="OTHER",route="unmatched",status="404"} 1`) {
		t.Fatalf("metrics=%q", scrape.Body.String())
	}
}
