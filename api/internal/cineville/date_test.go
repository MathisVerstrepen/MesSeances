package cineville

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// Independent wire fixture keeps JSON number/string choices separate from Go model types.
func wireDatePage(programDate, eventDate string) []byte {
	return []byte(`{"pageProps":{"cinemaId":639,"cines":[{"id":639,"cine":"katorzaquimper","nom_cine_public":"Katorza","adresse":"","adresse_ville":"Quimper","code_postal_1":"29000"}],"attributs":[],"prog":[{"visa":167934,"titre_cotecine":"Film","movie_data":[],"dates":[{"date":` + programDate + `,"showtimes":[{"id_cinema":639,"id_seance":1,"id_bordereau":9,"salle":4,"heure":"00:15","version":"VF","vfstrfr":"0","relief":"2D","attributs":""}]}]}],"progWithEvents":[{"visa":-693091020261,"titre_cotecine":"Event","movie_data":{"result":false},"dates":[{"date":` + eventDate + `,"showtimes":[{"id_cinema":639,"id_seance":2,"id_bordereau":10,"salle":1,"heure":"00:15","version":"VO","vfstrfr":"0","relief":"2D","attributs":""}]}]}]}}`)
}

type wireDateFetcher struct {
	*fixtureFetcher
	body []byte
}

func (f wireDateFetcher) FetchCinema(_ context.Context, build, route string) ([]byte, error) {
	f.calls = append(f.calls, build+":"+route)
	return f.body, nil
}

func TestSyncNumericDatesInBothProgramArrays(t *testing.T) {
	for _, dates := range [][2]string{{"20260914", "20261201"}, {`"20260914"`, `"20261201"`}, {"20260914", `"20261201"`}} {
		body := wireDatePage(dates[0], dates[1])
		page, err := parsePage(body, fixtureCinema())
		if err != nil || page.Program[0].Dates[0].Date != "20260914" || page.Events[0].Dates[0].Date != "20261201" {
			t.Fatal("date wire decoding failed")
		}
		f := wireDateFetcher{singleFetcher(t, page), body}
		data, summary, err := Sync(t.Context(), f, fixtureOptions())
		if err != nil || summary.Showtimes != 2 || summary.Requests != 2 || data.Window.Through != "2026-12-01" {
			t.Fatalf("sync dates: summary=%+v err=%v", summary, err)
		}
		for i, want := range []string{"2026-09-14T00:15:00+02:00", "2026-12-01T00:15:00+01:00"} {
			s := data.Showtimes[i]
			if s.StartTime.Format(time.RFC3339) != want || s.ServiceDate != want[:10] || !s.EndTime.Equal(s.StartTime) {
				t.Fatal("calendar date, Paris offset or unknown end changed")
			}
		}
		if err := schedule.ValidateDataset(data, true); err != nil {
			t.Fatal("shared validation failed")
		}
	}
}

func TestSyncRejectsMalformedWireDatesInBothProgramArrays(t *testing.T) {
	for _, malformed := range []string{"null", "true", "false", "{}", "[]", "0", "-20260914", "2026914", "202609140", "20260914.0", "2.0260914e7", "20261301", "20260230", `""`, `" 20260914"`, `"20260914 "`, `"2026-09-14"`, `"20260230"`, `"private-synthetic-sentinel"`} {
		for _, array := range []string{"prog", "progWithEvents"} {
			t.Run(array+"/"+malformed, func(t *testing.T) {
				program, event := "20260914", "20261201"
				if array == "prog" {
					program = malformed
				} else {
					event = malformed
				}
				f := wireDateFetcher{singleFetcher(t, fixturePage(fixtureCinema())), wireDatePage(program, event)}
				wantRequests := 2
				if malformed == "null" {
					// Null fails page decoding; other date values fail normalization.
					f.builds = append(f.builds, "build-1")
					wantRequests = 4
				}
				data, _, err := Sync(t.Context(), f, fixtureOptions())
				if f.RequestCount() != wantRequests {
					t.Fatal("unexpected acquisition retry")
				}
				var re *RequestError
				if !errors.As(err, &re) || re.Operation != OperationCinema || re.Kind != syncproxy.FailureInvalidJSON || errors.Is(err, schedule.ErrDatasetValidation) || len(data.Showtimes) != 0 || strings.Contains(err.Error(), "private-synthetic-sentinel") {
					t.Fatal("malformed date not rejected with bounded payload error")
				}
			})
		}
	}
}
