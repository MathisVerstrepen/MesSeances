package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry            *prometheus.Registry
	httpRequests        *prometheus.CounterVec
	httpDuration        *prometheus.HistogramVec
	refreshes           *prometheus.CounterVec
	refreshDuration     *prometheus.HistogramVec
	revisions           *prometheus.GaugeVec
	syncRuns            *prometheus.CounterVec
	syncDuration        *prometheus.HistogramVec
	syncEnrichment      *prometheus.CounterVec
	syncLastSuccess     *prometheus.GaugeVec
	syncLastRecords     *prometheus.GaugeVec
	scheduleGenerated   prometheus.Gauge
	scheduleWindowStart prometheus.Gauge
	scheduleWindowEnd   prometheus.Gauge
	refreshLastSuccess  prometheus.Gauge
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	metrics := &Metrics{
		registry:            registry,
		httpRequests:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "messeances_http_requests_total", Help: "Completed HTTP requests."}, []string{"method", "route", "status"}),
		httpDuration:        prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "messeances_http_request_duration_seconds", Help: "HTTP request duration."}, []string{"method", "route"}),
		refreshes:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "messeances_schedule_refresh_total", Help: "Schedule refresh attempts."}, []string{"result", "stage", "reason"}),
		refreshDuration:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "messeances_schedule_refresh_duration_seconds", Help: "Schedule refresh duration."}, []string{"result"}),
		revisions:           prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "messeances_schedule_revision", Help: "Published schedule revisions."}, []string{"kind"}),
		syncRuns:            prometheus.NewCounterVec(prometheus.CounterOpts{Name: "messeances_sync_runs_total", Help: "Provider sync runs."}, []string{"provider", "result", "stage", "error_code"}),
		syncDuration:        prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "messeances_sync_duration_seconds", Help: "Provider sync duration."}, []string{"provider", "result"}),
		syncEnrichment:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "messeances_sync_enrichment_total", Help: "Provider sync enrichment outcomes."}, []string{"provider", "status"}),
		syncLastSuccess:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "messeances_sync_last_success_timestamp_seconds", Help: "Last successful provider sync timestamp."}, []string{"provider"}),
		syncLastRecords:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "messeances_sync_last_records", Help: "Records in last successful provider sync."}, []string{"provider", "kind"}),
		scheduleGenerated:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "messeances_schedule_generated_timestamp_seconds", Help: "Active schedule generation timestamp."}),
		scheduleWindowStart: prometheus.NewGauge(prometheus.GaugeOpts{Name: "messeances_schedule_window_start_timestamp_seconds", Help: "Active schedule window start timestamp."}),
		scheduleWindowEnd:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "messeances_schedule_window_end_timestamp_seconds", Help: "Exclusive active schedule window end timestamp."}),
		refreshLastSuccess:  prometheus.NewGauge(prometheus.GaugeOpts{Name: "messeances_schedule_refresh_last_success_timestamp_seconds", Help: "Last successful schedule refresh check timestamp."}),
	}
	registry.MustRegister(
		prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		metrics.httpRequests, metrics.httpDuration, metrics.refreshes, metrics.refreshDuration,
		metrics.revisions, metrics.syncRuns, metrics.syncDuration, metrics.syncEnrichment,
		metrics.syncLastSuccess, metrics.syncLastRecords,
		metrics.scheduleGenerated, metrics.scheduleWindowStart, metrics.scheduleWindowEnd, metrics.refreshLastSuccess,
	)
	return metrics
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveHTTP(method, route string, status int, duration time.Duration) {
	m.httpRequests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.httpDuration.WithLabelValues(method, route).Observe(duration.Seconds())
}

func (m *Metrics) ObserveScheduleRefresh(result, stage, reason string, duration time.Duration) {
	m.refreshes.WithLabelValues(result, stage, reason).Inc()
	m.refreshDuration.WithLabelValues(result).Observe(duration.Seconds())
}

func (m *Metrics) SetScheduleFreshness(generatedAt, windowStart, windowEnd time.Time) {
	m.scheduleGenerated.Set(float64(generatedAt.Unix()))
	m.scheduleWindowStart.Set(float64(windowStart.Unix()))
	m.scheduleWindowEnd.Set(float64(windowEnd.Unix()))
}

func (m *Metrics) SetScheduleRefreshLastSuccess(at time.Time) {
	m.refreshLastSuccess.Set(float64(at.Unix()))
}

func (m *Metrics) SetScheduleRevision(schedule, enrichment int64) {
	m.revisions.WithLabelValues("schedule").Set(float64(schedule))
	m.revisions.WithLabelValues("enrichment").Set(float64(enrichment))
}

func (m *Metrics) ObserveSync(provider, result, stage, errorCode, enrichmentStatus string, duration time.Duration, records map[string]int) {
	m.syncRuns.WithLabelValues(provider, result, stage, errorCode).Inc()
	m.syncDuration.WithLabelValues(provider, result).Observe(duration.Seconds())
	if result != "succeeded" {
		return
	}
	m.syncEnrichment.WithLabelValues(provider, enrichmentStatus).Inc()
	m.syncLastSuccess.WithLabelValues(provider).Set(float64(time.Now().Unix()))
	for kind, count := range records {
		m.syncLastRecords.WithLabelValues(provider, kind).Set(float64(count))
	}
}
