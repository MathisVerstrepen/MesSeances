package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestStatisticsHTTPContract(t *testing.T) {
	for _, test := range []struct {
		query  string
		totals schedule.StatisticsTotals
	}{
		{"", schedule.StatisticsTotals{Showtimes: 3, Movies: 2, Theaters: 3, Cities: 3}},
		{"city=removed", schedule.StatisticsTotals{}},
		{"date=2026-09-01", schedule.StatisticsTotals{}},
		{"city=lille&genre=drame&language=VOSTFR&format=SCREENX&theater=ugc-25&chain=ugc&pass=UGC_ILLIMITE", schedule.StatisticsTotals{Showtimes: 1, Movies: 1, Theaters: 1, Cities: 1}},
		{"city=lille", schedule.StatisticsTotals{Showtimes: 1, Movies: 1, Theaters: 1, Cities: 1}},
		{"theater=ugc-26", schedule.StatisticsTotals{Showtimes: 1, Movies: 1, Theaters: 1, Cities: 1}},
		{"city=lille&city=lyon", schedule.StatisticsTotals{Showtimes: 2, Movies: 2, Theaters: 2, Cities: 2}},
		{"theater=ugc-25&theater=ugc-26", schedule.StatisticsTotals{Showtimes: 2, Movies: 1, Theaters: 2, Cities: 2}},
		{"city=lille&city=lyon&theater=ugc-25&theater=ugc-26", schedule.StatisticsTotals{Showtimes: 1, Movies: 1, Theaters: 1, Cities: 1}},
		{"city=lille&city=lyon&theater=ugc-25&theater=ugc-99&format=4DX", schedule.StatisticsTotals{Showtimes: 1, Movies: 1, Theaters: 1, Cities: 1}},
		{"city=lille&city=+lille+&theater=ugc-25&theater=ugc-25", schedule.StatisticsTotals{Showtimes: 1, Movies: 1, Theaters: 1, Cities: 1}},
		{"city=removed&city=lille&theater=gone&theater=ugc-25", schedule.StatisticsTotals{Showtimes: 1, Movies: 1, Theaters: 1, Cities: 1}},
		{"city=removed&city=gone", schedule.StatisticsTotals{}},
		{"theater=removed&theater=gone", schedule.StatisticsTotals{}},
		{"city=lille&theater=ugc-26&theater=ugc-99", schedule.StatisticsTotals{}},
		{"city=lille,lyon", schedule.StatisticsTotals{}},
		{"theater=ugc-25,ugc-26", schedule.StatisticsTotals{}},
	} {
		t.Run(test.query, func(t *testing.T) {
			response := performRequest(t, testHandler(t), "/api/v1/statistics?"+test.query)
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body)
			}
			var result schedule.Statistics
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Heatmap) != 168 || len(result.Runtimes) != 4 || result.Timezone != "Europe/Paris" || result.Coverage.Completeness != "unknown" {
				t.Fatalf("result=%+v", result)
			}
			if result.Totals != test.totals {
				t.Fatalf("totals=%+v want=%+v", result.Totals, test.totals)
			}
			if len(result.Options.Cities) != 3 || len(result.Options.Theaters) != 3 {
				t.Fatalf("filtered inventory=%+v", result.Options)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &object); err != nil {
				t.Fatal(err)
			}
			keys := []string{"generated_at", "timezone", "range", "coverage", "options", "totals", "top_movies", "heatmap", "versions", "formats", "genres", "runtimes", "local", "concentration"}
			if len(object) != len(keys) {
				t.Fatalf("response properties=%v", object)
			}
			for _, key := range keys {
				if len(object[key]) == 0 {
					t.Errorf("missing %s", key)
				}
			}
			if strings.Contains(response.Body.String(), `:null`) && !strings.Contains(response.Body.String(), `"intersection":null`) {
				t.Fatal("unexpected null response field")
			}
		})
	}
}

