package schedule

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestEstimatedEndMaterialization(t *testing.T) {
	for _, test := range []struct {
		name                                      string
		sourceRuntime, publicRuntime, wantRuntime int
		public                                    bool
	}{
		{"source", 93, 0, 93, false},
		{"enriched", 0, 123, 123, true},
		{"resolved override", 93, 110, 110, true},
		{"unknown", 0, 0, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := cinevilleValidationDataset()
			d.Showtimes[0].Movie.RuntimeMinutes = test.sourceRuntime
			d.PublicMovies[0].RuntimeMinutes = test.publicRuntime
			if !test.public {
				d.PublicMovies = nil
			}
			r := d.Showtimes[0]
			got := materializeRecord(NewSnapshotView(d), r)
			if got.Movie.RuntimeMinutes != test.wantRuntime || !got.EndTime.Equal(r.EndTime) || !d.Showtimes[0].EndTime.Equal(r.StartTime) {
				t.Fatalf("runtime or source/canonical end changed: %+v", got)
			}
			if test.wantRuntime == 0 {
				if got.EstimatedEndTime != nil || got.EstimatedEndAdsMinutes != nil || showtimeDurationMinutes(got) != 0 {
					t.Fatalf("unknown runtime estimated: %+v", got)
				}
				return
			}
			want := r.StartTime.Add(time.Duration(test.wantRuntime+15) * time.Minute)
			if got.EstimatedEndTime == nil || !got.EstimatedEndTime.Equal(want) || got.EstimatedEndTime.Location() != time.UTC || got.EstimatedEndAdsMinutes == nil || *got.EstimatedEndAdsMinutes != 15 || showtimeDurationMinutes(got) != test.wantRuntime+15 {
				t.Fatalf("estimate: %+v, want %v", got, want)
			}
		})
	}
}

func TestEstimatedEndArithmeticBoundaries(t *testing.T) {
	start := time.Date(2026, 8, 15, 18, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name         string
		start, end   time.Time
		runtime, ads int
	}{
		{"zero runtime", start, start, 0, 15},
		{"negative runtime", start, start, -1, 15},
		{"unsafe runtime", start, start, math.MaxInt, 15},
		{"addition overflow", start, start, int(math.MaxInt64 / int64(time.Minute)), 15},
		{"negative ads", start, start, 93, -1},
		{"excess ads", start, start, 93, 121},
		{"reversed", start, start.Add(-time.Minute), 93, 15},
		{"zero start", time.Time{}, time.Time{}, 93, 15},
		{"zero end", start, time.Time{}, 93, 15},
		{"year overflow", time.Date(9999, 12, 31, 23, 0, 0, 0, time.UTC), time.Date(9999, 12, 31, 23, 0, 0, 0, time.UTC), 93, 15},
		{"invalid year", time.Date(0, 1, 1, 18, 0, 0, 0, time.UTC), time.Date(0, 1, 1, 18, 0, 0, 0, time.UTC), 93, 15},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := Showtime{StartTime: test.start, EndTime: test.end, Movie: Movie{RuntimeMinutes: test.runtime}}
			s.EstimatedEndTime, s.EstimatedEndAdsMinutes = estimateShowtimeEnd(s, test.ads)
			if s.EstimatedEndTime != nil || s.EstimatedEndAdsMinutes != nil {
				t.Fatalf("unsafe estimate: %+v", s)
			}
			if _, ok := usableShowtimeEnd(s); ok || showtimeDurationMinutes(s) != 0 {
				t.Fatalf("unsafe interval usable: %+v", s)
			}
		})
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, start, end string }{
		{"midnight", "2026-08-15T23:30:00+02:00", "2026-08-16T01:18:00+02:00"},
		{"spring DST", "2026-03-29T01:30:00+01:00", "2026-03-29T04:18:00+02:00"},
		{"autumn DST", "2026-10-25T02:30:00+02:00", "2026-10-25T03:18:00+01:00"},
	} {
		t.Run(test.name, func(t *testing.T) {
			start, err := time.Parse(time.RFC3339, test.start)
			if err != nil {
				t.Fatal(err)
			}
			s := Showtime{StartTime: start.In(location), EndTime: start, Movie: Movie{RuntimeMinutes: 93}}
			end, ads := estimateShowtimeEnd(s, DefaultBufferAdsMinutes)
			if end == nil || ads == nil || end.In(location).Format(time.RFC3339) != test.end || end.Sub(start) != 108*time.Minute {
				t.Fatalf("elapsed arithmetic: %v, %v", end, ads)
			}
		})
	}
}

func estimatedEndService(t *testing.T, runtime int, canonical bool) (*Service, Dataset) {
	t.Helper()
	d := cinevilleValidationDataset()
	d.PublicMovies[0].RuntimeMinutes = runtime
	r := &d.Showtimes[0]
	r.StartTime = r.StartTime.Add(-time.Hour) // Advertised 18:00 Paris.
	r.EndTime = r.StartTime
	if canonical {
		// A provider-independent known end, not a valid Cineville ingestion record.
		r.EndTime = r.StartTime.Add(100 * time.Minute)
	}
	service, err := NewService(testSource{view: NewSnapshotView(d)}, ServiceOptions{Now: testServiceNow})
	if err != nil {
		t.Fatal(err)
	}
	return service, d
}

