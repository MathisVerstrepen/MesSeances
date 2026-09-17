package cineville

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

// Synthetic fixtures describe only the public source schema, not captured documents.
func fixtureCinema() cinema {
	return cinema{ID: "639", Route: "katorzaquimper", Name: "Katorza", City: "Quimper", Postal: "29000"}
}
func fixtureFilm(id scalar) film {
	return film{Visa: id, Title: "Source title", Metadata: json.RawMessage(`[{"titre":"Metadata title","duree":"1h33","affichette":"safe-1.jpg","synopsis":"Summary","genreprincipal":"Drame","datedesortie":"2026-09-09"}]`), Dates: []programDate{{Date: "20260914", Showtimes: []session{{Cinema: "639", ID: "1", Bordereau: "9", Room: "4", Time: "00:15", Version: "VF", Subtitles: "0", Relief: "2D", Attributes: ""}}}}}
}
func fixturePage(c cinema) pageProps {
	f := fixtureFilm("167934")
	f.Dates[0].Showtimes[0].Cinema = c.ID
	return pageProps{Cinemas: []cinema{c}, CinemaID: c.ID, Program: []film{f}, Events: []film{}, Attributes: []json.RawMessage{}}
}
func jsonBytes(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func bootstrapBytes(t *testing.T, build string, catalog []cinema) []byte {
	t.Helper()
	b := jsonBytes(t, map[string]any{"buildId": build, "props": map[string]any{"pageProps": map[string]any{"cinemas": catalog}}})
	return []byte(`<html><script id="__NEXT_DATA__" type="application/json">` + string(b) + `</script></html>`)
}

type fixtureFetcher struct {
	t       *testing.T
	builds  []string
	catalog []cinema
	pages   map[string]pageProps
	calls   []string
	fail    func(build, route string) error
}

func (f *fixtureFetcher) Fetch(context.Context) ([]byte, error) {
	i := 0
	for _, call := range f.calls {
		if call == "bootstrap" {
			i++
		}
	}
	f.calls = append(f.calls, "bootstrap")
	if i >= len(f.builds) {
		f.t.Fatal("unbounded bootstrap refresh")
	}
	return bootstrapBytes(f.t, f.builds[i], f.catalog), nil
}
func (f *fixtureFetcher) FetchCinema(_ context.Context, build, route string) ([]byte, error) {
	f.calls = append(f.calls, build+":"+route)
	if f.fail != nil {
		if err := f.fail(build, route); err != nil {
			return nil, err
		}
	}
	p, ok := f.pages[route]
	if !ok {
		f.t.Fatal("unknown data route")
	}
	// Real Next.js pages serialize calendar dates as numbers, not strings.
	return numericFixtureDates.ReplaceAll(jsonBytes(f.t, map[string]any{"pageProps": p}), []byte(`"date":$1`)), nil
}

var numericFixtureDates = regexp.MustCompile(`"date":"([0-9]{8})"`)

func (f *fixtureFetcher) RequestCount() int { return len(f.calls) }
func fixtureOptions() SyncOptions {
	return SyncOptions{From: "2026-09-14", Now: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)}
}
func singleFetcher(t *testing.T, page pageProps) *fixtureFetcher {
	t.Helper()
	c := fixtureCinema()
	return &fixtureFetcher{t: t, builds: []string{"build-1"}, catalog: []cinema{c}, pages: map[string]pageProps{c.Route: page}}
}

func TestSyncNormalizesPostalCode(t *testing.T) {
	for _, test := range []struct {
		name, postal, want string
	}{
		{name: "normal", postal: "29600", want: "29600"},
		{name: "internal space", postal: "29 600", want: "29600"},
		{name: "ASCII whitespace", postal: " \t29\n600\r ", want: "29600"},
		{name: "nonbreaking space", postal: "29\u00a0600", want: "29600"},
		{name: "narrow nonbreaking space", postal: "29\u202f600", want: "29600"},
		{name: "leading zero", postal: "01 000", want: "01000"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fixtureCinema()
			c.Postal = test.postal
			fetcher := &fixtureFetcher{t: t, builds: []string{"build-1"}, catalog: []cinema{c}, pages: map[string]pageProps{c.Route: fixturePage(c)}}
			data, summary, err := Sync(t.Context(), fetcher, fixtureOptions())
			if err != nil {
				t.Fatal(err)
			}
			if len(data.Theaters) != 1 || data.Theaters[0].PostalCode != test.want || summary.Cinemas != 1 || summary.Requests != 2 {
				t.Fatalf("theaters=%+v summary=%+v", data.Theaters, summary)
			}
		})
	}
}

