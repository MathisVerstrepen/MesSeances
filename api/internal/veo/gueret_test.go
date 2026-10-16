package veo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// Synthetic opaque values only, never captured source tokens or HTML.
func gueretTestSource(start time.Time, tags []string) string {
	q := url.Values{"code_theater": {"P1394"}, "gid_entity": {"fixture_123.opaque"}, "tags": {strings.Join(tags, ",")}, "perfd": {start.Format("20060102")}, "perft": {start.Format("15:04")}}
	return "https://relay.mvtx.us/ticketing/dbz?" + q.Encode()
}

func gueretTestStart() time.Time {
	loc, _ := time.LoadLocation(schedule.Timezone)
	return time.Date(2026, 10, 16, 15, 0, 0, 0, loc)
}

func gueretTestChain(start time.Time) []string {
	return []string{gueretTestSource(start, []string{"Localization.Language.French", "Format.Projection.Digital"}),
		"https://relay.mvtx.us/?house_id=12&movie_id=34&perfd=" + start.Format("20060102") + "&perft=" + start.Format("15:04"),
		"https://achat.cinema-senechal.com/r/56",
		fmt.Sprintf("https://achat.cinema-senechal.com/reserver/F34/D%d/VF/78/", start.Unix())}
}

func gueretTestPage(canonical string) string {
	return `<form method="post" action="` + canonical + `"><input name="tickets"></form><div id="s_fin">fin prévue à 16h30</div><script>{"auditorium_showtime":"salle-5"}</script>`
}

func TestGueretCheckoutExactChainAndStrictForm(t *testing.T) {
	for _, version := range []string{"VF", "VO"} {
		t.Run(version, func(t *testing.T) {
			chain := gueretTestChain(gueretTestStart())
			chain[3] = strings.ReplaceAll(chain[3], "/VF/", "/"+version+"/")
			calls := 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				if calls >= 4 || req.URL.String() != chain[calls] || req.Method != http.MethodGet || req.Header.Get("Cookie") != "" {
					t.Fatal("wrong chain request")
				}
				hop := calls
				calls++
				if hop == 3 {
					return fixtureResponse(200, gueretTestPage(chain[3]), "text/html"), nil
				}
				r := fixtureResponse([]int{302, 302, 307}[hop], "", "text/html")
				target := chain[hop+1]
				if hop == 0 {
					target = strings.TrimPrefix(target, "https://relay.mvtx.us")
				}
				r.Header.Set("Location", target)
				r.Header.Add("Set-Cookie", "ignored=synthetic; Path=/")
				return r, nil
			})
			result, err := c.Checkout(t.Context(), chain[0], gueretTestStart())
			if err != nil || calls != 4 || result.canonicalURL != chain[3] || result.unavailableEnrichment {
				t.Fatal("closed chain failed", calls, err)
			}
			facts, err := parseCheckoutForTheater(result.Body, result.canonicalURL, "veo-G06DB")
			if err != nil || !facts.hasEnd || facts.hour != 16 || facts.minute != 30 || facts.room != "Salle 5" {
				t.Fatal("terminal facts", facts, err)
			}
			for _, body := range []string{
				`{"auditorium_showtime":"salle-5"}`,
				strings.Replace(gueretTestPage(chain[3]), `method="post"`, `method="get"`, 1),
				strings.Replace(gueretTestPage(chain[3]), `/78/`, `/79/`, 1),
				strings.Replace(gueretTestPage(chain[3]), `name="tickets"`, `type="password"`, 1),
				gueretTestPage(chain[3]) + `<form method="post" action="synthetic"></form>`,
				gueretTestPage(chain[3]) + `{"auditorium_showtime":null}`,
			} {
				if _, err := parseCheckoutForTheater([]byte(body), chain[3], "veo-G06DB"); err == nil {
					t.Fatal("unsafe terminal accepted")
				}
			}
			if _, err := parseCheckoutForTheater(result.Body, chain[3], "veo-P1028"); err == nil {
				t.Fatal("dedicated terminal escaped theater")
			}
			badAction := strings.Replace(gueretTestPage(chain[3]), "/78/", "/79/", 1)
			_, err = parseCheckoutForTheater([]byte(badAction), chain[3], "veo-G06DB")
			var rejection *CheckoutParseError
			if !errors.As(err, &rejection) || !rejection.actionOnly || rejection.actionDiff != actionTail || rejection.path == nil || !rejection.path.CanonicalValid {
				t.Fatal("strict action diagnostic/attestation changed", err)
			}
			factsPath := classifyDiagnosticActionPath(strings.TrimSuffix(chain[3], "/")+"/extra/", chain[3])
			if factsPath.Shape != "canonical_prefix_extra" || factsPath.SegmentCount != 6 || factsPath.ExtraCount != 1 {
				t.Fatal("dedicated path diagnostic lost")
			}
		})
	}
}

