package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsFamiliesUseExpectedBoundedLabels(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveScheduleRefresh("published", "none", "none", time.Second)
	metrics.SetScheduleRevision(7, 3)
	metrics.SetScheduleFreshness(time.Unix(100, 0), time.Unix(200, 0), time.Unix(300, 0))
	metrics.SetScheduleRefreshLastSuccess(time.Unix(400, 0))
	metrics.ObserveSync("ugc", "succeeded", "none", "none", "complete", time.Second, map[string]int{"showtimes": 12})
	metrics.ObserveSync("pathe", "succeeded", "none", "none", "degraded", time.Second, map[string]int{"showtimes": 7})
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	for _, expected := range []string{
		`messeances_schedule_refresh_total{reason="none",result="published",stage="none"} 1`,
		`messeances_schedule_revision{kind="schedule"} 7`,
		`messeances_schedule_revision{kind="enrichment"} 3`,
		`messeances_schedule_generated_timestamp_seconds 100`,
		`messeances_schedule_window_start_timestamp_seconds 200`,
		`messeances_schedule_window_end_timestamp_seconds 300`,
		`messeances_schedule_refresh_last_success_timestamp_seconds 400`,
		`messeances_sync_runs_total{error_code="none",provider="ugc",result="succeeded",stage="none"} 1`,
		`messeances_sync_enrichment_total{provider="ugc",status="complete"} 1`,
		`messeances_sync_last_records{kind="showtimes",provider="ugc"} 12`,
		`messeances_sync_runs_total{error_code="none",provider="pathe",result="succeeded",stage="none"} 1`,
		`messeances_sync_enrichment_total{provider="pathe",status="degraded"} 1`,
		`messeances_sync_last_records{kind="showtimes",provider="pathe"} 7`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in metrics", expected)
		}
	}
}

func TestSyncFailureMetricsExposeOnlyBoundedLabels(t *testing.T) {
	metrics := NewMetrics()
	for _, labels := range [][4]string{
		{"ugc", "failed", "client_creation", "client_creation_failed"},
		{"ugc", "failed", "provider_fetch", "provider_sync_failed"},
		{"ugc", "failed", "dataset_validation", "dataset_rejected"},
		{"ugc", "failed", "publication", "replacement_failed"},
		{"ugc", "succeeded", "none", "none"},
	} {
		metrics.ObserveSync(labels[0], labels[1], labels[2], labels[3], "degraded", time.Second, map[string]int{})
	}
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	for _, expected := range []string{
		`error_code="client_creation_failed",provider="ugc",result="failed",stage="client_creation"`,
		`error_code="provider_sync_failed",provider="ugc",result="failed",stage="provider_fetch"`,
		`error_code="dataset_rejected",provider="ugc",result="failed",stage="dataset_validation"`,
		`error_code="replacement_failed",provider="ugc",result="failed",stage="publication"`,
		`error_code="none",provider="ugc",result="succeeded",stage="none"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing bounded labels %q", expected)
		}
	}
	if strings.Contains(body, "synthetic-telemetry-secret") {
		t.Fatal("metric exposition leaked error cause")
	}
}
