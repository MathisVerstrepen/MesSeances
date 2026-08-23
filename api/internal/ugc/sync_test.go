package ugc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type fakeGetter struct {
	mu        sync.Mutex
	responses map[string][]byte
	finalURLs map[string]string
	calls     int
	failKind  string
	get       func(context.Context, string, string) (FetchResult, error)
}

func (f *fakeGetter) Get(ctx context.Context, kind, rawURL string) (FetchResult, error) {
	f.mu.Lock()
	f.calls++
	get := f.get
	if kind == f.failKind {
		f.mu.Unlock()
		return FetchResult{}, fmt.Errorf("synthetic transport failure")
	}
	body, ok := f.responses[rawURL]
	finalURL := rawURL
	if configured := f.finalURLs[rawURL]; configured != "" {
		finalURL = configured
	}
	f.mu.Unlock()
	if get != nil {
		return get(ctx, kind, rawURL)
	}
	if !ok {
		return FetchResult{}, fmt.Errorf("unexpected URL")
	}
	return FetchResult{Body: body, FinalURL: finalURL}, nil
}
func (f *fakeGetter) RequestCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func singleDateCinemaFixture(t *testing.T) []byte {
	return []byte(strings.Replace(string(readFixture(t, "cinema.html")), `<button id="nav_date_2026-08-16">16 août</button>`, "", 1))
}

func TestSyncCompleteDiscovery(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=25</loc></url></urlset>`)
	secondDayShowingsText := strings.ReplaceAll(string(readFixture(t, "showings.html")), "15/08/2026", "16/08/2026")
	secondDayShowingsText = strings.ReplaceAll(secondDayShowingsText, `data-showing="900"`, `data-showing="902"`)
	secondDayShowingsText = strings.ReplaceAll(secondDayShowingsText, `data-showing="901"`, `data-showing="903"`)
	secondDayShowings := []byte(secondDayShowingsText)
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, "https://www.ugc.fr/cinema.html?id=25": readFixture(t, "cinema.html"), "https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=25&date=15%2F08%2F2026&page=30007": readFixture(t, "showings.html"), "https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=25&date=16%2F08%2F2026&page=30007": secondDayShowings}}
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if data.Scope != schedule.ScopeAll || data.Window.Through != "2026-08-16" || summary.Cinemas != 1 || summary.Skipped != 0 || summary.Dates != 2 || summary.Showtimes != 4 || summary.Requests != 4 {
		t.Fatalf("data=%+v summary=%+v", data, summary)
	}
}

func TestSyncIncludesAdvertisedDateBeyondFormerLimit(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=25</loc></url></urlset>`)
	cinema := []byte(`<html><head><title>UGC Lille, cinéma à Lille (59000)</title></head><body><section id="cinema-heading"><h1>UGC Lille</h1><p class="address">1 rue Test 59000 Lille</p></section><input name="cinemaId" value="25"><button id="nav_date_2026-08-14"></button><button id="nav_date_2026-08-15"></button><button id="nav_date_2027-02-14"></button></body></html>`)
	showings := func(id, date string) []byte {
		return []byte(fmt.Sprintf(`<div id="bloc-showing-film-1"><a data-film="1" title="Film">Film</a><span>(1h30)</span><button data-showing="%s" data-film="1" data-cinema="25" data-version="VF" data-seancedate="%s" data-seancehour="12:00"></button></div>`, id, date))
	}
	getter := &fakeGetter{responses: map[string][]byte{
		SitemapURL:                             sitemap,
		"https://www.ugc.fr/cinema.html?id=25": cinema,
		"https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=25&date=15%2F08%2F2026&page=30007": showings("100", "15/08/2026"),
		"https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=25&date=14%2F02%2F2027&page=30007": showings("101", "14/02/2027"),
	}}
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if data.Window.Through != "2027-02-14" || len(data.Showtimes) != 2 || summary.Dates != 2 || summary.Requests != 4 {
		t.Fatalf("window=%+v summary=%+v showtimes=%+v", data.Window, summary, data.Showtimes)
	}
}