func TestEstimatedEndSlotsAdvertisingAndDeadlines(t *testing.T) {
	for _, canonical := range []bool{false, true} {
		for _, runtime := range []int{0, 93} {
			for _, ads := range []int{0, DefaultBufferAdsMinutes, 30, 120} {
				for _, includeAds := range []bool{false, true} {
					t.Run(fmt.Sprintf("canonical=%t/runtime=%d/ads=%d/include=%t", canonical, runtime, ads, includeAds), func(t *testing.T) {
						s, d := estimatedEndService(t, runtime, canonical)
						r := d.Showtimes[0]
						minutes := runtime + ads
						if canonical {
							minutes = 100
						}
						end := r.StartTime.Add(time.Duration(minutes) * time.Minute)
						query := SlotQuery{TheaterIDs: []string{r.TheaterID}, Date: r.ServiceDate, StartAfter: "17:00", FinishBefore: end.Format("15:04"), BufferAds: ads, IncludeAds: includeAds, Language: LanguageAll}
						got, err := s.SearchSlot(query)
						if err != nil {
							t.Fatal(err)
						}
						if !canonical && runtime == 0 || canonical && !includeAds && ads >= minutes {
							if len(got) != 0 {
								t.Fatalf("unusable interval returned: %+v", got)
							}
							return
						}
						if len(got) != 1 {
							t.Fatalf("exact finish rejected: %+v", got)
						}
						item := got[0]
						start := r.StartTime
						if !includeAds {
							start = start.Add(time.Duration(ads) * time.Minute)
						}
						resolved, usable := usableShowtimeEnd(item.Showtime)
						if !usable || !resolved.Equal(end) || !item.EffectiveEndTime.Equal(resolved) || !item.EffectiveStartTime.Equal(start) || item.SlackAfterMinutes != 0 || item.SlackBeforeMinutes != int(start.Sub(r.StartTime.Add(-time.Hour))/time.Minute) || !item.Showtime.EndTime.Equal(r.EndTime) {
							t.Fatalf("slot interval or canonical end: %+v", item)
						}
						if canonical {
							if item.Showtime.EstimatedEndTime != nil || item.Showtime.EstimatedEndAdsMinutes != nil {
								t.Fatal("known end estimated")
							}
						} else if item.Showtime.EstimatedEndAdsMinutes == nil || *item.Showtime.EstimatedEndAdsMinutes != ads {
							t.Fatal("stale estimate ads context")
						}
						query.FinishBefore = end.Add(-time.Minute).Format("15:04")
						got, err = s.SearchSlot(query)
						if err != nil || len(got) != 0 {
							t.Fatalf("early finish accepted: %+v, %v", got, err)
						}
					})
				}
			}
		}
	}
}

func TestEstimatedEndScheduleSurfaces(t *testing.T) {
	for _, runtime := range []int{0, 93} {
		s, d := estimatedEndService(t, runtime, false)
		r := d.Showtimes[0]
		timeline, err := s.Timeline(TimelineQuery{Date: r.ServiceDate, TheaterIDs: []string{r.TheaterID}, Language: LanguageAll})
		if err != nil || len(timeline.Theaters) != 1 || len(timeline.Theaters[0].Showtimes) != 1 {
			t.Fatalf("timeline: %+v, %v", timeline, err)
		}
		theater, err := s.TheaterShowtimes(TheaterShowtimesQuery{Slug: d.Theaters[0].Slug, Date: r.ServiceDate})
		if err != nil || len(theater.Showtimes) != 1 {
			t.Fatalf("theater: %+v, %v", theater, err)
		}
		movie, err := s.MovieShowtimes(MovieShowtimesQuery{Slug: "film-1", Date: r.ServiceDate})
		if err != nil || len(movie.Theaters) != 1 || len(movie.Theaters[0].Showtimes) != 1 {
			t.Fatalf("movie: %+v, %v", movie, err)
		}
		wantDuration := 0
		if runtime > 0 {
			wantDuration = runtime + DefaultBufferAdsMinutes
		}
		for _, item := range []TimelineShowtime{timeline.Theaters[0].Showtimes[0], theater.Showtimes[0]} {
			if item.DurationMinutes != wantDuration || item.StartOffsetMinutes != 600 {
				t.Fatalf("timeline metrics: %+v", item)
			}
		}
		for _, item := range []Showtime{timeline.Theaters[0].Showtimes[0].Showtime, theater.Showtimes[0].Showtime, movie.Theaters[0].Showtimes[0]} {
			if !item.EndTime.Equal(r.StartTime) || (item.EstimatedEndTime != nil) != (runtime > 0) {
				t.Fatalf("surface estimate: %+v", item)
			}
			if runtime > 0 && (!item.EstimatedEndTime.Equal(r.StartTime.Add(108*time.Minute)) || item.EstimatedEndAdsMinutes == nil || *item.EstimatedEndAdsMinutes != 15) {
				t.Fatalf("default context: %+v", item)
			}
		}
	}
}
