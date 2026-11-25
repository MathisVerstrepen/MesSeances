package cotecine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"

	"messeances/api/internal/schedule"
)

type fixtureGetter struct {
	mu     sync.Mutex
	pages  map[string][]string
	calls  map[string]int
	failOn string
}

func (g *fixtureGetter) Programme(_ context.Context, website string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.calls[website]
	g.calls[website]++
	if website == g.failOn {
		return nil, errors.New("synthetic transport failure")
	}
	pages := g.pages[website]
	if len(pages) == 0 {
		return nil, errors.New("unknown site")
	}
	page := pages[min(n, len(pages)-1)]
	// Cinegroup serves ISO-8859-1.
	body, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(page))
	if err != nil {
		panic(err)
	}
	return body, nil
}

func (g *fixtureGetter) RequestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	total := 0
	for _, n := range g.calls {
		total += n
	}
	return total
}

var paris = mustLocation()

func mustLocation() *time.Location {
	l, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		panic(err)
	}
	return l
}

// now is Wednesday 2026-11-25, 14:00 in Paris.
var now = time.Date(2026, 11, 25, 14, 0, 0, 0, paris)

type fixtureSession struct {
	day     int // 1-based column
	clock   string
	tag     string // version tag file name
	link    bool
	linkSec int64 // overrides the booking instant when non-zero
	format  string
}

type fixtureFilm struct {
	id, title, duration, poster string
	sessions                    []fixtureSession
}

func instant(day int, clock string) time.Time {
	h, _ := strconv.Atoi(clock[:strings.Index(clock, "h")])
	m, _ := strconv.Atoi(clock[strings.Index(clock, "h")+1:])
	date := time.Date(2026, 11, 24+day, h, m, 0, 0, paris)
	if h < 3 {
		date = date.AddDate(0, 0, 1)
	}
	return date
}

