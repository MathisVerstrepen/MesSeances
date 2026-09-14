package httpapi

import (
	"encoding/json"
	"messeances/api/internal/schedule"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNoeCinemasPublicReadAPIs(t *testing.T) {
	for _, runtime := range []int{0, 123} {
		t.Run(strconv.Itoa(runtime), func(t *testing.T) {
			loc, _ := time.LoadLocation(schedule.Timezone)
			start := time.Date(2026, 9, 14, 19, 0, 0, 0, loc)
			id := "P8088-" + strings.Repeat("a", 64)
			r := schedule.ShowtimeRecord{Provider: schedule.ProviderNoeCinemas, ID: "noecinemas-showing-" + id, ProviderShowingID: id, TheaterID: "noecinemas-P8088", ServiceDate: "2026-09-14", Movie: schedule.MovieRecord{Provider: schedule.ProviderNoeCinemas, ProviderID: "cEvent_1", Slug: "noecinemas-film-cEvent_1", Title: "Event", PublicMovieID: 1}, StartTime: start, EndTime: start, Language: schedule.LanguageVFSTF, ProviderVersion: "VFSTF", Format: schedule.Format2D, BookingURL: "https://achat.cinema-laigle.com/reserver/r/123"}
			d := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderCombined, Scope: schedule.ScopeAll, Timezone: schedule.Timezone, GeneratedAt: start.UTC(), Window: schedule.Window{From: r.ServiceDate, Through: r.ServiceDate}, Showtimes: []schedule.ShowtimeRecord{r}, Theaters: []schedule.TheaterRecord{
				{Provider: schedule.ProviderNoeCinemas, ID: r.TheaterID, ProviderID: "P8088", Slug: r.TheaterID, Name: "Noé Test", Address: "1 rue Test", City: "L'Aigle", PostalCode: "61300", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}},
				{Provider: schedule.ProviderUGC, ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC", Address: "1 rue", City: "Paris", PostalCode: "75001", AvailableDates: []string{}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
			}, PublicMovies: []schedule.PublicMovieRecord{{ID: 1, IdentityAnchorProvider: schedule.ProviderNoeCinemas, IdentityAnchorSourceID: r.Movie.ProviderID, Title: r.Movie.Title, RuntimeMinutes: runtime, TMDBID: 42, TMDBRuntimeMinutes: runtime, UpdatedAt: start.UTC()}}, MovieSources: []schedule.PublicMovieSourceRecord{{Provider: schedule.ProviderNoeCinemas, SourceMovieID: r.Movie.ProviderID, SourceSlug: r.Movie.Slug, PublicMovieID: 1, Title: r.Movie.Title}}, MovieAliases: []schedule.MovieSlugAliasRecord{{Slug: r.Movie.Slug, PublicMovieID: 1, Kind: "source", Provider: schedule.ProviderNoeCinemas, SourceMovieID: r.Movie.ProviderID}}}
			if err := schedule.ValidateDataset(d, true); err != nil {
				t.Fatal(err)
			}
			service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(d)}, schedule.ServiceOptions{Now: func() time.Time { return start.Add(-time.Hour) }})
			if err != nil {
				t.Fatal(err)
			}
			handler := NewHandler(service, "http://localhost:3000")
			response := performRequest(t, handler, "/api/v1/theaters?chain=noecinemas")
			var theaters []schedule.TheaterCatalogItem
			if json.Unmarshal(response.Body.Bytes(), &theaters) != nil || response.Code != http.StatusOK || len(theaters) != 1 || theaters[0].Provider != schedule.ProviderNoeCinemas {
				t.Fatalf("chain=%s", response.Body.String())
			}
			for _, slug := range []string{"film-1", r.Movie.Slug} {
				response := performRequest(t, handler, "/api/v1/movies/"+slug+"/showtimes?date=2026-09-14")
				var detail schedule.MovieSchedule
				if json.Unmarshal(response.Body.Bytes(), &detail) != nil || response.Code != http.StatusOK || len(detail.Theaters) != 1 || len(detail.Theaters[0].Showtimes) != 1 {
					t.Fatalf("alias=%s", response.Body.String())
				}
				got := detail.Theaters[0].Showtimes[0]
				if got.Provider != schedule.ProviderNoeCinemas || got.ID != r.ID || got.Language != schedule.LanguageVFSTF || got.Room != "" || !got.EndTime.Equal(got.StartTime) || got.Movie.RuntimeMinutes != runtime || got.BookingURL == nil || *got.BookingURL != r.BookingURL {
					t.Fatalf("DTO=%+v", got)
				}
				if runtime == 0 && got.EstimatedEndTime != nil || runtime > 0 && (got.EstimatedEndTime == nil || !got.EstimatedEndTime.Equal(start.Add(138*time.Minute))) {
					t.Fatal("estimate")
				}
				if !strings.Contains(response.Body.String(), `"poster_url":null`) {
					t.Fatal("null poster")
				}
			}
			timeline := performRequest(t, handler, "/api/v1/timeline?date=2026-09-14&theaters=noecinemas-P8088")
			if timeline.Code != http.StatusOK || !strings.Contains(timeline.Body.String(), `"provider":"noecinemas"`) {
				t.Fatal("timeline")
			}
			slots := performRequest(t, handler, "/api/v1/search/slot?theaters=noecinemas-P8088&date=2026-09-14&start_after=18:00&finish_before=23:00&buffer_ads=0")
			var results []schedule.SlotResult
			if slots.Code != http.StatusOK || json.Unmarshal(slots.Body.Bytes(), &results) != nil {
				t.Fatal("slots")
			}
			if runtime > 0 && (len(results) != 1 || !results[0].EffectiveEndTime.Equal(start.Add(123*time.Minute))) {
				t.Fatal("slot estimate")
			}
			if runtime == 0 && len(results) != 0 {
				t.Fatal("unknown runtime slot invented")
			}
		})
	}
}
