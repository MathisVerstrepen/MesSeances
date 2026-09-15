package mk2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type fixtureFetcher struct {
	mu             sync.Mutex
	cinemas, films []byte
	pages          map[string][]byte
	calls          map[string]int
	failure        error
}

func (f *fixtureFetcher) get(key string, body []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[key]++
	return body, nil
}
func (f *fixtureFetcher) FetchCinemas(context.Context) ([]byte, error) {
	return f.get("cinemas", f.cinemas)
}
func (f *fixtureFetcher) FetchFilms(context.Context) ([]byte, error) { return f.get("films", f.films) }
func (f *fixtureFetcher) FetchComplex(_ context.Context, slug string) ([]byte, error) {
	if f.failure != nil {
		return nil, f.failure
	}
	return f.get(slug, f.pages[slug])
}
func encode(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func fixture(t *testing.T) (*fixtureFetcher, complex, SyncOptions) {
	t.Helper()
	c := cinema{ID: "0004", Name: "Bibliothèque", Address: " 128 avenue de France ", City: "PARIS", ComplexSlug: "bibliotheque"}
	empty := cinema{ID: "0005", Name: "MK2 Bibliothèque B", Address: c.Address, City: c.City, ComplexSlug: c.ComplexSlug}
	runtime := 93
	f := film{ID: "HO00006568", Title: "Event", Runtime: &runtime, Poster: schedule.MK2PosterPrefix + "HO00006568", Synopsis: "Synopsis", OpeningDate: "2026-09-16T00:00:00+02:00"}
	s := session{ID: "0004-140350", CinemaID: "0004", SessionID: "140350", FilmID: f.ID, ScheduledFilmID: "0004-" + f.ID, ShowTime: "2026-09-14T00:15:00+02:00", Attributes: []attribute{{"2D"}, {"VO"}, {"STFR"}}}
	p := complex{Slug: c.ComplexSlug, Zipcode: "75013", Cinemas: []cinema{c, empty}, Types: []sessionType{{Groups: []group{{Cinema: c, Film: f, Sessions: []session{s}}}}}}
	return &fixtureFetcher{cinemas: encode(t, map[string]any{"data": p.Cinemas}), films: encode(t, map[string]any{"data": []film{f}}), pages: map[string][]byte{p.Slug: encode(t, p)}, calls: map[string]int{}}, p, SyncOptions{From: "2026-09-14", Now: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)}
}
func TestSyncCatalogJoinDynamicHorizonAndUnknownEnd(t *testing.T) {
	f, p, options := fixture(t)
	s := p.Types[0].Groups[0].Sessions[0]
	for i, at := range []string{"2026-10-25T02:30:00+02:00", "2026-10-25T02:30:00+01:00", "2027-07-01T04:30:00+02:00", "2026-09-13T23:59:00+02:00"} {
		next := s
		next.ID, next.SessionID, next.ShowTime = fmt.Sprintf("0004-%d", i+1), fmt.Sprint(i+1), at
		p.Types[0].Groups[0].Sessions = append(p.Types[0].Groups[0].Sessions, next)
	}
	p.Types = append(p.Types, p.Types[0]) // regular and event groups share identical sessions
	f.pages[p.Slug] = encode(t, p)
	d, summary, err := Sync(t.Context(), f, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Theaters) != 2 || len(d.Showtimes) != 4 || d.Window.Through != "2027-07-01" || summary.Cinemas != 2 || summary.Movies != 1 || summary.Requests != 3 || summary.Showtimes != 4 {
		t.Fatalf("data=%+v summary=%+v", d, summary)
	}
	if !reflect.DeepEqual(f.calls, map[string]int{"cinemas": 1, "films": 1, "bibliotheque": 1}) {
		t.Fatalf("calls=%v", f.calls)
	}
	if d.Theaters[0].Name != "MK2 Bibliothèque" || d.Theaters[0].Address != "128 avenue de France" || d.Theaters[0].City != "Paris" || d.Theaters[0].PostalCode != "75013" || len(d.Theaters[0].AvailableDates) != 3 || len(d.Theaters[1].AvailableDates) != 0 || len(d.Theaters[0].AcceptedPasses) != 0 {
		t.Fatalf("cinemas=%+v", d.Theaters)
	}
	for _, r := range d.Showtimes {
		if !r.EndTime.Equal(r.StartTime) || r.Movie.RuntimeMinutes != 93 || r.Room != "" || r.Language != schedule.LanguageVOSTFR || r.FirstPartDurationMinutes != 0 || r.Movie.PosterURL != schedule.MK2PosterPrefix+r.Movie.ProviderID || r.BookingURL != schedule.MK2BookingPrefix+"0004&sessionId="+strings.TrimPrefix(r.ProviderShowingID, "0004-") {
			t.Fatalf("record=%+v", r)
		}
	}
}
func TestSyncFirstNonemptySynopsisWins(t *testing.T) {
	for _, tc := range []struct {
		name     string
		catalog  []string
		embedded string
		want     string
	}{
		{"duplicate catalog", []string{"First synopsis", "Different synopsis"}, "First synopsis", "First synopsis"},
		{"catalog versus embedded", []string{"Catalog synopsis"}, "Embedded synopsis", "Catalog synopsis"},
		{"empty catalog then nonempty duplicate", []string{"  ", " First synopsis ", "Later synopsis"}, "Embedded synopsis", "First synopsis"},
		{"embedded fills missing catalog", []string{"", "  "}, " Embedded synopsis ", "Embedded synopsis"},
		{"empty embedded preserves catalog", []string{"Catalog synopsis"}, "", "Catalog synopsis"},
		{"all missing", []string{"", "  "}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, p, options := fixture(t)
			g := &p.Types[0].Groups[0]
			g.Film.ID = "HO00000105"
			g.Film.Poster = schedule.MK2PosterPrefix + g.Film.ID
			g.Sessions[0].FilmID = g.Film.ID
			g.Sessions[0].ScheduledFilmID = g.Cinema.ID + "-" + g.Film.ID
			var catalog []film
			for _, synopsis := range tc.catalog {
				movie := g.Film
				movie.Synopsis = synopsis
				catalog = append(catalog, movie)
			}
			g.Film.Synopsis = tc.embedded
			f.films = encode(t, map[string]any{"data": catalog})
			f.pages[p.Slug] = encode(t, p)
			d, summary, err := Sync(t.Context(), f, options)
			if err != nil {
				t.Fatal(err)
			}
			if summary.Movies != 1 || len(d.Showtimes) != 1 || d.Showtimes[0].Movie.ProviderID != "HO00000105" || d.Showtimes[0].Movie.Overview != tc.want {
				t.Fatalf("data=%+v summary=%+v", d, summary)
			}
			if !reflect.DeepEqual(f.calls, map[string]int{"cinemas": 1, "films": 1, p.Slug: 1}) {
				t.Fatalf("calls=%v", f.calls)
			}
		})
	}
}