func TestGueretCheckoutRejectsWrongEdgesBeforeSend(t *testing.T) {
	chain := gueretTestChain(gueretTestStart())
	type mutation struct {
		name        string
		hop, status int
		location    string
		duplicate   bool
	}
	cases := []mutation{
		{"early-source-200", 0, 200, "", false}, {"early-root-200", 1, 200, "", false}, {"early-alias-200", 2, 200, "", false},
		{"wrong-first-status", 0, 307, chain[1], false}, {"missing", 0, 302, "", false}, {"duplicate", 0, 302, chain[1], true},
		{"skip-root", 0, 302, chain[2], false}, {"loop-source", 0, 302, chain[0], false},
		{"bad-day", 0, 302, strings.Replace(chain[1], "20261016", "20261017", 1), false},
		{"bad-clock", 0, 302, strings.Replace(chain[1], "15:00", "15:01", 1), false},
		{"duplicate-key", 0, 302, chain[1] + "&house_id=1", false},
		{"encoded-key", 0, 302, strings.Replace(chain[1], "house_id", "house%5fid", 1), false},
		{"injection", 0, 302, strings.Replace(chain[1], "movie_id=34", "movie_id=34%26x%3D1", 1), false},
		{"wrong-root-status", 1, 307, chain[2], false}, {"skip-alias", 1, 302, chain[3], false},
		{"foreign-host", 1, 302, strings.Replace(chain[2], "achat.", "evil.achat.", 1), false},
		{"loop-root", 1, 302, chain[1], false}, {"alias-query", 1, 302, chain[2] + "?x=1", false},
		{"wrong-alias-status", 2, 302, chain[3], false},
		{"misbound-epoch", 2, 307, strings.Replace(chain[3], fmt.Sprint(gueretTestStart().Unix()), fmt.Sprint(gueretTestStart().Unix()+1), 1), false},
		{"main-host", 2, 307, strings.Replace(chain[3], "achat.cinema-senechal.com", "achat.veocinemas.fr", 1), false},
		{"fourth-hop", 3, 302, chain[3], false},
	}
	for _, suffix := range []string{"#", "?", "?x=1", "/extra", "\n", "\r", "\t"} {
		cases = append(cases, mutation{"canonical-suffix", 2, 307, chain[3] + suffix, false})
	}
	for _, replacement := range []struct{ old, new string }{{"https:", "http:"}, {"https://", "https://user@"}, {".com/", ".com:443/"}, {"/reserver/", "/%72eserver/"}, {"/reserver/", "/x/../reserver/"}, {"/VF/", "/VOSTF/"}, {"/F34/", "/F0/"}} {
		cases = append(cases, mutation{"canonical-shape", 2, 307, strings.Replace(chain[3], replacement.old, replacement.new, 1), false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(req *http.Request) (*http.Response, error) {
				hop := calls
				calls++
				if hop > tc.hop {
					t.Fatal("unapproved target sent")
				}
				status, target := 200, ""
				if hop < 3 {
					status, target = []int{302, 302, 307}[hop], chain[hop+1]
				}
				if hop == tc.hop {
					status, target = tc.status, tc.location
				}
				r := fixtureResponse(status, "synthetic", "text/html")
				if target != "" {
					r.Header.Set("Location", target)
				}
				if hop == tc.hop && tc.duplicate {
					r.Header.Add("Location", target)
				}
				return r, nil
			})
			_, err := c.Checkout(t.Context(), chain[0], gueretTestStart())
			var failure *RequestError
			if !errors.As(err, &failure) || failure.Kind != syncproxy.FailureRedirect || calls != tc.hop+1 {
				t.Fatal("wrong rejection", calls, err)
			}
		})
	}
	for _, start := range []time.Time{gueretTestStart().Add(time.Minute), gueretTestStart().Add(time.Second)} {
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			t.Fatal("misbound initial source sent")
			return nil, errShape
		})
		if _, err := c.Checkout(t.Context(), chain[0], start); err == nil {
			t.Fatal("misbound source accepted")
		}
	}
}