func TestSyncAcceptsActiveCinemasAboveFormerLimit(t *testing.T) {
	var sitemap strings.Builder
	sitemap.WriteString(`<?xml version="1.0"?><urlset>`)
	for id := 1; id <= 257; id++ {
		fmt.Fprintf(&sitemap, `<url><loc>https://www.ugc.fr/cinema.html?id=%d</loc></url>`, id)
	}
	sitemap.WriteString(`</urlset>`)
	getter := &fakeGetter{get: func(_ context.Context, kind, rawURL string) (FetchResult, error) {
		if kind == "sitemap" {
			return FetchResult{Body: []byte(sitemap.String()), FinalURL: rawURL}, nil
		}
		fields := strings.Fields(kind)
		if len(fields) < 2 {
			return FetchResult{}, fmt.Errorf("unexpected request kind")
		}
		id := fields[1]
		if fields[0] == "cinema" {
			body := fmt.Sprintf(`<html><head><title>UGC Test, cinéma à Lille (59000)</title></head><body><section id="cinema-heading"><h1>UGC %s</h1><p class="address">1 rue Test 59000 Lille</p></section><input name="cinemaId" value="%s"><button id="nav_date_2026-08-15"></button></body></html>`, id, id)
			return FetchResult{Body: []byte(body), FinalURL: rawURL}, nil
		}
		if fields[0] == "showings" {
			id = fields[2]
			showingID := "9" + id
			body := fmt.Sprintf(`<div id="bloc-showing-film-1"><a data-film="1" title="Film">Film</a><span>(1h30)</span><button data-showing="%s" data-film="1" data-cinema="%s" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div>`, showingID, id)
			return FetchResult{Body: []byte(body), FinalURL: rawURL}, nil
		}
		return FetchResult{}, fmt.Errorf("unexpected request kind")
	}}
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
	if err != nil || len(data.Theaters) != 257 || len(data.Showtimes) != 257 || summary.Cinemas != 257 || summary.Showtimes != 257 {
		t.Fatalf("theaters=%d showtimes=%d summary=%+v err=%v", len(data.Theaters), len(data.Showtimes), summary, err)
	}
}

func TestSyncPreservesCinemaDateAndShowtimeOrderAcrossReverseCompletion(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=44</loc></url><url><loc>https://www.ugc.fr/cinema.html?id=25</loc></url></urlset>`)
	cinemaURLs := map[string][]byte{
		"https://www.ugc.fr/cinema.html?id=25": singleDateCinemaFixture(t),
		"https://www.ugc.fr/cinema.html?id=44": readFixture(t, "cinema-44.html"),
	}
	showingURLs := map[string][]byte{
		"https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=25&date=15%2F08%2F2026&page=30007": readFixture(t, "showings.html"),
		"https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=44&date=15%2F08%2F2026&page=30007": readFixture(t, "showings-44.html"),
	}
	cinemaStarted := make(chan string, 2)
	showingsStarted := make(chan string, 2)
	cinemaRelease := map[string]chan struct{}{"25": make(chan struct{}), "44": make(chan struct{})}
	showingsRelease := map[string]chan struct{}{"25": make(chan struct{}), "44": make(chan struct{})}
	getter := &fakeGetter{get: func(ctx context.Context, kind, rawURL string) (FetchResult, error) {
		if kind == "sitemap" {
			return FetchResult{Body: sitemap, FinalURL: rawURL}, nil
		}
		if strings.HasPrefix(kind, "cinema ") {
			id := strings.TrimPrefix(kind, "cinema ")
			cinemaStarted <- id
			select {
			case <-cinemaRelease[id]:
				return FetchResult{Body: cinemaURLs[rawURL], FinalURL: rawURL}, nil
			case <-ctx.Done():
				return FetchResult{}, ctx.Err()
			}
		}
		if strings.HasPrefix(kind, "showings cinema ") {
			fields := strings.Fields(kind)
			id := fields[2]
			showingsStarted <- id
			select {
			case <-showingsRelease[id]:
				return FetchResult{Body: showingURLs[rawURL], FinalURL: rawURL}, nil
			case <-ctx.Done():
				return FetchResult{}, ctx.Err()
			}
		}
		return FetchResult{}, fmt.Errorf("unexpected request kind")
	}}
	type syncOutcome struct {
		data    schedule.Dataset
		summary SyncSummary
		err     error
	}
	done := make(chan syncOutcome, 1)
	go func() {
		data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
		done <- syncOutcome{data: data, summary: summary, err: err}
	}()
	firstCinema, secondCinema := <-cinemaStarted, <-cinemaStarted
	if firstCinema == secondCinema {
		t.Fatalf("cinema starts=%q,%q", firstCinema, secondCinema)
	}
	close(cinemaRelease["44"])
	close(cinemaRelease["25"])
	firstShowings, secondShowings := <-showingsStarted, <-showingsStarted
	if firstShowings == secondShowings {
		t.Fatalf("showings starts=%q,%q", firstShowings, secondShowings)
	}
	close(showingsRelease["44"])
	close(showingsRelease["25"])
	outcome := <-done
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if len(outcome.data.Theaters) != 2 || outcome.data.Theaters[0].ProviderID != "25" || outcome.data.Theaters[1].ProviderID != "44" {
		t.Fatalf("theaters=%+v", outcome.data.Theaters)
	}
	if len(outcome.data.Showtimes) != 3 || outcome.data.Showtimes[0].ProviderShowingID != "900" || outcome.data.Showtimes[1].ProviderShowingID != "901" || outcome.data.Showtimes[2].ProviderShowingID != "944" {
		t.Fatalf("showtimes=%+v", outcome.data.Showtimes)
	}
	if outcome.summary.Cinemas != 2 || outcome.summary.Dates != 2 || outcome.summary.Requests != 5 || outcome.summary.Showtimes != 3 {
		t.Fatalf("summary=%+v", outcome.summary)
	}
}