func TestMergeMovieRejectsOtherConflictsDespiteSynopsisDifference(t *testing.T) {
	for name, mutate := range map[string]func(*schedule.MovieRecord){
		"identity":                  func(m *schedule.MovieRecord) { m.ProviderID = "HO2" },
		"title":                     func(m *schedule.MovieRecord) { m.Title = "Other title" },
		"title internal whitespace": func(m *schedule.MovieRecord) { m.Title = "Ti tle" },
		"runtime":                   func(m *schedule.MovieRecord) { m.RuntimeMinutes = 109 },
		"poster":                    func(m *schedule.MovieRecord) { m.PosterURL = schedule.MK2PosterPrefix + "HO2" },
		"release date":              func(m *schedule.MovieRecord) { m.ReleaseDate = "2026-01-07" },
		"genres":                    func(m *schedule.MovieRecord) { m.Genres = []string{"Comedy"} },
	} {
		t.Run(name, func(t *testing.T) {
			a := schedule.MovieRecord{Provider: schedule.ProviderMK2, ProviderID: "HO1", Title: "Title", RuntimeMinutes: 108, PosterURL: schedule.MK2PosterPrefix + "HO1", ReleaseDate: "2026-01-06", Genres: []string{"Drama"}, Overview: "First synopsis"}
			b := a
			b.Overview = "Different synopsis"
			mutate(&b)
			if _, err := mergeMovie(a, b); !errors.Is(err, schedule.ErrDatasetValidation) {
				t.Fatalf("expected dataset validation error, got %v", err)
			}
		})
	}
}