func TestGueretCheckoutRetryConcurrencyAndCancellation(t *testing.T) {
	start := gueretTestStart()
	var first, second atomic.Int32
	transport := func(pool int) roundTripFunc {
		return func(req *http.Request) (*http.Response, error) {
			b := req.Context().Value(checkoutBindingKey{}).(checkoutBinding)
			hop := gueretHop(req.URL, b)
			chain := gueretTestChain(b.start)
			if hop == 3 {
				if pool == 1 {
					first.Add(1)
					return fixtureResponse(503, "synthetic", "text/html"), nil
				}
				second.Add(1)
				return fixtureResponse(200, gueretTestPage(chain[3]), "text/html"), nil
			}
			r := fixtureResponse([]int{302, 302, 307}[hop], "", "text/html")
			r.Header.Set("Location", chain[hop+1])
			return r, nil
		}
	}
	c := testClient(t, transport(1), transport(2))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			s := start.Add(time.Duration(i) * time.Minute)
			chain := gueretTestChain(s)
			r, err := c.Checkout(t.Context(), chain[0], s)
			if err != nil || r.canonicalURL != chain[3] || r.pool.index != 2 || r.pool.count != 2 || r.unavailableEnrichment {
				t.Error("metadata crossed request/retry", err)
			}
		})
	}
	wg.Wait()
	if first.Load() == 0 || second.Load() != 8 {
		t.Fatal("retry path not exercised")
	}
	for _, status := range []int{403, 429} {
		calls := 0
		c := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return fixtureResponse(status, "synthetic", "text/html"), nil
		})
		_, err := c.Checkout(t.Context(), gueretTestChain(start)[0], start)
		var failure *RequestError
		if !errors.As(err, &failure) || failure.Kind != syncproxy.FailureChallenge || calls != 1 {
			t.Fatal("challenge retry softened", err, calls)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	c = testClient(t, func(req *http.Request) (*http.Response, error) { cancel(); return nil, req.Context().Err() })
	if _, err := c.Checkout(ctx, gueretTestChain(start)[0], start); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

type gueretFixtureGetter struct {
	*fixtureGetter
	checkout func(context.Context, string, time.Time) (CheckoutResult, error)
}

func (g gueretFixtureGetter) Checkout(ctx context.Context, raw string, start time.Time) (CheckoutResult, error) {
	if g.checkout != nil {
		return g.checkout(ctx, raw, start)
	}
	if !schedule.ValidVeoGueretRelayURL(raw) {
		return g.fixtureGetter.Checkout(ctx, raw, start)
	}
	canonical := gueretTestChain(start)[3]
	return CheckoutResult{Body: []byte(gueretTestPage(canonical)), canonicalURL: canonical}, nil
}

func newGueretFixture(t *testing.T, mutation func(Operation, string, []byte) []byte) gueretFixtureGetter {
	t.Helper()
	f := &fixtureGetter{}
	f.modify = func(op Operation, raw string, b []byte) ([]byte, error) {
		u, _ := url.Parse(raw)
		dedicated := strings.HasPrefix(raw, GueretOrigin)
		if op == OperationCinemas && strings.HasPrefix(raw, MainOrigin) {
			nodes := []string{`{"id":"G06DB","name":"Le Sénéchal","timeZone":"Europe/Paris","practicalInfo":{"location":{"address":"Canonical address","city":"Guéret","zip":"23000"}}}`}
			for i := 1; i <= 17; i++ {
				nodes = append(nodes, fmt.Sprintf(`{"id":"P%04d","name":"Fixture","timeZone":"Europe/Paris","practicalInfo":{"location":{"address":"1 rue","city":"Fixture","zip":"75001"}}}`, i))
			}
			b = []byte(`{"data":{"allTheater":{"nodes":[` + strings.Join(nodes, ",") + `]}}}`)
		}
		if op == OperationProgram {
			if dedicated && u.Query().Get("theaterId") != "P1394" || !dedicated && u.Query().Get("theaterId") == "G06DB" {
				t.Error("wrong program acquisition identity")
			}
		}
		if op == OperationSchedule {
			var target struct{ ID string }
			if err := decodeJSON([]byte(u.Query().Get("theaters")), &target); err != nil {
				return nil, err
			}
			if dedicated {
				if target.ID != "P1394" || mutation == nil && !strings.Contains(raw, "2026-10-29") {
					t.Error("wrong upstream/horizon")
				}
				rows := []string{}
				loc, _ := time.LoadLocation(schedule.Timezone)
				for _, date := range []string{"2026-10-15", "2026-10-28"} {
					start, _ := time.ParseInLocation("2006-01-02T15:04:05", date+"T23:30:00", loc)
					tags := []string{"Localization.Version.Original", "Format.Projection.3D"}
					row := fixtureSession("gueret-"+date, start.Format("2006-01-02T15:04:05"), "SENECHAL5")
					row = strings.ReplaceAll(strings.ReplaceAll(row, `"provider":"default"`, `"provider":"relay"`), "https://achat.veocinemas.fr/fixture/r/123", gueretTestSource(start, tags))
					rows = append(rows, fmt.Sprintf(`%q:[%s]`, date, row))
				}
				b = []byte(`{"P1394":{"schedule":{"123":{` + strings.Join(rows, ",") + `}}}}`)
			} else {
				b = []byte(strings.ReplaceAll(string(b), `"P1028"`, fmt.Sprintf("%q", target.ID)))
			}
		}
		if op == OperationCastelia {
			b = []byte(strings.ReplaceAll(string(b), "https://images.ticketingcine.fr/assets/global/600/FR/fixture.jpg", ""))
		}
		if mutation != nil {
			b = mutation(op, raw, b)
		}
		return b, nil
	}
	return gueretFixtureGetter{fixtureGetter: f}
}

func TestGueretUnknownEndsStartedCutoffAndRelayIsolation(t *testing.T) {
	g := newGueretFixture(t, nil)
	o := fixtureOptions()
	d, err := acquire(t.Context(), g, o, &SyncSummary{})
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation(schedule.Timezone)
	cutoff := time.Date(2026, 10, 15, 23, 30, 0, 0, loc)
	var calls atomic.Int32
	g.checkout = func(ctx context.Context, raw string, start time.Time) (CheckoutResult, error) {
		if !schedule.ValidVeoGueretRelayURL(raw) {
			return g.fixtureGetter.Checkout(ctx, raw, start)
		}
		if !start.After(cutoff) {
			t.Error("started relay was fetched")
		}
		calls.Add(1)
		canonical := gueretTestChain(start)[3]
		return CheckoutResult{Body: []byte(`<form method="post" action="` + canonical + `"></form><script>{"auditorium_showtime":null}</script>`), canonicalURL: canonical}, nil
	}
	if err := enrichCheckouts(t.Context(), g, d.Showtimes, &SyncSummary{}, cutoff); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("started fixed cutoff changed", calls.Load())
	}
	for _, r := range d.Showtimes {
		if r.TheaterID == "veo-G06DB" && (!r.EndTime.Equal(r.StartTime) || r.Room != "SENECHAL5" || r.Movie.RuntimeMinutes == 0 || !schedule.ValidVeoBookingURL(r.BookingURL, "G06DB", r.ProviderShowingID)) {
			t.Fatal("unknown end or source room invented")
		}
	}
	start := gueretTestStart()
	var s showtimeResponse
	if err := decodeJSON([]byte(fixtureSession("synthetic", start.Format("2006-01-02T15:04:05"), "")), &s); err != nil {
		t.Fatal(err)
	}
	s.Data.Ticketing[0].Provider = "relay"
	s.Data.Ticketing[0].URLs = []string{gueretTestSource(start, s.Tags)}
	for _, c := range []cinema{{ID: "P1028", origin: MainOrigin}, {ID: "G06DB", origin: MainOrigin}, {ID: "P1394", origin: GueretOrigin}} {
		if _, err := parseShowtime(s, c, schedule.MovieRecord{}, "2026-10-16", loc); err == nil {
			t.Fatal("relay selection escaped dedicated canonical mapping")
		}
	}
}