func TestSyncSkipsStaleSitemapCinemaAndKeepsCompleteScope(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=2</loc></url><url><loc>https://www.ugc.fr/cinema.html?id=25</loc></url></urlset>`)
	staleURL := "https://www.ugc.fr/cinema.html?id=2"
	validURL := "https://www.ugc.fr/cinema.html?id=25"
	showingsURL := "https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=25&date=15%2F08%2F2026&page=30007"
	getter := &fakeGetter{
		responses: map[string][]byte{SitemapURL: sitemap, staleURL: readFixture(t, "cinemas-directory.html"), validURL: singleDateCinemaFixture(t), showingsURL: readFixture(t, "showings.html")},
		finalURLs: map[string]string{staleURL: "https://www.ugc.fr/cinemas.html?id=1"},
	}
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if data.Scope != schedule.ScopeAll || len(data.Theaters) != 1 || data.Theaters[0].ProviderID != "25" || len(data.Showtimes) != 2 {
		t.Fatalf("data=%+v", data)
	}
	if summary.Scope != schedule.ScopeAll || summary.Cinemas != 1 || summary.Skipped != 1 || summary.Requests != 4 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestSyncDeduplicatesCinemaAliasAndCanonicalRegardlessOfSitemapOrder(t *testing.T) {
	tests := []struct {
		name    string
		entries string
	}{
		{name: "alias then canonical", entries: `<url><loc>https://www.ugc.fr/cinema.html?id=50</loc></url><url><loc>https://www.ugc.fr/cinema.html?id=44</loc></url>`},
		{name: "canonical then alias", entries: `<url><loc>https://www.ugc.fr/cinema.html?id=44</loc></url><url><loc>https://www.ugc.fr/cinema.html?id=50</loc></url>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sitemap := []byte(`<?xml version="1.0"?><urlset>` + test.entries + `</urlset>`)
			aliasURL := "https://www.ugc.fr/cinema.html?id=50"
			canonicalURL := "https://www.ugc.fr/cinema.html?id=44"
			showingsURL := "https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=44&date=15%2F08%2F2026&page=30007"
			getter := &fakeGetter{
				responses: map[string][]byte{SitemapURL: sitemap, aliasURL: readFixture(t, "cinema-44.html"), canonicalURL: readFixture(t, "cinema-44.html"), showingsURL: readFixture(t, "showings-44.html")},
				finalURLs: map[string]string{aliasURL: canonicalURL},
			}
			data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
			if err != nil {
				t.Fatal(err)
			}
			if data.Scope != schedule.ScopeAll || len(data.Theaters) != 1 || data.Theaters[0].ProviderID != "44" || len(data.Showtimes) != 1 || data.Showtimes[0].TheaterID != "ugc-44" {
				t.Fatalf("data=%+v", data)
			}
			if summary.Cinemas != 1 || summary.Showtimes != 1 || summary.Skipped != 1 || summary.Requests != 4 {
				t.Fatalf("summary=%+v", summary)
			}
		})
	}
}

func TestSyncDiagnosticCinemaAliasUsesCanonicalIdentity(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=50</loc></url></urlset>`)
	aliasURL := "https://www.ugc.fr/cinema.html?id=50"
	canonicalURL := "https://www.ugc.fr/cinema.html?id=44"
	showingsURL := "https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=44&date=15%2F08%2F2026&page=30007"
	getter := &fakeGetter{
		responses: map[string][]byte{SitemapURL: sitemap, aliasURL: readFixture(t, "cinema-44.html"), showingsURL: readFixture(t, "showings-44.html")},
		finalURLs: map[string]string{aliasURL: canonicalURL},
	}
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", CinemaID: "50", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if data.Scope != schedule.ScopeSingle || len(data.Theaters) != 1 || data.Theaters[0].ProviderID != "44" || len(data.Showtimes) != 1 || data.Showtimes[0].TheaterID != "ugc-44" {
		t.Fatalf("data=%+v", data)
	}
	if summary.Scope != schedule.ScopeSingle || summary.Cinemas != 1 || summary.Skipped != 0 || summary.Requests != 3 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestSyncRejectsMalformedCinemaAliasFinalURL(t *testing.T) {
	invalid := []string{
		"https://www.ugc.fr/cinema.html?id=44&extra=1",
		"https://www.ugc.fr/cinema.html?id=44&id=45",
		"https://www.ugc.fr/cinema.html?id=%zz",
		"https://user@www.ugc.fr/cinema.html?id=44",
		"https://www.ugc.fr:443/cinema.html?id=44",
		"https://WWW.UGC.FR/cinema.html?id=44",
		"https://www.ugc.fr/cinema.html?id=44#fragment",
		"http://www.ugc.fr/cinema.html?id=44",
		"https://www.ugc.fr/cinema.html?id=0",
		"https://www.ugc.fr/cinema.html?id=044",
		"https://www.ugc.fr/%63inema.html?id=44",
	}
	for _, finalURL := range invalid {
		t.Run(finalURL, func(t *testing.T) {
			sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=50</loc></url></urlset>`)
			aliasURL := "https://www.ugc.fr/cinema.html?id=50"
			getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, aliasURL: readFixture(t, "cinema-44.html")}, finalURLs: map[string]string{aliasURL: finalURL}}
			_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Now()})
			if err == nil || err.Error() != "cinema 50 response has unexpected final URL" {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestSyncDiagnosticStaleCinemaFailsAsInactive(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=2</loc></url></urlset>`)
	staleURL := "https://www.ugc.fr/cinema.html?id=2"
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, staleURL: readFixture(t, "cinemas-directory.html")}, finalURLs: map[string]string{staleURL: "https://www.ugc.fr/cinemas.html?id=1"}}
	data, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", CinemaID: "2", Now: time.Now()})
	if err == nil || err.Error() != "cinema 2 is inactive: redirected to UGC cinema directory" || len(data.Theaters) != 0 {
		t.Fatalf("data=%+v error=%v", data, err)
	}
}

func TestSyncFailsWhenAllSitemapCinemasAreStale(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=2</loc></url></urlset>`)
	staleURL := "https://www.ugc.fr/cinema.html?id=2"
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, staleURL: readFixture(t, "cinemas-directory.html")}, finalURLs: map[string]string{staleURL: "https://www.ugc.fr/cinemas.html?id=1"}}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Now()})
	if err == nil || err.Error() != "sync produced no active cinemas" || !errors.Is(err, schedule.ErrDatasetValidation) {
		t.Fatalf("error=%v", err)
	}
}

func TestSyncFailsWithoutShowtimes(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=25</loc></url></urlset>`)
	cinemaURL := "https://www.ugc.fr/cinema.html?id=25"
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, cinemaURL: readFixture(t, "cinema.html")}}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-20", Now: time.Now()})
	if err == nil || err.Error() != "sync produced no showtimes" || !errors.Is(err, schedule.ErrDatasetValidation) {
		t.Fatalf("error=%v", err)
	}
}