func TestSyncRejectsOtherMovieConflictsDespiteSynopsisDifference(t *testing.T) {
	for name, mutate := range map[string]func(*film){
		"title":                     func(f *film) { f.Title = "Other title" },
		"title internal whitespace": func(f *film) { f.Title = "Ev ent" },
		"runtime":                   func(f *film) { runtime := 109; f.Runtime = &runtime },
		"invalid runtime":           func(f *film) { runtime := -1; f.Runtime = &runtime },
		"release date":              func(f *film) { f.OpeningDate = "2026-01-07T00:00:00Z" },
		"genres":                    func(f *film) { f.Genres[0].Name = "Comedy" },
	} {
		for _, source := range []string{"catalog", "embedded duplicate", "catalog versus embedded"} {
			if source == "catalog versus embedded" && strings.HasPrefix(name, "title") {
				continue // Cross-source titles are covered by the catalog precedence tests.
			}
			t.Run(name+"/"+source, func(t *testing.T) {
				f, p, options := fixture(t)
				original := p.Types[0].Groups[0].Film
				original.Genres = []struct {
					Name string `json:"name"`
				}{{Name: "Drama"}}
				changed := original
				changed.Genres = append([]struct {
					Name string `json:"name"`
				}{}, original.Genres...)
				changed.Synopsis = "Different synopsis"
				mutate(&changed)
				if source == "catalog versus embedded" {
					changed.Title = "Event label"
				}
				catalog := []film{original}
				p.Types[0].Groups[0].Film = original
				wantCalls := map[string]int{"cinemas": 1, "films": 1}
				if source == "catalog" {
					catalog = append(catalog, changed)
				} else {
					if source == "embedded duplicate" {
						p.Types[0].Groups = append(p.Types[0].Groups, p.Types[0].Groups[0])
					}
					p.Types[0].Groups[0].Film = changed
					wantCalls[p.Slug] = 1
				}
				f.films = encode(t, map[string]any{"data": catalog})
				f.pages[p.Slug] = encode(t, p)
				d, summary, err := Sync(t.Context(), f, options)
				if !errors.Is(err, schedule.ErrDatasetValidation) || !reflect.DeepEqual(d, schedule.Dataset{}) || summary != (SyncSummary{}) || !reflect.DeepEqual(f.calls, wantCalls) {
					t.Fatalf("data=%+v summary=%+v calls=%v err=%v", d, summary, f.calls, err)
				}
			})
		}
	}
}

func TestSyncCatalogTitleWinsOverEmbeddedEventLabel(t *testing.T) {
	for _, tc := range []struct {
		id, catalog, embedded string
	}{
		{"HO00000105", "Nomadland", "Comment habiter le monde ?"},
		{"HO00006416", "Messidor", "Comment mettre en œuvre son\u00a0émancipation ?"},
		{"HO00006417", "Network", "Que faire de nos colères ?"},
		{"HO00006418", "Sur la planche", "La parole peut-elle libérer ?"},
		{"HO1", "Le triangle d'or", "Le  triangle d'or"},
		{"HO2", "Le triangle d'or", "Le\ttriangle d'or"},
		{"HO3", "Le triangle d'or", "Le triangle d’or"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			f, p, options := fixture(t)
			g := &p.Types[0].Groups[0]
			g.Film.ID, g.Film.Title = tc.id, tc.catalog
			g.Film.Poster = schedule.MK2PosterPrefix + tc.id
			g.Sessions[0].FilmID = tc.id
			g.Sessions[0].ScheduledFilmID = g.Cinema.ID + "-" + tc.id
			canonical := g.Film
			f.films = encode(t, map[string]any{"data": []film{canonical}})
			g.Film.Title, g.Film.Synopsis = tc.embedded, "Event synopsis"
			// Duplicate event groups must still deduplicate their shared session.
			p.Types = append(p.Types, p.Types[0])
			f.pages[p.Slug] = encode(t, p)
			d, summary, err := Sync(t.Context(), f, options)
			if err != nil || summary.Movies != 1 || summary.Showtimes != 1 || summary.Requests != 3 || len(d.Showtimes) != 1 {
				t.Fatalf("data=%+v summary=%+v err=%v", d, summary, err)
			}
			want, err := parseMovie(canonical)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(d.Showtimes[0].Movie, want) {
				t.Fatalf("movie=%+v want=%+v", d.Showtimes[0].Movie, want)
			}
		})
	}
}

