package cinemovida

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func jsonBody(v any) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return body
}
func census() []cinema {
	var result []cinema
	for _, id := range []string{"P0176", "P0218", "P7951", "P0876"} {
		c := cinema{ID: id, Name: "Ciné-Movida " + id, TimeZone: schedule.Timezone}
		c.PracticalInfo.Location.Address = "1 rue du cinéma"
		c.PracticalInfo.Location.City = "Paris"
		c.PracticalInfo.Location.Zip = "75001"
		result = append(result, c)
	}
	return result
}
func cinemaBody(items []cinema) []byte {
	return jsonBody(map[string]any{"data": map[string]any{"allTheater": map[string]any{"nodes": items}}})
}
func session(theater, date, clock string) showtimeResponse {
	var s showtimeResponse
	body := fmt.Sprintf(`{"id":"same raw session","startsAt":%q,"tags":["Localization.Language.French","Format.Projection.3d","Auditorium.Experience.MX4D"],"screen":{"name":" Salle 10 PREMIUM MX4D "},"data":{"ticketing":[{"provider":"default","type":"DESKTOP","urls":[%q]},{"provider":"relay","type":"MOBILE","urls":["https://relay.mvtx.us/ignored"]}]}}`, date+"T"+clock, "https://"+schedule.CinemovidaBookingHost(theater)+"/reserver/r/123")
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		panic(err)
	}
	return s
}

type fixtureGetter struct {
	get      func(context.Context, Operation, string) ([]byte, error)
	checkout func(context.Context, string, time.Time) ([]byte, error)
	count    atomic.Int32
}

func (f *fixtureGetter) Get(ctx context.Context, op Operation, raw string) ([]byte, error) {
	f.count.Add(1)
	return f.get(ctx, op, raw)
}
func (f *fixtureGetter) Checkout(ctx context.Context, raw string, start time.Time) ([]byte, error) {
	f.count.Add(1)
	if f.checkout != nil {
		return f.checkout(ctx, raw, start)
	}
	return []byte(`<span id="s_fin">(fin prévue à 02h15)</span>`), nil
}
func (f *fixtureGetter) RequestCount() int { return int(f.count.Load()) }
func goodFixture() *fixtureGetter {
	return &fixtureGetter{get: func(ctx context.Context, op Operation, raw string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		u, _ := url.Parse(raw)
		switch op {
		case OperationCinemas:
			return cinemaBody(census()), nil
		case OperationProgram:
			if u.Query().Get("theaterId") == "P0876" {
				return []byte(`{"movieIds":{"titleAsc":[],"releaseAsc":[]},"scheduledDays":{}}`), nil
			}
			return []byte(`{"movieIds":{"titleAsc":[123,"cEvent_1"],"releaseAsc":["cEvent_1",123]},"scheduledDays":{"123":["2026-10-16","2026-10-17","2026-10-18"],"cEvent_1":["2026-10-17"]}}`), nil
		case OperationMovies:
			return []byte(`[{"id":123,"title":"Film","runtime":6000,"poster":"https://fr.web.img6.acsta.net/img/a.jpg","genres":["Drame"],"synopsis":"Résumé","releaseDate":"2026-10-01"},{"id":"cEvent_1","title":"Événement","runtime":null,"poster":null}]`), nil
		case OperationSchedule:
			var c struct{ ID string }
			_ = json.Unmarshal([]byte(u.Query().Get("theaters")), &c)
			return jsonBody(map[string]any{c.ID: map[string]any{"schedule": map[string]any{
				"123": map[string]any{"2026-10-17": []showtimeResponse{session(c.ID, "2026-10-17", "00:05:00+02:00")}, "2026-10-18": []showtimeResponse{func() showtimeResponse {
					s := session(c.ID, "2026-10-18", "23:55:00+02:00")
					s.ID = "last session"
					return s
				}()}},
				"cEvent_1": map[string]any{"2026-10-17": []showtimeResponse{}},
			}}}), nil
		}
		return nil, fmt.Errorf("unexpected operation")
	}}
}