func TestSyncRejectsUnexpectedCinemaFinalURL(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=2</loc></url></urlset>`)
	cinemaURL := "https://www.ugc.fr/cinema.html?id=2"
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, cinemaURL: readFixture(t, "cinemas-directory.html")}, finalURLs: map[string]string{cinemaURL: "https://www.ugc.fr/cinemas.html?id=1&unexpected=true"}}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Now()})
	if err == nil || err.Error() != "cinema 2 response has unexpected final URL" {
		t.Fatalf("error=%v", err)
	}
}

func TestSyncExpectedCinemaURLKeepsParseFailureTerminal(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=2</loc></url></urlset>`)
	cinemaURL := "https://www.ugc.fr/cinema.html?id=2"
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, cinemaURL: readFixture(t, "cinemas-directory.html")}}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Now()})
	if err == nil || err.Error() != "parse cinema 2: cinema identity missing or conflicting" {
		t.Fatalf("error=%v", err)
	}
}
func TestSyncStopsOnFailure(t *testing.T) {
	sitemap := readFixture(t, "sitemap.xml")
	siblingStarted := make(chan struct{})
	siblingExited := make(chan struct{})
	var queuedStarted atomic.Bool
	getter := &fakeGetter{get: func(ctx context.Context, kind, rawURL string) (FetchResult, error) {
		switch kind {
		case "sitemap":
			return FetchResult{Body: sitemap, FinalURL: rawURL}, nil
		case "cinema 3":
			<-siblingStarted
			return FetchResult{}, fmt.Errorf("synthetic transport failure")
		case "cinema 25":
			close(siblingStarted)
			<-ctx.Done()
			close(siblingExited)
			return FetchResult{}, ctx.Err()
		case "cinema 46":
			queuedStarted.Store(true)
			return FetchResult{}, fmt.Errorf("queued request started")
		default:
			return FetchResult{}, fmt.Errorf("unexpected request")
		}
	}}
	data, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Now: time.Now()})
	if err == nil || err.Error() != "synthetic transport failure" || len(data.Theaters) != 0 || queuedStarted.Load() {
		t.Fatalf("data=%+v error=%v queued_started=%v", data, err, queuedStarted.Load())
	}
	if getter.RequestCount() != 3 {
		t.Fatalf("calls=%d", getter.RequestCount())
	}
	select {
	case <-siblingExited:
	default:
		t.Fatal("sync returned before sibling worker exited")
	}
}