func TestSyncCatalogAndEmbeddedComplementMissingMovieMetadata(t *testing.T) {
	for _, source := range []string{"catalog", "embedded"} {
		for _, missingTitle := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/missing-title=%t", source, missingTitle), func(t *testing.T) {
				f, p, options := fixture(t)
				full := p.Types[0].Groups[0].Film
				full.Title = "Canonical title"
				full.Genres = []struct {
					Name string `json:"name"`
				}{{Name: "Drama"}}
				partial := film{ID: full.ID, Title: "Event label"}
				if missingTitle {
					partial.Title = "  "
				}
				catalog, embedded := full, partial
				if source == "catalog" {
					catalog, embedded = partial, full
				}
				f.films = encode(t, map[string]any{"data": []film{catalog}})
				p.Types[0].Groups[0].Film = embedded
				f.pages[p.Slug] = encode(t, p)
				d, summary, err := Sync(t.Context(), f, options)
				if err != nil || summary.Movies != 1 || len(d.Showtimes) != 1 {
					t.Fatalf("data=%+v summary=%+v err=%v", d, summary, err)
				}
				want, err := parseMovie(full)
				if err != nil {
					t.Fatal(err)
				}
				if source == "catalog" && !missingTitle {
					want.Title = partial.Title
				}
				if !reflect.DeepEqual(d.Showtimes[0].Movie, want) {
					t.Fatalf("movie=%+v want=%+v", d.Showtimes[0].Movie, want)
				}
			})
		}
	}
}

func TestSyncTitleCasePreservesFirstSpelling(t *testing.T) {
	for _, source := range []string{"catalog duplicate", "embedded"} {
		t.Run(source, func(t *testing.T) {
			f, p, options := fixture(t)
			first := p.Types[0].Groups[0].Film
			first.Title = "Le triangle d'or"
			second := first
			second.Title = "Le Triangle d'or"
			catalog := []film{first}
			if source == "catalog duplicate" {
				catalog = append(catalog, second)
			}
			p.Types[0].Groups[0].Film = second
			f.films = encode(t, map[string]any{"data": catalog})
			f.pages[p.Slug] = encode(t, p)
			d, summary, err := Sync(t.Context(), f, options)
			if err != nil || summary.Movies != 1 || len(d.Showtimes) != 1 || d.Showtimes[0].Movie.Title != first.Title {
				t.Fatalf("data=%+v summary=%+v err=%v", d, summary, err)
			}
		})
	}
	for _, title := range []string{"Le  triangle d'or", "Le\ttriangle d'or", "Le triangle d’or"} {
		t.Run(title, func(t *testing.T) {
			f, p, options := fixture(t)
			catalog := p.Types[0].Groups[0].Film
			catalog.Title = "Le triangle d'or"
			duplicate := catalog
			duplicate.Title = title
			f.films = encode(t, map[string]any{"data": []film{catalog, duplicate}})
			d, summary, err := Sync(t.Context(), f, options)
			if !errors.Is(err, schedule.ErrDatasetValidation) || !reflect.DeepEqual(d, schedule.Dataset{}) || summary != (SyncSummary{}) {
				t.Fatalf("data=%+v summary=%+v err=%v", d, summary, err)
			}
		})
	}
}