func TestSyncCensusCalendarMetadataAndSourceEnds(t *testing.T) {
	f := goodFixture()
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	d, summary, err := Sync(t.Context(), f, SyncOptions{From: "2026-10-16", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Theaters) != 4 || len(d.Showtimes) != 6 || summary.Cinemas != 4 || summary.Jobs != 3 || summary.EndsRecovered != 6 || summary.EndsAttempted != 6 || d.Window.From != "2026-10-17" || d.Window.Through != "2026-10-18" {
		t.Fatalf("incomplete acquisition: %+v", summary)
	}
	seen := map[string]bool{}
	for _, r := range d.Showtimes {
		if seen[r.ID] || r.Room != "Salle 10 PREMIUM MX4D" || r.Format != schedule.Format4DX || !r.EndTime.After(r.StartTime) || r.FirstPartDurationMinutes != 0 {
			t.Fatal("lost session identity or fields", r.ID)
		}
		seen[r.ID] = true
		if r.Movie.Overview != "Résumé" || r.Movie.ReleaseDate != "2026-10-01" || len(r.Movie.Genres) != 1 {
			t.Fatal("metadata lost")
		}
		if r.StartTime.Day() == 18 && r.EndTime.Day() != 19 {
			t.Fatal("end did not roll to next date")
		}
	}
	for _, c := range d.Theaters {
		if c.ProviderID == "P0876" {
			if len(c.AvailableDates) != 0 {
				t.Fatal("invented empty cinema dates")
			}
			continue
		}
		if strings.Join(c.AvailableDates, ",") != "2026-10-17,2026-10-18" {
			t.Fatal("advertised calendar dates lost")
		}
	}
	for _, c := range d.Coverage {
		if c.ServiceDate == "2026-10-17" && (c.Status != schedule.CoverageUnknown || c.Basis != schedule.CoverageAcceptedOmission) {
			t.Fatal("accepted omission falsely complete")
		}
		if c.ServiceDate == "2026-10-18" && (c.Status != schedule.CoverageUnknown || c.Basis != schedule.CoverageAcceptedOmission) {
			t.Fatal("possible midnight omission falsely complete")
		}
	}
}

func TestSyncUnknownRuntimeEndAndOptionalFailures(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "transport", "challenge", "cancel", "required"} {
		t.Run(mode, func(t *testing.T) {
			f := goodFixture()
			original := f.get
			f.get = func(ctx context.Context, op Operation, raw string) ([]byte, error) {
				if op == OperationMovies {
					return []byte(`[{"id":123,"title":"Film","runtime":null,"poster":null},{"id":"cEvent_1","title":"Event","runtime":null}]`), nil
				}
				if mode == "required" && op == OperationSchedule {
					return nil, &RequestError{Operation: op, Kind: syncproxy.FailureStatus, StatusCode: 404}
				}
				return original(ctx, op, raw)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.checkout = func(context.Context, string, time.Time) ([]byte, error) {
				switch mode {
				case "missing":
					return []byte(`<p>Séance passée, achat impossible</p>`), nil
				case "transport":
					return nil, &RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureTransport}
				case "challenge":
					return nil, &RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureChallenge, StatusCode: 403}
				case "cancel":
					cancel()
					return nil, context.Canceled
				}
				return []byte(`<span class="s_fin">(fin prévue à 02h15)</span>`), nil
			}
			d, _, err := Sync(ctx, f, SyncOptions{From: "2026-10-17", Now: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)})
			fatal := mode == "challenge" || mode == "cancel" || mode == "required"
			if fatal {
				if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
					t.Fatal("failed acquisition returned publishable partial snapshot")
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range d.Showtimes {
				if r.Movie.RuntimeMinutes != 0 || r.Movie.PosterURL != "" {
					t.Fatal("null metadata invented")
				}
				if r.EndTime.Equal(r.StartTime) != (mode != "valid") {
					t.Fatal("canonical end incorrectly depends on runtime or optional lookup")
				}
			}
		})
	}
}

func TestCensusAndMovieValidation(t *testing.T) {
	for _, mutate := range []func([]cinema) []cinema{
		func(c []cinema) []cinema { return c[:3] },
		func(c []cinema) []cinema { c[1] = c[0]; return c },
		func(c []cinema) []cinema { c[0].ID = "P9999"; return c },
		func(c []cinema) []cinema { c[0].TimeZone = "UTC"; return c },
	} {
		if _, err := parseCinemas(cinemaBody(mutate(census()))); err == nil {
			t.Fatal("invalid census accepted")
		}
	}
	for _, body := range []string{`[{"id":1,"title":"A"},{"id":1,"title":"B"}]`, `[{"id":"01","title":"A"}]`, `[{"id":1,"title":"A","runtime":-1}]`, `[{"id":1,"title":"A","poster":"https://x.acsta.net/a/../b.jpg"}]`} {
		if _, err := parseMovies([]byte(body)); err == nil {
			t.Fatal("bad movies accepted")
		}
	}
	m, err := parseMovies([]byte(`[{"id":"cEvent_1","title":"Local event","runtime":null,"poster":null}]`))
	if err != nil || m["cEvent_1"].Slug != "cinemovida-film-cEvent_1" || m["cEvent_1"].RuntimeMinutes != 0 {
		t.Fatal("local event lost", err)
	}
}

