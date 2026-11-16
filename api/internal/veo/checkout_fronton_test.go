package veo

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestFrontonEntryDiscardsLiteralRoomAndEnd(t *testing.T) {
	row := retryTestRow(906)
	row.TheaterID = "veo-P3863"
	row.ProviderShowingID = schedule.VeoShowingID("P3863", "906")
	row.ID = "veo-showing-" + row.ProviderShowingID
	row.BookingURL = strings.ReplaceAll(row.BookingURL, "toulouse", "fronton")
	page := toulouseEntryPage(row.StartTime)
	page.canonicalURL = strings.ReplaceAll(page.canonicalURL, "toulouse", "fronton")
	page.Body = []byte(strings.ReplaceAll(strings.ReplaceAll(string(page.Body), "toulouse", "fronton"), "salle-8", "cine-fronton"))
	facts, err := resolveCheckout(row, page, false)
	if err != nil || !facts.entryUnavailable || !facts.end.Equal(row.StartTime) || facts.hasEnd || facts.room != "" {
		t.Fatal("general Fronton page contributed session facts", err)
	}
}

// Read-only programme plus two future checkouts; no publication or raw logging.
func TestProxyFrontonEntryContractIntegration(t *testing.T) {
	path := os.Getenv("VEO_FRONTON_LIVE_PROXY_FILE")
	if path == "" {
		t.Skip("VEO_FRONTON_LIVE_PROXY_FILE not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("proxy file unavailable")
	}
	proxies, parseErr := syncproxy.Parse(f)
	closeErr := f.Close()
	if parseErr != nil || closeErr != nil {
		t.Fatal("proxy file invalid")
	}
	c, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal("client unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	get := func(op Operation, raw string) []byte {
		t.Helper()
		b, err := c.Get(ctx, op, raw)
		if err != nil {
			t.Fatal("source request failed")
		}
		return b
	}
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	from := serviceDate(now.In(loc))
	p, err := parseProgram(get(OperationProgram, programURL(MainOrigin, "P3863")), from, loc)
	if err != nil {
		t.Fatal("programme rejected")
	}
	_, through := programDates(p)
	if through == "" {
		t.Fatal("no advertised dates")
	}
	ids := []string{}
	for id, dates := range p {
		if len(dates) > 0 {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	movies := map[string]schedule.MovieRecord{}
	for _, batch := range batchMovieIDs(MainOrigin, ids) {
		parsed, err := parseMovies(get(OperationMovies, moviesURL(MainOrigin, batch)))
		if err != nil {
			t.Fatal("movie metadata rejected")
		}
		for id, movie := range parsed {
			movies[id] = movie
		}
	}
	rows, _, err := parseSchedule(get(OperationSchedule, scheduleURL(MainOrigin, "P3863", from, through)), cinema{ID: "P3863"}, p, movies, loc, from, from, through)
	if err != nil {
		t.Fatal("sessions rejected")
	}
	selected := futureCheckoutSamples(rows, now, 2)
	if len(selected) == 0 {
		t.Fatal("no future checkout sample")
	}
	for _, row := range selected {
		response, err := c.Checkout(ctx, row.BookingURL, row.StartTime)
		if err != nil {
			t.Fatal("checkout request failed")
		}
		facts, err := resolveCheckout(row, response, false)
		if err != nil {
			t.Fatal("checkout rejected")
		}
		if facts.entryUnavailable && (!facts.end.Equal(row.StartTime) || facts.hasEnd || facts.room != "") {
			t.Fatal("general entry retained session facts")
		}
		t.Logf("cinema=P3863 showing=%s entry_unavailable=%t exact_end=%t", row.ProviderShowingID, facts.entryUnavailable, facts.hasEnd)
	}
	t.Logf("sessions=%d checkout_samples=%d requests=%d", len(rows), len(selected), c.RequestCount())
}