func TestGueretDedicatedAcquisitionKeepsTwentyCanonicalCinemas(t *testing.T) {
	g := newGueretFixture(t, nil)
	d, s, err := Sync(t.Context(), g, fixtureOptions())
	if err != nil || len(d.Theaters) != 20 || s.Cinemas != 20 || d.Window.Through != "2026-10-28" {
		t.Fatal("inventory/horizon", s, err)
	}
	found := false
	for _, theater := range d.Theaters {
		if theater.ProviderID == "P1394" {
			t.Fatal("duplicate acquisition identity leaked")
		}
		if theater.ProviderID == "G06DB" {
			found = true
			if theater.Name != "Le Sénéchal" || theater.Address != "Canonical address" || len(theater.AvailableDates) != 2 {
				t.Fatal("canonical metadata lost")
			}
		}
	}
	if !found {
		t.Fatal("G06DB missing")
	}
	count := 0
	for _, r := range d.Showtimes {
		if r.TheaterID == "veo-G06DB" {
			count++
			if r.ProviderShowingID != schedule.VeoShowingID("G06DB", "gueret-"+r.ServiceDate) || r.Room != "SENECHAL5" || !schedule.ValidVeoGueretRelayURL(r.BookingURL) || r.EndTime.Format("15:04") != "16:30" || r.Language != schedule.LanguageVOSTFR {
				t.Fatal("canonical row/source facts lost")
			}
		}
	}
	if count != 2 {
		t.Fatal("dedicated pairs missing", count)
	}
	for _, c := range d.Coverage {
		if c.TheaterID == "veo-G06DB" && c.Status != schedule.CoverageComplete {
			t.Fatal("source coverage lost")
		}
	}
}