func TestSyncValidatesLowerBoundBeforeRequests(t *testing.T) {
	getter := &fakeGetter{responses: map[string][]byte{}, failKind: "sitemap"}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-10-18", Now: time.Now()})
	if err == nil || err.Error() != "synthetic transport failure" || getter.calls != 1 {
		t.Fatalf("error=%v calls=%d", err, getter.calls)
	}
	getter = &fakeGetter{responses: map[string][]byte{}}
	_, _, err = Sync(context.Background(), getter, SyncOptions{From: "invalid", Now: time.Now()})
	if err == nil || err.Error() != "invalid from date" || getter.calls != 0 {
		t.Fatalf("error=%v calls=%d", err, getter.calls)
	}
}

func TestRunIndexedPhaseUsesExactlyTwoWorkersAndPreservesOrder(t *testing.T) {
	jobs := []int{0, 1, 2, 3}
	started := make(chan int, len(jobs))
	gates := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})}
	var active atomic.Int32
	var maximum atomic.Int32
	done := make(chan struct {
		results []int
		err     error
	}, 1)
	go func() {
		results, err := runIndexedPhase(context.Background(), jobs, func(_ context.Context, job int) (int, error) {
			current := active.Add(1)
			for {
				previous := maximum.Load()
				if current <= previous || maximum.CompareAndSwap(previous, current) {
					break
				}
			}
			started <- job
			<-gates[job]
			active.Add(-1)
			return job * 10, nil
		})
		done <- struct {
			results []int
			err     error
		}{results: results, err: err}
	}()
	first, second := <-started, <-started
	if first == second || maximum.Load() != ugcWorkerCount {
		t.Fatalf("started=%d,%d maximum=%d", first, second, maximum.Load())
	}
	select {
	case third := <-started:
		t.Fatalf("third job %d started while two active", third)
	default:
	}
	close(gates[second])
	third := <-started
	close(gates[first])
	fourth := <-started
	close(gates[fourth])
	close(gates[third])
	outcome := <-done
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	want := []int{0, 10, 20, 30}
	for index := range want {
		if outcome.results[index] != want[index] {
			t.Fatalf("results=%v want=%v", outcome.results, want)
		}
	}
	if maximum.Load() != ugcWorkerCount {
		t.Fatalf("maximum=%d", maximum.Load())
	}
}

func TestRunIndexedPhaseFirstErrorCancelsSiblingAndQueuedJobs(t *testing.T) {
	original := errors.New("original phase failure")
	siblingStarted := make(chan struct{})
	siblingExited := make(chan struct{})
	var queuedStarted atomic.Bool
	results, err := runIndexedPhase(context.Background(), []int{0, 1, 2}, func(ctx context.Context, job int) (int, error) {
		switch job {
		case 0:
			<-siblingStarted
			return 0, original
		case 1:
			close(siblingStarted)
			<-ctx.Done()
			close(siblingExited)
			return 0, ctx.Err()
		default:
			queuedStarted.Store(true)
			return job, nil
		}
	})
	if !errors.Is(err, original) || results != nil || queuedStarted.Load() {
		t.Fatalf("results=%v error=%v queued_started=%v", results, err, queuedStarted.Load())
	}
	select {
	case <-siblingExited:
	default:
		t.Fatal("phase returned before in-flight sibling exited")
	}
}
