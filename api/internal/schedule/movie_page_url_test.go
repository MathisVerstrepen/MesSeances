package schedule

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMoviePageURL(t *testing.T) {
	for _, tc := range []struct {
		provider             Provider
		theater, movie, want string
	}{
		{ProviderUGC, "46", "17747", "https://www.ugc.fr/film.html?id=17747"},
		{ProviderPathe, "lille", "le-corset", "https://www.pathe.fr/films/le-corset"},
		{ProviderCineode, "597", "le-corset", "https://www.cineode.fr/arletty/film/le-corset/"},
		{ProviderOcine, "309", "123", "https://www.ocine.fr/maubeuge/film/123/"},
		{ProviderMegarama, "EMS0565", "ABCDE", "https://www.ticketingcine.com/film/ABCDE.html"},
		{ProviderMajestic, "EMS0700", "ABCDE", "https://www.ticketingcine.com/film/ABCDE.html"},
		{ProviderCinewest, "ticketingcine-EMS1185", "ticketingcine-ABCDE", "https://www.ticketingcine.com/film/ABCDE.html"},
		{ProviderVeo, "EMS1361", "ticketingcine-ABCDE", "https://www.ticketingcine.com/film/ABCDE.html"},
		{ProviderMegarama, "EMS0565", "EMS0565-emsx0565HC123", ""},
		{ProviderMajestic, "EMS0700", "EMS0700-emsx0700HC123", ""},
		{ProviderCinewest, "cineoffice-royanlelido", "cineoffice-502", "https://www.cine-royan.com/film.html#/media/502"},
		{ProviderCinewest, "cineoffice-cognaclegalaxy", "cineoffice-1913", "https://www.cine-cognac.com/film.html#/media/1913"},
		{ProviderCinewest, "cineoffice-neverscinemazarin", "cineoffice-4461", "https://www.cinemazarin-nevers.fr/film.html#/media/4461"},
		{ProviderCinewest, "cineoffice-mouanssartouxlastrada", "cineoffice-4222", "https://lastrada.cinewest06.fr/film.html#/media/4222"},
		{ProviderCinewest, "cineoffice-vitreaurore", "cineoffice-1911", "https://www.aurorecinema.fr/film.html#/media/1911"},
		{ProviderCinewest, "cineoffice-mouginslesbalcons", "cineoffice-4263", "https://lesbalcons.cinewest06.fr/film.html#/media/4263"},
		{ProviderCinewest, "cineoffice-ploermelcinelac", "cineoffice-502", "https://cinelac.fr/film.html#/media/502"},
		{ProviderCinewest, "cineoffice-saintesatlanticcine", "cineoffice-502", "https://www.atlantic-cine.fr/film.html#/media/502"},
		{ProviderCinewest, "cineoffice-aurillaclecristal", "cineoffice-502", "https://cineaurillac.fr/film.html#/media/502"},
		{ProviderCinewest, "cineoffice-unknown", "cineoffice-502", ""},
		{ProviderCinewest, "ticketingcine-EMS1185", "cineoffice-502", ""},
		{ProviderCinewest, "cineoffice-royanlelido", "cineoffice-0502", ""},
		{ProviderCinewest, "cineoffice-royanlelido", "cineoffice-502/../503", ""},
		{ProviderCinewest, "cineoffice-royanlelido", "webediamovies-502", ""},
		{ProviderVeo, "P0793", "cineoffice-502", ""},
		{ProviderVeo, "P0793", "cEvent_1", ""},
		{ProviderKinepolis, "KLOM", "HO123", ""},
		{ProviderMK2, "0004", "HO00006568", ""},
		{ProviderCGR, "W8010", "123", ""},
		{ProviderCineville, "639", "123", ""},
		{ProviderGrandEcran, "G028P", "cEvent_1", ""},
		{ProviderNoeCinemas, "P8088", "cEvent_1", ""},
		{ProviderCinemovida, "P0176", "cEvent_1", ""},
		{ProviderCineode, "unknown", "123", ""},
		{ProviderOcine, "unknown", "123", ""},
		{ProviderUGC, "46", "../17747", ""},
		{ProviderPathe, "lille", "https://evil.example", ""},
	} {
		t.Run(string(tc.provider)+"/"+tc.theater+"/"+tc.movie, func(t *testing.T) {
			theater := TheaterRecord{Provider: tc.provider, ProviderID: tc.theater, ID: string(tc.provider) + "-" + tc.theater}
			movie := MovieRecord{Provider: tc.provider, ProviderID: tc.movie, Slug: "film-213", Title: "Titre modifié"}
			got := moviePageURL(theater, movie)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("unexpected URL %q", *got)
				}
			} else if got == nil || *got != tc.want {
				t.Fatalf("URL=%v want=%q", got, tc.want)
			}
			movie.Provider = ProviderCombined
			if moviePageURL(theater, movie) != nil {
				t.Fatal("mismatched provider must not link")
			}
		})
	}
}

