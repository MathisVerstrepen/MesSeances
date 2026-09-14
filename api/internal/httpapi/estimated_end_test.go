package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func estimatedEndHandler(t *testing.T, runtime int, canonical bool) http.Handler {
	t.Helper()
	d := fixtureDataset(t)
	d.Theaters = d.Theaters[:1]
	d.Showtimes = d.Showtimes[:1]
	r := &d.Showtimes[0]
	r.Movie.RuntimeMinutes, r.Movie.Enrichment = runtime, nil
	r.StartTime = r.StartTime.Add(6 * time.Hour)
	r.EndTime = r.StartTime
	if canonical {
		r.EndTime = r.StartTime.Add(100 * time.Minute)
	}
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(d)}, schedule.ServiceOptions{
		Now: func() time.Time { return time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(service, "http://localhost:3000")
}

func assertEstimatedEndJSON(t *testing.T, showing map[string]json.RawMessage, wantEnd string, wantAds *int) {
	t.Helper()
	end, hasEnd := showing["estimated_end_time"]
	ads, hasAds := showing["estimated_end_ads_minutes"]
	if !hasEnd || !hasAds {
		t.Fatalf("required nullable keys missing: %v", showing)
	}
	if wantAds == nil {
		if string(end) != "null" || string(ads) != "null" {
			t.Fatalf("estimate must be explicit null: end=%s ads=%s", end, ads)
		}
		return
	}
	if string(end) != strconv.Quote(wantEnd) || string(ads) != strconv.Itoa(*wantAds) {
		t.Fatalf("estimate end=%s ads=%s, want %s/%d", end, ads, wantEnd, *wantAds)
	}
}

func TestEstimatedEndHTTPResponseSurfaces(t *testing.T) {
	for _, test := range []struct {
		name      string
		runtime   int
		canonical bool
	}{
		{"estimated", 93, false}, {"unknown", 0, false}, {"known no runtime", 0, true}, {"known different runtime", 93, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := estimatedEndHandler(t, test.runtime, test.canonical)
			for _, endpoint := range []string{
				"/api/v1/movies/ugc-film-200/showtimes?date=2026-08-15",
				"/api/v1/theaters/ugc-lille/showtimes?date=2026-08-15",
				"/api/v1/timeline?date=2026-08-15&theaters=ugc-25",
			} {
				response := performRequest(t, handler, endpoint)
				if response.Code != http.StatusOK {
					t.Fatalf("%s: status=%d body=%s", endpoint, response.Code, response.Body.String())
				}
				var result struct {
					Showtimes []map[string]json.RawMessage `json:"showtimes"`
					Theaters  []struct {
						Showtimes []map[string]json.RawMessage `json:"showtimes"`
					} `json:"theaters"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				showings := result.Showtimes
				if len(result.Theaters) > 0 {
					showings = result.Theaters[0].Showtimes
				}
				if len(showings) != 1 {
					t.Fatalf("%s: showings=%v", endpoint, showings)
				}
				var ads *int
				wantDuration := "0"
				if test.runtime > 0 && !test.canonical {
					value := 15
					ads = &value
					wantDuration = "108"
				}
				wantCanonical := `"2026-08-15T16:00:00Z"`
				if test.canonical {
					wantCanonical = `"2026-08-15T17:40:00Z"`
					wantDuration = "100"
				}
				assertEstimatedEndJSON(t, showings[0], "2026-08-15T17:48:00Z", ads)
				if string(showings[0]["end_time"]) != wantCanonical {
					t.Fatalf("canonical end changed: %s", showings[0]["end_time"])
				}
				if duration, ok := showings[0]["duration_minutes"]; ok && string(duration) != wantDuration {
					t.Fatalf("duration=%s want %s", duration, wantDuration)
				}
			}
		})
	}
}

func TestEstimatedEndHTTPSlotContexts(t *testing.T) {
	const base = "/api/v1/search/slot?theaters=ugc-25&date=2026-08-15&start_after=17:00&finish_before=23:00"
	for _, canonical := range []bool{false, true} {
		for _, buffer := range []string{"", "0", "30", "120"} {
			for _, includeAds := range []bool{false, true} {
				t.Run(fmt.Sprintf("canonical=%t/buffer=%s/include=%t", canonical, buffer, includeAds), func(t *testing.T) {
					ads := schedule.DefaultBufferAdsMinutes
					target := base + "&include_ads=" + strconv.FormatBool(includeAds)
					if buffer != "" {
						var err error
						ads, err = strconv.Atoi(buffer)
						if err != nil {
							t.Fatal(err)
						}
						target += "&buffer_ads=" + buffer
					}
					response := performRequest(t, estimatedEndHandler(t, 93, canonical), target)
					if response.Code != http.StatusOK {
						t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
					}
					var results []struct {
						Showtime       map[string]json.RawMessage `json:"showtime"`
						EffectiveEnd   time.Time                  `json:"effective_end_time"`
						EffectiveStart time.Time                  `json:"effective_start_time"`
						BufferAds      int                        `json:"buffer_ads_minutes"`
					}
					if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
						t.Fatal(err)
					}
					if canonical && !includeAds && ads >= 100 {
						if len(results) != 0 {
							t.Fatal("invalid attendance returned")
						}
						return
					}
					if len(results) != 1 {
						t.Fatalf("results=%+v", results)
					}
					start := time.Date(2026, 8, 15, 16, 0, 0, 0, time.UTC)
					end := start.Add(time.Duration(93+ads) * time.Minute)
					wantAds := &ads
					if canonical {
						end = start.Add(100 * time.Minute)
						wantAds = nil
					}
					assertEstimatedEndJSON(t, results[0].Showtime, end.Format(time.RFC3339), wantAds)
					if !includeAds {
						start = start.Add(time.Duration(ads) * time.Minute)
					}
					if !results[0].EffectiveEnd.Equal(end) || !results[0].EffectiveStart.Equal(start) || results[0].BufferAds != ads {
						t.Fatalf("effective interval: %+v", results[0])
					}
				})
			}
		}
	}
	for _, buffer := range []string{"-1", "121", "wat", "1.5", ""} {
		response := performRequest(t, estimatedEndHandler(t, 93, false), base+"&buffer_ads="+buffer)
		assertAPIError(t, response, http.StatusBadRequest, "invalid_query", "Le paramètre buffer_ads doit être un entier compris entre 0 et 120.")
	}
}
