package schedulepg

import (
	"context"
	"reflect"
	"testing"
	"time"

	"messeances/api/internal/cotecine"
	"messeances/api/internal/schedule"
)

type cotecineFixtureGetter []byte

func (g cotecineFixtureGetter) Programme(context.Context, string) ([]byte, error) { return g, nil }
func (cotecineFixtureGetter) RequestCount() int                                   { return 1 }

// cotecineProgramme is a Cinegroup programme for Friday 16 and Saturday 17
// October 2026: a national film with a bookable session and a repertory film
// listed by slug.
const cotecineProgramme = `<!DOCTYPE html><html><head><script>dataLayer.push({"theaterID":"1824","theaterName":"Cinéma Confluences Varennes"});</script></head><body><div class="cadre-in"><div><div class="seances_print"></div>` +
	`<div class="hr_film"><h2><a href="https://www.cinema-confluences.com/varennes/film/628177/" title="Voir la fiche du film Moulin">Moulin</a></h2><img src="https://fr.web.img5.acsta.net/c_180_240_50_50/img/0b/e9/a.jpg" class="hr_aff"/><p class="hr_p hr_dur">Durée : <strong>1h47</strong></p>` +
	`<div class="hr_tablehor"><a class="hr_jour"><span class="nom_jour">Ven.</span> 16</a><a class="hr_jour"><span class="nom_jour">Sam.</span> 17</a>` +
	`<div class="tab_seances jour1"><div class="frow"><div class="fcel celtags"><img src="https://static.cotecine.fr/img/tags/tag_vost_new.png"/></div><a href="https://www.cinema-confluences.com/varennes/reserver/F628177/D1792175400/VO/188300/" class="seance hr_seance"><span class="hor">20h30</span></a></div></div>` +
	`<div class="tab_seances jour2"></div></div></div>` +
	`<div class="hr_film"><h2><a href="https://www.cinema-confluences.com/varennes/film/lady-bird-0000/" title="Voir la fiche du film Lady Bird">Lady Bird</a></h2>` +
	`<div class="hr_tablehor"><a class="hr_jour"><span class="nom_jour">Ven.</span> 16</a><a class="hr_jour"><span class="nom_jour">Sam.</span> 17</a>` +
	`<div class="tab_seances jour1"></div><div class="tab_seances jour2"><div class="frow"><div class="fcel celtags"><img src="https://static.cotecine.fr/img/tags/tag_vf_new.png"/></div><div class="seance hr_seance"><span class="hor">14h00</span></div></div></div></div></div>` +
	`</div></div></body></html>`

func cotecineTestDataset(t *testing.T) Dataset {
	t.Helper()
	data, _, err := cotecine.Sync(t.Context(), cotecineFixtureGetter(cotecineProgramme), cotecine.SyncOptions{From: "2026-10-16", Now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), Sources: []cotecine.Source{{TheaterID: "1824", Website: "https://www.cinema-confluences.com/varennes/", Address: "Espace commercial du Bréau - RD606", PostalCode: "77130", City: "Varennes-sur-Seine"}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// The Cotecine provider, registered by migration 088, publishes, persists and
// reloads like the built-in providers.
func TestCotecineRoundTripIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	d := cotecineTestDataset(t)
	if len(d.Showtimes) != 2 {
		t.Fatalf("fixture showtimes=%d", len(d.Showtimes))
	}
	if _, err := store.Replace(t.Context(), []Dataset{d}); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Showtimes) != 2 || len(loaded.Theaters) != 1 || loaded.Theaters[0].ID != "cotecine-1824" || loaded.Theaters[0].PostalCode != "77130" {
		t.Fatalf("loaded=%+v", loaded)
	}
	for i, s := range loaded.Showtimes {
		want := d.Showtimes[i]
		if s.ID != want.ID || s.BookingURL != want.BookingURL || !s.StartTime.Equal(want.StartTime) || !s.EndTime.Equal(want.EndTime) || s.Language != want.Language || s.Movie.ProviderID != want.Movie.ProviderID || s.Movie.PublicMovieID <= 0 {
			t.Fatalf("showtime %d=%+v want=%+v", i, s, want)
		}
	}
	historyCount(t, pool, `SELECT count(*) FROM screening_history_showtimes WHERE provider='cotecine'`, 2)
	again, _, err := store.Load(t.Context())
	if err != nil || !reflect.DeepEqual(loaded, again) {
		t.Fatal("reload changed", err)
	}
	if schedule.ValidateDataset(d, true) != nil {
		t.Fatal("fixture invalid")
	}
}
