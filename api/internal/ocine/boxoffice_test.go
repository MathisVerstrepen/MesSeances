package ocine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/boxoffice"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestBoxofficeSyncRetainsBookingsDatesAndCheckoutFacts(t *testing.T) {
	o := fixtureOptions()
	client, err := newClient([]*http.Client{{CheckRedirect: checkoutRedirect, Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		for _, id := range cinemaIDs {
			if boxofficeURL(req.URL, id) {
				site := ocineBoxofficeSite(id)
				body := map[string]string{
					site.origin + boxoffice.CatalogPath:                                           fmt.Sprintf(`{"data":{"allTheater":{"nodes":[{"id":%q,"name":"Ociné fixture","timeZone":"Europe/Paris","practicalInfo":{"location":{"address":"1 rue","city":"Ville","zip":"75001"}}}]}}}`, site.sourceID),
					boxoffice.ProgramURL(site.origin, site.sourceID):                              `{"movieIds":{"titleAsc":[123],"releaseAsc":[123]},"scheduledDays":{"123":["2026-10-16"]}}`,
					boxoffice.MoviesURL(site.origin, []string{"123"}):                             `[{"id":123,"title":"Fixture","runtime":5400,"poster":"https://all.web.img.acsta.net/img/aa/test.jpg"}]`,
					boxoffice.ScheduleURL(site.origin, site.sourceID, "2026-10-16", "2026-10-16"): fmt.Sprintf(`{%q:{"schedule":{"123":{"2026-10-16":[{"id":"fixture","startsAt":"2026-10-17T01:00:00","tags":["Localization.Language.French"],"screen":{"name":"Salle 4"},"data":{"ticketing":[{"provider":"default","type":"DESKTOP","urls":["https://achat.ocine.fr/%s/r/45"]}]}}]}}}}`, site.sourceID, schedule.OcineSlug(id)),
				}[req.URL.String()]
				if body == "" || !json.Valid([]byte(body)) {
					t.Errorf("invalid fixture request %s", req.URL.Path)
				}
				r := testResponse(http.StatusOK, body)
				r.Request = req
				r.Header.Set("Content-Type", "application/json")
				return r, nil
			}
			if _, ok := schedule.OcineEntryID(req.URL.String(), id); ok {
				start := fixtureInstant("01h00")
				r := testResponse(http.StatusTemporaryRedirect, "")
				r.Header.Set("Location", fmt.Sprintf("https://achat.ocine.fr/%s/reserver/F678/D%d/VF/4/", schedule.OcineSlug(id), start.Unix()))
				return r, nil
			}
			if _, _, _, _, ok := canonicalCheckoutParts(req.URL.String(), id); ok {
				body := fmt.Sprintf(`<script>{"movieID":"678","date_showtime":%q,"auditorium_showtime":"Salle 4"}</script><p>Fin prévue à 02h45</p>`, fixtureInstant("01h00").Format(time.RFC3339))
				r := testResponse(http.StatusOK, string(fixturePage(id, body)))
				r.Request = req
				return r, nil
			}
		}
		t.Errorf("unapproved request %s", req.URL.Path)
		return nil, errors.New("unexpected request")
	})}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, s, err := Sync(t.Context(), client, o)
	if err != nil || len(d.Theaters) != 3 || len(d.Showtimes) != 3 || s.EndsRecovered != 3 {
		t.Fatalf("incomplete Boxoffice sync: %v (%+v)", err, s)
	}
	for _, row := range d.Showtimes {
		if row.ServiceDate != o.From || row.Movie.ProviderID != "123" || row.EndTime.Sub(row.StartTime) != 105*time.Minute ||
			!strings.HasSuffix(row.ProviderShowingID, "-r45") || !strings.HasSuffix(row.BookingURL, "/r/45") || len(d.Coverage) != 0 {
			t.Fatalf("source facts lost: %+v", row)
		}
	}
}

func TestBoxofficeCheckoutRejectsUnboundRedirect(t *testing.T) {
	start := fixtureInstant("18h00")
	epoch := strconv.FormatInt(start.Unix(), 10)
	route := "https://achat.ocine.fr/maubeuge/r/45"
	row := schedule.ShowtimeRecord{ProviderShowingID: "309-123-" + epoch + "-r45", BookingURL: route, StartTime: start,
		Movie: schedule.MovieRecord{ProviderID: "123"}, Language: schedule.LanguageVF}
	for _, target := range []string{
		"https://achat.ocine.fr/dunkerque/reserver/F678/D" + epoch + "/VF/4/",
		"https://achat.ocine.fr/maubeuge/reserver/F678/D" + epoch + "/VO/4/",
		"https://achat.ocine.fr/maubeuge/reserver/F678/D123/VF/4/",
		"https://www.ocine.fr/maubeuge/reserver/F678/D" + epoch + "/VF/4/",
	} {
		client, err := newClient([]*http.Client{{CheckRedirect: checkoutRedirect, Transport: testTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != route {
				t.Errorf("unbound redirect was requested")
			}
			r := testResponse(http.StatusTemporaryRedirect, "")
			r.Header.Set("Location", target)
			return r, nil
		})}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = client.CheckoutBoxoffice(t.Context(), row, "309")
		var bad *RequestError
		if !errors.As(err, &bad) || bad.Kind != syncproxy.FailureRedirect {
			t.Fatalf("unexpected result for unbound redirect: %v", err)
		}
	}
}
