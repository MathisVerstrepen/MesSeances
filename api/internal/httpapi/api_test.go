package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type fixtureSource struct {
	data schedule.Dataset
}

func (s fixtureSource) Snapshot() schedule.Dataset { return s.data }

func testHandler(t *testing.T) http.Handler {
	return testHandlerWithAdmin(t, AdminOptions{})
}

func testHandlerWithAdmin(t *testing.T, options AdminOptions) http.Handler {
	t.Helper()
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	showtime := func(id, theaterID, movieID, title, poster, clock string) schedule.ShowtimeRecord {
		start, err := time.ParseInLocation("2006-01-02 15:04", "2026-08-15 "+clock, location)
		if err != nil {
			t.Fatal(err)
		}
		record := schedule.ShowtimeRecord{
			ID:                "ugc-showing-" + id,
			ProviderShowingID: id,
			ServiceDate:       "2026-08-15",
			TheaterID:         theaterID,
			Movie: schedule.MovieRecord{
				ProviderID:     movieID,
				Slug:           "ugc-film-" + movieID,
				Title:          title,
				RuntimeMinutes: 100,
				PosterURL:      poster,
			},
			StartTime:       start,
			EndTime:         start.Add(100 * time.Minute),
			Language:        schedule.LanguageVOSTFR,
			ProviderVersion: schedule.LanguageVOSTFR,
			Format:          "2D",
			Room:            "Salle 1",
			BookingURL:      "https://www.ugc.fr/reservationSeances.html?id=" + id,
		}
		if movieID == "200" {
			record.Movie.Enrichment = &schedule.MovieEnrichment{TMDBID: 42, Overview: "Résumé", ReleaseDate: "2026-01-02", Genres: []string{"Drame"}, PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg", BackdropURL: "https://image.tmdb.org/t/p/w780/a.jpg"}
		}
		switch id {
		case "100":
			record.Format = schedule.FormatScreenX
		case "101":
			record.Format = schedule.FormatLaserUltra
		case "102":
			record.Format = schedule.Format4DX
		}
		return record
	}
	data := schedule.Dataset{
		SchemaVersion: schedule.SchemaVersion,
		Provider:      schedule.ProviderUGC,
		Scope:         schedule.ScopeAll,
		GeneratedAt:   time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC),
		Timezone:      schedule.Timezone,
		Window:        schedule.Window{From: "2026-08-15", Through: "2026-08-15"},
		Theaters: []schedule.TheaterRecord{
			{ID: "ugc-25", ProviderID: "25", Slug: "ugc-lille", Name: "UGC Lille", Address: "1 rue de Lille", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
			{ID: "ugc-26", ProviderID: "26", Slug: "ugc-villeneuve", Name: "UGC Villeneuve", Address: "2 rue de Villeneuve", City: "Villeneuve d'Ascq", PostalCode: "59650", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
			{ID: "ugc-99", ProviderID: "99", Slug: "ugc-lyon", Name: "UGC Lyon", Address: "3 rue de Lyon", City: "Lyon", PostalCode: "69000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}},
		},
		Showtimes: []schedule.ShowtimeRecord{
			showtime("100", "ugc-25", "200", "Film A", "https://static.ugc.fr/posters/200.jpg", "12:00"),
			showtime("101", "ugc-26", "200", "Film A", "https://static.ugc.fr/posters/200.jpg", "18:00"),
			showtime("102", "ugc-99", "201", "Film B", "", "12:30"),
		},
	}
	service, err := schedule.NewService(fixtureSource{data: data}, schedule.ServiceOptions{
		DefaultCity: "Lille",
		CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewHandlerWithAdmin(service, "http://localhost:3000", options)
}

func performRequest(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type=%q", got)
	}
	return response
}

func TestTheatersTransport(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/theaters?city=lille")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var theaters []schedule.Theater
	if err := json.Unmarshal(response.Body.Bytes(), &theaters); err != nil {
		t.Fatal(err)
	}
	if len(theaters) != 2 || theaters[0].ID != "ugc-25" || theaters[1].ID != "ugc-26" || theaters[0].PostalCode != "59000" || theaters[0].Provider != schedule.ProviderUGC {
		t.Fatalf("theaters=%+v", theaters)
	}

	empty := performRequest(t, handler, "/api/v1/theaters?chain=pathe")
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != "[]" {
		t.Fatalf("non-UGC status=%d body=%s", empty.Code, empty.Body.String())
	}
}

func TestTheatersKinepolisChainAndCombinedProviderDTOs(t *testing.T) {
	location, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 8, 15, 20, 0, 0, 0, location)
	ugc := schedule.TheaterRecord{Provider: schedule.ProviderUGC, ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC Lille", Address: "Lille", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}
	kine := schedule.TheaterRecord{Provider: schedule.ProviderKinepolis, ID: "kinepolis-LOM", ProviderID: "LOM", Slug: "kinepolis-LOM", Name: "Kinepolis Lomme", City: "Lomme", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{}}
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderCombined, Scope: schedule.ScopeAll, GeneratedAt: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC), Timezone: schedule.Timezone, Window: schedule.Window{From: "2026-08-15", Through: "2026-08-15"}, Theaters: []schedule.TheaterRecord{ugc, kine}, Showtimes: []schedule.ShowtimeRecord{{Provider: schedule.ProviderUGC, ID: "ugc-showing-1", ProviderShowingID: "1", ServiceDate: "2026-08-15", TheaterID: "ugc-25", Movie: schedule.MovieRecord{Provider: schedule.ProviderUGC, ProviderID: "1", Slug: "ugc-film-1", Title: "Film partagé", RuntimeMinutes: 90, Enrichment: &schedule.MovieEnrichment{TMDBID: 42}}, StartTime: start, EndTime: start.Add(90 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: "2D", BookingURL: "https://www.ugc.fr/reservationSeances.html?id=1"}, {Provider: schedule.ProviderKinepolis, ID: "kinepolis-showing-VS1", ProviderShowingID: "VS1", ServiceDate: "2026-08-15", TheaterID: "kinepolis-LOM", Movie: schedule.MovieRecord{Provider: schedule.ProviderKinepolis, ProviderID: "HO1", Slug: "kinepolis-film-HO1", Title: "Film partagé", RuntimeMinutes: 100, Enrichment: &schedule.MovieEnrichment{TMDBID: 42}}, StartTime: start, EndTime: start.Add(100 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: "IMAX", BookingURL: "https://kinepolis.fr/direct-vista-redirect/VS1/0/LOM/0"}}}
	service, err := schedule.NewService(fixtureSource{data: data}, schedule.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service, "http://localhost:3000")
	response := performRequest(t, handler, "/api/v1/theaters?chain=kinepolis")
	var theaters []schedule.Theater
	if err := json.Unmarshal(response.Body.Bytes(), &theaters); err != nil {
		t.Fatal(err)
	}
	if len(theaters) != 1 || theaters[0].Provider != schedule.ProviderKinepolis {
		t.Fatalf("theaters=%+v", theaters)
	}
	combined := performRequest(t, handler, "/api/v1/theaters")
	if !strings.Contains(combined.Body.String(), `"provider":"ugc"`) || !strings.Contains(combined.Body.String(), `"provider":"kinepolis"`) {
		t.Fatalf("combined=%s", combined.Body.String())
	}
	timeline := performRequest(t, handler, "/api/v1/timeline?date=2026-08-15&theaters=kinepolis-LOM")
	if !strings.Contains(timeline.Body.String(), `"provider":"kinepolis"`) || !strings.Contains(timeline.Body.String(), `"slug":"tmdb-film-42"`) {
		t.Fatalf("timeline=%s", timeline.Body.String())
	}
	catalog := performRequest(t, handler, "/api/v1/movies?page_size=1")
	var movies schedule.MovieCatalog
	if err := json.Unmarshal(catalog.Body.Bytes(), &movies); err != nil {
		t.Fatal(err)
	}
	if catalog.Code != http.StatusOK || movies.Total != 1 || len(movies.Items) != 1 || movies.Items[0].Slug != "tmdb-film-42" {
		t.Fatalf("catalog status=%d payload=%+v", catalog.Code, movies)
	}
	detail := performRequest(t, handler, "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15")
	var movieSchedule schedule.MovieSchedule
	if err := json.Unmarshal(detail.Body.Bytes(), &movieSchedule); err != nil {
		t.Fatal(err)
	}
	if detail.Code != http.StatusOK || len(movieSchedule.Theaters) != 2 {
		t.Fatalf("detail status=%d payload=%+v", detail.Code, movieSchedule)
	}
	want := map[string]struct {
		provider string
		booking  string
	}{"ugc-showing-1": {schedule.ProviderUGC, "https://www.ugc.fr/reservationSeances.html?id=1"}, "kinepolis-showing-VS1": {schedule.ProviderKinepolis, "https://kinepolis.fr/direct-vista-redirect/VS1/0/LOM/0"}}
	for _, theater := range movieSchedule.Theaters {
		for _, showtime := range theater.Showtimes {
			expected, exists := want[showtime.ID]
			if !exists || showtime.Provider != expected.provider || showtime.Movie.Provider != expected.provider || showtime.Movie.Slug != "tmdb-film-42" || showtime.BookingURL == nil || *showtime.BookingURL != expected.booking {
				t.Fatalf("showtime=%+v", showtime)
			}
			delete(want, showtime.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing showtimes=%+v", want)
	}
	for _, slug := range []string{"ugc-film-1", "kinepolis-film-HO1"} {
		obsolete := performRequest(t, handler, "/api/v1/movies/"+slug+"/showtimes?date=2026-08-15")
		assertAPIError(t, obsolete, http.StatusNotFound, "not_found", "Film introuvable.")
	}
}

func TestTimelineMediaTransportAndContractIsolation(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/timeline?date=2026-08-15&theaters=ugc-25,ugc-26,ugc-99")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	theaters := payload["theaters"].([]any)
	matched := theaters[0].(map[string]any)["showtimes"].([]any)[0].(map[string]any)
	repeated := theaters[1].(map[string]any)["showtimes"].([]any)[0].(map[string]any)
	unmatched := theaters[2].(map[string]any)["showtimes"].([]any)[0].(map[string]any)
	if matched["backdrop_url"] != "https://image.tmdb.org/t/p/w780/a.jpg" || unmatched["backdrop_url"] != nil {
		t.Fatalf("matched=%+v unmatched=%+v", matched, unmatched)
	}
	if matched["poster_url"] != "https://image.tmdb.org/t/p/w500/a.jpg" || repeated["poster_url"] != "https://image.tmdb.org/t/p/w500/a.jpg" || unmatched["poster_url"] != nil {
		t.Fatalf("matched=%+v repeated=%+v unmatched=%+v", matched, repeated, unmatched)
	}
	if _, exists := unmatched["poster_url"]; !exists {
		t.Fatalf("nullable poster field omitted: %+v", unmatched)
	}
	if movie := matched["movie"].(map[string]any); hasAnyKey(movie, "backdrop_url", "poster_url") {
		t.Fatalf("nested movie gained timeline media: %+v", movie)
	}
	if matched["start_offset_minutes"] != float64(240) || matched["duration_minutes"] != float64(100) {
		t.Fatalf("timeline timing changed: %+v", matched)
	}
	slotResponse := performRequest(t, handler, "/api/v1/search/slot?theaters=ugc-25,ugc-99&date=2026-08-15&start_after=12:00&finish_before=15:00&buffer_ads=0")
	if slotResponse.Code != http.StatusOK {
		t.Fatalf("slot status=%d body=%s", slotResponse.Code, slotResponse.Body.String())
	}
	var slots []map[string]any
	if err := json.Unmarshal(slotResponse.Body.Bytes(), &slots); err != nil {
		t.Fatal(err)
	}
	if len(slots) != 2 || slots[0]["poster_url"] != "https://image.tmdb.org/t/p/w500/a.jpg" || slots[0]["backdrop_url"] != "https://image.tmdb.org/t/p/w780/a.jpg" {
		t.Fatalf("matched slot media=%+v", slots)
	}
	if poster, exists := slots[1]["poster_url"]; !exists || poster != nil {
		t.Fatalf("missing slot poster must serialize as explicit null: %+v", slots[1])
	}
	if backdrop, exists := slots[1]["backdrop_url"]; !exists || backdrop != nil {
		t.Fatalf("missing slot backdrop must serialize as explicit null: %+v", slots[1])
	}
	if showtime := slots[0]["showtime"].(map[string]any); hasAnyKey(showtime, "poster_url", "backdrop_url") || hasAnyKey(showtime["movie"].(map[string]any), "poster_url", "backdrop_url") {
		t.Fatalf("slot media leaked into nested showtime: %+v", showtime)
	}
	movieResponse := performRequest(t, handler, "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15")
	if movieResponse.Code != http.StatusOK {
		t.Fatalf("movie showtimes status=%d body=%s", movieResponse.Code, movieResponse.Body.String())
	}
	var moviePayload map[string]any
	if err := json.Unmarshal(movieResponse.Body.Bytes(), &moviePayload); err != nil {
		t.Fatal(err)
	}
	movieShowtime := moviePayload["theaters"].([]any)[0].(map[string]any)["showtimes"].([]any)[0].(map[string]any)
	if moviePayload["backdrop_url"] != "https://image.tmdb.org/t/p/w780/a.jpg" {
		t.Fatalf("movie showtimes root backdrop=%+v", moviePayload)
	}
	if hasAnyKey(movieShowtime, "poster_url", "backdrop_url", "start_offset_minutes", "duration_minutes") {
		t.Fatalf("timeline fields leaked to movie showtime: %+v", movieShowtime)
	}
	if hasAnyKey(moviePayload["movie"].(map[string]any), "backdrop_url") {
		t.Fatalf("backdrop leaked to movie catalog item: %+v", moviePayload["movie"])
	}
	catalogResponse := performRequest(t, handler, "/api/v1/movies?page_size=2")
	if catalogResponse.Code != http.StatusOK || strings.Contains(catalogResponse.Body.String(), "backdrop_url") {
		t.Fatalf("backdrop leaked to catalog: status=%d body=%s", catalogResponse.Code, catalogResponse.Body.String())
	}
}

func hasAnyKey(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, exists := values[key]; exists {
			return true
		}
	}
	return false
}

func TestMoviesTransport(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/movies?search=film&page=1&page_size=1")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var catalog schedule.MovieCatalog
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Page != 1 || catalog.PageSize != 1 || catalog.Total != 2 || len(catalog.Items) != 1 || catalog.Items[0].Slug != "tmdb-film-42" || catalog.Items[0].PosterURL == nil || catalog.Items[0].TMDBID == nil || *catalog.Items[0].TMDBID != 42 || len(catalog.Items[0].Genres) != 1 {
		t.Fatalf("catalog=%+v", catalog)
	}
	if strings.Contains(response.Body.String(), `"movie":{"slug":"tmdb-film-42","tmdb_id"`) {
		t.Fatal("nested movie contract unexpectedly enriched")
	}
	all := performRequest(t, handler, "/api/v1/movies?page_size=2")
	if !strings.Contains(all.Body.String(), `"tmdb_id":null,"overview":null,"release_date":null,"genres":[]`) {
		t.Fatalf("unmatched null/empty contract missing: %s", all.Body.String())
	}

	empty := performRequest(t, handler, "/api/v1/movies?currently_screened=false")
	if empty.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", empty.Code, empty.Body.String())
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Total != 0 || catalog.Page != 1 || catalog.PageSize != 24 || len(catalog.Items) != 0 {
		t.Fatalf("empty catalog=%+v", catalog)
	}

	for _, test := range []struct {
		name   string
		target string
		want   string
	}{
		{name: "explicit sort", target: "/api/v1/movies?sort=title_desc&page_size=1", want: "ugc-film-201"},
		{name: "missing sort defaults", target: "/api/v1/movies?page_size=1", want: "tmdb-film-42"},
		{name: "invalid sort defaults", target: "/api/v1/movies?sort=invalid&page_size=1", want: "tmdb-film-42"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := performRequest(t, handler, test.target)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var sorted schedule.MovieCatalog
			if err := json.Unmarshal(response.Body.Bytes(), &sorted); err != nil {
				t.Fatal(err)
			}
			if len(sorted.Items) != 1 || sorted.Items[0].Slug != test.want {
				t.Fatalf("catalog=%+v", sorted)
			}
			if strings.Contains(response.Body.String(), "showtime_count") || strings.Contains(response.Body.String(), "showtimes_count") {
				t.Fatalf("internal count leaked: %s", response.Body.String())
			}
		})
	}
}

func TestMovieShowtimesTransport(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15&theaters=ugc-26%20,%20ugc-25")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result schedule.MovieSchedule
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Movie.Slug != "tmdb-film-42" || result.Date != "2026-08-15" || len(result.Theaters) != 2 || result.Theaters[0].ID != "ugc-25" || result.Theaters[0].Showtimes[0].Movie.Slug != "tmdb-film-42" || result.Theaters[0].Showtimes[0].StartTime.Location() != time.UTC {
		t.Fatalf("schedule=%+v", result)
	}
	if result.BackdropURL == nil || *result.BackdropURL != "https://image.tmdb.org/t/p/w780/a.jpg" {
		t.Fatalf("backdrop=%v", result.BackdropURL)
	}
	missing := performRequest(t, handler, "/api/v1/movies/ugc-film-201/showtimes?date=2026-08-15")
	if missing.Code != http.StatusOK {
		t.Fatalf("missing backdrop status=%d body=%s", missing.Code, missing.Body.String())
	}
	var missingPayload map[string]any
	if err := json.Unmarshal(missing.Body.Bytes(), &missingPayload); err != nil {
		t.Fatal(err)
	}
	if backdrop, exists := missingPayload["backdrop_url"]; !exists || backdrop != nil {
		t.Fatalf("missing backdrop must serialize as explicit null: %+v", missingPayload)
	}
	obsolete := performRequest(t, handler, "/api/v1/movies/ugc-film-200/showtimes?date=2026-08-15")
	assertAPIError(t, obsolete, http.StatusNotFound, "not_found", "Film introuvable.")

	notFound := performRequest(t, handler, "/api/v1/movies/inconnu/showtimes?date=2026-08-15")
	assertAPIError(t, notFound, http.StatusNotFound, "not_found", "Film introuvable.")
}

func TestSearchSlotExactTheatersTransport(t *testing.T) {
	handler := testHandler(t)
	response := performRequest(t, handler, "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:00&finish_before=15:00")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var results []schedule.SlotResult
	if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Theater.ID != "ugc-99" || results[0].BufferAdsMinutes != 20 || !results[0].EffectiveStartTime.Equal(results[0].Showtime.StartTime) || !results[0].EffectiveEndTime.Equal(results[0].Showtime.EndTime.Add(20*time.Minute)) {
		t.Fatalf("results=%+v", results)
	}
}

func TestSearchSlotIncludeAdsTransport(t *testing.T) {
	handler := testHandler(t)
	includedResponse := performRequest(t, handler, "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:30&finish_before=14:30")
	excludedResponse := performRequest(t, handler, "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:50&finish_before=14:30&include_ads=false")
	if includedResponse.Code != http.StatusOK || excludedResponse.Code != http.StatusOK {
		t.Fatalf("included status=%d body=%s excluded status=%d body=%s", includedResponse.Code, includedResponse.Body.String(), excludedResponse.Code, excludedResponse.Body.String())
	}
	var included, excluded []schedule.SlotResult
	if err := json.Unmarshal(includedResponse.Body.Bytes(), &included); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(excludedResponse.Body.Bytes(), &excluded); err != nil {
		t.Fatal(err)
	}
	if len(included) != 1 || len(excluded) != 1 {
		t.Fatalf("included=%+v excluded=%+v", included, excluded)
	}
	if !included[0].EffectiveStartTime.Equal(included[0].Showtime.StartTime) || !excluded[0].EffectiveStartTime.Equal(excluded[0].Showtime.StartTime.Add(20*time.Minute)) || !included[0].EffectiveEndTime.Equal(excluded[0].EffectiveEndTime) {
		t.Fatalf("included=%+v excluded=%+v", included[0], excluded[0])
	}
	if included[0].BufferAdsMinutes != 20 || excluded[0].BufferAdsMinutes != 20 || included[0].SlackBeforeMinutes != 0 || excluded[0].SlackBeforeMinutes != 0 || included[0].SlackAfterMinutes != 0 || excluded[0].SlackAfterMinutes != 0 {
		t.Fatalf("included=%+v excluded=%+v", included[0], excluded[0])
	}

	tooLateIncluded := performRequest(t, handler, "/api/v1/search/slot?theaters=ugc-99&date=2026-08-15&start_after=12:50&finish_before=14:30&include_ads=true")
	if tooLateIncluded.Code != http.StatusOK || tooLateIncluded.Body.String() != "[]\n" {
		t.Fatalf("too-late included status=%d body=%s", tooLateIncluded.Code, tooLateIncluded.Body.String())
	}
}

func TestSearchSlotFormatTransport(t *testing.T) {
	handler := testHandler(t)
	filtered := performRequest(t, handler, "/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=11:00&finish_before=15:00&buffer_ads=0&format=SCREENX")
	if filtered.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", filtered.Code, filtered.Body.String())
	}
	var results []schedule.SlotResult
	if err := json.Unmarshal(filtered.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Showtime.ID != "ugc-showing-100" || results[0].Showtime.Format != schedule.FormatScreenX {
		t.Fatalf("results=%+v", results)
	}
	omitted := performRequest(t, handler, "/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=11:00&finish_before=21:00&buffer_ads=0")
	if omitted.Code != http.StatusOK || !strings.Contains(omitted.Body.String(), `"format":"SCREENX"`) || !strings.Contains(omitted.Body.String(), `"format":"LASER_ULTRA"`) {
		t.Fatalf("omitted format status=%d body=%s", omitted.Code, omitted.Body.String())
	}
}

func TestInvalidQueriesTransport(t *testing.T) {
	handler := testHandler(t)
	tests := []struct {
		name    string
		target  string
		message string
	}{
		{"page zero", "/api/v1/movies?page=0", "Le paramètre page doit être un entier supérieur ou égal à 1."},
		{"page omitted value", "/api/v1/movies?page=", "Le paramètre page doit être un entier supérieur ou égal à 1."},
		{"page text", "/api/v1/movies?page=un", "Le paramètre page doit être un entier supérieur ou égal à 1."},
		{"page size zero", "/api/v1/movies?page_size=0", "Le paramètre page_size doit être un entier compris entre 1 et 100."},
		{"page size capped", "/api/v1/movies?page_size=101", "Le paramètre page_size doit être un entier compris entre 1 et 100."},
		{"screened boolean", "/api/v1/movies?currently_screened=non", "Le paramètre currently_screened doit être true ou false."},
		{"screened numeric boolean", "/api/v1/movies?currently_screened=1", "Le paramètre currently_screened doit être true ou false."},
		{"showtimes date required", "/api/v1/movies/tmdb-film-42/showtimes", "Le paramètre date est requis."},
		{"showtimes scopes", "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15&city=Lille&theaters=ugc-25", "Les paramètres city et theaters sont mutuellement exclusifs."},
		{"showtimes unknown theater", "/api/v1/movies/tmdb-film-42/showtimes?date=2026-08-15&theaters=inconnu", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
		{"slot missing scope", "/api/v1/search/slot?date=2026-08-15&start_after=12:00&finish_before=15:00", "Le paramètre city ou theaters est requis."},
		{"slot duplicate scopes", "/api/v1/search/slot?city=Lille&theaters=ugc-25&date=2026-08-15&start_after=12:00&finish_before=15:00", "Les paramètres city et theaters sont mutuellement exclusifs."},
		{"slot empty theater", "/api/v1/search/slot?theaters=&date=2026-08-15&start_after=12:00&finish_before=15:00", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
		{"slot unknown theater", "/api/v1/search/slot?theaters=inconnu&date=2026-08-15&start_after=12:00&finish_before=15:00", "Le paramètre theaters contient un identifiant de cinéma inconnu."},
		{"slot empty format", "/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=12:00&finish_before=15:00&format=", "Le paramètre format doit être ALL, 2D, 3D, IMAX, DOLBY, SCREENX, LASER_ULTRA ou 4DX."},
		{"slot invalid format", "/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=12:00&finish_before=15:00&format=screenx", "Le paramètre format doit être ALL, 2D, 3D, IMAX, DOLBY, SCREENX, LASER_ULTRA ou 4DX."},
		{"slot invalid include ads", "/api/v1/search/slot?city=Lille&date=2026-08-15&start_after=12:00&finish_before=15:00&include_ads=0", "Le paramètre include_ads doit être true ou false."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performRequest(t, handler, test.target)
			assertAPIError(t, response, http.StatusBadRequest, "invalid_query", test.message)
		})
	}
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
	var result errorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error.Code != code || result.Error.Message != message {
		t.Fatalf("error=%+v", result.Error)
	}
}
