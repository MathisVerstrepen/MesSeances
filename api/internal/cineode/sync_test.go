package cineode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding/charmap"

	"messeances/api/internal/schedule"
)

var fixtureNow = time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)

func film(id string) string {
	return `<div class="hr_film"><div class="hr_film_infos"><h2><a href="https://www.cineode.fr/` + schedule.CineodeSlug(id) + `/film/atelier-local/" title="Voir la fiche du film L'été"><span class="hr_movietags">AVP</span>L'été</a></h2><img class="hr_aff" src="https://fr.web.img6.acsta.net/r_180_240/img/aa/poster.jpg"></div><div class="hr_tablehor"><a class="hr_jour"><span class="nom_jour">Ven.</span>16</a><a class="hr_jour"><span class="nom_jour">Sam.</span>17</a><div class="tab_seances jour1"><div class="ftab"><div class="frow"><div class="fcel celtags"><img src="tag_vf_new.png"><img src="tag_imax_new.png"><img src="tag_ad_new.png"></div><div class="fcel"><span class="hor">20h30</span><span class="hor">20h30</span></div></div><div class="frow"><div class="fcel celtags"><img src="tag_vost_new.png"></div><div class="aucune">Pas de séances</div></div></div></div><div class="tab_seances jour2"><div class="ftab"><div class="frow"><div class="fcel celtags"><img src="tag_vost_new.png"></div><div class="fcel"><span class="hor">0h15</span></div></div></div></div></div></div>`
}
func fixturePage(id, blocks, address string) []byte {
	s := `<html><body><script>dataLayer=[{"theaterID":"` + id + `","theaterName":"Cinéma source"}];</script><div class="soustitre">` + address + `</div><div id="mod_ccweb_cine_jeu_large"><div class="cadre-in"><div><div class="seances_print"></div>` + blocks + `</div></div></div><div id="footer_new"></div></body></html>`
	b, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(s))
	if err != nil {
		panic(err)
	}
	return b
}

type fixtureGetter struct {
	mu     sync.Mutex
	calls  map[string]int
	change func(string, string, []byte) ([]byte, error)
	empty  bool
}

func (g *fixtureGetter) Page(ctx context.Context, id, route string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.calls[id+"/"+route]++
	g.mu.Unlock()
	var b []byte
	if route == "seances" {
		blocks := ""
		if !g.empty && (id == "4640" || id == "597") {
			blocks = film(id)
		}
		address := "1 rue Source, 59000 Lille"
		if id == "991" {
			address = ""
		}
		if id == "4386" {
			address = "D'HAUTEVILLE"
		}
		b = fixturePage(id, blocks, address)
	}
	if g.change != nil {
		return g.change(id, route, b)
	}
	return b, nil
}
func (g *fixtureGetter) RequestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.calls {
		n += c
	}
	return n
}
func newFixtureGetter() *fixtureGetter { return &fixtureGetter{calls: map[string]int{}} }

func TestCensusSourceFactsMetadataAndStableKeys(t *testing.T) {
	g := newFixtureGetter()
	d, s, err := Sync(t.Context(), g, SyncOptions{From: "2026-10-16", Now: fixtureNow})
	if err != nil {
		t.Fatal(err)
	}
	if s.Cinemas != 37 || s.Movies != 1 || s.Showtimes != 4 || s.Requests != 39 || len(d.Coverage) != 0 || len(d.Theaters) != 37 {
		t.Fatal("census/counters/duplicate/coverage", s)
	}
	for _, th := range d.Theaters {
		if g.calls[th.ProviderID+"/seances"] != 1 {
			t.Fatal("programme request duplicated")
		}
		if th.ProviderID == "991" && (th.City != "Montmirail" || th.Address != "" || th.PostalCode != "") {
			t.Fatal("unknown address invented")
		}
		if th.ProviderID == "4386" && (th.City != "Plateau d'Hauteville" || th.Address != "D'HAUTEVILLE" || th.PostalCode != "") {
			t.Fatal("partial address lost")
		}
		if th.ProviderID != "4640" && th.ProviderID != "597" && (th.AvailableDates == nil || len(th.AvailableDates) != 0) {
			t.Fatal("empty cinema lost")
		}
	}
	r := d.Showtimes[0]
	if r.Movie.ProviderID != "atelier-local" || r.Movie.Title != "L'été" || r.Movie.RuntimeMinutes != 0 || r.Movie.PosterURL != "https://fr.web.img6.acsta.net/img/aa/poster.jpg" || r.Format != schedule.FormatIMAX || r.Room != "" || r.FirstPartDurationMinutes != 0 || !r.EndTime.Equal(r.StartTime) || !schedule.CineodeShowingMatches(r.ProviderShowingID, "4640", "atelier-local", r.StartTime) || r.BookingURL != schedule.CineodeWebsite("4640") {
		t.Fatal("source facts changed", r)
	}
	r = d.Showtimes[1]
	if r.ServiceDate != "2026-10-17" || r.Language != schedule.LanguageVOSTFR || r.ProviderVersion != "VOSTF" {
		t.Fatal("calendar date/version changed")
	}
	// From filters retained sessions only, never shifts the source calendar.
	d, _, err = Sync(t.Context(), newFixtureGetter(), SyncOptions{From: "2026-10-17", Now: fixtureNow})
	if err != nil || len(d.Showtimes) != 2 || len(d.Theaters[0].AvailableDates) != 1 {
		t.Fatal("from filter", err)
	}
}

