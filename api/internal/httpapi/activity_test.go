package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type fakeActivityReader struct {
	calls           int
	query           schedule.TheaterActivityQuery
	ctx             context.Context
	err             error
	previewTheaters []string
}

func (f *fakeActivityReader) TheaterActivity(ctx context.Context, q schedule.TheaterActivityQuery) (schedule.TheaterActivity, error) {
	f.calls++
	f.query = q
	f.ctx = ctx
	return schedule.TheaterActivity{GeneratedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Timezone: schedule.Timezone, Theater: schedule.Theater{ID: q.Slug, Slug: q.Slug, AvailableDates: []string{}, AcceptedPasses: []string{}}, Coverage: schedule.ActivityCoverage{Completeness: "unknown", Bootstrap: "baseline", ReturnMinimumBreakDays: 28}, Items: []schedule.ActivityEvent{}, Limit: q.Limit}, f.err
}

func (f *fakeActivityReader) ActivityPreview(_ context.Context, ids []string) ([]schedule.ActivityPreviewItem, error) {
	f.calls++
	f.previewTheaters = ids
	return []schedule.ActivityPreviewItem{}, f.err
}

func TestHomeActivityPreviewHTTP(t *testing.T) {
	f := &fakeActivityReader{}
	h := NewHandlerWithOptions(nil, "", HandlerOptions{Activity: f})
	post := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		return response
	}
	const path = "/api/v1/theaters/activity-preview"
	response := post(path, `["ugc-25","ugc-99"]`)
	if response.Code != 200 || response.Body.String() != "[]\n" || response.Header().Get("Cache-Control") != "no-store" || !slices.Equal(f.previewTheaters, []string{"ugc-25", "ugc-99"}) || f.calls != 1 {
		t.Fatal(response.Code, response.Body.String(), f)
	}
	for _, body := range []string{`null`, `{}`, `[null]`, `["ugc-25","ugc-25"]`, `["a b"]`, `["ugc-25"] []`} {
		if response := post(path, body); response.Code != 400 || f.calls != 1 {
			t.Fatal(body, response.Code, response.Body.String(), f.calls)
		}
	}
	if response := post(path+"?theaters=ugc-25", `[]`); response.Code != 400 || f.calls != 1 {
		t.Fatal(response.Code, response.Body.String(), f.calls)
	}
}

func TestWatchlistScreeningPreviewAccountBoundary(t *testing.T) {
	const path = "/api/v1/account/watchlist/next-screenings"
	h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
	for _, tc := range []struct {
		body, header, value string
		status              int
	}{
		{`["ugc-25"]`, "Origin", "https://example.com", 403},
		{`["ugc-25"]`, "Cookie", accountCookieName + "=invalid", 401},
		{`{"movie_slugs":["film-1"]}`, "Cookie", accountCookieName + "=invalid", 400},
	} {
		r := theaterPreferenceRequest(t, "POST", path, tc.body)
		r.Header.Set(tc.header, tc.value)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
		assertAccountHeaders(t, w)
	}
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
