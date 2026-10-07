package cinewest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func fixtureTicketProgram(t *testing.T, f *fixtureFetcher, site string) ticketProgram {
	t.Helper()
	var envelope struct {
		Result struct {
			Schedule ticketProgram `json:"schedule"`
		} `json:"result"`
	}
	if err := json.Unmarshal(f.programs[site], &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Result.Schedule
}

func TestCapitoleTicketLanguageTimeAndURLBounds(t *testing.T) {
	for _, tc := range []struct {
		name, version, booking, poster string
		runtime, first                 minutes
		language                       schedule.Language
	}{
		{"VF", "VF", "https://www.capitolestudios.com/#showsession?id=emsx137800000001", "", 90, 15, schedule.LanguageVF},
		{"VO", "VO", "https://www.capitolestudios.com/#showsession?id=emsx118500000001", "https://images.monnaie-services.com/movie_poster/600/FRCP137/12345678.webp?token=x", 90, 0, schedule.LanguageVO},
		{"legacy checkout not new", "VF", "https://www.capitolestudios-reserver.cotecine.fr/reserver/r/123", "", 0, 20, schedule.LanguageVF},
		{"local film", "VF", "https://ws.ticketingcine.com/site", "https://all.web.img.acsta.net/img/6f/af/6faf7d9aa879bd9374e773f31db44956.jpg", 120, 10, schedule.LanguageVF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			p := fixtureTicketProgram(t, f, "EMS1378")
			event := &p.Events[0]
			event.ID = "CP137"
			if tc.name == "local film" {
				event.ID = "emsx1378HC123"
			}
			event.Duration, event.Poster = tc.runtime, tc.poster
			s := &event.Sessions[0]
			s.Version, s.Booking, s.FirstPart, s.Date = tc.version, tc.booking, tc.first, "202609150100"
			setFixtureTicketProgram(f, "EMS1378", p)
			d, err := syncFixture(t, f)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range d.Showtimes {
				if r.TheaterID != "cinewest-ticketingcine-EMS1378" {
					continue
				}
				found = true
				wantBooking := tc.booking
				if tc.name != "VF" {
					wantBooking = "https://www.capitolestudios.com/"
				}
				duration := time.Duration(tc.runtime+tc.first) * time.Minute
				if tc.runtime == 0 {
					duration = 0
				}
				if r.Language != tc.language || r.ProviderVersion != tc.version || r.ServiceDate != "2026-09-14" || r.StartTime.Hour() != 1 || r.EndTime.Sub(r.StartTime) != duration || r.BookingURL != wantBooking || r.Movie.PosterURL != "" {
					t.Fatal("Capitole bounds/time", r)
				}
			}
			if !found {
				t.Fatal("Capitole missing")
			}
		})
	}
}

func setFixtureTicketProgram(f *fixtureFetcher, site string, p ticketProgram) {
	f.programs[site] = jsonFixture(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"schedule": p}})
}

func TestCapitoleTicketProgramContracts(t *testing.T) {
	f := newFixture()
	p := fixtureTicketProgram(t, f, "EMS1378")
	p.Name, p.City, p.Zip = "Capitole Studios", "Le Pontet", "84130"
	p.Events[0].ID = "CP137"
	p.Events[0].Poster = "https://images.monnaie-services.com/movie_poster/120/FRCP137/12345678.webp"
	p.Events[0].Sessions[0].Booking = "https://www.capitolestudios.com#showsession?id=emsx137800000001"
	setFixtureTicketProgram(f, "EMS1378", p)
	d, err := syncFixture(t, f)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("ticketingcine-EMS1378\x00emsx137800000001"))
	expected := "ticketingcine-" + hex.EncodeToString(hash[:])
	found := false
	for _, s := range d.Showtimes {
		if s.TheaterID != "cinewest-ticketingcine-EMS1378" {
			continue
		}
		found = true
		if s.ProviderShowingID != expected || s.BookingURL != p.Events[0].Sessions[0].Booking || s.Movie.PosterURL != strings.Replace(p.Events[0].Poster, "/120/", "/600/", 1) || s.EndTime.Sub(s.StartTime) != 105*time.Minute {
			t.Fatal(s)
		}
	}
	if !found {
		t.Fatal("Capitole missing")
	}
	p.Events = []ticketEvent{}
	setFixtureTicketProgram(f, "EMS1378", p)
	d, err = syncFixture(t, f)
	if err != nil || len(d.Theaters) != 13 || len(d.Showtimes) != 12 {
		t.Fatal("empty program", err)
	}
	for _, c := range d.Theaters {
		if c.ProviderID == "ticketingcine-EMS1378" && len(c.AvailableDates) != 0 {
			t.Fatal(c)
		}
	}
}

func TestCapitoleTicketRejectsInvalidIdentityAtomically(t *testing.T) {
	for name, mutate := range map[string]func(*ticketProgram){
		"program":         func(p *ticketProgram) { p.ID = "wrong" },
		"site":            func(p *ticketProgram) { p.EMS.ID = "1185" },
		"session":         func(p *ticketProgram) { p.Events[0].Sessions[0].ID = "emsx118500000001" },
		"local film site": func(p *ticketProgram) { p.Events[0].ID = "emsx1185HC12" },
		"null events":     func(p *ticketProgram) { p.Events = nil },
		"null sessions":   func(p *ticketProgram) { p.Events[0].Sessions = nil },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			p := fixtureTicketProgram(t, f, "EMS1378")
			mutate(&p)
			setFixtureTicketProgram(f, "EMS1378", p)
			d, err := syncFixture(t, f)
			if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatal("partial snapshot accepted")
			}
		})
	}
}