func TestPublishedPostersRuntimeAndMetadataMerge(t *testing.T) {
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	for _, poster := range []string{"https://static.cotecine.fr/tb/Affiches/180x240/4332_638323.JPG", "https://static.cotecine.fr/tb/Affiches/180x240/LE+BON+LA+BRUTE.JPG"} {
		block := strings.Replace(film("4640"), "https://fr.web.img6.acsta.net/r_180_240/img/aa/poster.jpg", poster, 1)
		block = strings.Replace(block, `<img class="hr_aff"`, `<p class="hr_dur">Durée : <strong>1h48</strong> - Sortie : 21 octobre</p><img class="hr_aff"`, 1)
		p, err := parseProgramme(fixturePage("4640", block, "1 rue, 59000 Lille"), "4640", "2026-10-16", fixtureNow, loc)
		if err != nil || len(p.movies) != 1 || p.movies[0].PosterURL != poster || p.movies[0].RuntimeMinutes != 108 || !p.showtimes[0].EndTime.Equal(p.showtimes[0].StartTime) {
			t.Fatal("published poster/runtime rewritten", err)
		}
	}
	block := strings.Replace(film("4640"), "https://fr.web.img6.acsta.net/r_180_240/img/aa/poster.jpg", "https://static.cotecine.fr/tb/Affiches/180x240/r_180_240/a.JPG", 1)
	if _, err := parseProgramme(fixturePage("4640", block, "1 rue, 59000 Lille"), "4640", "2026-10-16", fixtureNow, loc); err == nil {
		t.Fatal("unapproved Cotecine rewrite accepted")
	}
	g := newFixtureGetter()
	g.change = func(id, route string, b []byte) ([]byte, error) {
		if id == "4640" && route == "seances" {
			return []byte(strings.Replace(string(b), `<img class="hr_aff" src="https://fr.web.img6.acsta.net/r_180_240/img/aa/poster.jpg">`, "", 1)), nil
		}
		return b, nil
	}
	d, _, err := Sync(t.Context(), g, SyncOptions{From: "2026-10-16", Now: fixtureNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range d.Showtimes {
		if r.Movie.PosterURL != "https://fr.web.img6.acsta.net/img/aa/poster.jpg" {
			t.Fatal("known metadata not merged")
		}
	}
}

func TestFullRecognizedEmptyCensusAndFailedAcquisition(t *testing.T) {
	g := newFixtureGetter()
	g.empty = true
	d, _, err := Sync(t.Context(), g, SyncOptions{From: "2026-10-16", Now: fixtureNow})
	if err != nil || len(d.Theaters) != 37 || len(d.Showtimes) != 0 {
		t.Fatal("empty census rejected", err)
	}
	partial := d
	partial.Theaters = partial.Theaters[:36]
	if schedule.ValidateDataset(partial, true) == nil {
		t.Fatal("partial empty census accepted")
	}
	for _, mode := range []string{"transport", "missing", "identity", "truncated", "version", "mixed versions", "conflicting duplicate", "conflicting movie", "header", "clock", "orphan", "city"} {
		t.Run(mode, func(t *testing.T) {
			g := newFixtureGetter()
			g.change = func(id, route string, b []byte) ([]byte, error) {
				if route != "seances" || id != "597" {
					return b, nil
				}
				s := string(b)
				switch mode {
				case "transport":
					return nil, context.DeadlineExceeded
				case "missing":
					return nil, nil
				case "identity":
					s = strings.Replace(s, `"theaterID":"597"`, `"theaterID":"592"`, 1)
				case "truncated":
					s = strings.TrimSuffix(s, "</html>")
				case "version":
					s = strings.ReplaceAll(s, "tag_vf_new.png", "tag_unknown.png")
				case "mixed versions":
					s = strings.Replace(s, `<img src="tag_vf_new.png">`, `<img src="tag_vf_new.png"><img src="tag_vost_new.png">`, 1)
				case "conflicting duplicate":
					s = strings.Replace(s, `</div></div><div class="frow">`, `</div></div><div class="frow"><div class="celtags"><img src="tag_vost_new.png"></div><span class="hor">20h30</span></div><div class="frow">`, 1)
				case "conflicting movie":
					s = strings.ReplaceAll(s, "L'\xe9t\xe9", "Other title")
				case "header":
					s = strings.Replace(s, "Sam.</span>17", "Sam.</span>18", 1)
				case "clock":
					s = strings.Replace(s, "20h30", "24h30", 1)
				case "orphan":
					s = strings.Replace(s, `</div><div id="footer_new">`, `</div><div class="hr_film"></div><div id="footer_new">`, 1)
				case "city":
					s = strings.Replace(s, "1 rue Source, 59000 Lille", "Label only", 1)
				}
				return []byte(s), nil
			}
			d, _, err := Sync(t.Context(), g, SyncOptions{From: "2026-10-16", Now: fixtureNow})
			if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatal("partial publication permitted", err)
			}
		})
	}
}

func TestMetadataPrecedenceSentinelAndFailure(t *testing.T) {
	g := newFixtureGetter()
	g.change = func(id, route string, b []byte) ([]byte, error) {
		if id != "981" {
			return b, nil
		}
		switch route {
		case "seances":
			return fixturePage(id, "", "12 rue Brinon, 02300 Chauny"), nil
		case "infos-cine":
			return []byte(`<html><body><script>Adresse : </strong><span>12 rue Brinon</span><br><span>02300 Chauny</span></script></body></html>`), nil
		case "contact":
			return []byte("<html><body><div class=\"page cadre\">7 rue Emile Planchon 34190 Ganges T\xe9l : 0000</div></body></html>"), nil
		}
		return b, nil
	}
	d, _, err := Sync(t.Context(), g, SyncOptions{From: "2026-10-16", Now: fixtureNow})
	if err != nil {
		t.Fatal(err)
	}
	for _, th := range d.Theaters {
		if th.ProviderID == "981" && (th.Address != "7 rue Emile Planchon" || th.PostalCode != "34190" || th.City != "Ganges") {
			t.Fatal("fallback address", th)
		}
	}
	if g.calls["981/infos-cine"] != 1 || g.calls["981/contact"] != 1 {
		t.Fatal("fallback route order")
	}
	g = newFixtureGetter()
	g.change = func(id, route string, b []byte) ([]byte, error) {
		if id == "991" && route == "infos-cine" {
			return nil, context.Canceled
		}
		return b, nil
	}
	if d, _, err := Sync(t.Context(), g, SyncOptions{From: "2026-10-16", Now: fixtureNow}); !errors.Is(err, context.Canceled) || len(d.Theaters) != 0 {
		t.Fatal("optional transport failure swallowed", err)
	}
	for _, address := range []string{"12 rue Source, 59 000 Lille", "12 rue Source - 59000 Lille", "12 rue Source 59000 Lille"} {
		th := schedule.TheaterRecord{}
		setAddress(&th, address)
		if th.PostalCode != "59000" || th.City != "Lille" || th.Address != "12 rue Source" {
			t.Fatal("address grammar", th)
		}
	}
	if safeAddress("12 rue Brinon, 02300 Chauny", "592") == "" {
		t.Fatal("own address sentinel rejected")
	}
}

func TestHeaderSequenceFilmBoundaryRolloverAndDST(t *testing.T) {
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		now     string
		headers []string
		want    string
	}{
		{"2026-12-31", []string{"jeu. 31", "ven. 1", "sam. 2"}, "2027-01-02"},
		{"2026-02-28", []string{"sam. 28", "dim. 1", "lun. 2"}, "2026-03-02"},
	} {
		now, _ := time.Parse(time.DateOnly, tc.now)
		dates, err := resolveDates(tc.headers, now, loc)
		if err != nil || dates[len(dates)-1].Format(time.DateOnly) != tc.want {
			t.Fatal("rollover", err)
		}
	}
	for _, date := range []string{"2026-03-29", "2026-10-25"} {
		day, _ := time.ParseInLocation(time.DateOnly, date, loc)
		if _, err := resolveClock(day, "2h30", loc); err == nil {
			t.Fatal("DST fold/gap accepted")
		}
	}
	_, err = parseProgramme(fixturePage("4640", film("4640")+strings.Replace(film("4640"), "Sam.</span>17", "Dim.</span>18", 1), "1 rue, 59000 Lille"), "4640", "2026-10-16", fixtureNow, loc)
	if err == nil {
		t.Fatal("cross-film header disagreement accepted")
	}
	for _, tc := range []struct {
		tags   string
		format schedule.Format
	}{
		{"3d_new imax", schedule.Format3D}, {"imax_new atmos", schedule.FormatIMAX}, {"atmos 4dx", schedule.FormatDolby}, {"motion_new", schedule.Format4DX}, {"ad_new", schedule.Format2D},
	} {
		tags := `<div class="celtags"><img src="tag_vf_new.png">`
		for _, tag := range strings.Fields(tc.tags) {
			tags += fmt.Sprintf(`<img src="tag_%s.png">`, tag)
		}
		tags += "</div>"
		n, err := html.Parse(strings.NewReader(tags))
		if err != nil {
			t.Fatal(err)
		}
		lang, version, format, err := attributes(n)
		if err != nil || lang != schedule.LanguageVF || version != "VF" || format != tc.format {
			t.Fatal("format priority", err)
		}
	}
}