func TestSyncAdmitsMissingCatalogFilmAndValidatesEveryEmbeddedOccurrence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*film)
		valid  bool
	}{
		{"consistent", func(*film) {}, true},
		{"title case", func(f *film) { f.Title = "EVENT" }, true},
		{"synopsis first", func(f *film) { f.Synopsis = "Another synopsis" }, true},
		{"title conflict", func(f *film) { f.Title = "Other" }, false},
		{"whitespace conflict", func(f *film) { f.Title = "Ev ent" }, false},
		{"runtime conflict", func(f *film) { runtime := 109; f.Runtime = &runtime }, false},
		{"identity conflict", func(f *film) { f.ID = "HO2" }, false},
		{"invalid identity", func(f *film) { f.ID = "invalid" }, false},
		{"invalid runtime", func(f *film) { runtime := -1; f.Runtime = &runtime }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, p, options := fixture(t)
			f.films = []byte(`{"data":[]}`)
			second := p.Types[0].Groups[0]
			// A second complex verifies shared identity merging across pages,
			// independent of concurrent fetch completion order.
			second.Cinema.ID = "0006"
			second.Cinema.ComplexSlug = "second"
			second.Sessions = append([]session{}, second.Sessions...)
			second.Sessions[0].CinemaID = "0006"
			second.Sessions[0].ID = "0006-140350"
			second.Sessions[0].ScheduledFilmID = "0006-" + second.Film.ID
			tc.mutate(&second.Film)
			page := complex{Slug: "second", Zipcode: "75013", Cinemas: []cinema{second.Cinema}, Types: []sessionType{{Groups: []group{second, second}}}}
			cinemas := append(append([]cinema{}, p.Cinemas...), second.Cinema)
			f.cinemas = encode(t, map[string]any{"data": cinemas})
			f.pages[page.Slug] = encode(t, page)
			d, summary, err := Sync(t.Context(), f, options)
			if !tc.valid {
				if !errors.Is(err, schedule.ErrDatasetValidation) || !reflect.DeepEqual(d, schedule.Dataset{}) || summary != (SyncSummary{}) {
					t.Fatalf("data=%+v summary=%+v err=%v", d, summary, err)
				}
				return
			}
			if err != nil || summary.Movies != 1 || summary.Requests != 4 || len(d.Theaters) != 3 || len(d.Showtimes) != 2 {
				t.Fatalf("data=%+v summary=%+v err=%v", d, summary, err)
			}
			for _, showing := range d.Showtimes {
				if showing.Movie.Title != "Event" || showing.Movie.Overview != "Synopsis" || showing.Movie.ProviderID != p.Types[0].Groups[0].Film.ID {
					t.Fatalf("movie=%+v", showing.Movie)
				}
			}
		})
	}
	t.Run("missing title", func(t *testing.T) {
		f, p, options := fixture(t)
		f.films = []byte(`{"data":[]}`)
		p.Types[0].Groups[0].Film.Title = " "
		f.pages[p.Slug] = encode(t, p)
		d, summary, err := Sync(t.Context(), f, options)
		if !errors.Is(err, schedule.ErrDatasetValidation) || !reflect.DeepEqual(d, schedule.Dataset{}) || summary != (SyncSummary{}) {
			t.Fatalf("data=%+v summary=%+v err=%v", d, summary, err)
		}
	})
}

func TestSyncRejectsPartialOrConflictingData(t *testing.T) {
	for name, mutate := range map[string]func(*fixtureFetcher, *complex){
		"absent cinemas":         func(f *fixtureFetcher, _ *complex) { f.cinemas = []byte(`{}`) },
		"absent films":           func(f *fixtureFetcher, _ *complex) { f.films = []byte(`{"data":null}`) },
		"wrong complex":          func(_ *fixtureFetcher, p *complex) { p.Slug = "other" },
		"missing cinema array":   func(_ *fixtureFetcher, p *complex) { p.Cinemas = nil },
		"missing types":          func(_ *fixtureFetcher, p *complex) { p.Types = nil },
		"missing groups":         func(_ *fixtureFetcher, p *complex) { p.Types[0].Groups = nil },
		"missing sessions":       func(_ *fixtureFetcher, p *complex) { p.Types[0].Groups[0].Sessions = nil },
		"missing postal":         func(_ *fixtureFetcher, p *complex) { p.Zipcode = "" },
		"missing catalog member": func(_ *fixtureFetcher, p *complex) { p.Cinemas = p.Cinemas[:1] },
		"orphan cinema":          func(_ *fixtureFetcher, p *complex) { p.Types[0].Groups[0].Cinema.ID = "9" },
		"orphan film":            func(_ *fixtureFetcher, p *complex) { p.Types[0].Groups[0].Film.ID = "HO9" },
		"conflicting film":       func(_ *fixtureFetcher, p *complex) { runtime := 109; p.Types[0].Groups[0].Film.Runtime = &runtime },
		"conflicting cinema":     func(_ *fixtureFetcher, p *complex) { p.Types[0].Groups[0].Cinema.Address = "Other" },
		"wrong showing cinema":   func(_ *fixtureFetcher, p *complex) { p.Types[0].Groups[0].Sessions[0].CinemaID = "0005" },
		"wrong showing film":     func(_ *fixtureFetcher, p *complex) { p.Types[0].Groups[0].Sessions[0].FilmID = "HO9" },
		"wrong scheduled film": func(_ *fixtureFetcher, p *complex) {
			p.Types[0].Groups[0].Sessions[0].ScheduledFilmID = "0005-HO00006568"
		},
		"wrong showing id": func(_ *fixtureFetcher, p *complex) { p.Types[0].Groups[0].Sessions[0].ID = "0004-1" },
		"no offset":        func(_ *fixtureFetcher, p *complex) { p.Types[0].Groups[0].Sessions[0].ShowTime = "2026-09-14T18:00:00" },
		"conflicting duplicate": func(_ *fixtureFetcher, p *complex) {
			s := p.Types[0].Groups[0].Sessions[0]
			s.ShowTime = "2026-09-14T18:00:00Z"
			p.Types[0].Groups[0].Sessions = append(p.Types[0].Groups[0].Sessions, s)
		},
		"whole chain empty": func(_ *fixtureFetcher, p *complex) { p.Types[0].Groups[0].Sessions = []session{} },
		"failure": func(f *fixtureFetcher, _ *complex) {
			f.failure = errors.New("https://user:password@proxy.test secret body")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, p, options := fixture(t)
			slug := p.Slug
			mutate(f, &p)
			f.pages[slug] = encode(t, p)
			d, summary, err := Sync(t.Context(), f, options)
			if err == nil || !reflect.DeepEqual(d, schedule.Dataset{}) || summary != (SyncSummary{}) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "password") {
				t.Fatalf("data=%+v summary=%+v err=%v", d, summary, err)
			}
		})
	}
}
func TestSyncCancellationIsTypedAndEmpty(t *testing.T) {
	f, _, options := fixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d, _, err := Sync(ctx, f, options)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(d, schedule.Dataset{}) || len(f.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, f.calls)
	}
}