func TestStatisticsVOFHTTPContract(t *testing.T) {
	data := fixtureDataset(t)
	data.PublicMovies = []schedule.PublicMovieRecord{
		{ID: 1, Title: "French", TMDBID: 101, OriginalLanguage: "fr"},
		{ID: 2, Title: "English", TMDBID: 102, OriginalLanguage: "en"},
		{ID: 3, Title: "Unknown"},
	}
	seed := data.Showtimes[0]
	data.Showtimes = nil
	for i, tc := range []struct {
		movie    int64
		language schedule.Language
	}{{1, schedule.LanguageVF}, {2, schedule.LanguageVF}, {3, schedule.LanguageVF}, {1, schedule.LanguageVFSME}, {1, schedule.LanguageVFSTF}, {1, schedule.LanguageVO}, {1, schedule.LanguageVOSTFR}, {1, ""}} {
		showing := seed
		showing.ID, showing.ProviderShowingID = fmt.Sprintf("ugc-showing-%d", 900+i), fmt.Sprint(900+i)
		showing.Movie.PublicMovieID, showing.Language = tc.movie, tc.language
		data.Showtimes = append(data.Showtimes, showing)
	}
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data)}, schedule.ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		raw, language string
		count         int
	}{
		{"", "", 8}, {"language=VOF", "VOF", 1}, {"language=+VOF+", "VOF", 1},
		{"language=" + strings.Repeat("+", 197) + "VOF", "VOF", 1},
		{"language=VOF" + strings.Repeat("&", 4096-len("language=VOF")), "VOF", 1},
		{"language=VF", "VF", 2}, {"language=VF_SME", "VF_SME", 1}, {"language=VFSTF", "VFSTF", 1},
		{"language=unknown", "unknown", 1}, {"language=VOF&film=film-2", "VOF", 0},
	} {
		t.Run(tc.raw[:min(60, len(tc.raw))], func(t *testing.T) {
			r := performRequest(t, NewHandler(service, ""), "/api/v1/statistics?"+tc.raw)
			var got schedule.Statistics
			if r.Code != http.StatusOK || r.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(r.Body.Bytes(), &got) != nil {
				t.Fatal(r.Code, r.Header(), r.Body.String())
			}
			if got.Totals.Showtimes != tc.count || !reflect.DeepEqual(got.Options.Languages, []string{"VF", "VFSTF", "VF_SME", "VO", "VOF", "VOSTFR", "unknown"}) {
				t.Fatal(got.Totals, got.Options.Languages)
			}
			sum := 0
			for _, bucket := range got.Versions {
				sum += bucket.Count
				label := bucket.Value
				if label == "unknown" {
					label = "Non renseigné"
				}
				if bucket.Label != label || tc.language != "" && (bucket.Value != tc.language || bucket.Count != tc.count) {
					t.Fatal("bucket", bucket)
				}
			}
			if sum != got.Totals.Showtimes || tc.count == 0 && len(got.Versions) != 0 {
				t.Fatal("version totals", got.Versions, got.Totals)
			}
		})
	}
	for _, raw := range statisticsVOFInvalidQueries() {
		r := performRequest(t, NewHandler(service, ""), "/api/v1/statistics?"+raw)
		if r.Code != http.StatusBadRequest || !strings.Contains(r.Body.String(), `"code":"invalid_query"`) || r.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(raw, r.Code, r.Body.String())
		}
	}
}

func statisticsVOFInvalidQueries() []string {
	return []string{
		"language=vof", "language=ORIGINAL", "language=ALL", "language=VOF,VF", "language=VOF%7CVOSTFR",
		"language=VOF&language=VOF", "language=", "language=+", "language=%zz", "language=%FF", "language=VOF%00",
		"language=" + strings.Repeat("+", 198) + "VOF",
		"language=VOF" + strings.Repeat("&", 4097-len("language=VOF")),
	}
}

func TestInfinityVisionStatisticsTransport(t *testing.T) {
	for _, tc := range []struct {
		query string
		count int
	}{{"", 2}, {"format=INFINITY_VISION", 1}, {"format=ICE", 1}, {"format=unknown", 0}} {
		r := performRequest(t, infinityVisionHandler(t), "/api/v1/statistics?"+tc.query)
		var got schedule.Statistics
		if r.Code != http.StatusOK || json.Unmarshal(r.Body.Bytes(), &got) != nil || got.Totals.Showtimes != tc.count {
			t.Fatal(r.Code, r.Body.String())
		}
		if !slices.Contains(got.Options.Formats, "INFINITY_VISION") || !slices.Contains(got.Options.Formats, "ICE") {
			t.Fatal(got.Options.Formats)
		}
		for _, bucket := range got.Formats {
			if bucket.Value != "INFINITY_VISION" && bucket.Value != "ICE" || bucket.Count != 1 {
				t.Fatal(got.Formats)
			}
		}
	}
	for _, format := range []string{"", "infinity_vision", "Infinity+Vision", "invented", "ALL"} {
		r := performRequest(t, infinityVisionHandler(t), "/api/v1/statistics?format="+format)
		if r.Code != http.StatusBadRequest {
			t.Fatal(format, r.Code, r.Body.String())
		}
	}
}

