package megarama

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type fixtureGetter struct {
	config      string
	programs    map[string]string
	poster      string
	posterErr   error
	mu          sync.Mutex
	posterCalls int
}

func (g *fixtureGetter) Config(context.Context) ([]byte, error) { return []byte(g.config), nil }
func (g *fixtureGetter) Program(_ context.Context, id, website string) ([]byte, error) {
	if !schedule.ValidMegaramaBookingURL(website, id, "") {
		return nil, errShape
	}
	body, ok := g.programs[id]
	if !ok {
		return nil, errShape
	}
	return []byte(body), nil
}
func (g *fixtureGetter) Poster(context.Context, string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.posterCalls++
	return []byte(g.poster), g.posterErr
}
func (g *fixtureGetter) RequestCount() int { return 0 }
func syncFixture() *fixtureGetter {
	return &fixtureGetter{config: configFixture, programs: map[string]string{"EMS0565": programFixture}}
}
func runFixture(t *testing.T, g *fixtureGetter) (schedule.Dataset, SyncSummary, error) {
	t.Helper()
	return Sync(t.Context(), g, SyncOptions{From: "2026-09-12", Now: time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)})
}

func TestSyncCompleteDeterministicDataset(t *testing.T) {
	g := syncFixture()
	d, s, err := runFixture(t, g)
	if err != nil || s.Cinemas != 1 || s.Showtimes != 1 {
		t.Fatalf("synthetic sync: %v", err)
	}
	r := d.Showtimes[0]
	if r.Language != schedule.LanguageVFSTF || r.Format != schedule.Format4DX || r.Room != "Salle 1" || r.FirstPartDurationMinutes != 10 || r.EndTime.Sub(r.StartTime) != 110*time.Minute || r.BookingURL != "https://bordeaux.megarama.fr/" {
		t.Fatal("source mapping")
	}
	if err := schedule.ValidateDataset(d, true); err != nil {
		t.Fatal(err)
	}
	second, _, err := runFixture(t, g)
	if err != nil || second.Showtimes[0].ID != r.ID {
		t.Fatal("unstable repeat")
	}
}

func addSecondCinema(g *fixtureGetter, empty bool) {
	second := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(configFixture, "EMS0565", "EMS1315"), "bordeaux.megarama.fr", "boulogne.megarama.fr"), `"123"`, `"456"`)
	start := strings.Index(second, `[{`) + 1
	end := strings.LastIndex(second, `]`)
	g.config = strings.Replace(g.config, `]}};`, `,`+second[start:end]+`]}};`, 1)
	body := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(programFixture, "0565", "1315"), `"123"`, `"456"`), `"duration":"100"`, `"duration":null`)
	if empty {
		body = `{"jsonrpc":"2.0","id":1,"result":{"schedule":{"id":"456","events":[]}}}`
	}
	g.programs["EMS1315"] = body
}

func TestSyncGlobalMergeAndEmptyCinema(t *testing.T) {
	for _, empty := range []bool{false, true} {
		g := syncFixture()
		addSecondCinema(g, empty)
		d, s, err := runFixture(t, g)
		if err != nil || len(d.Theaters) != 2 || s.Movies != 1 {
			t.Fatalf("merge/empty: %v", err)
		}
		if empty && len(d.Theaters[1].AvailableDates) != 0 {
			t.Fatal("empty cinema dates")
		}
		for _, r := range d.Showtimes {
			if r.Movie.RuntimeMinutes != 100 {
				t.Fatal("absent runtime not merged")
			}
		}
	}
	g := syncFixture()
	addSecondCinema(g, false)
	g.programs["EMS1315"] = strings.Replace(g.programs["EMS1315"], `"duration":null`, `"duration":101`, 1)
	if _, _, err := runFixture(t, g); !errors.Is(err, schedule.ErrDatasetValidation) {
		t.Fatal("conflicting runtime accepted")
	}
}

func TestSyncLocalIdentityAndUnknownEnd(t *testing.T) {
	g := syncFixture()
	g.programs["EMS0565"] = strings.ReplaceAll(strings.Replace(programFixture, `"duration":"100"`, `"duration":null`, 1), "ABCDE", "emsx0565HC12")
	d, _, err := runFixture(t, g)
	if err != nil {
		t.Fatal(err)
	}
	r := d.Showtimes[0]
	if r.Movie.ProviderID != "EMS0565-emsx0565HC12" || !r.EndTime.Equal(r.StartTime) || r.FirstPartDurationMinutes != 10 || g.posterCalls != 0 {
		t.Fatal("local unknown runtime mapping")
	}
}

func TestSyncPosterFallbackOnceAndFailures(t *testing.T) {
	for _, test := range []struct {
		err  error
		fail bool
	}{
		{nil, false}, {&RequestError{Kind: syncproxy.FailureStatus, StatusCode: 404}, false}, {&RequestError{Kind: syncproxy.FailureTransport}, false}, {&RequestError{Kind: syncproxy.FailureChallenge, StatusCode: 403}, true}, {&RequestError{Kind: syncproxy.FailureCanceled, cause: context.Canceled}, true},
	} {
		g := syncFixture()
		addSecondCinema(g, false)
		for id, body := range g.programs {
			g.programs[id] = strings.Replace(body, `"bill_url":"https://images.monnaie-services.com/movie_poster/120/FRABCDE/ABCDEFG1.webp"`, `"bill_url":""`, 1)
		}
		g.posterErr = test.err
		g.poster = `<meta property="og:image" content="https://images.monnaie-services.com/movie_poster/600/FRABCDE/ABCDEFG1.webp">`
		d, _, err := runFixture(t, g)
		if (err != nil) != test.fail || g.posterCalls != 1 {
			t.Fatalf("poster fallback: %v calls=%d", err, g.posterCalls)
		}
		if err == nil && test.err == nil && d.Showtimes[0].Movie.PosterURL == "" {
			t.Fatal("fallback absent")
		}
	}
}

func TestSyncIncompleteAndInvalidPublication(t *testing.T) {
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":1,"result":{"schedule":{"id":"123","events":[]}}}`,
		strings.Replace(programFixture, `"date":"202609121800"`, `"date":"202610250230"`, 1),
		strings.Replace(programFixture, `"date":"202609121800"`, `"date":"202609111800"`, 1),
		strings.Replace(programFixture, `"first_part_duration":10`, `"booking_url":"https://evil.test/","first_part_duration":10`, 1),
	} {
		g := syncFixture()
		g.programs["EMS0565"] = body
		if _, _, err := runFixture(t, g); err == nil {
			t.Error("invalid or empty batch accepted")
		}
	}
	g := syncFixture()
	addSecondCinema(g, false)
	g.programs["EMS1315"] = strings.Replace(g.programs["EMS1315"], "emsx131500000001", "emsx056500000001", 1)
	if _, _, err := runFixture(t, g); err == nil {
		t.Fatal("cross-cinema showing accepted")
	}
}

func TestSyncForwardHorizonAndCinemaDay(t *testing.T) {
	for _, test := range []struct{ wall, date, through string }{{"202609130030", "2026-09-12", "2026-09-13"}, {"202609120330", "2026-09-12", "2026-09-12"}, {"202707011800", "2027-07-01", "2027-07-01"}} {
		g := syncFixture()
		g.programs["EMS0565"] = strings.Replace(programFixture, "202609121800", test.wall, 1)
		d, _, err := runFixture(t, g)
		if err != nil || d.Window.Through != test.through || d.Showtimes[0].ServiceDate != test.date {
			t.Fatalf("cinema day %s: %v", test.wall, err)
		}
	}
}