func TestScheduleIdentityConflictsAndBinding(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	c := census()[0]
	m := schedule.MovieRecord{Provider: schedule.ProviderCinemovida, ProviderID: "123", Title: "Film"}
	program := map[string][]string{"123": {"2026-10-17"}}
	s := session(c.ID, "2026-10-17", "00:05:00+02:00")
	parse := func(sessions []showtimeResponse) ([]schedule.ShowtimeRecord, error) {
		return parseSchedule(jsonBody(map[string]any{c.ID: map[string]any{"schedule": map[string]any{"123": map[string]any{"2026-10-17": sessions}}}}), c, program, map[string]schedule.MovieRecord{"123": m}, loc, scheduleRange{from: "2026-10-17", through: "2026-10-17"}, map[string]bool{})
	}
	rows, err := parse([]showtimeResponse{s, s})
	if err != nil || len(rows) != 1 {
		t.Fatal("identical duplicates not collapsed", err)
	}
	conflict := s
	conflict.StartsAt = "2026-10-17T01:05:00+02:00"
	if _, err := parse([]showtimeResponse{s, conflict}); err == nil {
		t.Fatal("conflicting identity accepted")
	}
	for _, mutate := range []func(*showtimeResponse){
		func(s *showtimeResponse) { s.StartsAt = "2026-10-18T03:00:00+02:00" },
		func(s *showtimeResponse) {
			s.Data.Ticketing[0].URLs[0] = "https://achat.cinema-megacastillet.com/reserver/r/123"
		},
		func(s *showtimeResponse) { s.Tags = []string{"Localization.Language.ALL"} },
	} {
		s := session(c.ID, "2026-10-17", "00:05:00+02:00")
		mutate(&s)
		if _, err := parse([]showtimeResponse{s}); err == nil {
			t.Fatal("bad schedule accepted")
		}
	}
	for _, body := range []string{`{"P0176":{"schedule":{"999":{"2026-10-17":[]}}}}`, `{"P0176":{"schedule":{"123":{"2026-10-18":[]}}}}`} {
		if _, err := parseSchedule([]byte(body), c, program, map[string]schedule.MovieRecord{"123": m}, loc, scheduleRange{from: "2026-10-17", through: "2026-10-17"}, map[string]bool{}); !errors.Is(err, errSnapshotChanged) {
			t.Fatal("advertised relationship ignored", err)
		}
	}
}

func TestVersionFormatPrecedenceAndDST(t *testing.T) {
	for _, tc := range []struct {
		tags     []string
		language schedule.Language
		version  string
	}{
		{[]string{"Localization.Version.Original", "Localization.Language.French", "Showtime.Accessibility.OpenCaption"}, schedule.LanguageVOSTFR, "VOSTF"},
		{[]string{"Localization.Language.French", "Showtime.Accessibility.OpenCaption"}, schedule.Language("VFSTF"), "VFSTF"},
		{[]string{"Localization.Language.French"}, schedule.LanguageVF, "VF"},
	} {
		l, v, err := normalizeVersion(tc.tags)
		if err != nil || l != tc.language || v != tc.version {
			t.Fatal("version precedence", l, v, err)
		}
	}
	for _, tc := range []struct {
		tags   []string
		format schedule.Format
	}{
		{[]string{"Auditorium.Experience.IMAX", "Auditorium.Experience.MX4D", "Format.Projection.3d"}, schedule.FormatIMAX},
		{[]string{"Auditorium.Experience.MX4D", "Format.Projection.3d", "Auditorium.Experience.DolbyAtmos"}, schedule.Format4DX},
		{[]string{"Format.Projection.3d", "Auditorium.Experience.DolbyCinema"}, schedule.Format3D},
		{[]string{"Auditorium.Experience.DolbyCinema"}, schedule.FormatDolby},
		{[]string{"Auditorium.Experience.InfinityVision", "Auditorium.Experience.Ice", "Format.Projection.Laser", "Format.Projection.4k", "Auditorium.Experience.PLF"}, schedule.Format2D},
	} {
		if got := normalizeFormat(tc.tags); got != tc.format {
			t.Fatal("format precedence", got)
		}
	}
	loc, _ := time.LoadLocation(schedule.Timezone)
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{"2026-03-29T02:30:00", false}, {"2026-03-29T02:30:00+01:00", false}, {"2026-03-29T03:30:00+02:00", true},
		{"2026-10-25T02:30:00+02:00", true}, {"2026-10-25T02:30:00+01:00", true}, {"2026-10-25T00:30:00Z", false},
	} {
		if _, valid := parseStartTime(tc.raw, loc); valid != tc.valid {
			t.Fatal("DST offset policy", tc.raw)
		}
	}
}

