package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func newProviderReadHandler(t *testing.T, provider schedule.Provider, r schedule.ShowtimeRecord, theaters []schedule.TheaterRecord, runtime, tmdbID int) http.Handler {
	t.Helper()
	movie := schedule.PublicMovieRecord{ID: 1, IdentityAnchorProvider: r.Provider, IdentityAnchorSourceID: r.Movie.ProviderID, Title: r.Movie.Title, RuntimeMinutes: runtime, PosterURL: r.Movie.PosterURL, UpdatedAt: r.StartTime.UTC()}
	if tmdbID != 0 {
		movie.TMDBID, movie.TMDBRuntimeMinutes = int64(tmdbID), runtime
	}
	d := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: provider, Scope: schedule.ScopeAll, Timezone: schedule.Timezone, GeneratedAt: r.StartTime.UTC(), Window: schedule.Window{From: r.ServiceDate, Through: r.ServiceDate}, Showtimes: []schedule.ShowtimeRecord{r}, Theaters: theaters,
		PublicMovies: []schedule.PublicMovieRecord{movie},
		MovieSources: []schedule.PublicMovieSourceRecord{{Provider: r.Provider, SourceMovieID: r.Movie.ProviderID, SourceSlug: r.Movie.Slug, PublicMovieID: 1, Title: r.Movie.Title, RuntimeMinutes: r.Movie.RuntimeMinutes, PosterURL: r.Movie.PosterURL}},
		MovieAliases: []schedule.MovieSlugAliasRecord{{Slug: r.Movie.Slug, PublicMovieID: 1, Kind: "source", Provider: r.Provider, SourceMovieID: r.Movie.ProviderID}},
	}
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal(err)
	}
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(d)}, schedule.ServiceOptions{Now: func() time.Time { return r.StartTime.Add(-time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	return NewHandler(service, "http://localhost:3000")
}

func TestWebediaPublicReadAPIs(t *testing.T) {
	for _, test := range []struct {
		provider         schedule.Provider
		theater, booking string
		language         schedule.Language
	}{
		{schedule.ProviderGrandEcran, "G028P", "https://achat.grandecran.fr/test/r/123", schedule.LanguageVOSTFR},
		{schedule.ProviderNoeCinemas, "P8088", "https://achat.cinema-laigle.com/reserver/r/123", schedule.LanguageVFSTF},
	} {
		t.Run(string(test.provider), func(t *testing.T) {
			for _, runtime := range []int{0, 123} {
				t.Run(strconv.Itoa(runtime), func(t *testing.T) {
					loc, _ := time.LoadLocation(schedule.Timezone)
					start := time.Date(2026, 9, 14, 19, 0, 0, 0, loc)
					id := test.theater + "-" + strings.Repeat("a", 64)
					r := schedule.ShowtimeRecord{Provider: test.provider, ID: string(test.provider) + "-showing-" + id, ProviderShowingID: id, TheaterID: string(test.provider) + "-" + test.theater, ServiceDate: "2026-09-14", Movie: schedule.MovieRecord{Provider: test.provider, ProviderID: "cEvent_1", Slug: string(test.provider) + "-film-cEvent_1", Title: "Event", PublicMovieID: 1}, StartTime: start, EndTime: start, Language: test.language, ProviderVersion: string(test.language), Format: schedule.Format2D, BookingURL: test.booking}
					handler := newProviderReadHandler(t, schedule.ProviderCombined, r, []schedule.TheaterRecord{
						{Provider: test.provider, ID: r.TheaterID, ProviderID: test.theater, Slug: r.TheaterID, Name: "Test", Address: "1 rue Test", City: "L'Aigle", PostalCode: "61300", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}},
						{Provider: schedule.ProviderUGC, ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC", Address: "1 rue", City: "Paris", PostalCode: "75001", AvailableDates: []string{}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
					}, runtime, 42)
					response := performRequest(t, handler, "/api/v1/theaters?chain="+string(test.provider))
					var theaters []schedule.TheaterCatalogItem
					if json.Unmarshal(response.Body.Bytes(), &theaters) != nil || response.Code != http.StatusOK || len(theaters) != 1 || theaters[0].Provider != test.provider {
						t.Fatalf("chain=%s", response.Body.String())
					}
					for _, slug := range []string{"film-1", r.Movie.Slug} {
						response := performRequest(t, handler, "/api/v1/movies/"+slug+"/showtimes?date="+r.ServiceDate)
						var detail schedule.MovieSchedule
						if json.Unmarshal(response.Body.Bytes(), &detail) != nil || response.Code != http.StatusOK || len(detail.Theaters) != 1 || len(detail.Theaters[0].Showtimes) != 1 {
							t.Fatalf("alias=%s", response.Body.String())
						}
						got := detail.Theaters[0].Showtimes[0]
						if got.Provider != test.provider || got.ID != r.ID || got.Language != test.language || got.Room != "" || !got.EndTime.Equal(got.StartTime) || got.Movie.RuntimeMinutes != runtime || got.BookingURL == nil || *got.BookingURL != r.BookingURL {
							t.Fatalf("DTO=%+v", got)
						}
						if runtime == 0 && got.EstimatedEndTime != nil || runtime > 0 && (got.EstimatedEndTime == nil || !got.EstimatedEndTime.Equal(start.Add(138*time.Minute))) {
							t.Fatalf("estimate=%+v", got)
						}
						if !strings.Contains(response.Body.String(), `"poster_url":null`) {
							t.Fatal("null poster")
						}
					}
					query := "?date=" + r.ServiceDate + "&theaters=" + r.TheaterID
					timeline := performRequest(t, handler, "/api/v1/timeline"+query)
					if timeline.Code != http.StatusOK || !strings.Contains(timeline.Body.String(), `"provider":"`+string(test.provider)+`"`) {
						t.Fatalf("timeline=%s", timeline.Body.String())
					}
					slots := performRequest(t, handler, "/api/v1/search/slot"+query+"&start_after=18:00&finish_before=23:00&buffer_ads=0")
					var results []schedule.SlotResult
					if slots.Code != http.StatusOK || json.Unmarshal(slots.Body.Bytes(), &results) != nil {
						t.Fatalf("slots=%s", slots.Body.String())
					}
					if runtime > 0 && (len(results) != 1 || !results[0].EffectiveEndTime.Equal(start.Add(123*time.Minute))) {
						t.Fatal("slot estimate")
					}
					if runtime == 0 && len(results) != 0 {
						t.Fatal("unknown runtime slot invented")
					}
				})
			}
		})
	}
}
