package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type fakeHistoryReader struct {
	calls   int
	stats   schedule.StatisticsQuery
	options schedule.HistoryOptionsQuery
	err     error
	ctx     context.Context
}

func (f *fakeHistoryReader) HistoryStatistics(ctx context.Context, q schedule.StatisticsQuery) (schedule.HistoryStatistics, error) {
	f.calls++
	f.stats = q
	f.ctx = ctx
	return schedule.HistoryStatistics{Mode: "history"}, f.err
}
func (f *fakeHistoryReader) HistoryOptions(ctx context.Context, q schedule.HistoryOptionsQuery) (schedule.HistoryOptions, error) {
	f.calls++
	f.options = q
	f.ctx = ctx
	return schedule.HistoryOptions{Items: []schedule.HistoryOption{}, Selected: []schedule.HistoryOption{}}, f.err
}

func TestHistoryHTTPContract(t *testing.T) {
	f := &fakeHistoryReader{}
	handler := NewHandlerWithOptions(nil, "http://localhost:3000", HandlerOptions{History: f})
	response := performRequest(t, handler, "/api/v1/statistics/history?date=0001-01-01&date_to=9999-12-31&city=lille&city=lyon&city=lille&genre=+DRAME+&language=VF_SME&film=+film-1+&format=INFINITY_VISION")
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), `"mode":"history"`) {
		t.Fatal(response.Code, response.Body.String())
	}
	want := schedule.StatisticsQuery{Date: "0001-01-01", DateTo: "9999-12-31", City: []string{"lille", "lyon"}, Theater: []string{}, Genre: "drame", Language: "VF_SME", Film: "film-1", Format: "INFINITY_VISION"}
	if !reflect.DeepEqual(f.stats, want) {
		t.Fatal("parsed", f.stats)
	}
	response = performRequest(t, handler, "/api/v1/statistics/history/options?kind=city&q=+LILLE+&selected=lyon&selected=lyon&selected=absent")
	if response.Code != 200 || response.Body.String() != "{\"items\":[],\"selected\":[],\"has_more\":false}\n" {
		t.Fatal(response.Code, response.Body.String())
	}
	if !reflect.DeepEqual(f.options, schedule.HistoryOptionsQuery{Kind: "city", Q: "lille", Selected: []string{"lyon", "absent"}}) {
		t.Fatal(f.options)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequestWithContext(ctx, "GET", "/api/v1/statistics/history", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if !errors.Is(f.ctx.Err(), context.Canceled) {
		t.Fatal("context not propagated")
	}
}

func TestHistoryHTTPVOFContract(t *testing.T) {
	for _, raw := range []string{"language=VOF", "language=+VOF+", "language=" + strings.Repeat("+", 197) + "VOF", "language=VOF" + strings.Repeat("&", 4096-len("language=VOF"))} {
		f := &fakeHistoryReader{}
		h := NewHandlerWithOptions(nil, "", HandlerOptions{History: f})
		r := performRequest(t, h, "/api/v1/statistics/history?"+raw)
		if r.Code != http.StatusOK || r.Header().Get("Cache-Control") != "no-store" || f.calls != 1 || f.stats.Language != "VOF" {
			t.Fatal(raw, r.Code, r.Body.String(), f.calls, f.stats)
		}
	}
	for _, raw := range statisticsVOFInvalidQueries() {
		f := &fakeHistoryReader{}
		h := NewHandlerWithOptions(nil, "", HandlerOptions{History: f})
		r := performRequest(t, h, "/api/v1/statistics/history?"+raw)
		if r.Code != http.StatusBadRequest || r.Header().Get("Cache-Control") != "no-store" || f.calls != 0 || !strings.Contains(r.Body.String(), `"code":"invalid_query"`) {
			t.Fatal(raw, r.Code, r.Body.String(), f.calls)
		}
	}
}

func TestHistoryHTTPInvalidBounds(t *testing.T) {
	stats := []string{"date_to=2026-01-01", "date=0000-01-01", "date=10000-01-01", "date=2026-2-01", "date=2026-02-30", "date=2026-08-16&date_to=2026-08-15", "city=%FF", "city=%zz", "city=x;y=z", "unknown=x", "mode=history", "chain=other", "language=ALL", "language=VFSME", "format=imax", "city[]=lille"}
	for _, key := range []string{"date", "date_to", "chain", "language", "format", "genre", "pass"} {
		stats = append(stats, key+"=", key+"=x&"+key+"=x")
	}
	stats = append(stats, "format=infinity_vision", "format=Infinity+Vision", "format=invented", "format=ALL")
	for _, key := range []string{"city", "theater"} {
		stats = append(stats, key+"=x&"+key+"=", strings.Repeat(key+"=x&", 51), key+"="+url.QueryEscape(strings.Repeat("é", 100)+"x"))
	}
	options := []string{"", "kind=other", "kind=city&kind=city", "kind=city&q=", "kind=city&q=x&q=y", "kind=city&selected=", "kind=city&q=%FF", "kind=city&q=%zz", "kind=city&date=2026-01-01", "kind=city&selected=%FF", "kind=city&" + strings.Repeat("selected=x&", 51), "kind=city&q=" + url.QueryEscape(strings.Repeat("é", 100)+"x")}
	for path, queries := range map[string][]string{"/api/v1/statistics/history": stats, "/api/v1/statistics/history/options": options} {
		for _, q := range queries {
			t.Run(path+"/"+q[:min(50, len(q))], func(t *testing.T) {
				f := &fakeHistoryReader{}
				h := NewHandlerWithOptions(nil, "", HandlerOptions{History: f})
				r := performRequest(t, h, path+"?"+q)
				if r.Code != 400 || f.calls != 0 || !strings.Contains(r.Body.String(), `"code":"invalid_query"`) || r.Header().Get("Cache-Control") != "no-store" {
					t.Fatal(r.Code, r.Body.String(), f.calls)
				}
			})
		}
		base := "city=lille"
		if strings.HasSuffix(path, "options") {
			base = "kind=city"
		}
		for _, size := range []int{4096, 4097} {
			f := &fakeHistoryReader{}
			h := NewHandlerWithOptions(nil, "", HandlerOptions{History: f})
			r := performRequest(t, h, path+"?"+base+strings.Repeat("&", size-len(base)))
			want := 200
			if size == 4097 {
				want = 400
			}
			if r.Code != want {
				t.Fatal("raw bound", size, r.Code)
			}
		}
	}
	for _, key := range []string{"city", "theater", "genre", "pass"} {
		for _, n := range []int{200, 201} {
			_, err := parseHistoryStatisticsQuery(key + "=" + strings.Repeat("x", n))
			if (err != nil) != (n > 200) {
				t.Fatal("decoded bound", key, n, err)
			}
		}
	}
	for _, key := range []string{"city", "theater"} {
		for _, n := range []int{50, 51} {
			_, err := parseHistoryStatisticsQuery(strings.Repeat(key+"=x&", n))
			if (err != nil) != (n > 50) {
				t.Fatal("occurrence bound", key, n, err)
			}
		}
	}
	for _, n := range []int{50, 51} {
		_, err := parseHistoryOptionsQuery("kind=city&" + strings.Repeat("selected=x&", n))
		if (err != nil) != (n > 50) {
			t.Fatal("selected bound", n, err)
		}
	}
	for _, q := range []string{"kind=city&q=%25", "kind=city&q=_", "kind=city&q=" + url.QueryEscape(strings.Repeat("é", 100))} {
		if _, err := parseHistoryOptionsQuery(q); err != nil {
			t.Fatal("valid option", err)
		}
	}
}

func TestHistoryHTTPErrorsAndRateLimit(t *testing.T) {
	for _, path := range []string{"/api/v1/statistics/history", "/api/v1/statistics/history/options?kind=city"} {
		for _, tc := range []struct {
			err    error
			code   string
			status int
		}{{schedule.ErrHistoryUnavailable, "history_unavailable", 503}, {schedule.ErrHistoryBusy, "history_busy", 503}, {schedule.ErrHistoryQueryTimeout, "history_query_timeout", 503}, {errors.New("SELECT secret FROM raw_provider_response https://private"), "internal_error", 500}} {
			r := performRequest(t, NewHandlerWithOptions(nil, "", HandlerOptions{History: &fakeHistoryReader{err: tc.err}}), path)
			if r.Code != tc.status || !strings.Contains(r.Body.String(), `"code":"`+tc.code+`"`) || strings.Contains(r.Body.String(), "secret") || r.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(r.Code, r.Body.String())
			}
		}
		r := performRequest(t, NewHandlerWithOptions(nil, "", HandlerOptions{}), path)
		if r.Code != 503 || !strings.Contains(r.Body.String(), "history_unavailable") {
			t.Fatal("nil reader", r.Code)
		}
		h := NewHandlerWithOptions(nil, "", HandlerOptions{History: &fakeHistoryReader{}})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, nil))
		if rec.Code != 405 || !strings.Contains(rec.Body.String(), "method_not_allowed") {
			t.Fatal("method", rec.Code)
		}
	}
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	h := NewHandlerWithOptions(nil, "", HandlerOptions{History: &fakeHistoryReader{}, RateLimitClock: func() time.Time { return now }})
	for i := 0; i < expensiveReadBurst; i++ {
		path := "/api/v1/statistics/history"
		if i%2 == 0 {
			path += "/options?kind=city"
		}
		r := requestFrom(t, h, "GET", path, "192.0.2.1:1234", nil)
		if r.Code != 200 {
			t.Fatal("burst", i, r.Code)
		}
	}
	assertRateLimited(t, requestFrom(t, h, "GET", "/api/v1/statistics/history", "192.0.2.1:1234", nil), "1")
}