func TestSnapshotDriftRetriesWholeCensusThenFailsClosed(t *testing.T) {
	for _, recover := range []bool{false, true} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			f := goodFixture()
			original := f.get
			var scans atomic.Int32
			f.get = func(ctx context.Context, op Operation, raw string) ([]byte, error) {
				if op == OperationCinemas {
					scans.Add(1)
				}
				if op == OperationMovies && (!recover || scans.Load() == 1) {
					return []byte(`[]`), nil
				}
				return original(ctx, op, raw)
			}
			d, _, err := Sync(t.Context(), f, SyncOptions{From: "2026-10-17", Now: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)})
			if scans.Load() != 2 || recover && (err != nil || len(d.Showtimes) != 6) || !recover && (err == nil || len(d.Theaters) != 0) {
				t.Fatal("snapshot retry/atomicity", scans.Load(), err)
			}
		})
	}
}

func TestScheduleServerFailureSplitsOnlyFullCalendarWindows(t *testing.T) {
	c := census()[0]
	loc, _ := time.LoadLocation(schedule.Timezone)
	var broad, days atomic.Int32
	f := &fixtureGetter{get: func(_ context.Context, op Operation, raw string) ([]byte, error) {
		if op != OperationSchedule {
			t.Fatal("unexpected acquisition")
		}
		u, _ := url.Parse(raw)
		from, to := u.Query().Get("from"), u.Query().Get("to")
		if from == "2026-10-17T00:00:00" && to == "2026-10-20T00:00:00" {
			broad.Add(1)
			return nil, &RequestError{Operation: op, Kind: syncproxy.FailureServer, StatusCode: 503}
		}
		date := from[:10]
		end, _ := time.Parse(time.DateOnly, date)
		if to != end.AddDate(0, 0, 2).Format(time.DateOnly)+"T00:00:00" {
			t.Fatal("non-calendar fallback", from, to)
		}
		days.Add(1)
		s := session(c.ID, date, "23:55:00+02:00")
		s.ID = date
		return jsonBody(map[string]any{c.ID: map[string]any{"schedule": map[string]any{"123": map[string]any{date: []showtimeResponse{s}}}}}), nil
	}}
	fetcher := scheduleFetcher{getter: f, cinema: c, movies: map[string]schedule.MovieRecord{"123": {Provider: schedule.ProviderCinemovida, ProviderID: "123", Title: "Film"}}, location: loc, unknown: map[string]bool{}, cache: map[string][]schedule.ShowtimeRecord{}}
	rows, err := fetcher.fetch(t.Context(), map[string][]string{"123": {"2026-10-17", "2026-10-18"}}, "2026-10-17", "2026-10-18")
	if err != nil || len(rows) != 2 || broad.Load() != 1 || days.Load() != 2 || rows[0].ServiceDate != "2026-10-17" || rows[1].ServiceDate != "2026-10-18" {
		t.Fatal("bounded fallback", err)
	}
}

func TestAllEmptyCircuitStillRejectsWithoutInventedDates(t *testing.T) {
	f := goodFixture()
	original := f.get
	f.get = func(ctx context.Context, op Operation, raw string) ([]byte, error) {
		if op == OperationProgram {
			return []byte(`{"movieIds":{"titleAsc":[],"releaseAsc":[]},"scheduledDays":{}}`), nil
		}
		if op != OperationCinemas {
			t.Fatal("empty circuit performed unrelated acquisition")
		}
		return original(ctx, op, raw)
	}
	d, _, err := Sync(t.Context(), f, SyncOptions{From: "2026-10-17", Now: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)})
	if err == nil || len(d.Theaters) != 0 || f.RequestCount() != 5 {
		t.Fatal("all-empty circuit published or invented dates", err)
	}
}

