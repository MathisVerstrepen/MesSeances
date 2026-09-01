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
	requestID := "0123456789abcdef0123456789abcdef"
	remoteAddress := "192.0.2.77"
	var logs bytes.Buffer
	metrics := NewMetrics()
	router := chi.NewRouter()
	router.Use(HTTPMiddleware(NewLogger(&logs), metrics))
	router.Get("/items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	ctx := WithHTTPRequestMetadata(t.Context(), requestID, RateLimitKeyForwardedClient)
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/items/"+secret+"?token="+secret, nil)
	request.RemoteAddr = remoteAddress + ":1234"
	request.Header.Set("Authorization", "Bearer "+secret)
	request.Header.Set("X-Forwarded-For", remoteAddress)
	request.Header.Set("X-Messeances-Internal-Token", strings.Repeat("a", 64))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	logOutput := logs.String()
	if response.Code != http.StatusNoContent || !strings.Contains(logOutput, `"route":"/items/{id}"`) || !strings.Contains(logOutput, `"request_id":"`+requestID+`"`) || !strings.Contains(logOutput, `"rate_limit_key_class":"forwarded_client"`) {
		t.Fatalf("status=%d logs=%q", response.Code, logs.String())
	}

	scrape := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(scrape, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	body := scrape.Body.String()
	if !strings.Contains(body, `messeances_http_requests_total{method="GET",route="/items/{id}",status="204"} 1`) {
		t.Fatalf("metrics=%q", body)
	}
	for _, forbidden := range []string{secret, remoteAddress, strings.Repeat("a", 64), "Authorization", "X-Forwarded-For", "X-Messeances-Internal-Token"} {
		if strings.Contains(logOutput, forbidden) || strings.Contains(body, forbidden) {
			t.Fatalf("output contains forbidden marker %q: logs=%q metrics=%q", forbidden, logOutput, body)
		}
	}
}

func TestHTTPMetadataBoundsUnexpectedValues(t *testing.T) {
	var logs bytes.Buffer
	router := chi.NewRouter()
	router.Use(HTTPMiddleware(NewLogger(&logs), NewMetrics()))
	router.Get("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	ctx := WithHTTPRequestMetadata(t.Context(), "sensitive-invalid-request-id", RateLimitKeyClass("sensitive-invalid-class"))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil))
	if strings.Contains(logs.String(), "sensitive") || !strings.Contains(logs.String(), `"request_id":"unknown"`) || !strings.Contains(logs.String(), `"rate_limit_key_class":"unknown_peer"`) {
		t.Fatalf("logs=%q", logs.String())
	}
}

func TestHTTPMiddlewareNormalizesUnknownMethodAndUnmatchedRoute(t *testing.T) {
	metrics := NewMetrics()
	router := chi.NewRouter()
	router.Use(HTTPMiddleware(NewLogger(&bytes.Buffer{}), metrics))
	router.Get("/known", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/missing", nil))
	scrape := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(scrape, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if !strings.Contains(scrape.Body.String(), `messeances_http_requests_total{method="OTHER",route="unmatched",status="404"} 1`) {
		t.Fatalf("metrics=%q", scrape.Body.String())
	}
}
