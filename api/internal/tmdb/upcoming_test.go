package tmdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestUpcomingDiscoverStrictContract(t *testing.T) {
	for _, test := range []struct {
		name, body string
		valid      bool
	}{
		{"empty", `{"page":1,"total_pages":0,"total_results":0,"results":[]}`, true},
		{"movie", `{"page":1,"total_pages":1,"total_results":1,"results":[{"id":42,"release_date":"1900-01-01"}]}`, true},
		{"missing array", `{"page":1,"total_pages":0,"total_results":0}`, false},
		{"null array", `{"page":1,"total_pages":0,"total_results":0,"results":null}`, false},
		{"missing page", `{"total_pages":0,"total_results":0,"results":[]}`, false},
		{"wrong page", `{"page":2,"total_pages":0,"total_results":0,"results":[]}`, false},
		{"over ceiling", `{"page":1,"total_pages":501,"total_results":10001,"results":[]}`, false},
		{"incomplete", `{"page":1,"total_pages":1,"total_results":2,"results":[{"id":42}]}`, false},
		{"invalid ID", `{"page":1,"total_pages":1,"total_results":1,"results":[{"id":0}]}`, false},
		{"bad totals", `{"page":1,"total_pages":2,"total_results":1,"results":[{"id":42}]}`, false},
		{"truncated", `{"page":1,`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if r.URL.Path != "/3/discover/movie" || len(q) != 9 || q.Get("region") != "FR" || q.Get("language") != "fr-FR" || q.Get("with_release_type") != "2|3" || q.Get("release_date.gte") != "2026-09-14" || q.Get("release_date.lte") != "2027-09-13" || q.Get("sort_by") != "primary_release_date.asc" || q.Get("include_adult") != "false" || q.Get("include_video") != "false" || q.Get("page") != "1" {
					t.Errorf("incorrect discovery query")
				}
				_, _ = w.Write([]byte(test.body))
			}, "fixture-token")
			result, err := client.DiscoverMovies(context.Background(), "2026-09-14", "2027-09-13", 1)
			if (err == nil) != test.valid {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestUpcomingFirstFrenchTheatricalEvidence(t *testing.T) {
	for _, test := range []struct {
		name, results, want string
		invalid             bool
	}{
		{"first FR not worldwide", `[{"iso_3166_1":"US","release_dates":[{"type":3,"release_date":"2000-01-01T00:00:00Z"}]},{"iso_3166_1":"FR","release_dates":[{"type":3,"release_date":"2026-10-01T00:00:00+14:00"}]}]`, "2026-10-01", false},
		{"past limited excludes rerelease", `[{"iso_3166_1":"FR","release_dates":[{"type":3,"release_date":"2026-10-01T00:00:00Z"},{"type":2,"release_date":"1990-03-04T00:00:00.000Z"}]}]`, "1990-03-04", false},
		{"all FR records", `[{"iso_3166_1":"FR","release_dates":[{"type":3,"release_date":"2026-10-01T00:00:00Z"}]},{"iso_3166_1":"FR","release_dates":[{"type":2,"release_date":"2026-09-30T23:00:00-12:00"}]}]`, "2026-09-30", false},
		{"malformed non-theatrical FR dates", `[{"iso_3166_1":"FR","release_dates":[{"type":1,"release_date":"bad"}]}]`, "", true},
		{"ignore non FR", `[{"iso_3166_1":"US","release_dates":[{"type":3,"release_date":"bad","note":42}]}]`, "", false},
		{"empty", `[]`, "", false},
		{"empty FR", `[{"iso_3166_1":"FR","release_dates":[]}]`, "", false},
		{"null", `null`, "", true},
		{"missing FR dates", `[{"iso_3166_1":"FR"}]`, "", true},
		{"null FR dates", `[{"iso_3166_1":"FR","release_dates":null}]`, "", true},
		{"missing relevant date", `[{"iso_3166_1":"FR","release_dates":[{"type":3}]}]`, "", true},
		{"not timestamp", `[{"iso_3166_1":"FR","release_dates":[{"type":3,"release_date":"2026-10-01"}]}]`, "", true},
		{"invalid leap", `[{"iso_3166_1":"FR","release_dates":[{"type":3,"release_date":"2027-02-29T00:00:00Z"}]}]`, "", true},
		{"invalid timezone hour", `[{"iso_3166_1":"FR","release_dates":[{"type":3,"release_date":"2026-10-01T00:00:00+24:00"}]}]`, "", true},
		{"invalid timezone minute", `[{"iso_3166_1":"FR","release_dates":[{"type":3,"release_date":"2026-10-01T00:00:00+00:60"}]}]`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/3/movie/42/release_dates" {
					t.Errorf("incorrect path")
				}
				_, _ = fmt.Fprintf(w, `{"id":42,"results":%s}`, test.results)
			}, "fixture-token")
			evidence, err := client.FrenchReleaseEvidence(context.Background(), 42)
			if (err != nil) != test.invalid || evidence.FrenchReleaseDate != test.want {
				t.Fatalf("evidence=%+v error=%v", evidence, err)
			}
		})
	}
	for _, body := range []string{`{"id":43,"results":[]}`, `{"id":42}`, `{"id":42,"results":`} {
		client := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }, "fixture-token")
		if _, err := client.FrenchReleaseEvidence(context.Background(), 42); err == nil {
			t.Fatal("accepted malformed release response")
		}
	}
}

func TestUpcomingTransportFailureSafety(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("private upstream contents"))
			}, "fixture-token")
			_, err := client.FrenchReleaseEvidence(context.Background(), 42)
			if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "fixture-token") {
				t.Fatalf("unsafe error: %v", err)
			}
			if errors.Is(err, ErrNotFound) != (status == 404) || errors.Is(err, ErrStop) != (status == 401 || status == 403 || status == 429) {
				t.Fatalf("wrong category: %v", err)
			}
		})
	}
	client := testClient(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("canceled request sent") }, "fixture-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.DiscoverMovies(ctx, "2026-09-14", "2027-09-13", 1); err == nil {
		t.Fatal("cancellation ignored")
	}
}
