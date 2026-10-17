package ocine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"

	"messeances/api/internal/schedule"
)

func fixturePage(id, content string) []byte {
	name := map[string]string{"309": "Ociné Maubeuge", "310": "Ociné Saint-Omer", "461": "Ociné Dunkerque"}[id]
	return []byte(`<html><head><meta charset="utf-8"></head><body><script>{"theaterID":"` + id + `","theaterName":"` + name + `"}</script>` + content + `</body></html>`)
}

func TestSourceIdentityAndGenuineAbsentAddress(t *testing.T) {
	for _, id := range cinemaIDs {
		empty := fixturePage(id, `<script>popup='<strong>Adresse :</strong><span></span>';</script>`)
		address, city, postal, err := parseAddress(empty, id)
		if err != nil || address != "" || postal != "" || city == "" {
			t.Fatal("missing address changed", id, err)
		}
		bad := []byte(strings.Replace(string(empty), "Ociné ", "Other cinema ", 1))
		if _, _, _, err := parseAddress(bad, id); err == nil {
			t.Fatal("wrong source name accepted")
		}
	}
}

func TestBookingEpochRetainsPublishedSeconds(t *testing.T) {
	g := fullFixture()
	old := strconv.FormatInt(fixtureInstant("18h00").Unix(), 10)
	next := strconv.FormatInt(fixtureInstant("18h00").Unix()+30, 10)
	g.grids["309"] = fixtureGrid("309", fixtureFilm("309", "123", strings.ReplaceAll(fixtureSession("309", "123", "VF", "4", "18h00"), "/D"+old+"/", "/D"+next+"/")))
	d, _, err := Sync(t.Context(), g, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range d.Showtimes {
		if s.TheaterID == "ocine-309" && (s.StartTime.Second() != 30 || !strings.Contains(s.ProviderShowingID, "-"+next+"-")) {
			t.Fatal("booking instant rounded", s.ProviderShowingID)
		}
	}
}

func TestSessionFormatOverridesDayTags(t *testing.T) {
	if got := formatTags(map[string]bool{"3d": true}, map[string]bool{"2d": true}); got != schedule.Format2D {
		t.Fatal("explicit session 2D replaced by day format", got)
	}
}
func TestCalendarYearBoundaryAndLongAdvertisedHorizon(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 12, 31, 0, 0, 0, 0, loc)
	weekdays := []string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}
	var headers []string
	for i := range 45 {
		d := start.AddDate(0, 0, i)
		headers = append(headers, fmt.Sprintf("%s %d", weekdays[d.Weekday()], d.Day()))
	}
	dates, err := resolveDates(headers, start.AddDate(0, 0, -7), loc)
	if err != nil || len(dates) != 45 || dates[1].Format(time.DateOnly) != "2027-01-01" {
		t.Fatal("calendar boundary/horizon", err)
	}
	headers[1] = "sam. 1"
	if _, err := resolveDates(headers, start, loc); err == nil {
		t.Fatal("contradictory calendar accepted")
	}
}

func fixtureFilm(id, film, rows string) string {
	return `<div class="hr_film"><h2><a href="https://www.ocine.fr/` + schedule.OcineSlug(id) + `/film/` + film + `/" title="Voir la fiche du film L'été &amp; l'amitié"><span>AVP</span> L'été</a></h2><span class="hr_dur">Durée : 1h30</span><img class="hr_aff" src="https://fr.web.img6.acsta.net/r_160_240/pictures/test.jpg"><div class="hr_jour"><span class="nom_jour">Sam.</span>17</div><div class="tab_seances jour1"><div class="frow"><div class="celtags"><img class="tag-VF"><img class="tag-VO"><img class="tag-3D"></div>` + rows + `</div></div></div>`
}

func fixtureGrid(id, films string) []byte {
	return fixturePage(id, `<div class="cadre-in"><div><div class="seances_print"></div>`+films+`</div></div><div id="mod_ccweb_cine_jeu_large"></div><footer id="footer_new"></footer>`)
}
func fixtureSession(id, film, version, hall, clock string) string {
	t := fixtureInstant(clock)
	return `<a class="seance hr_seance" href="https://www.ocine.fr/` + schedule.OcineSlug(id) + `/reserver/F` + film + `/D` + strconv.FormatInt(t.Unix(), 10) + `/` + version + `/` + hall + `/"><span class="hor">` + clock + `</span><img class="tag-SAL04"></a>`
}
func fixtureInstant(clock string) time.Time {
	loc, _ := time.LoadLocation(schedule.Timezone)
	m := clockPattern.FindStringSubmatch(clock)
	h, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	return time.Date(2026, 10, 17, h, minute, 0, 0, loc)
}

type fixtureGetter struct {
	grids       map[string][]byte
	checkoutErr error
	requests    atomic.Int32
}