func TestGueretDedicatedDisagreementIsAtomic(t *testing.T) {
	for _, kind := range []string{"metadata-collision", "schedule-key", "missing-pair", "extra-pair", "wrong-tags", "wrong-time", "wrong-provider", "cross-cinema"} {
		t.Run(kind, func(t *testing.T) {
			g := newGueretFixture(t, func(op Operation, raw string, b []byte) []byte {
				if !strings.HasPrefix(raw, GueretOrigin) {
					return b
				}
				text := string(b)
				switch kind {
				case "metadata-collision":
					if op == OperationMovies {
						text = strings.ReplaceAll(text, "Fixture movie", "Conflicting movie")
					}
				case "schedule-key":
					if op == OperationSchedule {
						text = strings.ReplaceAll(text, "P1394", "G06DB")
					}
				case "missing-pair":
					if op == OperationProgram {
						text = strings.ReplaceAll(text, "2026-10-28", "2026-10-29")
					}
				case "extra-pair":
					if op == OperationSchedule {
						text = strings.ReplaceAll(text, `"2026-10-15":[`, `"2026-10-20":[`)
					}
				case "wrong-tags":
					if op == OperationSchedule {
						text = strings.ReplaceAll(text, "tags=Localization.Version.Original", "tags=Localization.Language.French")
					}
				case "wrong-time":
					if op == OperationSchedule {
						text = strings.ReplaceAll(text, "perft=23%3A30", "perft=23%3A31")
					}
				case "wrong-provider":
					if op == OperationSchedule {
						text = strings.ReplaceAll(text, `"provider":"relay"`, `"provider":"default"`)
					}
				case "cross-cinema":
					if op == OperationSchedule {
						text = strings.ReplaceAll(text, "code_theater=P1394", "code_theater=G06DB")
					}
				}
				return []byte(text)
			})
			d, _, err := Sync(t.Context(), g, fixtureOptions())
			if err == nil || len(d.Theaters) != 0 || len(d.Showtimes) != 0 {
				t.Fatal("source disagreement published", err)
			}
		})
	}
}