func TestSyncUnionMetadataAndUnknownEnds(t *testing.T) {
	c := fixtureCinema()
	p := fixturePage(c)
	duplicate := fixtureFilm("167934")
	duplicate.Metadata = json.RawMessage(`{"result":false,"message":"synthetic private sentinel"}`)
	p.Program[0].Metadata = json.RawMessage(`[]`)
	known := fixtureFilm("167934")
	known.Dates[0].Date = "20261201"
	known.Dates[0].Showtimes[0].ID = "2"
	event := fixtureFilm("-693091020261")
	event.Metadata = duplicate.Metadata
	event.Title = "Future event"
	event.Dates[0].Date = "20270701"
	event.Dates[0].Showtimes[0].ID = "3"
	past := fixtureFilm("2714300920262")
	past.Dates[0].Date = "20260913"
	past.Dates[0].Showtimes[0].ID = "4"
	p.Events = []film{duplicate, known, event, past, {Dates: []programDate{}}}
	fetcher := singleFetcher(t, p)
	d, summary, err := Sync(t.Context(), fetcher, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Cinemas != 1 || summary.Movies != 2 || summary.Showtimes != 3 || summary.Requests != 2 || d.Window.Through != "2027-07-01" {
		t.Fatalf("summary=%+v window=%+v", summary, d.Window)
	}
	if strings.Join(d.Theaters[0].AvailableDates, ",") != "2026-09-14,2026-12-01,2027-07-01" || d.Theaters[0].Address != "" {
		t.Fatal("dates/address")
	}
	for _, s := range d.Showtimes {
		if !s.EndTime.Equal(s.StartTime) || s.FirstPartDurationMinutes != 0 || s.Room != "4" || s.Movie.Title == "Metadata title" {
			t.Fatal("source contract")
		}
		if s.Movie.ProviderID == "167934" && (s.Movie.RuntimeMinutes != 93 || s.Movie.PosterURL != schedule.CinevillePosterPrefix+"safe-1.jpg" || s.Movie.Overview != "Summary" || s.Movie.ReleaseDate != "2026-09-09") {
			t.Fatal("metadata merge")
		}
	}
	first := d.Showtimes[0]
	if first.ServiceDate != "2026-09-14" || first.StartTime.Format(time.RFC3339) != "2026-09-14T00:15:00+02:00" || d.Showtimes[1].StartTime.Format(time.RFC3339) != "2026-12-01T00:15:00+01:00" || first.BookingURL != "https://www.cineville.fr/vad/639/1/9" {
		t.Fatal("calendar date/offset/booking")
	}
	if d.Showtimes[2].Movie.Slug != "cineville-film--693091020261" || d.Showtimes[2].Movie.RuntimeMinutes != 0 || d.Showtimes[2].Movie.PosterURL != "" {
		t.Fatal("event gaps")
	}
}

func TestSyncDynamicCatalogAliasesAndScopedIDs(t *testing.T) {
	routes := []string{"beaupreau", "bruz", "concarneau", "darcy", "dijon", "dorlisheim", "henin", "laroche", "laval", "lorient", "katorza", "morlaix", "pontlabbe", "lespontsdece", "katorzaquimper", "quimper", "club6", "stnazaire", "stsebastien", "savenay", "tregueux", "garenne", "parclann", "vernsurseiche"}
	ids := []scalar{"4670", "4037", "697", "2704", "2714", "465", "709", "685", "707", "693", "691", "4672", "4153", "1874", "639", "695", "4673", "689", "687", "4682", "4674", "681", "683", "4099"}
	f := &fixtureFetcher{t: t, builds: []string{"current_build"}, pages: map[string]pageProps{}}
	for i, route := range routes {
		c := fixtureCinema()
		c.ID, c.Route = ids[i], route
		f.catalog = append(f.catalog, c)
		f.pages[route] = fixturePage(c)
	}
	f.catalog = append(f.catalog, cinema{ID: "4676", Route: "siege"})
	d, summary, err := Sync(t.Context(), f, fixtureOptions())
	if err != nil || len(d.Theaters) != 24 || summary.Showtimes != 24 || summary.Movies != 1 || summary.Requests != 25 {
		t.Fatalf("summary=%+v error=%v", summary, err)
	}
	seen := map[string]bool{}
	for i, s := range d.Showtimes {
		if seen[s.ID] || s.ID != "cineville-showing-"+string(ids[i])+"-1" {
			t.Fatal("scoped identity")
		}
		seen[s.ID] = true
	}
}

func TestSyncRefreshRestartsEntireAcquisition(t *testing.T) {
	for _, test := range []struct {
		name       string
		failure    RequestError
		sameBuild  bool
		persistent bool
		wantError  bool
	}{
		{name: "404 new build", failure: RequestError{Kind: syncproxy.FailureStatus, StatusCode: 404}},
		{name: "404 same build", failure: RequestError{Kind: syncproxy.FailureStatus, StatusCode: 404}, sameBuild: true, wantError: true},
		{name: "second 404", failure: RequestError{Kind: syncproxy.FailureStatus, StatusCode: 404}, persistent: true, wantError: true},
		{name: "content type new build", failure: RequestError{Kind: syncproxy.FailureContentType}},
		{name: "content type same build", failure: RequestError{Kind: syncproxy.FailureContentType}, sameBuild: true},
		{name: "persistent content type new build", failure: RequestError{Kind: syncproxy.FailureContentType}, persistent: true, wantError: true},
		{name: "persistent content type same build", failure: RequestError{Kind: syncproxy.FailureContentType}, sameBuild: true, persistent: true, wantError: true},
		{name: "invalid JSON new build", failure: RequestError{Kind: syncproxy.FailureInvalidJSON}},
		{name: "invalid JSON same build", failure: RequestError{Kind: syncproxy.FailureInvalidJSON}, sameBuild: true},
		{name: "persistent invalid JSON new build", failure: RequestError{Kind: syncproxy.FailureInvalidJSON}, persistent: true, wantError: true},
		{name: "persistent invalid JSON same build", failure: RequestError{Kind: syncproxy.FailureInvalidJSON}, sameBuild: true, persistent: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := fixtureCinema()
			c2 := c
			c2.ID, c2.Route = "707", "laval"
			f := singleFetcher(t, fixturePage(c))
			f.catalog = append(f.catalog, c2)
			f.pages[c2.Route] = fixturePage(c2)
			f.builds = append(f.builds, "build-2")
			if test.sameBuild {
				f.builds[1] = "build-1"
			}
			failed := false
			f.fail = func(_, route string) error {
				if route == c2.Route && (!failed || test.persistent) {
					failed = true
					// The already acquired first page must not survive the restart.
					fresh := fixturePage(c)
					fresh.Program[0].Visa = "123"
					fresh.Program[0].Dates[0].Showtimes[0].ID = "2"
					f.pages[c.Route] = fresh
					return fmt.Errorf("synthetic-private-body: %w", &test.failure)
				}
				return nil
			}
			d, summary, err := Sync(t.Context(), f, fixtureOptions())
			wantCalls := []string{"bootstrap", "build-1:" + c.Route, "build-1:" + c2.Route, "bootstrap"}
			if !test.sameBuild || test.failure.Kind != syncproxy.FailureStatus {
				wantCalls = append(wantCalls, f.builds[1]+":"+c.Route, f.builds[1]+":"+c2.Route)
			}
			if !reflect.DeepEqual(f.calls, wantCalls) {
				t.Fatalf("calls=%v want=%v", f.calls, wantCalls)
			}
			if test.wantError {
				var re *RequestError
				if !errors.As(err, &re) || re.Kind != test.failure.Kind || re.StatusCode != test.failure.StatusCode || re.Operation != OperationCinema {
					t.Fatalf("err=%v", err)
				}
				if !reflect.DeepEqual(d, schedule.Dataset{}) || summary != (SyncSummary{}) || strings.Contains(err.Error(), "synthetic-private-body") || errors.Unwrap(err) != nil {
					t.Fatal("partial dataset or unredacted error on refresh failure")
				}
				return
			}
			if err != nil || len(d.Showtimes) != 2 || summary.Cinemas != 2 || summary.Movies != 2 || summary.Showtimes != 2 || summary.Requests != 6 {
				t.Fatalf("summary=%+v err=%v", summary, err)
			}
			if d.Showtimes[0].ProviderShowingID != string(c.ID)+"-2" || d.Showtimes[0].Movie.ProviderID != "123" {
				t.Fatal("stale page survived restart")
			}
		})
	}
}