func (g *fixtureGetter) RequestCount() int { return int(g.requests.Load()) }
func (g *fixtureGetter) Page(ctx context.Context, id, route string) ([]byte, error) {
	g.requests.Add(1)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if route == "seances" {
		return g.grids[id], nil
	}
	return fixturePage(id, `<script>popup='<strong>Adresse :</strong><span>Rue de l\'été &amp; du port<br><b>Centre</b></span><br><span>59 140 Dunkerque</span>';</script>`), nil
}
func (g *fixtureGetter) Checkout(ctx context.Context, raw string) ([]byte, error) {
	g.requests.Add(1)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if g.checkoutErr != nil {
		return nil, g.checkoutErr
	}
	for _, id := range cinemaIDs {
		f, e, _, _, ok := schedule.OcineBookingParts(raw, id)
		if !ok {
			continue
		}
		epoch, _ := strconv.ParseInt(e, 10, 64)
		loc, _ := time.LoadLocation(schedule.Timezone)
		start := time.Unix(epoch, 0).In(loc)
		return fixturePage(id, `<script>{"movieID":"`+f+`","date_showtime":"`+start.Format(time.RFC3339)+`","auditorium_showtime":"salle-7"}</script><p>fin prévue à `+start.Add(100*time.Minute).Format("15h04")+`</p>`), nil
	}
	return nil, errShape
}
func fullFixture() *fixtureGetter {
	g := &fixtureGetter{grids: map[string][]byte{}}
	for _, id := range cinemaIDs {
		rows := fixtureSession(id, "123", "VF", "4", "18h00") + fixtureSession(id, "123", "VO", "4", "21h00") + `<div class="seance hr_seance past"><span class="hor">10h00</span></div>`
		g.grids[id] = fixtureGrid(id, fixtureFilm(id, "atelier-local", rows)+fixtureFilm(id, "456", fixtureSession(id, "456", "VF", "4", "18h00")))
	}
	return g
}
func fixtureOptions() SyncOptions {
	return SyncOptions{From: "2026-10-17", Now: fixtureInstant("12h00")}
}

func TestSyncFaithfulCompleteCensus(t *testing.T) {
	g := fullFixture()
	d, s, err := Sync(t.Context(), g, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Theaters) != 3 || len(d.Showtimes) != 12 || s.EndsAttempted != 9 || s.EndsRecovered != 9 || s.RoomsMissing != 3 || s.Requests != 15 || len(d.Coverage) != 0 {
		t.Fatalf("unexpected census: %+v", s)
	}
	for _, th := range d.Theaters {
		if th.Address != "Rue de l'été & du port, Centre" || th.City != "Dunkerque" || th.PostalCode != "59140" || len(th.AvailableDates) != 1 {
			t.Fatalf("address/calendar: %+v", th)
		}
	}
	for _, s := range d.Showtimes {
		if s.Movie.Title != "L'été & l'amitié" || s.Movie.PosterURL != "https://fr.web.img6.acsta.net/pictures/test.jpg" || s.Format != schedule.Format3D {
			t.Fatalf("movie/format: %+v", s)
		}
		if s.BookingURL == "" {
			if s.Language != "" || s.ProviderVersion != "" || s.Room != "" || !s.EndTime.Equal(s.StartTime) {
				t.Fatal("invented fallback fields")
			}
		} else if s.Room != "Salle 4" || s.EndTime.Sub(s.StartTime) != 100*time.Minute {
			t.Fatal("lost published room/end")
		}
	}
}

