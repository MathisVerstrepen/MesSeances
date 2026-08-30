package pathe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type fakeGetter struct {
	mu        sync.Mutex
	responses map[string][]byte
	calls     []string
	get       func(context.Context, Operation, string) ([]byte, error)
}

func (f *fakeGetter) Get(ctx context.Context, operation Operation, rawURL string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, string(operation)+" "+rawURL)
	get := f.get
	body, ok := f.responses[rawURL]
	f.mu.Unlock()
	if get != nil {
		return get(ctx, operation, rawURL)
	}
	if !ok {
		return nil, fmt.Errorf("unexpected synthetic URL")
	}
	return append([]byte(nil), body...), nil
}

func (f *fakeGetter) RequestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeGetter) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func completeResponses(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		CinemasURL:                           fixture(t, "cinemas.json"),
		ShowsURL:                             fixture(t, "shows.json"),
		cinemaProgramURL("lille"):            fixture(t, "program-lille.json"),
		cinemaProgramURL("zeta"):             fixture(t, "program-zeta.json"),
		movieShowtimesURL("film-a", "lille"): fixture(t, "showtimes-film-a-lille.json"),
		movieShowtimesURL("film-b", "lille"): fixture(t, "showtimes-film-b-lille.json"),
		eventShowtimesURL("event-a", "lille", "2026-08-16"): fixture(t, "showtimes-event-a-lille-2026-08-16.json"),
	}
}

func TestSyncEndpointGraphAndExactDatasetMapping(t *testing.T) {
	getter := &fakeGetter{responses: completeResponses(t)}
	generated := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	dataset, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: generated})
	if err != nil {
		t.Fatal(err)
	}
	if dataset.Provider != schedule.ProviderPathe || dataset.Scope != schedule.ScopeAll || dataset.GeneratedAt != generated || dataset.Window != (schedule.Window{From: "2026-08-15", Through: "2026-08-16"}) {
		t.Fatalf("metadata=%+v", dataset)
	}
	if len(dataset.Theaters) != 2 || dataset.Theaters[0].ProviderID != "lille" || dataset.Theaters[1].ProviderID != "zeta" || !reflect.DeepEqual(dataset.Theaters[0].AvailableDates, []string{"2026-08-15", "2026-08-16"}) || len(dataset.Theaters[1].AvailableDates) != 0 {
		t.Fatalf("theaters=%+v", dataset.Theaters)
	}
	if summary.Cinemas != 2 || summary.Movies != 2 || summary.Events != 1 || summary.Jobs != 3 || summary.Requests != 7 || summary.Showtimes != 4 || summary.GeneratedAt != generated {
		t.Fatalf("summary=%+v", summary)
	}
	if len(dataset.Showtimes) != 4 {
		t.Fatalf("showtimes=%+v", dataset.Showtimes)
	}
	byID := map[string]schedule.ShowtimeRecord{}
	for _, showing := range dataset.Showtimes {
		byID[showing.ProviderShowingID] = showing
	}
	first := byID["V3308S135392"]
	if first.ID != "pathe-showing-V3308S135392" || first.TheaterID != "pathe-lille" || first.Movie.ProviderID != "film-a" || first.Movie.Slug != "pathe-film-film-a" || first.Movie.Title != "Film A" || first.Movie.RuntimeMinutes != 110 || first.Movie.PosterURL != "https://www.pathe.fr/posters/film-a.jpg" || !reflect.DeepEqual(first.Movie.Genres, []string{"Drame", "Aventure"}) || first.Language != schedule.LanguageVF || first.ProviderVersion != "vf" || first.Format != schedule.FormatIMAX || first.Room != "IMAX" || first.BookingURL != "https://s.pathe.fr/fr/V3308S135392/booking" || first.EndTime.Sub(first.StartTime) != 130*time.Minute {
		t.Fatalf("first=%+v", first)
	}
	postMidnight := byID["V3308S135393"]
	if postMidnight.ServiceDate != "2026-08-15" || postMidnight.StartTime.Format("2006-01-02 15:04") != "2026-08-16 01:30" || postMidnight.EndTime.Sub(postMidnight.StartTime) != 110*time.Minute || postMidnight.Language != schedule.LanguageVOSTFR || postMidnight.ProviderVersion != "vost" || postMidnight.Format != schedule.Format4DX || postMidnight.Room != "4" {
		t.Fatalf("postMidnight=%+v", postMidnight)
	}
	filmB := byID["V3308S135394"]
	if filmB.Language != schedule.LanguageVFSME || filmB.Format != schedule.Format3D || filmB.Movie.PosterURL != "" || filmB.StartTime.Format(providerTimeLayout) != "2026-08-16 23:30:00" || filmB.EndTime.Format(providerTimeLayout) != "2026-08-17 01:05:00" {
		t.Fatalf("filmB=%+v", filmB)
	}
	event := byID["V3308S135395"]
	if event.Language != schedule.LanguageVO || event.Format != schedule.FormatICE || event.Movie.PosterURL != "https://media.pathe.fr/posters/event-a.jpg" || event.Movie.RuntimeMinutes != 180 || event.EndTime.Sub(event.StartTime) != 200*time.Minute {
		t.Fatalf("event=%+v", event)
	}

	calls := getter.callList()
	sort.Strings(calls)
	wantCalls := []string{
		string(OperationCinemaProgram) + " " + cinemaProgramURL("lille"),
		string(OperationCinemaProgram) + " " + cinemaProgramURL("zeta"),
		string(OperationCinemas) + " " + CinemasURL,
		string(OperationEventTimes) + " " + eventShowtimesURL("event-a", "lille", "2026-08-16"),
		string(OperationMovieTimes) + " " + movieShowtimesURL("film-a", "lille"),
		string(OperationMovieTimes) + " " + movieShowtimesURL("film-b", "lille"),
		string(OperationShows) + " " + ShowsURL,
	}
	sort.Strings(wantCalls)
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls=%v want=%v", calls, wantCalls)
	}
	for _, call := range calls {
		if call == string(OperationEventTimes)+" "+movieShowtimesURL("event-a", "lille") {
			t.Fatal("event pair route called")
		}
	}
}