func TestSyncPagePayloadRecovery(t *testing.T) {
	for _, body := range []string{`{"synthetic-private-body":`, `{"pageProps":{"cinemaId":"synthetic-private-body"}}`} {
		for _, sameBuild := range []bool{false, true} {
			for _, persistent := range []bool{false, true} {
				t.Run(fmt.Sprintf("validJSON=%t/sameBuild=%t/persistent=%t", json.Valid([]byte(body)), sameBuild, persistent), func(t *testing.T) {
					c := fixtureCinema()
					c2 := c
					c2.ID, c2.Route = "707", "laval"
					freshBuild := "build-2"
					if sameBuild {
						freshBuild = "build-1"
					}
					bootstraps := 0
					var calls []string
					client := testClient(t, func(r *http.Request) (*http.Response, error) {
						if r.URL.String() == BootstrapURL {
							bootstraps++
							calls = append(calls, "bootstrap")
							build := "build-1"
							if bootstraps > 1 {
								build = freshBuild
							}
							return response(http.StatusOK, "text/html", string(bootstrapBytes(t, build, []cinema{c, c2}))), nil
						}
						calls = append(calls, r.URL.Path)
						p := fixturePage(c)
						if strings.HasSuffix(r.URL.Path, "/"+c2.Route+".json") {
							if bootstraps == 1 || persistent {
								return response(http.StatusOK, "application/json", body), nil
							}
							p = fixturePage(c2)
						} else if bootstraps > 1 {
							p.Program[0].Visa = "123"
							p.Program[0].Dates[0].Showtimes[0].ID = "2"
						}
						return response(http.StatusOK, "application/json", string(jsonBytes(t, map[string]any{"pageProps": p}))), nil
					})
					data, summary, err := Sync(t.Context(), client, fixtureOptions())
					path := func(build, route string) string { return "/_next/data/" + build + "/programmes/" + route + ".json" }
					want := []string{"bootstrap", path("build-1", c.Route), path("build-1", c2.Route), "bootstrap", path(freshBuild, c.Route), path(freshBuild, c2.Route)}
					if !reflect.DeepEqual(calls, want) || client.RequestCount() != 6 {
						t.Fatalf("unexpected recovery requests: %v", calls)
					}
					if persistent {
						var re *RequestError
						if !errors.As(err, &re) || re.Operation != OperationCinema || re.Kind != syncproxy.FailureInvalidJSON || re.StatusCode != 0 {
							t.Fatalf("payload classification: %v", err)
						}
						if !reflect.DeepEqual(data, schedule.Dataset{}) || summary != (SyncSummary{}) || strings.Contains(err.Error(), "synthetic-private-body") || errors.Unwrap(err) != nil {
							t.Fatal("partial dataset or unredacted payload error")
						}
						return
					}
					if err != nil || summary.Requests != 6 || summary.Cinemas != 2 || summary.Movies != 2 || summary.Showtimes != 2 {
						t.Fatalf("summary=%+v err=%v", summary, err)
					}
					if data.Showtimes[0].ProviderShowingID != string(c.ID)+"-2" || data.Showtimes[0].Movie.ProviderID != "123" {
						t.Fatal("stale page survived restart")
					}
				})
			}
		}
	}
}