func TestAttributesAndOptionalMovieFields(t *testing.T) {
	for _, tc := range []struct {
		names    []string
		language schedule.Language
		version  string
		format   schedule.Format
	}{
		{[]string{"VF", "2D"}, "VF", "VF", "2D"},
		{[]string{"3D", "STFR", "VO"}, "VOSTFR", "VO+STFR", "3D"},
		{[]string{"STFR", "VF", "2D"}, "VFSTF", "VF+STFR", "2D"},
		{[]string{"VO", "IMAX", "3D"}, "VO", "VO", "IMAX"},
		{[]string{"Muet", "4DX", "2D"}, "", "Muet", "4DX"},
	} {
		attrs := []attribute{}
		for _, n := range tc.names {
			attrs = append(attrs, attribute{n})
		}
		l, v, f, err := parseAttributes(attrs)
		if err != nil || l != tc.language || v != tc.version || f != tc.format {
			t.Fatalf("attributes=%v got=%s,%s,%s,%v", tc.names, l, v, f, err)
		}
	}
	for _, names := range [][]string{{}, {"VF"}, {"2D"}, {"VF", "VO", "2D"}, {"Muet", "VF", "2D"}, {"Muet", "STFR", "2D"}, {"VF", "IMAX", "4DX"}, {"VF", "2D", "3D"}, {"VF", "UNKNOWN"}} {
		attrs := []attribute{}
		for _, n := range names {
			attrs = append(attrs, attribute{n})
		}
		if _, _, _, err := parseAttributes(attrs); err == nil {
			t.Fatalf("accepted %v", names)
		}
	}
	for _, runtime := range []string{`-1`, `1.5`, `"93"`, `9223372036854775808`, `1000000000000`} {
		rows, err := parseCatalog[film]([]byte(`{"data":[{"id":"HO1","title":"Test","runTime":`+runtime+`}]}`), OperationFilms)
		if err == nil {
			_, err = parseMovie(rows[0])
		}
		if err == nil {
			t.Fatalf("accepted runtime %s", runtime)
		}
	}
	for _, runtime := range []string{`0`, `null`, `93`} {
		rows, err := parseCatalog[film]([]byte(`{"data":[{"id":"HO1","title":"Test","runTime":`+runtime+`,"graphicUrl":"https://evil.test/secret","openingDate":"bad"}]}`), OperationFilms)
		if err != nil {
			t.Fatal(err)
		}
		m, err := parseMovie(rows[0])
		if err != nil || m.PosterURL != "" || m.ReleaseDate != "" {
			t.Fatalf("movie=%+v err=%v", m, err)
		}
	}
	a, _ := parseMovie(film{ID: "HO1", Title: "Title"})
	b, _ := parseMovie(film{ID: "HO1", Synopsis: "Overview"})
	merged, err := mergeMovie(a, b)
	if err != nil || merged.Title != "Title" || merged.Overview != "Overview" {
		t.Fatalf("merged=%+v err=%v", merged, err)
	}
}