func TestSyncKeepsCinemaWithEmptyArrayProgramSentinel(t *testing.T) {
	dataset, _, err := Sync(context.Background(), &fakeGetter{responses: completeResponses(t)}, SyncOptions{From: "2026-08-15", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for _, theater := range dataset.Theaters {
		if theater.ProviderID == "zeta" {
			if theater.AvailableDates == nil || len(theater.AvailableDates) != 0 {
				t.Fatalf("empty-program theater dates=%v", theater.AvailableDates)
			}
			return
		}
	}
	t.Fatal("empty-program theater omitted")
}

func TestParseMovieShowtimeResponseSkipsAbsentAdvertisedDate(t *testing.T) {
	location, _ := time.LoadLocation(schedule.Timezone)
	job := showtimeJob{
		movie:   show{slug: "film", title: "Film", runtime: 90, isMovie: true},
		theater: cinema{slug: "cinema", name: "Cinéma", address: "1 rue", city: "Lille", postalCode: "59000"},
		dates:   []string{"2026-08-15"},
	}
	for name, body := range map[string][]byte{
		"absent advertised date": []byte(`{"2026-08-16":[]}`),
		"present empty array":    []byte(`{"2026-08-15":[]}`),
	} {
		t.Run(name, func(t *testing.T) {
			records, err := parseMovieShowtimeResponse(body, job, location)
			if err != nil || records == nil || len(records) != 0 {
				t.Fatalf("records=%+v err=%v", records, err)
			}
		})
	}
	for name, body := range map[string][]byte{
		"malformed present value":      []byte(`{"2026-08-15":{}}`),
		"null present value":           []byte(`{"2026-08-15":null}`),
		"malformed unadvertised value": []byte(`{"2026-08-16":{}}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseMovieShowtimeResponse(body, job, location); err == nil {
				t.Fatal("malformed present date accepted")
			}
		})
	}
}

func TestSyncSkipsAbsentAdvertisedMovieDateWithoutDuplicateRequest(t *testing.T) {
	responses := completeResponses(t)
	responses[movieShowtimesURL("film-a", "lille")] = fixture(t, "showtimes-film-a-missing-advertised.json")
	getter := &fakeGetter{responses: responses}
	dataset, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Requests != 7 || summary.Jobs != 3 || summary.Showtimes != 2 || len(dataset.Showtimes) != 2 || dataset.Window.Through != "2026-08-16" {
		t.Fatalf("summary=%+v window=%+v showtimes=%d", summary, dataset.Window, len(dataset.Showtimes))
	}
	foundTheater := false
	for _, theater := range dataset.Theaters {
		if theater.ProviderID == "lille" && !reflect.DeepEqual(theater.AvailableDates, []string{"2026-08-15", "2026-08-16"}) {
			t.Fatalf("available dates=%v", theater.AvailableDates)
		}
		if theater.ProviderID == "lille" {
			foundTheater = true
		}
	}
	if !foundTheater {
		t.Fatal("theater omitted")
	}
	wantCall := string(OperationMovieTimes) + " " + movieShowtimesURL("film-a", "lille")
	count := 0
	for _, call := range getter.callList() {
		if call == wantCall {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("movie pair request count=%d", count)
	}
}

func TestSyncOutputDeterministicDespiteOutOfOrderCompletion(t *testing.T) {
	responses := completeResponses(t)
	run := func(delays map[string]time.Duration) schedule.Dataset {
		getter := &fakeGetter{responses: responses}
		getter.get = func(ctx context.Context, _ Operation, rawURL string) ([]byte, error) {
			timer := time.NewTimer(delays[rawURL])
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
				return responses[rawURL], nil
			}
		}
		dataset, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		return dataset
	}
	first := run(map[string]time.Duration{cinemaProgramURL("lille"): 10 * time.Millisecond, movieShowtimesURL("film-a", "lille"): 10 * time.Millisecond})
	second := run(map[string]time.Duration{cinemaProgramURL("zeta"): 10 * time.Millisecond, eventShowtimesURL("event-a", "lille", "2026-08-16"): 10 * time.Millisecond})
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("non-deterministic output:\n%s\n%s", firstJSON, secondJSON)
	}
}

func TestSyncFiltersAdvertisedLowerBoundBeforeShowtimeJobs(t *testing.T) {
	responses := completeResponses(t)
	getter := &fakeGetter{responses: responses}
	dataset, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-16", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Movies != 1 || summary.Events != 1 || summary.Jobs != 2 || summary.Requests != 6 || len(dataset.Showtimes) != 2 || dataset.Window.Through != "2026-08-16" {
		t.Fatalf("dataset=%+v summary=%+v calls=%v", dataset, summary, getter.callList())
	}
	for _, call := range getter.callList() {
		if call == string(OperationMovieTimes)+" "+movieShowtimesURL("film-a", "lille") {
			t.Fatal("stale movie pair job created")
		}
	}
}

func TestSyncRejectsDuplicateShowingOrphanAndEmptyDataset(t *testing.T) {
	t.Run("empty cinema list", func(t *testing.T) {
		responses := completeResponses(t)
		responses[CinemasURL] = []byte(`[]`)
		_, _, err := Sync(context.Background(), &fakeGetter{responses: responses}, SyncOptions{From: "2026-08-15", Now: time.Now()})
		if err == nil || !errors.Is(err, schedule.ErrDatasetValidation) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("same suffix with distinct full token", func(t *testing.T) {
		responses := completeResponses(t)
		body := fixture(t, "showtimes-duplicate.json")
		responses[movieShowtimesURL("film-b", "lille")] = bytes.Replace(body, []byte("V3308S135392"), []byte("V1S135392"), 1)
		dataset, _, err := Sync(context.Background(), &fakeGetter{responses: responses}, SyncOptions{From: "2026-08-15", Now: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		identities := map[string]bool{}
		for _, showing := range dataset.Showtimes {
			identities[showing.ProviderShowingID] = true
		}
		if !identities["V3308S135392"] || !identities["V1S135392"] {
			t.Fatalf("identities=%v", identities)
		}
	})
	t.Run("duplicate showing", func(t *testing.T) {
		responses := completeResponses(t)
		responses[movieShowtimesURL("film-b", "lille")] = fixture(t, "showtimes-duplicate.json")
		_, _, err := Sync(context.Background(), &fakeGetter{responses: responses}, SyncOptions{From: "2026-08-15", Now: time.Now()})
		if err == nil || !errors.Is(err, schedule.ErrDatasetValidation) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("orphan show", func(t *testing.T) {
		responses := completeResponses(t)
		responses[cinemaProgramURL("lille")] = fixture(t, "program-orphan.json")
		if _, _, err := Sync(context.Background(), &fakeGetter{responses: responses}, SyncOptions{From: "2026-08-15", Now: time.Now()}); err == nil {
			t.Fatal("orphan reference accepted")
		}
	})
	t.Run("unknown version", func(t *testing.T) {
		responses := completeResponses(t)
		responses[movieShowtimesURL("film-b", "lille")] = fixture(t, "showtimes-unknown-version.json")
		if _, _, err := Sync(context.Background(), &fakeGetter{responses: responses}, SyncOptions{From: "2026-08-15", Now: time.Now()}); err == nil {
			t.Fatal("unknown version accepted")
		}
	})
	t.Run("empty active dataset", func(t *testing.T) {
		responses := completeResponses(t)
		responses[cinemaProgramURL("lille")] = []byte(`{"days":{},"shows":{}}`)
		_, _, err := Sync(context.Background(), &fakeGetter{responses: responses}, SyncOptions{From: "2026-08-15", Now: time.Now()})
		if err == nil || !errors.Is(err, schedule.ErrDatasetValidation) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestSyncRejectsInvalidFromBeforeRequests(t *testing.T) {
	getter := &fakeGetter{responses: completeResponses(t)}
	if _, _, err := Sync(context.Background(), getter, SyncOptions{From: "15-08-2026", Now: time.Now()}); err == nil || getter.RequestCount() != 0 {
		t.Fatalf("requests=%d err=%v", getter.RequestCount(), err)
	}
}