func TestCapturedMoviePageURLValidation(t *testing.T) {
	const valid = "https://www.cgrcinemas.fr/films-a-l-affiche/123-source-title/"
	for _, raw := range []string{valid, "http://www.cgrcinemas.fr/films-a-l-affiche/123-source-title/", strings.Replace(valid, "www.cgrcinemas.fr", "evil.example", 1), strings.Replace(valid, "123-", "1234-", 1), valid + "?secret=x", valid + "#x", strings.Replace(valid, "https://", "https://user@", 1), strings.Replace(valid, "source-title", "../title", 1), strings.Replace(valid, "123", "%31%32%33", 1)} {
		want := raw == valid
		if ValidSourceMoviePageURL(ProviderCGR, "123", raw) != want {
			t.Fatalf("wrong validation: %q", raw)
		}
		movie := MovieRecord{Provider: ProviderCGR, ProviderID: "123", SourcePageURL: raw, Title: "Edited canonical title"}
		got := moviePageURL(TheaterRecord{Provider: ProviderCGR}, movie)
		if want && (got == nil || *got != valid) || !want && got != nil {
			t.Fatalf("wrong URL projection for %q", raw)
		}
	}
}

func TestCapturedKinepolisMoviePage(t *testing.T) {
	const valid = "https://kinepolis.fr/movies/detail/37692/HO00016712/0"
	for _, raw := range []string{valid, strings.Replace(valid, "HO00016712", "HO00016885", 1), strings.Replace(valid, "37692", "037692", 1), strings.Replace(valid, "kinepolis.fr", "kinepolis.fr.evil.example", 1), strings.Replace(valid, "37692", "../37692", 1), valid + "?secret=x", valid + "#x", strings.Replace(valid, "https://", "https://user@", 1)} {
		if ValidSourceMoviePageURL(ProviderKinepolis, "HO00016712", raw) != (raw == valid) {
			t.Fatalf("wrong native URL validation: %q", raw)
		}
	}
	movie := MovieRecord{Provider: ProviderKinepolis, ProviderID: "HO00016712", Title: "Edited title", SourcePageURL: valid}
	got := moviePageURL(TheaterRecord{Provider: ProviderKinepolis, ID: "kinepolis-KLOM"}, movie)
	if got == nil || *got != valid {
		t.Fatal("captured Kinepolis URL not projected")
	}
}

func TestMovieShowtimesMovieURLsUseEachTheatersSource(t *testing.T) {
	data := combinedTestDataset()
	// A single canonical film unites UGC and Kinepolis source identities.
	data.PublicMovies = []PublicMovieRecord{{ID: 213, Title: "Titre canonique modifié"}}
	filtered := []ShowtimeRecord{}
	for _, showing := range data.Showtimes {
		if showing.Movie.ProviderID == "200" || showing.Movie.ProviderID == "HO200" {
			showing.Movie.PublicMovieID = 213
			filtered = append(filtered, showing)
		}
	}
	data.Showtimes = filtered
	service, err := NewService(newTestSource(data), ServiceOptions{Now: testServiceNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []MovieShowtimesQuery{
		{Slug: "film-213", Date: "2026-08-15", Page: 1},
		{Slug: "film-213", Date: "2026-08-15", TheaterIDs: []string{"ugc-25"}},
	} {
		result, err := service.MovieShowtimes(query)
		if err != nil || len(result.Theaters) == 0 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		for _, theater := range result.Theaters {
			if theater.Provider == ProviderUGC {
				if theater.MovieURL == nil || *theater.MovieURL != "https://www.ugc.fr/film.html?id=200" {
					t.Fatalf("wrong UGC source: %+v", theater)
				}
			} else if theater.MovieURL != nil {
				t.Fatalf("unsupported source must be null: %+v", theater)
			}
			raw, err := json.Marshal(theater)
			if err != nil || !strings.Contains(string(raw), `"movie_url":`) {
				t.Fatalf("missing wire field: %s (%v)", raw, err)
			}
		}
	}
}
