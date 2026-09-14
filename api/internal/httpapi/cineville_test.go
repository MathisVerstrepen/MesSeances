package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestCinevillePublicDTOChainTimelineAndAlias(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 8, 15, 19, 0, 0, 0, loc)
	r := schedule.ShowtimeRecord{Provider: schedule.ProviderCineville, ID: "cineville-showing-639-1", ProviderShowingID: "639-1", TheaterID: "cineville-639", ServiceDate: "2026-08-15", Movie: schedule.MovieRecord{Provider: schedule.ProviderCineville, ProviderID: "-693091020261", Slug: "cineville-film--693091020261", Title: "Event", PublicMovieID: 1}, StartTime: start, EndTime: start, Language: schedule.LanguageVFSTF, ProviderVersion: "VF", Format: schedule.FormatIMAX, Room: "4", BookingURL: "https://www.cineville.fr/vad/639/1/9"}
	d := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderCombined, Scope: schedule.ScopeAll, Timezone: schedule.Timezone, GeneratedAt: start.UTC(), Window: schedule.Window{From: r.ServiceDate, Through: r.ServiceDate}, Showtimes: []schedule.ShowtimeRecord{r}, Theaters: []schedule.TheaterRecord{
		{Provider: schedule.ProviderCineville, ID: r.TheaterID, ProviderID: "639", Slug: r.TheaterID, Name: "Katorza", City: "Quimper", PostalCode: "29000", AvailableDates: []string{r.ServiceDate}, AcceptedPasses: []string{}},
		{Provider: schedule.ProviderUGC, ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC", Address: "1 rue", City: "Paris", PostalCode: "75001", AvailableDates: []string{}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
	}, PublicMovies: []schedule.PublicMovieRecord{{ID: 1, IdentityAnchorProvider: schedule.ProviderCineville, IdentityAnchorSourceID: r.Movie.ProviderID, Title: r.Movie.Title, RuntimeMinutes: 123, TMDBID: 42, TMDBRuntimeMinutes: 123, UpdatedAt: start.UTC()}}, MovieSources: []schedule.PublicMovieSourceRecord{{Provider: schedule.ProviderCineville, SourceMovieID: r.Movie.ProviderID, SourceSlug: r.Movie.Slug, PublicMovieID: 1, Title: r.Movie.Title}}, MovieAliases: []schedule.MovieSlugAliasRecord{{Slug: r.Movie.Slug, PublicMovieID: 1, Kind: "source", Provider: schedule.ProviderCineville, SourceMovieID: r.Movie.ProviderID}}}
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal(err)
	}
	service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(d)}, schedule.ServiceOptions{Now: func() time.Time { return start.Add(-time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service, "http://localhost:3000")
	response := performRequest(t, handler, "/api/v1/theaters?chain=cineville")
	var theaters []schedule.TheaterCatalogItem
	if json.Unmarshal(response.Body.Bytes(), &theaters) != nil || response.Code != http.StatusOK || len(theaters) != 1 || theaters[0].Provider != schedule.ProviderCineville {
		t.Fatalf("chain response=%s", response.Body.String())
	}
	combined := performRequest(t, handler, "/api/v1/theaters")
	if !strings.Contains(combined.Body.String(), `"provider":"ugc"`) || !strings.Contains(combined.Body.String(), `"provider":"cineville"`) {
		t.Fatal("combined providers")
	}
	for _, slug := range []string{"film-1", r.Movie.Slug} {
		response := performRequest(t, handler, "/api/v1/movies/"+slug+"/showtimes?date=2026-08-15")
		var detail schedule.MovieSchedule
		if json.Unmarshal(response.Body.Bytes(), &detail) != nil || response.Code != http.StatusOK || len(detail.Theaters) != 1 || len(detail.Theaters[0].Showtimes) != 1 {
			t.Fatalf("alias response=%s", response.Body.String())
		}
		got := detail.Theaters[0].Showtimes[0]
		if got.Provider != schedule.ProviderCineville || got.ID != r.ID || got.Room != "4" || got.Language != schedule.LanguageVFSTF || got.Format != schedule.FormatIMAX || !got.EndTime.Equal(got.StartTime) || got.Movie.RuntimeMinutes != 123 || got.BookingURL == nil || *got.BookingURL != r.BookingURL {
			t.Fatalf("contract=%+v", got)
		}
	}
	timeline := performRequest(t, handler, "/api/v1/timeline?date=2026-08-15&theaters=cineville-639")
	if timeline.Code != http.StatusOK || !strings.Contains(timeline.Body.String(), `"duration_minutes":0`) || !strings.Contains(timeline.Body.String(), `"provider":"cineville"`) {
		t.Fatalf("timeline=%s", timeline.Body.String())
	}
	slots := performRequest(t, handler, "/api/v1/search/slot?theaters=cineville-639&date=2026-08-15&start_after=18:00&finish_before=23:00&buffer_ads=0")
	if slots.Code != http.StatusOK || strings.TrimSpace(slots.Body.String()) != "[]" {
		t.Fatalf("unknown end offered as fit=%s", slots.Body.String())
	}
}