// page renders a Cinegroup programme with three day columns starting today.
func page(theater, website string, films ...fixtureFilm) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html><html><head><script>window.dataLayer.push({"theaterID":"%s","theaterName":"Cinéma Test"});</script></head><body><div class="cadre-in"><div><div class="seances_print"><a href="x.pdf">Imprimer</a></div>`, theater)
	for _, f := range films {
		fmt.Fprintf(&b, `<div class="hr_film  aff_l "><div class="hr_film_infos"><h2><a href="%sfilm/%s/" title="Voir la fiche du film %s"><span class="flag_avp">En avant-première</span> %s</a><div class="hr_movietags"><img src="https://static.cotecine.fr/img/tags/tag_touspublics_new.png"/></div></h2>`, website, f.id, f.title, f.title)
		if f.poster != "" {
			fmt.Fprintf(&b, `<img src="%s" class="hr_aff"/>`, f.poster)
		}
		if f.duration != "" {
			fmt.Fprintf(&b, `<p class="hr_p hr_dur">Durée : <strong>%s</strong> - Sortie : <strong>25 nov</strong></p>`, f.duration)
		}
		b.WriteString(`</div><div class="hr_tablehor"><div class="choix_jour ftab"><div class="frow fhead lesjours">`)
		for i, h := range []string{"Mer.</span> 25", "Jeu.</span> 26", "Ven.</span> 27"} {
			fmt.Fprintf(&b, `<div class="fcel jour%d"><a href="#jour%d" class="hr_jour"><span class="nom_jour">%s</a></div>`, i+1, i+1, h)
		}
		b.WriteString(`</div></div>`)
		for day := 1; day <= 3; day++ {
			fmt.Fprintf(&b, `<div class="tab_seances jour%d"><div class="ftab">`, day)
			for _, s := range f.sessions {
				if s.day != day {
					continue
				}
				fmt.Fprintf(&b, `<div class="frow"><div class="fcel celtags"><img src="https://static.cotecine.fr/img/tags/%s.png" class="tag"/></div><div class="fcel">`, s.tag)
				extra := ""
				if s.format != "" {
					extra = fmt.Sprintf(`<img src="%simage/tags/%s.png" class="tag"/>`, website, s.format)
				}
				if s.link {
					sec := instant(day, s.clock).Unix()
					if s.linkSec != 0 {
						sec = s.linkSec
					}
					fmt.Fprintf(&b, `<a href="%sreserver/F%s/D%d/VF/1156/" class="seance hr_seance sc_mor"><span class="hor">%s</span><span class="infos_seance">%s</span></a>`, website, f.id, sec, s.clock, extra)
				} else {
					fmt.Fprintf(&b, `<div class="seance hr_seance sc_mor past"><span class="hor">%s</span><span class="infos_seance">%s</span></div>`, s.clock, extra)
				}
				b.WriteString(`</div></div>`)
			}
			b.WriteString(`</div></div>`)
		}
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`</div></div></body></html>`)
	return b.String()
}

const (
	siteA = "https://www.cinema-a.fr/"
	siteB = "https://www.groupe.fr/cinema-b/"
)

func sources() []Source {
	return []Source{
		{TheaterID: "1824", Website: siteA, Address: "1 rue A", PostalCode: "77130", City: "Varennes-sur-Seine"},
		{TheaterID: "4464", Website: siteB, Address: "24 cours Napoléon", PostalCode: "20000", City: "Ajaccio"},
	}
}

func national(sessions ...fixtureSession) fixtureFilm {
	return fixtureFilm{id: "628177", title: "Moulin", duration: "1h47", poster: "https://fr.web.img5.acsta.net/c_180_240_50_50/img/0b/e9/a.jpg", sessions: sessions}
}

func fixtures() *fixtureGetter {
	return &fixtureGetter{calls: map[string]int{}, pages: map[string][]string{
		siteA: {page("1824", siteA,
			national(
				fixtureSession{day: 1, clock: "20h30", tag: "tag_vf_new", link: true, format: "tag_dolby_new"},
				fixtureSession{day: 2, clock: "0h15", tag: "tag_vost_new", link: true},
				fixtureSession{day: 1, clock: "10h00", tag: "tag_vf_new"},
			),
			fixtureFilm{id: "1974-une-partie-de-campagne-2002", title: "1974, une partie de campagne", sessions: []fixtureSession{
				{day: 3, clock: "18h00", tag: "tag_vfst_new", link: true, linkSec: 0},
				{day: 3, clock: "21h00", tag: "tag_unknown_new", link: true},
			}},
		)},
		siteB: {page("4464", siteB,
			national(fixtureSession{day: 2, clock: "14h00", tag: "tag_vf_new", link: true}),
		)},
	}}
}

func run(g *fixtureGetter, sources ...Source) (schedule.Dataset, SyncSummary, error) {
	return Sync(context.Background(), g, SyncOptions{From: "2026-11-25", Now: now, Sources: sources})
}

func TestSyncBuildsValidatedSnapshot(t *testing.T) {
	g := fixtures()
	data, summary, err := run(g, sources()...)
	if err != nil {
		t.Fatal(err)
	}
	if data.Provider != schedule.ProviderCotecine || len(data.Theaters) != 2 || data.Window.Through != "2026-11-27" {
		t.Fatalf("data=%+v", data)
	}
	if summary.Cinemas != 2 || summary.Movies != 2 || summary.Showtimes != 5 || summary.Skipped != 1 || summary.Requests != 2 || summary.MovieConflicts != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	theater := data.Theaters[0]
	if theater.ID != "cotecine-1824" || theater.Name != "Cinéma Test" || theater.Address != "1 rue A" || theater.PostalCode != "77130" || theater.City != "Varennes-sur-Seine" {
		t.Fatalf("theater=%+v", theater)
	}
	byID := map[string]schedule.ShowtimeRecord{}
	for _, s := range data.Showtimes {
		byID[s.ProviderShowingID] = s
	}
	evening := byID[schedule.CotecineShowingID("1824", "628177", instant(1, "20h30"))]
	if evening.Language != schedule.LanguageVF || evening.Format != schedule.FormatDolby || evening.ServiceDate != "2026-11-25" || !evening.EndTime.Equal(evening.StartTime.Add(107*time.Minute)) ||
		evening.BookingURL != fmt.Sprintf("%sreserver/F628177/D%d/VF/1156/", siteA, instant(1, "20h30").Unix()) || evening.Movie.PosterURL != "https://fr.web.img5.acsta.net/img/0b/e9/a.jpg" {
		t.Fatalf("evening=%+v", evening)
	}
	overnight := byID[schedule.CotecineShowingID("1824", "628177", instant(2, "0h15"))]
	if overnight.ServiceDate != "2026-11-26" || overnight.StartTime.Day() != 27 || overnight.Language != schedule.LanguageVOSTFR || overnight.ProviderVersion != "VOSTF" {
		t.Fatalf("overnight=%+v", overnight)
	}
	past := byID[schedule.CotecineShowingID("1824", "628177", instant(1, "10h00"))]
	if past.BookingURL != "" || past.Format != schedule.Format2D {
		t.Fatalf("past=%+v", past)
	}
	repertory := byID[schedule.CotecineShowingID("1824", "1824-1974-une-partie-de-campagne-2002", instant(3, "18h00"))]
	if repertory.Movie.ProviderID != "1824-1974-une-partie-de-campagne-2002" || repertory.Language != schedule.LanguageVFSTF || repertory.EndTime != repertory.StartTime {
		t.Fatalf("repertory=%+v", repertory)
	}
	if other := byID[schedule.CotecineShowingID("4464", "628177", instant(2, "14h00"))]; other.TheaterID != "cotecine-4464" || other.Movie.Title != "Moulin" {
		t.Fatalf("second site=%+v", other)
	}
	if _, err := schedule.PreparePublication(data); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSkipsBookingRoutesContradictingTheClock(t *testing.T) {
	g := fixtures()
	g.pages[siteB] = []string{page("4464", siteB, national(fixtureSession{day: 2, clock: "14h00", tag: "tag_vf_new", link: true, linkSec: instant(2, "15h00").Unix()}))}
	data, summary, err := run(g, sources()...)
	if err != nil || summary.Skipped != 2 || len(data.Showtimes) != 4 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestSyncKeepsFirstSiteRecordForConflictingFilm(t *testing.T) {
	g := fixtures()
	film := national(fixtureSession{day: 2, clock: "14h00", tag: "tag_vf_new", link: true})
	film.title, film.duration = "Moulin (version longue)", "2h10"
	g.pages[siteB] = []string{page("4464", siteB, film)}
	data, summary, err := run(g, sources()...)
	if err != nil || summary.MovieConflicts != 1 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	for _, s := range data.Showtimes {
		if s.Movie.ProviderID == "628177" && (s.Movie.Title != "Moulin" || s.Movie.RuntimeMinutes != 107) {
			t.Fatalf("film not bound to the first record: %+v", s.Movie)
		}
	}
}

func TestSyncRefetchesEmptyProgrammeOnce(t *testing.T) {
	empty := page("4464", siteB)
	g := fixtures()
	g.pages[siteB] = []string{empty, g.pages[siteB][0]}
	if _, summary, err := run(g, sources()...); err != nil || summary.Requests != 3 || summary.Showtimes != 5 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	g = fixtures()
	g.pages[siteB] = []string{empty}
	data, summary, err := run(g, sources()...)
	if err != nil || summary.Requests != 3 || len(data.Theaters) != 2 || len(data.Theaters[1].AvailableDates) != 0 {
		t.Fatalf("confirmed empty summary=%+v err=%v", summary, err)
	}
}

func TestSyncFailsAtomically(t *testing.T) {
	g := fixtures()
	g.failOn = siteB
	if data, _, err := run(g, sources()...); err == nil || len(data.Showtimes) != 0 {
		t.Fatal("transport failure published a partial snapshot")
	}
	for name, body := range map[string]string{
		"foreign theater": page("9999", siteB, national()),
		"truncated":       strings.TrimSuffix(page("4464", siteB, national()), "</body></html>"),
		"foreign film":    strings.Replace(page("4464", siteB, national()), siteB+"film/", siteA+"film/", 1),
		"orphan clock":    strings.Replace(page("4464", siteB, national()), `<div class="cadre-in">`, `<span class="hor">20h00</span><div class="cadre-in">`, 1),
	} {
		g := fixtures()
		g.pages[siteB] = []string{body}
		if _, _, err := run(g, sources()...); !errors.Is(err, ErrShape) || !strings.Contains(err.Error(), "4464") {
			t.Fatalf("%s err=%v", name, err)
		}
	}
}

func TestSyncValidatesSources(t *testing.T) {
	g := fixtures()
	if _, _, err := run(g); !errors.Is(err, ErrNoSources) {
		t.Fatalf("no sources err=%v", err)
	}
	valid := sources()[0]
	for _, mutate := range []func(*Source){
		func(s *Source) { s.TheaterID = "0" },
		func(s *Source) { s.Website = "https://www.cinema-a.fr" },
		func(s *Source) { s.Website = "http://www.cinema-a.fr/" },
		func(s *Source) { s.Website = "https://www.cinema-a.fr/?x=1" },
		func(s *Source) { s.Address = " " },
		func(s *Source) { s.PostalCode = "7713" },
		func(s *Source) { s.City = "" },
	} {
		s := valid
		mutate(&s)
		if _, _, err := run(g, s); !errors.Is(err, ErrShape) {
			t.Fatalf("source=%+v err=%v", s, err)
		}
	}
	if _, _, err := run(g, valid, valid); !errors.Is(err, ErrShape) {
		t.Fatal("duplicate source accepted")
	}
	if g.RequestCount() != 0 {
		t.Fatal("invalid sources reached the network")
	}
}