func TestSyncPagePayloadRefreshBootstrapFailsClosed(t *testing.T) {
	bootstraps := 0
	client := testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == BootstrapURL {
			bootstraps++
			if bootstraps == 1 {
				return response(http.StatusOK, "text/html", string(bootstrapBytes(t, "build-1", []cinema{fixtureCinema()}))), nil
			}
			return response(http.StatusOK, "text/html", "<html>synthetic-private-body</html>"), nil
		}
		return response(http.StatusOK, "application/json", `{"pageProps":null}`), nil
	})
	data, summary, err := Sync(t.Context(), client, fixtureOptions())
	var re *RequestError
	if !errors.As(err, &re) || re.Operation != OperationBootstrap || re.Kind != syncproxy.FailureInvalidJSON {
		t.Fatalf("refresh classification: %v", err)
	}
	if client.RequestCount() != 3 || !reflect.DeepEqual(data, schedule.Dataset{}) || summary != (SyncSummary{}) {
		t.Fatal("partial dataset or unexpected retry")
	}
}

func TestSyncContentTypeRecoveryFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		media  string
		body   string
		kind   syncproxy.FailureKind
	}{
		{name: "persistent content type", status: http.StatusOK, media: "text/html", body: "<html>synthetic-private-body</html>", kind: syncproxy.FailureContentType},
		{name: "challenge", status: http.StatusOK, media: "text/html", body: "<html>cf-chl challenge-platform</html>", kind: syncproxy.FailureChallenge},
		{name: "forbidden", status: http.StatusForbidden, media: "text/html", body: "synthetic-private-body", kind: syncproxy.FailureChallenge},
		{name: "rate limited", status: http.StatusTooManyRequests, media: "text/html", body: "synthetic-private-body", kind: syncproxy.FailureChallenge},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == BootstrapURL {
					return response(http.StatusOK, "text/html", string(bootstrapBytes(t, "build-1", []cinema{fixtureCinema()}))), nil
				}
				return response(test.status, test.media, test.body), nil
			})
			data, summary, err := Sync(t.Context(), client, fixtureOptions())
			var re *RequestError
			if !errors.As(err, &re) || re.Operation != OperationCinema || re.Kind != test.kind {
				t.Fatalf("err=%v", err)
			}
			wantRequests := 2
			if test.kind == syncproxy.FailureContentType {
				wantRequests = 4
			}
			if client.RequestCount() != wantRequests || !reflect.DeepEqual(data, schedule.Dataset{}) || summary != (SyncSummary{}) {
				t.Fatal("partial dataset or unexpected retry")
			}
		})
	}
}

