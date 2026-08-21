package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestNewLoggerWritesJSONAtInfo(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output)
	logger.Debug("hidden")
	logger.Info("visible", "component", "test")
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if record["msg"] != "visible" || record["level"] != "INFO" || record["component"] != "test" {
		t.Fatalf("unexpected record: %#v", record)
	}
	if strings.Contains(output.String(), "hidden") {
		t.Fatal("debug record was emitted")
	}
}

func TestHTTPMiddlewareUsesBoundedRouteAndMethodLabels(t *testing.T) {
	var logs bytes.Buffer
	metrics := NewMetrics()
	router := chi.NewRouter()
	router.Use(HTTPMiddleware(slog.New(slog.NewJSONHandler(&logs, nil)), metrics))
	router.Get("/movies/{slug}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("TRACE", "/movies/secret-movie", nil))

	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `messeances_http_requests_total{method="OTHER",route="unmatched",status="405"} 1`) {
		t.Fatalf("missing bounded metric: %s", body)
	}
	if strings.Contains(body, "secret-movie") || strings.Contains(logs.String(), "secret-movie") {
		t.Fatal("concrete path leaked")
	}
}
