package kinepolis

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestSourceMoviePageUsesNativeIdentitiesAndEventPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name                string
		corporate           any
		event, sessionEvent map[string]any
		want                string
	}{
		{"regular", json.Number("37692"), nil, nil, "0"},
		{"active event", "37692", map[string]any{"isActive": true, "code": "0000000035"}, nil, "0000000035"},
		{"session override", "37692", map[string]any{"isActive": true, "code": "0000000035"}, map[string]any{"isActive": true, "code": "0000000042"}, "0000000042"},
		{"inactive session", "37692", map[string]any{"isActive": true, "code": "0000000035"}, map[string]any{"isActive": false, "code": "0000000042"}, "0000000035"},
		{"inactive event", "37692", map[string]any{"isActive": false, "code": "0000000035"}, nil, "0"},
		{"missing corporate", nil, nil, nil, ""},
		{"invalid corporate", "../37692", nil, nil, ""},
		{"fractional corporate", json.Number("37692.5"), nil, nil, ""},
		{"invalid active event", "37692", map[string]any{"isActive": true, "code": "../x"}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			film := parseFilm(map[string]any{"id": "HO00016712", "title": "Edited / canonical title", "corporateId": tc.corporate, "event": tc.event, "sessionEvent": tc.sessionEvent})
			want := ""
			if tc.want != "" {
				want = "https://kinepolis.fr/movies/detail/37692/HO00016712/" + tc.want
			}
			if film.pageURL != want {
				t.Fatalf("page=%q want=%q", film.pageURL, want)
			}
		})
	}
}

func TestParseScheduleRetainsSeparateHOPageIdentities(t *testing.T) {
	body := withCatalogComplexes(t, []byte(`Drupal.settings.variables={"complexes":[],"current_movies":{"films":[{"id":"HO00016712","corporateId":37692,"title":"Godzilla Minus Zero","duration":120},{"id":"HO00016885","corporateId":37692,"title":"Godzilla Minus Zero","duration":120}],"sessions":[{"complexOperator":"KLOM","showtime":"2026-11-06T18:00:00+01:00","vistaSessionId":"1","film":{"id":"HO00016712"}},{"complexOperator":"KLOM","showtime":"2026-11-06T20:30:00+01:00","vistaSessionId":"2","film":{"id":"HO00016885"}}]}};`))
	data, _, err := parseSchedule(body, "2026-11-06", time.Now())
	if err != nil || len(data.Showtimes) != 2 {
		t.Fatalf("showings=%d err=%v", len(data.Showtimes), err)
	}
	for _, record := range data.Showtimes {
		want := "https://kinepolis.fr/movies/detail/37692/" + record.Movie.ProviderID + "/0"
		if record.Movie.SourcePageURL != want || !schedule.ValidSourceMoviePageURL(schedule.ProviderKinepolis, record.Movie.ProviderID, record.Movie.SourcePageURL) {
			t.Fatal("source page lost or not bound to its HO identity")
		}
		if strings.Contains(record.Movie.SourcePageURL, "godzilla") {
			t.Fatal("title should not determine the native identity URL")
		}
	}
}