func TestSyncRejectsMalformedAndConflictingSessions(t *testing.T) {
	tests := map[string]func(*pageProps){
		"wrong page cinema":      func(p *pageProps) { p.CinemaID = "707" },
		"missing catalog":        func(p *pageProps) { p.Cinemas = nil },
		"wrong catalog identity": func(p *pageProps) { p.Cinemas[0].Route = "other" },
		"missing program":        func(p *pageProps) { p.Program = nil },
		"missing events":         func(p *pageProps) { p.Events = nil },
		"missing attributes":     func(p *pageProps) { p.Attributes = nil },
		"missing dates":          func(p *pageProps) { p.Program[0].Dates = nil },
		"missing sessions":       func(p *pageProps) { p.Program[0].Dates[0].Showtimes = nil },
		"empty dataset":          func(p *pageProps) { p.Program = []film{} },
		"invalid visa":           func(p *pageProps) { p.Program[0].Visa = "9223372036854775808" },
		"missing title":          func(p *pageProps) { p.Program[0].Title = ""; p.Program[0].Metadata = json.RawMessage(`[]`) },
		"metadata malformed":     func(p *pageProps) { p.Program[0].Metadata = json.RawMessage(`{"result":true}`) },
		"wrong session cinema":   func(p *pageProps) { p.Program[0].Dates[0].Showtimes[0].Cinema = "707" },
		"zero session":           func(p *pageProps) { p.Program[0].Dates[0].Showtimes[0].ID = "0" },
		"invalid bordereau":      func(p *pageProps) { p.Program[0].Dates[0].Showtimes[0].Bordereau = "01" },
		"invalid room":           func(p *pageProps) { p.Program[0].Dates[0].Showtimes[0].Room = "Main" },
		"invalid date":           func(p *pageProps) { p.Program[0].Dates[0].Date = "20260230" },
		"invalid time":           func(p *pageProps) { p.Program[0].Dates[0].Showtimes[0].Time = "24:15" },
		"unsupported version":    func(p *pageProps) { p.Program[0].Dates[0].Showtimes[0].Version = "EN" },
		"unsupported relief":     func(p *pageProps) { p.Program[0].Dates[0].Showtimes[0].Relief = "" },
		"conflicting time": func(p *pageProps) {
			f := fixtureFilm("167934")
			f.Dates[0].Showtimes[0].Time = "01:15"
			p.Events = []film{f}
		},
		"conflicting film": func(p *pageProps) { p.Events = []film{fixtureFilm("123")} },
		"conflicting room": func(p *pageProps) {
			f := fixtureFilm("167934")
			f.Dates[0].Showtimes[0].Room = "5"
			p.Events = []film{f}
		},
		"conflicting booking": func(p *pageProps) {
			f := fixtureFilm("167934")
			f.Dates[0].Showtimes[0].Bordereau = "10"
			p.Events = []film{f}
		},
		"conflicting language": func(p *pageProps) {
			f := fixtureFilm("167934")
			f.Dates[0].Showtimes[0].Version = "VO"
			p.Events = []film{f}
		},
		"conflicting format": func(p *pageProps) {
			f := fixtureFilm("167934")
			f.Dates[0].Showtimes[0].Relief = "3D"
			p.Events = []film{f}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			p := fixturePage(fixtureCinema())
			mutate(&p)
			fetcher := singleFetcher(t, p)
			wantRequests := 2
			switch name {
			case "wrong page cinema", "missing catalog", "wrong catalog identity", "missing program", "missing events", "missing attributes":
				fetcher.builds = append(fetcher.builds, "build-1")
				wantRequests = 4
			}
			d, summary, err := Sync(t.Context(), fetcher, fixtureOptions())
			if !reflect.DeepEqual(d, schedule.Dataset{}) || summary != (SyncSummary{}) || fetcher.RequestCount() != wantRequests {
				t.Fatal("partial dataset or unexpected retry")
			}
			if name == "empty dataset" {
				var re *RequestError
				if !errors.Is(err, schedule.ErrDatasetValidation) || errors.As(err, &re) {
					t.Fatalf("final validation err=%v", err)
				}
				return
			}
			var re *RequestError
			if !errors.As(err, &re) || re.Operation != OperationCinema || re.Kind != syncproxy.FailureInvalidJSON || errors.Is(err, schedule.ErrDatasetValidation) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestSyncMalformedResponsesArePayloadFailures(t *testing.T) {
	for _, op := range []Operation{OperationBootstrap, OperationCinema} {
		t.Run(string(op), func(t *testing.T) {
			client := testClient(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == BootstrapURL {
					body := "<html>synthetic-private-body</html>"
					if op == OperationCinema {
						body = string(bootstrapBytes(t, "build-1", []cinema{fixtureCinema()}))
					}
					return response(http.StatusOK, "text/html", body), nil
				}
				return response(http.StatusOK, "application/json", `{"pageProps":{"cinemaId":"synthetic-private-body"}}`), nil
			})
			data, summary, err := Sync(t.Context(), client, fixtureOptions())
			var re *RequestError
			if !errors.As(err, &re) || re.Operation != op || re.Kind != syncproxy.FailureInvalidJSON || re.StatusCode != 0 || errors.Is(err, schedule.ErrDatasetValidation) {
				t.Fatalf("payload classification: %v", err)
			}
			wantRequests := 1
			if op == OperationCinema {
				wantRequests = 4
			}
			if client.RequestCount() != wantRequests || !reflect.DeepEqual(data, schedule.Dataset{}) || summary != (SyncSummary{}) {
				t.Fatal("partial dataset or unexpected retry")
			}
			if strings.Contains(err.Error(), "synthetic-private-body") || errors.Unwrap(err) != nil {
				t.Fatal("payload error retained provider data")
			}
		})
	}
}

func TestSyncCancellationAndErrorRedaction(t *testing.T) {
	f := singleFetcher(t, fixturePage(fixtureCinema()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := Sync(ctx, f, fixtureOptions()); !errors.Is(err, context.Canceled) || len(f.calls) != 0 {
		t.Fatal("cancellation")
	}
	f.fail = func(string, string) error {
		return fmt.Errorf("synthetic-private-body: %w", &RequestError{Kind: syncproxy.FailureStatus, StatusCode: 404})
	}
	f.builds = append(f.builds, "build-1")
	if _, _, err := Sync(t.Context(), f, fixtureOptions()); err == nil || strings.Contains(err.Error(), "synthetic-private-body") {
		t.Fatal("unredacted failure")
	}
}
