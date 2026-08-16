package ugc

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"movieflow/api/internal/schedule"
)

type fakeGetter struct {
	responses map[string][]byte
	finalURLs map[string]string
	calls     int
	failKind  string
}

func (f *fakeGetter) Get(_ context.Context, kind, rawURL string) (FetchResult, error) {
	f.calls++
	if kind == f.failKind {
		return FetchResult{}, fmt.Errorf("synthetic transport failure")
	}
	body, ok := f.responses[rawURL]
	if !ok {
		return FetchResult{}, fmt.Errorf("unexpected URL")
	}
	finalURL := rawURL
	if configured := f.finalURLs[rawURL]; configured != "" {
		finalURL = configured
	}
	return FetchResult{Body: body, FinalURL: finalURL}, nil
}
func (f *fakeGetter) RequestCount() int { return f.calls }
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestSyncCompleteDiscovery(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=25</loc></url></urlset>`)
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, "https://www.ugc.fr/cinema.html?id=25": readFixture(t, "cinema.html"), "https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=25&date=15%2F08%2F2026&page=30007": readFixture(t, "showings.html")}}
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Through: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if data.Scope != schedule.ScopeAll || summary.Cinemas != 1 || summary.Skipped != 0 || summary.Dates != 1 || summary.Showtimes != 2 || summary.Requests != 3 {
		t.Fatalf("data=%+v summary=%+v", data, summary)
	}
}

func TestSyncSkipsStaleSitemapCinemaAndKeepsCompleteScope(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=2</loc></url><url><loc>https://www.ugc.fr/cinema.html?id=25</loc></url></urlset>`)
	staleURL := "https://www.ugc.fr/cinema.html?id=2"
	validURL := "https://www.ugc.fr/cinema.html?id=25"
	showingsURL := "https://www.ugc.fr/showingsCinemaAjaxAction!getShowingsForCinemaPage.action?cinemaId=25&date=15%2F08%2F2026&page=30007"
	getter := &fakeGetter{
		responses: map[string][]byte{SitemapURL: sitemap, staleURL: readFixture(t, "cinemas-directory.html"), validURL: readFixture(t, "cinema.html"), showingsURL: readFixture(t, "showings.html")},
		finalURLs: map[string]string{staleURL: "https://www.ugc.fr/cinemas.html?id=1"},
	}
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Through: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
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
			data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Through: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
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
	data, summary, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Through: "2026-08-15", CinemaID: "50", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
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
			_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Through: "2026-08-15", Now: time.Now()})
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
	data, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Through: "2026-08-15", CinemaID: "2", Now: time.Now()})
	if err == nil || err.Error() != "cinema 2 is inactive: redirected to UGC cinema directory" || len(data.Theaters) != 0 {
		t.Fatalf("data=%+v error=%v", data, err)
	}
}

func TestSyncFailsWhenAllSitemapCinemasAreStale(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=2</loc></url></urlset>`)
	staleURL := "https://www.ugc.fr/cinema.html?id=2"
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, staleURL: readFixture(t, "cinemas-directory.html")}, finalURLs: map[string]string{staleURL: "https://www.ugc.fr/cinemas.html?id=1"}}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Through: "2026-08-15", Now: time.Now()})
	if err == nil || err.Error() != "sync produced no active cinemas" {
		t.Fatalf("error=%v", err)
	}
}

func TestSyncFailsWithoutShowtimes(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=25</loc></url></urlset>`)
	cinemaURL := "https://www.ugc.fr/cinema.html?id=25"
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, cinemaURL: readFixture(t, "cinema.html")}}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-20", Through: "2026-08-20", Now: time.Now()})
	if err == nil || err.Error() != "sync produced no showtimes" {
		t.Fatalf("error=%v", err)
	}
}

func TestSyncRejectsUnexpectedCinemaFinalURL(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=2</loc></url></urlset>`)
	cinemaURL := "https://www.ugc.fr/cinema.html?id=2"
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, cinemaURL: readFixture(t, "cinemas-directory.html")}, finalURLs: map[string]string{cinemaURL: "https://www.ugc.fr/cinemas.html?id=1&unexpected=true"}}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Through: "2026-08-15", Now: time.Now()})
	if err == nil || err.Error() != "cinema 2 response has unexpected final URL" {
		t.Fatalf("error=%v", err)
	}
}

func TestSyncExpectedCinemaURLKeepsParseFailureTerminal(t *testing.T) {
	sitemap := []byte(`<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=2</loc></url></urlset>`)
	cinemaURL := "https://www.ugc.fr/cinema.html?id=2"
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: sitemap, cinemaURL: readFixture(t, "cinemas-directory.html")}}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Through: "2026-08-15", Now: time.Now()})
	if err == nil || err.Error() != "parse cinema 2: cinema identity missing or conflicting" {
		t.Fatalf("error=%v", err)
	}
}
func TestSyncStopsOnFailure(t *testing.T) {
	getter := &fakeGetter{responses: map[string][]byte{SitemapURL: readFixture(t, "sitemap.xml")}, failKind: "cinema 3"}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-08-15", Through: "2026-08-15", Now: time.Now()})
	if err == nil {
		t.Fatal("failure skipped")
	}
	if getter.calls != 2 {
		t.Fatalf("calls=%d", getter.calls)
	}
}

func TestSyncDateWindowUsesParisCalendarDays(t *testing.T) {
	getter := &fakeGetter{responses: map[string][]byte{}, failKind: "sitemap"}
	_, _, err := Sync(context.Background(), getter, SyncOptions{From: "2026-10-18", Through: "2026-10-31", Now: time.Now()})
	if err == nil || err.Error() != "synthetic transport failure" || getter.calls != 1 {
		t.Fatalf("error=%v calls=%d", err, getter.calls)
	}
	getter = &fakeGetter{responses: map[string][]byte{}}
	_, _, err = Sync(context.Background(), getter, SyncOptions{From: "2026-10-18", Through: "2026-11-01", Now: time.Now()})
	if err == nil || err.Error() != "invalid date window" || getter.calls != 0 {
		t.Fatalf("error=%v calls=%d", err, getter.calls)
	}
}

func TestCanAppendShowtimesBoundaries(t *testing.T) {
	if !canAppendShowtimes(schedule.MaxShowtimes-1, 1) {
		t.Fatal("maximum total should be accepted")
	}
	if canAppendShowtimes(schedule.MaxShowtimes, 1) || canAppendShowtimes(-1, 1) {
		t.Fatal("showtime total overflow accepted")
	}
}