func TestSyncServiceBucketsPreserveCalendarBoundaries(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprint(split), func(t *testing.T) {
			f := goodFixture()
			original := f.get
			f.get = func(ctx context.Context, op Operation, raw string) ([]byte, error) {
				u, _ := url.Parse(raw)
				switch op {
				case OperationProgram:
					return []byte(`{"movieIds":{"titleAsc":[123]},"scheduledDays":{"123":["2026-10-16","2026-10-17","2026-10-18"]}}`), nil
				case OperationMovies:
					return []byte(`[{"id":123,"title":"Film","runtime":6000}]`), nil
				case OperationSchedule:
					from, to := u.Query().Get("from"), u.Query().Get("to")
					if split && from == "2026-10-16T00:00:00" && to == "2026-10-20T00:00:00" {
						return nil, &RequestError{Operation: op, Kind: syncproxy.FailureServer, StatusCode: 503}
					}
					var c struct{ ID string }
					_ = json.Unmarshal([]byte(u.Query().Get("theaters")), &c)
					dates := map[string][]showtimeResponse{}
					for _, tc := range []struct{ bucket, date, clock string }{
						{"2026-10-16", "2026-10-16", "23:55:00"},
						{"2026-10-16", "2026-10-17", "00:05:00"},
						{"2026-10-17", "2026-10-17", "00:06:00"},
						{"2026-10-17", "2026-10-17", "23:55:00"},
						{"2026-10-18", "2026-10-18", "23:55:00"},
						{"2026-10-18", "2026-10-19", "00:05:00"},
					} {
						s := session(c.ID, tc.date, tc.clock)
						s.ID = tc.date + tc.clock
						if tc.date == "2026-10-16" {
							s.Data.Ticketing = nil
						}
						// Model a server filtering starts, then grouping by source bucket.
						if s.StartsAt >= from && s.StartsAt < to {
							dates[tc.bucket] = append(dates[tc.bucket], s)
						}
					}
					return jsonBody(map[string]any{c.ID: map[string]any{"schedule": map[string]any{"123": dates}}}), nil
				}
				return original(ctx, op, raw)
			}
			d, summary, err := Sync(t.Context(), f, SyncOptions{From: "2026-10-17", Now: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)})
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Showtimes) != 20 || summary.EndsRecovered != 20 || d.Window.From != "2026-10-17" || d.Window.Through != "2026-10-19" {
				t.Fatalf("calendar boundary dropped: %+v window=%+v", summary, d.Window)
			}
			for _, theater := range d.Theaters {
				if strings.Join(theater.AvailableDates, ",") != "2026-10-17,2026-10-18,2026-10-19" {
					t.Fatal("stale or missing observed date", theater.AvailableDates)
				}
			}
			for _, record := range d.Showtimes {
				if record.ServiceDate != record.StartTime.Format(time.DateOnly) || record.Room != "Salle 10 PREMIUM MX4D" || !record.EndTime.After(record.StartTime) {
					t.Fatal("calendar/source fields lost")
				}
			}
			for _, c := range d.Coverage {
				if c.ServiceDate == "2026-10-19" {
					if c.Status != schedule.CoverageUnknown || c.Basis != schedule.CoverageUnproven {
						t.Fatal("spillover date falsely complete")
					}
				} else if c.Status != schedule.CoverageComplete {
					t.Fatal("complete advertised response lost")
				}
			}
		})
	}
}

func TestShowtimeBucketAndTimestampValidation(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	c := census()[0]
	for _, tc := range []struct {
		bucket, starts string
		valid          bool
	}{
		{"2026-12-15", "2026-12-16T00:05:00", true},
		{"2026-12-16", "2026-12-16T00:05:00", true},
		{"2026-12-14", "2026-12-16T00:05:00", false},
		{"2026-12-17", "2026-12-16T00:05:00", false},
		{"2026-12-15", "2026-12-16T03:00:00", false},
		{"2026-03-28", "2026-03-29T02:30:00", false},
		{"2026-03-28", "2026-03-29T02:30:00+01:00", false},
		{"2026-12-15", "not-a-timestamp", false},
	} {
		t.Run(tc.bucket+"/"+tc.starts, func(t *testing.T) {
			s := session(c.ID, "2026-12-16", "00:05:00")
			s.StartsAt = tc.starts
			r, err := parseShowtime(s, c, schedule.MovieRecord{}, tc.bucket, loc)
			if (err == nil) != tc.valid || tc.valid && r.ServiceDate != "2026-12-16" {
				t.Fatal("bucket/timestamp validation", err)
			}
		})
	}
}