func TestSyncAtomicFailuresDedupAndEmpty(t *testing.T) {
	for _, kind := range []string{"missing-cinema", "truncated", "wrong-identity", "epoch-mismatch", "duplicate-conflict", "checkout-failure", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			g := fullFixture()
			ctx := t.Context()
			switch kind {
			case "missing-cinema":
				delete(g.grids, "461")
			case "truncated":
				g.grids["309"] = []byte(strings.ReplaceAll(string(g.grids["309"]), "</html>", ""))
			case "wrong-identity":
				g.grids["309"] = []byte(strings.ReplaceAll(string(g.grids["309"]), `"theaterID":"309"`, `"theaterID":"310"`))
			case "epoch-mismatch":
				g.grids["309"] = []byte(strings.ReplaceAll(string(g.grids["309"]), ">18h00<", ">19h00<"))
			case "duplicate-conflict":
				g.grids["309"] = fixtureGrid("309", fixtureFilm("309", "123", fixtureSession("309", "123", "VF", "4", "18h00")+fixtureSession("309", "123", "VO", "4", "18h00")))
			case "checkout-failure":
				g.checkoutErr = errors.New("synthetic failure")
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			d, _, err := Sync(ctx, g, fixtureOptions())
			if err == nil || len(d.Showtimes) != 0 || len(d.Theaters) != 0 {
				t.Fatal("partial dataset escaped", err)
			}
		})
	}
	g := fullFixture()
	for _, id := range cinemaIDs {
		row := fixtureSession(id, "123", "VF", "4", "18h00")
		g.grids[id] = fixtureGrid(id, fixtureFilm(id, "123", row+row))
	}
	d, s, err := Sync(t.Context(), g, fixtureOptions())
	if err != nil || len(d.Showtimes) != 3 || s.EndsAttempted != 3 {
		t.Fatal("exact dedup", err, s)
	}
	for _, id := range cinemaIDs {
		g.grids[id] = fixtureGrid(id, "")
	}
	d, _, err = Sync(t.Context(), g, fixtureOptions())
	if err != nil || len(d.Theaters) != 3 || len(d.Showtimes) != 0 {
		t.Fatal("validated empty census", err)
	}
	if _, err := schedule.PreparePublication(d); err != nil {
		t.Fatal("complete empty publication", err)
	}
	d.Theaters = d.Theaters[:2]
	if schedule.ValidateDataset(d, true) == nil {
		t.Fatal("incomplete empty census accepted")
	}
	if _, err := schedule.PreparePublication(d); err == nil {
		t.Fatal("incomplete empty publication accepted")
	}
}

func TestOptionalFieldsCharsetAndRoomRecovery(t *testing.T) {
	g := fullFixture()
	for _, id := range cinemaIDs {
		grid := string(fixtureGrid(id, fixtureFilm(id, "123", strings.ReplaceAll(fixtureSession(id, "123", "VF", "4", "18h00"), `<img class="tag-SAL04">`, ""))))
		grid = strings.ReplaceAll(grid, `<span class="hr_dur">Durée : 1h30</span>`, "")
		grid = strings.ReplaceAll(grid, `<img class="hr_aff" src="https://fr.web.img6.acsta.net/r_160_240/pictures/test.jpg">`, "")
		grid = strings.ReplaceAll(grid, `charset="utf-8"`, `charset="iso-8859-1"`)
		encoded, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(grid))
		if err != nil {
			t.Fatal(err)
		}
		g.grids[id] = encoded
	}
	d, _, err := Sync(t.Context(), g, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range d.Showtimes {
		if s.Movie.Title != "L'été & l'amitié" || s.Movie.RuntimeMinutes != 0 || s.Movie.PosterURL != "" || s.Room != "Salle 7" {
			t.Fatal("missing/decoded source fields")
		}
	}
}

func TestCalendarMidnightDSTAndCheckoutUnknowns(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	for _, test := range []struct {
		day, clock string
		count      int
	}{{"2026-03-29", "02h30", 0}, {"2026-10-25", "02h30", 2}, {"2026-10-17", "00h30", 1}} {
		day, _ := time.ParseInLocation(time.DateOnly, test.day, loc)
		if got := len(clockInstants(day, test.clock, loc)); got != test.count {
			t.Fatal(test, got)
		}
	}
	for _, test := range []struct {
		start, end string
		wantErr    bool
	}{{"2026-10-17T23:30:00+02:00", "01h30", false}, {"2026-10-25T01:30:00+02:00", "02h30", true}, {"2026-10-17T18:00:00+02:00", "", false}} {
		start, _ := time.Parse(time.RFC3339, test.start)
		epoch := strconv.FormatInt(start.Unix(), 10)
		s := schedule.ShowtimeRecord{StartTime: start, BookingURL: "https://www.ocine.fr/maubeuge/reserver/F123/D" + epoch + "/VF/4/"}
		content := `<script>{"movieID":"123","date_showtime":"` + test.start + `"}</script>`
		if test.end != "" {
			content += "<p>fin prévue à " + test.end + "</p>"
		}
		end, room, err := parseCheckout(fixturePage("309", content), s, "309", loc)
		if (err != nil) != test.wantErr {
			t.Fatal(test, err)
		}
		if !test.wantErr && (room != "" || test.end == "" && !end.Equal(start)) {
			t.Fatal("unknown fields invented")
		}
	}
	date, _ := time.ParseInLocation(time.DateOnly, "2026-10-25", loc)
	movie := schedule.MovieRecord{ProviderID: "123"}
	epoch := time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC).Unix()
	body := `<a class="hr_seance" href="https://www.ocine.fr/maubeuge/reserver/F123/D` + fmt.Sprint(epoch) + `/VO/4/"><span class="hor">02h30</span></a>`
	doc, _, _ := document(fixturePage("309", body))
	n := byClass(doc, "hr_seance")[0]
	if _, err := readSession(n, "02h30", date, movie, "309", nil, loc); err != nil {
		t.Fatal("epoch failed to disambiguate DST", err)
	}
}
