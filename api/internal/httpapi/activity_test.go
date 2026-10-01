package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type fakeActivityReader struct {
	calls int
	query schedule.TheaterActivityQuery
	ctx   context.Context
	err   error
}

func (f *fakeActivityReader) TheaterActivity(ctx context.Context, q schedule.TheaterActivityQuery) (schedule.TheaterActivity, error) {
	f.calls++
	f.query = q
	f.ctx = ctx
	return schedule.TheaterActivity{GeneratedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Timezone: schedule.Timezone, Theater: schedule.Theater{ID: q.Slug, Slug: q.Slug, AvailableDates: []string{}, AcceptedPasses: []string{}}, Coverage: schedule.ActivityCoverage{Completeness: "unknown", Bootstrap: "baseline", ReturnMinimumBreakDays: 28}, Items: []schedule.ActivityEvent{}, Limit: q.Limit}, f.err
}

func TestActivityHTTPContract(t *testing.T) {
	f := &fakeActivityReader{}
	h := NewHandlerWithOptions(nil, "", HandlerOptions{Activity: f})
	r := performRequest(t, h, "/api/v1/theaters/ugc-25/activity?limit=100")
	if r.Code != 200 || r.Header().Get("Cache-Control") != "no-store" || f.query.Slug != "ugc-25" || f.query.Limit != 100 {
		t.Fatal(r.Code, r.Body.String(), f.query)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(r.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 7 || string(fields["items"]) != "[]" || string(fields["next_cursor"]) != "null" || !strings.Contains(string(fields["coverage"]), `"history_started_at":null`) {
		t.Fatal(fields)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, "GET", "/api/v1/theaters/ugc-25/activity", nil))
	if !errors.Is(f.ctx.Err(), context.Canceled) {
		t.Fatal("context lost")
	}
}

func TestActivityHTTPValidationAndErrors(t *testing.T) {
	for _, raw := range []string{"limit=0", "limit=-1", "limit=101", "limit=01", "limit=1.0", "limit=+1", "limit=", "limit=2&limit=3", "cursor=", "cursor=x&cursor=y", "date=2026-10-01", "cursor=%FF", "cursor=%zz", "limit=1;x=y", strings.Repeat("x", 2049)} {
		f := &fakeActivityReader{}
		h := NewHandlerWithOptions(nil, "", HandlerOptions{Activity: f})
		r := performRequest(t, h, "/api/v1/theaters/ugc-25/activity?"+raw)
		if r.Code != 400 || f.calls != 0 || r.Header().Get("Cache-Control") != "no-store" || !strings.Contains(r.Body.String(), `"code":"invalid_query"`) {
			t.Fatal(raw, r.Code, r.Body.String(), f.calls)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{schedule.ErrHistoryUnavailable, 503, "history_unavailable"}, {schedule.ErrHistoryBusy, 503, "history_busy"}, {schedule.ErrHistoryQueryTimeout, 503, "history_query_timeout"}, {&schedule.NotFoundError{Message: "absent"}, 404, "not_found"}, {errors.New("private database detail"), 500, "internal_error"}} {
		f := &fakeActivityReader{err: tc.err}
		r := performRequest(t, NewHandlerWithOptions(nil, "", HandlerOptions{Activity: f}), "/api/v1/theaters/unknown/activity")
		if r.Code != tc.status || r.Header().Get("Cache-Control") != "no-store" || !strings.Contains(r.Body.String(), `"code":"`+tc.code+`"`) || strings.Contains(r.Body.String(), "private") {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	r := performRequest(t, NewHandlerWithOptions(nil, "", HandlerOptions{}), "/api/v1/theaters/ugc-25/activity")
	if r.Code != http.StatusServiceUnavailable {
		t.Fatal(r.Code)
	}
}