func TestStatisticsHTTPRejectsInvalidQuery(t *testing.T) {
	queries := []string{
		"city=%zz", "city=lille&city=%zz", "theater=ugc-25&theater=%zz", "city=lille;theater=ugc-25", "unsupported=1", "city[]=lille", "theater[]=ugc-25", "date=", "date_to=", "city=", "city=+", "date=2026-08-15&date=2026-08-15", "language=VF&language=VO",
		"chain=other", "language=ALL", "language=vf", "format=ALL", "format=imax", "language=VF,VOSTFR", "date_to=2026-08-16", "date=2026-08-14", "date=2026-02-30", "date=2026-8-15", "date=2026-08-15T00:00:00Z", "date=+2026-08-15", "date=2026-08-15&date_to=2026-08-14", "date=2026-08-15&date_to=2026-09-15", "date=2026-08-15&date_to=invalid",
		"city=" + strings.Repeat("x", 201), "pass=" + url.QueryEscape(strings.Repeat("é", 101)), "city=" + strings.Repeat("x", 4096),
	}
	for key, value := range map[string]string{"date": "2026-08-15", "date_to": "2026-08-15", "chain": "ugc", "language": "VF", "format": "2D", "genre": "drame", "pass": "UGC_ILLIMITE"} {
		queries = append(queries, key+"="+value+"&"+key+"="+value)
	}
	for _, key := range []string{"city", "theater"} {
		queries = append(queries,
			key+"=known&"+key+"=", key+"=known&"+key+"=+", key+"=known&"+key,
			strings.Repeat(key+"=x&", 51),
			key+"="+url.QueryEscape(strings.Repeat("é", 100)+"x"),
			key+"="+url.QueryEscape("known"+strings.Repeat(" ", 196)),
		)
	}
	for _, query := range queries {
		t.Run(query[:min(80, len(query))], func(t *testing.T) {
			response := performRequest(t, testHandler(t), "/api/v1/statistics?"+query)
			var result errorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusBadRequest || result.Error.Code != "invalid_query" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
	for _, key := range []string{"date", "date_to", "chain", "language", "format", "genre", "pass"} {
		for _, raw := range []string{key + "=", key + "=x&" + key + "=y"} {
			if _, err := parseStatisticsQuery(raw); err == nil {
				t.Errorf("accepted %q", raw)
			}
		}
	}
}

func TestStatisticsHTTPParsingScalarsAndBounds(t *testing.T) {
	query, err := parseStatisticsQuery("city=+lille+&theater=ugc-25&chain=ugc&language=VF_SME&format=LASER_ULTRA&genre=" + url.QueryEscape(" Comédie ") + "&pass=" + url.QueryEscape("CINÉ PASS"))
	if err != nil {
		t.Fatal(err)
	}
	want := schedule.StatisticsQuery{City: []string{"lille"}, Theater: []string{"ugc-25"}, Chain: "ugc", Language: "VF_SME", Format: "LASER_ULTRA", Genre: "Comédie", Pass: "CINÉ PASS"}
	if !reflect.DeepEqual(query, want) {
		t.Fatalf("query=%+v", query)
	}
	for _, raw := range []string{"city=" + strings.Repeat("x", 200), "pass=" + url.QueryEscape(strings.Repeat("é", 100)), strings.Repeat("&", 4096)} {
		if _, err := parseStatisticsQuery(raw); err != nil {
			t.Fatalf("valid boundary rejected: %v", err)
		}
	}
	if _, err := parseStatisticsQuery(strings.Repeat("&", 4097)); err == nil {
		t.Fatal("oversized raw query accepted")
	}
}

func TestStatisticsHTTPFilmScalar(t *testing.T) {
	for _, tc := range []struct {
		name, raw, film string
		invalid         bool
	}{
		{"absent", "", "", false},
		{"canonical trimmed", "film=+film-1+", "film-1", false},
		{"opaque", "film=" + url.QueryEscape("É &+/?#,film-1"), "É &+/?#,film-1", false},
		{"200 bytes", "film=" + strings.Repeat("x", 200), strings.Repeat("x", 200), false},
		{"200 UTF8 bytes", "film=" + url.QueryEscape(strings.Repeat("é", 100)), strings.Repeat("é", 100), false},
		{"padded boundary", "film=x" + strings.Repeat("+", 199), "x", false},
		{"raw boundary", "film=film-1" + strings.Repeat("&", 4085), "film-1", false},
		{"bare", "film", "", true},
		{"empty", "film=", "", true},
		{"blank", "film=+%09", "", true},
		{"duplicate", "film=film-1&film=film-1", "", true},
		{"array", "film[]=film-1", "", true},
		{"NUL", "film=film-1%00", "", true},
		{"invalid UTF8", "film=%FF", "", true},
		{"invalid escape", "film=%zz", "", true},
		{"201 bytes before trim", "film=x" + strings.Repeat("+", 200), "", true},
		{"201 UTF8 bytes", "film=" + url.QueryEscape(strings.Repeat("é", 100)+"x"), "", true},
		{"raw overflow", "film=film-1" + strings.Repeat("&", 4086), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, history := range []bool{false, true} {
				reader := &fakeHistoryReader{}
				handler, path := testHandler(t), "/api/v1/statistics"
				parse := parseStatisticsQuery
				if history {
					handler = NewHandlerWithOptions(nil, "", HandlerOptions{History: reader})
					path, parse = path+"/history", parseHistoryStatisticsQuery
				}
				q, err := parse(tc.raw)
				if (err != nil) != tc.invalid || !tc.invalid && q.Film != tc.film {
					t.Fatalf("history=%t query=%+v err=%v", history, q, err)
				}
				r := performRequest(t, handler, path+"?"+tc.raw)
				want := http.StatusOK
				if tc.invalid {
					want = http.StatusBadRequest
					if !strings.Contains(r.Body.String(), `"code":"invalid_query"`) || reader.calls != 0 {
						t.Fatal(r.Body.String(), reader.calls)
					}
				} else if history && (reader.calls != 1 || reader.stats.Film != tc.film) {
					t.Fatal("film not forwarded", reader.stats, reader.calls)
				}
				if r.Code != want || r.Header().Get("Cache-Control") != "no-store" {
					t.Fatal(history, r.Code, r.Body.String())
				}
			}
		})
	}
}

func TestStatisticsHTTPParsingSelections(t *testing.T) {
	values := url.Values{
		"city":    {" lyon ", "lille", "lyon", "é&=,+/", "removed"},
		"theater": {" ugc-26 ", "ugc-25", "ugc-26", "ciné &+", "gone"},
	}
	query, err := parseStatisticsQuery(values.Encode())
	if err != nil || !slices.Equal(query.City, []string{"lyon", "lille", "é&=,+/", "removed"}) || !slices.Equal(query.Theater, []string{"ugc-26", "ugc-25", "ciné &+", "gone"}) {
		t.Fatalf("query=%+v error=%v", query, err)
	}
	empty, err := parseStatisticsQuery("")
	if err != nil || len(empty.City) != 0 || len(empty.Theater) != 0 {
		t.Fatalf("empty query=%+v error=%v", empty, err)
	}
	for _, key := range []string{"city", "theater"} {
		for _, count := range []int{50, 51} {
			for _, duplicates := range []bool{false, true} {
				values := make([]string, count)
				for i := range values {
					values[i] = "x"
					if !duplicates {
						values[i] = fmt.Sprintf("x%d", i)
					}
				}
				query, err := parseStatisticsQuery(url.Values{key: values}.Encode())
				if (err != nil) != (count > 50) {
					t.Fatalf("%s count=%d duplicates=%t error=%v", key, count, duplicates, err)
				}
				if err == nil {
					selection := query.City
					if key == "theater" {
						selection = query.Theater
					}
					want := count
					if duplicates {
						want = 1
					}
					if len(selection) != want {
						t.Fatalf("selection=%v want length=%d", selection, want)
					}
				}
			}
		}
	}
	query, err = parseStatisticsQuery(strings.Repeat("city=lille&", 50) + strings.Repeat("theater=ugc-25&", 50))
	if err != nil || !slices.Equal(query.City, []string{"lille"}) || !slices.Equal(query.Theater, []string{"ugc-25"}) {
		t.Fatalf("independent bounds: query=%+v error=%v", query, err)
	}
}

func TestStatisticsHTTPValueAndRawByteBounds(t *testing.T) {
	for _, key := range []string{"city", "theater", "chain", "language", "format", "genre", "pass"} {
		for _, value := range []string{strings.Repeat("x", 200), strings.Repeat("é", 100), "x" + strings.Repeat(" ", 199)} {
			if _, err := parseStatisticsQuery(url.Values{key: {value}}.Encode()); err != nil {
				t.Fatalf("%s valid 200-byte boundary: %v", key, err)
			}
			if _, err := parseStatisticsQuery(url.Values{key: {value + " "}}.Encode()); err == nil {
				t.Fatalf("%s accepted 201 bytes before trimming", key)
			}
		}
	}
	// Padding uses ignored separators, leaving the actual filters valid at 4096.
	base := "city=lille&city=lyon&theater=ugc-25&theater=ugc-99"
	for _, size := range []int{4096, 4097} {
		raw := base + strings.Repeat("&", size-len(base))
		response := performRequest(t, testHandler(t), "/api/v1/statistics?"+raw)
		if size == 4097 {
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_query"`) || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("4097-byte status=%d body=%s", response.Code, response.Body)
			}
			continue
		}
		var result schedule.Statistics
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || result.Totals.Showtimes != 2 {
			t.Fatalf("4096-byte status=%d totals=%+v", response.Code, result.Totals)
		}
	}
}

func TestStatisticsHTTPAvailabilityStaleAndMethod(t *testing.T) {
	data := readinessFixtureDataset(t)
	for name, view := range map[string]*schedule.SnapshotView{"absent": nil, "catalog": schedule.NewSnapshotView(data, schedule.SnapshotRevision{})} {
		t.Run(name, func(t *testing.T) {
			service, err := schedule.NewService(fixtureSource{view: view}, schedule.ServiceOptions{})
			if err != nil {
				t.Fatal(err)
			}
			response := performRequest(t, NewHandler(service, "http://localhost:3000"), "/api/v1/statistics")
			if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"schedule_unavailable"`) || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data)}, schedule.ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(t, NewHandler(service, "http://localhost:3000"), "/api/v1/statistics")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"stale":true`) {
		t.Fatalf("stale status=%d body=%s", response.Code, response.Body)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/statistics", nil)
	response = httptest.NewRecorder()
	testHandler(t).ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || !strings.Contains(response.Body.String(), `"code":"method_not_allowed"`) {
		t.Fatalf("method status=%d body=%s", response.Code, response.Body)
	}
}

func TestStatisticsHTTPExpensiveReadLimit(t *testing.T) {
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	handler := testHandlerWithOptions(t, HandlerOptions{RateLimitClock: func() time.Time { return now }})
	for i := 0; i < expensiveReadBurst; i++ {
		response := requestFrom(t, handler, http.MethodGet, "/api/v1/statistics", "192.0.2.1:1234", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("request=%d status=%d", i, response.Code)
		}
	}
	assertRateLimited(t, requestFrom(t, handler, http.MethodGet, "/api/v1/statistics", "192.0.2.1:1234", nil), "1")
}

func TestStatisticsHTTPEmptySnapshotArraysAndInternalError(t *testing.T) {
	data := readinessFixtureDataset(t)
	data.Theaters, data.Showtimes = nil, nil
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data)}, schedule.ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service, "http://localhost:3000")
	response := performRequest(t, handler, "/api/v1/statistics")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "null") {
		t.Fatalf("empty snapshot status=%d body=%s", response.Code, response.Body)
	}
	var result schedule.Statistics
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Totals != (schedule.StatisticsTotals{}) || len(result.Options.Theaters) != 0 || len(result.TopMovies.ByShowtimes) != 0 || len(result.Versions) != 0 {
		t.Fatalf("empty snapshot=%+v", result)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/statistics", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"internal_error"`) || strings.Contains(response.Body.String(), "context") {
		t.Fatalf("unexpected failure status=%d body=%s", response.Code, response.Body)
	}
}
