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
	metrics.ObserveScheduleRefresh("published", time.Second)
	metrics.SetScheduleRevision(7, 3)
	metrics.ObserveSync("ugc", "succeeded", "none", "complete", time.Second, map[string]int{"showtimes": 12})
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	for _, expected := range []string{
		`messeances_schedule_refresh_total{result="published"} 1`,
		`messeances_schedule_revision{kind="schedule"} 7`,
		`messeances_schedule_revision{kind="enrichment"} 3`,
		`messeances_sync_runs_total{error_code="none",provider="ugc",result="succeeded"} 1`,
		`messeances_sync_enrichment_total{provider="ugc",status="complete"} 1`,
		`messeances_sync_last_records{kind="showtimes",provider="ugc"} 12`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in metrics", expected)
		}
	}
}
