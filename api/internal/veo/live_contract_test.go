package veo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const diagnosticShowing = "G0699-cc55d99abaed49fe875ef9d3e1b5433f1152b829ab36e07421f2736c58a34703"

const pathDiagnosticShowing = "G0699-8b40dda8161481324ad1903685c0c4c30f55922a3b287a7f09c17b07ccc0f5df"

// This exhausted-grant replacement is deliberately direct: the mapping was
// already verified. No acquisition, enrichment retry, action fetch or Sync.
func TestProxyCheckoutPathStructureDiagnosticIntegration(t *testing.T) {
	path, enabled := os.LookupEnv("VEO_PATH_DIAGNOSTIC_PROXY_FILE")
	if !enabled {
		t.Skip("path-structure diagnostic opt-in required")
	}
	if path != "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt" || os.Getenv("VEO_PATH_DIAGNOSTIC_SHOWING_ID") != pathDiagnosticShowing || os.Getenv("VEO_PREFIX_DIAGNOSTIC_PROXY_FILE") != "" || os.Getenv("VEO_DIAGNOSTIC_PROXY_FILE") != "" || os.Getenv("VEO_LIVE_PROXY_FILE") != "" {
		t.Fatal("diagnostic configuration rejected before HTTP")
	}
	start, err := time.Parse(time.RFC3339, "2026-10-23T20:50:00+02:00")
	const source = "https://achat.veocinemas.fr/toulouse/r/8177"
	if err != nil || !start.After(time.Now()) || !schedule.ValidVeoBookingURL(source, "G0699", pathDiagnosticShowing) {
		t.Fatal("diagnostic identity expired or invalid before HTTP")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("proxy configuration unavailable")
	}
	defer func() { _ = f.Close() }()
	proxies, err := syncproxy.Parse(f)
	if err != nil || len(proxies) != 5 {
		t.Fatal("proxy configuration changed or invalid")
	}
	// A parsed entry is not a physical file line. The single production client
	// consequently reports local position 1/1, not historical full-pool 3/5.
	clients, err := syncproxy.NewHTTPClients(proxies[2:3], 20*time.Second, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		t.Fatal("proxy client unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 55*time.Second)
	defer cancel()
	ledger := &diagnosticLedger{maxTotal: 2, maxCheckout: 2, cancel: cancel}
	clients[0].Transport = diagnosticTransport{clients[0].Transport, ledger}
	c, err := newClient(clients, nil)
	if err != nil {
		t.Fatal("production client unavailable")
	}
	defer func() {
		ledger.mu.Lock()
		defer ledger.mu.Unlock()
		t.Logf("http_gets=%d acquisition_gets=%d checkout_gets=%d budget_blocked=%t executor_attempts=%d parsed_proxy_entry=3 parsed_proxy_count=5", ledger.total, ledger.acquisition, ledger.checkout, ledger.blocked, c.RequestCount())
	}()
	result, err := c.Checkout(ctx, source, start)
	if err != nil {
		t.Fatal("main_reason=client_or_budget_failure no_replacement=true")
	}
	index, count := result.pool.index, result.pool.count
	t.Logf("cinema_id=G0699 showing_id=%s client_local_position=%d client_local_pool_size=%d unavailable=%t canonical_present=%t", pathDiagnosticShowing, index, count, result.unavailableEnrichment, result.canonicalURL != "")
	if result.unavailableEnrichment {
		t.Fatal("main_reason=unavailable no_page_classification=true")
	}
	structure := classifyDiagnosticCheckoutPage(result.Body, result.canonicalURL)
	encoded, err := json.Marshal(structure)
	if err != nil {
		t.Fatal("diagnostic serialization failed")
	}
	t.Logf("value_free_structure=%s", encoded)
	facts, err := parseCheckoutForTheater(result.Body, result.canonicalURL, "veo-G0699")
	if err != nil {
		var rejected *CheckoutParseError
		if errors.As(err, &rejected) {
			t.Fatalf("main_reason=%s form_subreason=%s action_differences=%s historical_root_cause_unproven=true", rejected.Reason, rejected.FormSubreason(), diagnosticActionNames(rejected.ActionDifferences()))
		}
		t.Fatal("main_reason=parser_failure")
	}
	if _, err := exactEnd(start, facts); err != nil {
		t.Fatal("main_reason=end_resolution")
	}
	t.Logf("main_reason=accepted end_published=%t room_present=%t historical_response_unknown=true full_sync=false", facts.hasEnd, facts.room != "")
}

// This wrapper counts every production RoundTrip, including retries and hops,
// before sending it. It does not alter the production redirect/charset policy.
type diagnosticLedger struct {
	mu                           sync.Mutex
	total, acquisition, checkout int
	blocked                      bool
	maxTotal, maxCheckout        int
	cancel                       context.CancelFunc
}
type diagnosticTransport struct {
	base   http.RoundTripper
	ledger *diagnosticLedger
}

func (d diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	d.ledger.mu.Lock()
	checkout := r.URL.Host == "achat.veocinemas.fr"
	maxTotal, maxCheckout := d.ledger.maxTotal, d.ledger.maxCheckout
	if maxTotal == 0 {
		maxTotal, maxCheckout = 16, 8
	}
	if r.Method != http.MethodGet || d.ledger.total >= maxTotal || checkout && d.ledger.checkout >= maxCheckout {
		d.ledger.blocked = true
		d.ledger.mu.Unlock()
		if d.ledger.cancel != nil {
			d.ledger.cancel()
		}
		return nil, errors.New("diagnostic budget blocked")
	}
	d.ledger.total++
	if checkout {
		d.ledger.checkout++
	} else {
		d.ledger.acquisition++
	}
	d.ledger.mu.Unlock()
	return d.base.RoundTrip(r)
}

// Prefix diagnostic has a separate opt-in; the older exhausted probes must not
// be enabled alongside it. Production mapping, sort and checkout worker policy
// are retained; there is deliberately no Sync/publication call here.
func TestProxyConcurrentCheckoutPrefixDiagnosticIntegration(t *testing.T) {
	path, enabled := os.LookupEnv("VEO_PREFIX_DIAGNOSTIC_PROXY_FILE")
	if !enabled {
		t.Skip("concurrent-prefix diagnostic opt-in required")
	}
	if path != "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt" || os.Getenv("VEO_PREFIX_DIAGNOSTIC_SHOWING_ID") != diagnosticShowing || os.Getenv("VEO_DIAGNOSTIC_PROXY_FILE") != "" || os.Getenv("VEO_LIVE_PROXY_FILE") != "" {
		t.Fatal("diagnostic configuration rejected before HTTP")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("proxy configuration unavailable")
	}
	defer func() { _ = f.Close() }()
	proxies, err := syncproxy.Parse(f)
	if err != nil {
		t.Fatal("proxy configuration invalid")
	}
	clients, err := syncproxy.NewHTTPClients(proxies, 15*time.Second, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		t.Fatal("proxy clients unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 175*time.Second)
	defer cancel()
	ledger := &diagnosticLedger{maxTotal: 400, maxCheckout: 360, cancel: cancel}
	for _, client := range clients {
		client.Transport = diagnosticTransport{client.Transport, ledger}
	}
	c, err := newClient(clients, nil)
	if err != nil {
		t.Fatal("production client unavailable")
	}
	defer func() {
		ledger.mu.Lock()
		defer ledger.mu.Unlock()
		t.Logf("http_gets=%d acquisition_gets=%d checkout_gets=%d budget_blocked=%t executor_attempts=%d", ledger.total, ledger.acquisition, ledger.checkout, ledger.blocked, c.RequestCount())
	}()
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal("timezone unavailable")
	}
	now := time.Now()
	from := serviceDate(now.In(loc))
	get := func(op Operation, u string) []byte {
		t.Helper()
		body, err := c.Get(ctx, op, u)
		if err != nil {
			t.Fatal("main_reason=client_failure phase=acquisition")
		}
		return body
	}
	p, err := parseProgram(get(OperationProgram, programURL(MainOrigin, "G0699")), from, loc)
	if err != nil {
		t.Fatal("main_reason=source_changed phase=program")
	}
	_, through := programDates(p)
	if through == "" {
		t.Fatal("main_reason=source_changed target_found=false")
	}
	// parseSchedule requires the real per-movie records; no invented bookings or
	// identities. Only this cinema's advertised movie IDs are requested.
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
		if err != nil || len(parsed) != len(batch) {
			t.Fatal("main_reason=source_changed phase=movies")
		}
		for _, id := range batch {
			movie, ok := parsed[id]
			if !ok {
				t.Fatal("main_reason=source_changed phase=movies")
			}
			movies[id] = movie
		}
	}
	rows, _, err := parseSchedule(get(OperationSchedule, scheduleURL(MainOrigin, "G0699", from, through)), cinema{ID: "G0699"}, p, movies, loc, from, from, through)
	if err != nil {
		t.Fatal("main_reason=source_changed phase=schedule")
	}
	prefix, err := concurrentDiagnosticPrefix(rows, now)
	if err != nil {
		t.Fatal("main_reason=source_or_budget_blocked prefix_selected=false")
	}
	t.Logf("cinema_id=G0699 target_showing_id=%s prefix_sessions=%d worker_limit=%d client_pool_size=%d", diagnosticShowing, len(prefix), CheckoutWorkerCount, len(clients))
	summary := SyncSummary{}
	if err := enrichCheckouts(ctx, c, prefix, &summary, now); err != nil {
		var rejected *CheckoutParseError
		if errors.As(err, &rejected) && rejected.CinemaID == "G0699" && schedule.ValidVeoIdentity("showing", rejected.ShowingID) && strings.HasPrefix(rejected.ShowingID, "G0699-") {
			index, count := rejected.ClientPoolPosition()
			t.Fatalf("main_reason=%s form_subreason=%s action_difference_bits=%d action_differences=%s client_pool_position=%d client_pool_size=%d cinema_id=%s showing_id=%s", rejected.Reason, rejected.FormSubreason(), rejected.ActionDifferences(), diagnosticActionNames(rejected.ActionDifferences()), index, count, rejected.CinemaID, rejected.ShowingID)
		}
		t.Fatal("main_reason=client_or_context_failure")
	}
	t.Logf("main_reason=accepted prefix_sessions=%d checkouts=%d exact_ends=%d historical_root_reproduced=false full_sync=false", len(prefix), summary.Checkouts, summary.ExactEnds)
}

func concurrentDiagnosticPrefix(rows []schedule.ShowtimeRecord, now time.Time) ([]schedule.ShowtimeRecord, error) {
	future := []schedule.ShowtimeRecord{}
	seen := map[string]bool{}
	for _, row := range rows {
		if row.TheaterID != "veo-G0699" || !schedule.ValidVeoIdentity("showing", row.ProviderShowingID) || !strings.HasPrefix(row.ProviderShowingID, "G0699-") || seen[row.ProviderShowingID] {
			return nil, errShape
		}
		seen[row.ProviderShowingID] = true
		if row.StartTime.After(now) {
			future = append(future, row)
		}
	}
	// Exact acquire() ordering for this single cinema, not the live sample helper.
	sort.Slice(future, func(i, j int) bool {
		if !future[i].StartTime.Equal(future[j].StartTime) {
			return future[i].StartTime.Before(future[j].StartTime)
		}
		return future[i].ID < future[j].ID
	})
	for i, row := range future {
		if row.ProviderShowingID == diagnosticShowing {
			expected, _ := time.Parse(time.RFC3339, "2026-10-23T17:30:00+02:00")
			if i >= 180 || !row.StartTime.Equal(expected) || row.BookingURL != "https://achat.veocinemas.fr/toulouse/r/8170" {
				return nil, errShape
			}
			return future[:i+1], nil
		}
	}
	return nil, errShape
}

func diagnosticActionNames(bits uint32) string {
	names := [...]string{"scheme", "authority", "path_shape", "slug", "film", "epoch", "version", "tail", "trailing_slash", "query", "fragment", "relative", "whitespace", "escaping", "userinfo", "unparseable", "representation"}
	if bits == 0 || bits&^uint32((1<<len(names))-1) != 0 {
		return "none"
	}
	selected := []string{}
	for i, name := range names {
		if bits&(1<<i) != 0 {
			selected = append(selected, name)
		}
	}
	return strings.Join(selected, ",")
}

func TestDiagnosticHTTPBudget(t *testing.T) {
	for _, checkout := range []bool{false, true} {
		ledger := &diagnosticLedger{}
		sent := 0
		d := diagnosticTransport{roundTripFunc(func(*http.Request) (*http.Response, error) {
			sent++
			return nil, errors.New("synthetic transport failure")
		}), ledger}
		u := MainOrigin + "/api/gatsby-source-boxofficeapi/scheduledMovies?theaterId=G0699"
		limit := 16
		if checkout {
			u = "https://achat.veocinemas.fr/toulouse/r/8170"
			limit = 8
		}
		r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
		if err != nil {
			t.Fatal(err)
		}
		for range limit + 4 {
			response, _ := d.RoundTrip(r)
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
		}
		if sent != limit || ledger.total != limit || !ledger.blocked {
			t.Fatal("GET cap not enforced")
		}
	}
}

func TestConcurrentDiagnosticHTTPBudget(t *testing.T) {
	for _, checkout := range []bool{false, true} {
		ledger := &diagnosticLedger{maxTotal: 400, maxCheckout: 360}
		var sent atomic.Int64
		d := diagnosticTransport{roundTripFunc(func(*http.Request) (*http.Response, error) {
			sent.Add(1)
			return nil, errors.New("synthetic transport failure")
		}), ledger}
		u := MainOrigin + "/api/gatsby-source-boxofficeapi/scheduledMovies?theaterId=G0699"
		limit := 400
		if checkout {
			u, limit = "https://achat.veocinemas.fr/toulouse/r/8170", 360
		}
		var group sync.WaitGroup
		for range 16 {
			group.Go(func() {
				for range 30 {
					r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
					if err != nil {
						t.Error("synthetic request invalid")
						return
					}
					response, _ := d.RoundTrip(r)
					if response != nil && response.Body != nil {
						_ = response.Body.Close()
					}
				}
			})
		}
		group.Wait()
		if int(sent.Load()) != limit || ledger.total != limit || !ledger.blocked {
			t.Fatal("concurrent before-send budget exceeded")
		}
	}
}

func TestConcurrentDiagnosticPrefixBoundaries(t *testing.T) {
	start, _ := time.Parse(time.RFC3339, "2026-10-23T17:30:00+02:00")
	now := start.Add(-24 * time.Hour)
	row := func(raw string, at time.Time) schedule.ShowtimeRecord {
		id := schedule.VeoShowingID("G0699", raw)
		return schedule.ShowtimeRecord{TheaterID: "veo-G0699", ID: "veo-showing-" + id, ProviderShowingID: id, StartTime: at}
	}
	target := row("unused", start)
	target.ProviderShowingID, target.ID, target.BookingURL = diagnosticShowing, "veo-showing-"+diagnosticShowing, "https://achat.veocinemas.fr/toulouse/r/8170"
	rows := []schedule.ShowtimeRecord{row("after", start.Add(time.Second)), target, row("boundary", now)}
	for i := range 179 {
		rows = append(rows, row(fmt.Sprint(i), start.Add(-time.Duration(i+1)*time.Minute)))
	}
	prefix, err := concurrentDiagnosticPrefix(rows, now)
	if err != nil || len(prefix) != 180 || prefix[len(prefix)-1].ProviderShowingID != diagnosticShowing || rows[0].StartTime != start.Add(time.Second) {
		t.Fatal("prefix order/cutoff/ending/mutation wrong")
	}
	if _, err := concurrentDiagnosticPrefix(append(rows, row("overbudget", start.Add(-200*time.Minute))), now); err == nil {
		t.Fatal("prefix over 180 permitted")
	}
	if _, err := concurrentDiagnosticPrefix(rows, start); err == nil {
		t.Fatal("started/missing target substituted")
	}
	if _, err := concurrentDiagnosticPrefix(append(rows, target), now); err == nil {
		t.Fatal("duplicate identity permitted")
	}
	bad := target
	bad.TheaterID = "veo-P3863"
	if _, err := concurrentDiagnosticPrefix([]schedule.ShowtimeRecord{bad}, now); err == nil {
		t.Fatal("wrong cinema permitted")
	}
}

// Opt in ONLY with this exact -run selection. Never a full Sync or replacement
// sample: a changed/missing target is unreproducible, not permission to crawl.
func TestProxyExactCheckoutDiagnosticIntegration(t *testing.T) {
	path, enabled := os.LookupEnv("VEO_DIAGNOSTIC_PROXY_FILE")
	if !enabled {
		t.Skip("exact-target diagnostic opt-in required")
	}
	target := os.Getenv("VEO_DIAGNOSTIC_SHOWING_ID")
	if path != "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt" || target != diagnosticShowing || !schedule.ValidVeoIdentity("showing", target) {
		t.Fatal("diagnostic configuration rejected before HTTP")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("proxy configuration unavailable")
	}
	defer func() { _ = f.Close() }()
	proxies, err := syncproxy.Parse(f)
	if err != nil {
		t.Fatal("proxy configuration invalid")
	}
	clients, err := syncproxy.NewHTTPClients(proxies, 15*time.Second, syncproxy.NewRedirectChecker(allowedURL))
	if err != nil {
		t.Fatal("proxy clients unavailable")
	}
	ledger := &diagnosticLedger{}
	for _, c := range clients {
		c.Transport = diagnosticTransport{c.Transport, ledger}
	}
	c, err := newClient(clients, nil)
	if err != nil {
		t.Fatal("production client unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 115*time.Second)
	defer cancel()
	defer func() {
		ledger.mu.Lock()
		defer ledger.mu.Unlock()
		t.Logf("http_gets=%d acquisition_gets=%d checkout_gets=%d budget_blocked=%t executor_attempts=%d", ledger.total, ledger.acquisition, ledger.checkout, ledger.blocked, c.RequestCount())
	}()
	t.Logf("cinema_id=G0699 showing_id=%s", target)
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal("timezone unavailable")
	}
	now := time.Now()
	from := serviceDate(now.In(loc))
	programBody, err := c.Get(ctx, OperationProgram, programURL(MainOrigin, "G0699"))
	if err != nil {
		t.Fatal("main_reason=client_failure phase=program")
	}
	p, err := parseProgram(programBody, from, loc)
	if err != nil {
		t.Fatal("main_reason=source_changed phase=program")
	}
	_, through := programDates(p)
	if through == "" {
		t.Fatal("main_reason=source_changed target_found=false")
	}
	b, err := c.Get(ctx, OperationSchedule, scheduleURL(MainOrigin, "G0699", from, through))
	if err != nil {
		t.Fatal("main_reason=client_failure phase=schedule")
	}
	var response scheduleResponse
	if decodeJSON(b, &response) != nil || len(response) != 1 || response["G0699"].Schedule == nil {
		t.Fatal("main_reason=source_changed phase=schedule")
	}
	var row schedule.ShowtimeRecord
	found := 0
	for movie, days := range response["G0699"].Schedule {
		for date, sessions := range days {
			for _, s := range sessions {
				if schedule.VeoShowingID("G0699", s.ID) != target {
					continue
				}
				found++
				if !contains(p[movie], date) {
					t.Fatal("main_reason=source_changed advertised=false")
				}
				row, err = parseShowtime(s, cinema{ID: "G0699"}, schedule.MovieRecord{}, date, loc)
				if err != nil {
					t.Fatal("main_reason=source_changed target_valid=false")
				}
			}
		}
	}
	expectedStart, _ := time.Parse(time.RFC3339, "2026-10-23T17:30:00+02:00")
	if found != 1 || !row.StartTime.Equal(expectedStart) || !row.StartTime.After(now) || row.BookingURL != "https://achat.veocinemas.fr/toulouse/r/8170" {
		t.Fatal("main_reason=source_changed exact_target=false")
	}
	result, err := c.Checkout(ctx, row.BookingURL, row.StartTime)
	if err != nil {
		t.Fatal("main_reason=client_failure phase=checkout")
	}
	u, _ := url.Parse(result.canonicalURL)
	t.Logf("target_found=true target_future=true canonical_present=%t canonical_bound=%t unavailable=%t", result.canonicalURL != "", boundCheckoutURL(u, checkoutBinding{source: row.BookingURL, start: row.StartTime}), result.unavailableEnrichment)
	logDiagnosticFormEquality(t, result)
	if result.unavailableEnrichment {
		t.Fatal("main_reason=source_changed unavailable=true")
	}
	facts, err := parseCheckoutForTheater(result.Body, result.canonicalURL, row.TheaterID)
	if err != nil {
		var rejected *CheckoutParseError
		if errors.As(err, &rejected) {
			t.Fatalf("main_reason=%s form_subreason=%s", rejected.Reason, rejected.FormSubreason())
		}
		t.Fatal("main_reason=parser_failure")
	}
	if _, err := exactEnd(row.StartTime, facts); err != nil {
		t.Fatal("main_reason=end_resolution")
	}
	t.Logf("main_reason=accepted form_subreason=none published_end=%t room_present=%t", facts.hasEnd, facts.room != "")
}

func logDiagnosticFormEquality(t *testing.T, result CheckoutResult) {
	t.Helper()
	root, err := xhtml.Parse(bytes.NewReader(result.Body))
	if err != nil {
		t.Log("html_parse_ok=false")
		return
	}
	forms, actions, methods := 0, 0, 0
	action, method := "", ""
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "form" {
			forms++
			for _, a := range n.Attr {
				if a.Key == "action" {
					actions++
					action = a.Val
				}
				if a.Key == "method" {
					methods++
					method = a.Val
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	u, err := url.Parse(action)
	canonical, canonicalErr := url.Parse(result.canonicalURL)
	comparable := actions == 1 && err == nil && canonicalErr == nil && result.canonicalURL != ""
	host, slug, film, epoch, version, tail := false, false, false, false, false, false
	if comparable {
		host = u.Scheme == canonical.Scheme && u.Host == canonical.Host
		a, c := strings.Split(u.Path, "/"), strings.Split(canonical.Path, "/")
		if len(a) == 8 && len(c) == 8 {
			slug, film, epoch, version, tail = a[1] == c[1], a[3] == c[3], a[4] == c[4], a[5] == c[5], a[6] == c[6]
		}
	}
	t.Logf("dom_forms=%d action_attributes=%d method_attributes=%d method_post=%t action_exact=%t action_host_equal=%t action_slug_equal=%t action_film_equal=%t action_epoch_equal=%t action_version_equal=%t action_tail_equal=%t", forms, actions, methods, methods == 1 && strings.EqualFold(method, "post"), actions == 1 && action == result.canonicalURL, host, slug, film, epoch, version, tail)
}

// Opt-in read-only sample: never invokes Sync or publication, never a chain
// checkout crawl. Logical calls (not retry attempts) are separately capped.
func TestProxyScheduleContractIntegration(t *testing.T) {
	path, ok := os.LookupEnv("VEO_LIVE_PROXY_FILE")
	if !ok {
		t.Skip("set VEO_LIVE_PROXY_FILE for bounded proxy contract")
	}
	if path != "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt" {
		t.Fatal("live contract requires approved proxy path")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("approved proxy file unavailable")
	}
	defer func() { _ = f.Close() }()
	proxies, err := syncproxy.Parse(f)
	if err != nil {
		t.Fatal("invalid proxy configuration")
	}
	c, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 290*time.Second)
	defer cancel()
	now := time.Now()
	logical, checkoutPages := 0, 0
	defer func() {
		t.Logf("logical_requests=%d executor_attempts=%d checkout_pages=%d", logical, c.RequestCount(), checkoutPages)
	}()
	get := func(op Operation, u string) []byte {
		t.Helper()
		logical++
		if logical > 16 {
			t.Fatal("logical request budget exceeded")
		}
		b, err := c.Get(ctx, op, u)
		if err != nil {
			t.Fatalf("source blocked: %v", err)
		}
		return b
	}
	loc, _ := time.LoadLocation(schedule.Timezone)
	from := serviceDate(now.In(loc))
	type checkoutSample struct {
		cinema string
		rows   []schedule.ShowtimeRecord
	}
	samples := []checkoutSample{}
	for _, sample := range []struct{ origin, id string }{{MainOrigin, "W3351"}, {SeteOrigin, "W0067"}} {
		cinemas, err := parseCinemas(get(OperationCinemas, sample.origin+CatalogPath), sample.origin)
		if err != nil {
			t.Fatal("source blocked: catalog structure")
		}
		var cinema cinema
		for _, item := range cinemas {
			if item.ID == sample.id {
				cinema = item
			}
		}
		if cinema.ID == "" {
			t.Fatal("source blocked: representative cinema absent")
		}
		p, err := parseProgram(get(OperationProgram, programURL(sample.origin, cinema.ID)), from, loc)
		if err != nil {
			t.Fatal("source blocked: advertised index structure/consistency")
		}
		_, through := programDates(p)
		if through == "" {
			t.Fatal("source blocked: representative index empty; no fallback horizon")
		}
		ids := []string{}
		for id, dates := range p {
			if len(dates) > 0 {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		if len(batchMovieIDs(sample.origin, ids)) != 1 {
			t.Fatal("source blocked: sample exceeds single bounded metadata batch")
		}
		movies, err := parseMovies(get(OperationMovies, moviesURL(sample.origin, ids)))
		if err != nil {
			t.Fatal("source blocked: movie structure")
		}
		rows, _, err := parseSchedule(get(OperationSchedule, scheduleURL(sample.origin, cinema.ID, from, through)), cinema, p, movies, loc, from, from, through)
		if err != nil {
			t.Fatalf("source blocked: schedule consistency (%v)", err)
		}
		if len(rows) == 0 {
			t.Fatal("source blocked: no representative sessions")
		}
		selected := futureCheckoutSamples(rows, now, 2)
		if len(selected) == 0 {
			t.Fatal("source blocked: no future representative checkout")
		}
		samples = append(samples, checkoutSample{sample.id, selected})
		t.Logf("circuit=%s catalog=%d movies=%d sessions=%d through=%s future_checkout_samples=%d", sample.id, len(cinemas), len(movies), len(rows), through, len(selected))
	}
	_, rows, err := parseCastelia(get(OperationCastelia, ProgramURL), from, loc)
	if err != nil {
		t.Fatal("source blocked: Castelia structure")
	}
	t.Logf("castelia_sessions=%d", len(rows))
	// Acquire all three surfaces before visiting any checkout, so a failed page
	// cannot hide circuit or Castelia acquisition evidence.
	for _, sample := range samples {
		exact := 0
		knownRuntime := 0
		missingRoom := 0
		for _, row := range sample.rows {
			if row.Movie.RuntimeMinutes > 0 {
				knownRuntime++
			}
			logical++
			checkoutPages++
			if logical > 16 || checkoutPages > 4 {
				t.Fatal("checkout request budget exceeded")
			}
			result, err := c.Checkout(ctx, row.BookingURL, row.StartTime)
			if err != nil {
				t.Fatalf("source blocked: %v", err)
			}
			facts, err := parseCheckout(result.Body, result.canonicalURL)
			if err != nil {
				t.Fatal("source blocked: checkout structure")
			}
			if _, err := exactEnd(row.StartTime, facts); err != nil {
				t.Fatal("source blocked: checkout end ambiguity")
			}
			if facts.hasEnd {
				exact++
			}
			if row.Room == "" && facts.room == "" {
				missingRoom++
			}
		}
		t.Logf("circuit=%s checkout_pages=%d known_runtime=%d exact_ends=%d source_gap_unpublished_end=%d source_gap_missing_room=%d", sample.cinema, len(sample.rows), knownRuntime, exact, len(sample.rows)-exact, missingRoom)
	}
}

// Prefer known-runtime near-term films to exercise published end clocks, but do
// not invent a clock when the valid future booking page does not publish one.
func futureCheckoutSamples(rows []schedule.ShowtimeRecord, now time.Time, limit int) []schedule.ShowtimeRecord {
	future := make([]schedule.ShowtimeRecord, 0, len(rows))
	for _, row := range rows {
		if row.StartTime.After(now) {
			future = append(future, row)
		}
	}
	sort.Slice(future, func(i, j int) bool {
		a, b := future[i], future[j]
		if (a.Movie.RuntimeMinutes > 0) != (b.Movie.RuntimeMinutes > 0) {
			return a.Movie.RuntimeMinutes > 0
		}
		if !a.StartTime.Equal(b.StartTime) {
			return a.StartTime.Before(b.StartTime)
		}
		return a.ID < b.ID
	})
	selected := []schedule.ShowtimeRecord{}
	seen := map[string]bool{}
	for _, row := range future {
		if len(selected) >= limit {
			break
		}
		if !seen[row.BookingURL] {
			selected = append(selected, row)
			seen[row.BookingURL] = true
		}
	}
	return selected
}

func TestFutureCheckoutSampleCutoffAndPreference(t *testing.T) {
	now := fixtureOptions().Now
	rows := []schedule.ShowtimeRecord{
		{ID: "past", BookingURL: "past", StartTime: now.Add(-time.Second)},
		{ID: "boundary", BookingURL: "boundary", StartTime: now},
		{ID: "unknown", BookingURL: "unknown", StartTime: now.Add(time.Second)},
		{ID: "later", BookingURL: "later", StartTime: now.Add(48 * time.Hour), Movie: schedule.MovieRecord{RuntimeMinutes: 120}},
		{ID: "near", BookingURL: "near", StartTime: now.Add(time.Hour), Movie: schedule.MovieRecord{RuntimeMinutes: 90}},
		{ID: "duplicate", BookingURL: "near", StartTime: now.Add(2 * time.Hour), Movie: schedule.MovieRecord{RuntimeMinutes: 90}},
	}
	selected := futureCheckoutSamples(rows, now, 2)
	if len(selected) != 2 || selected[0].ID != "near" || selected[1].ID != "later" || rows[0].ID != "past" {
		t.Fatal("wrong sample, cutoff, or mutation", selected)
	}
	selected = futureCheckoutSamples(rows[:3], now, 2)
	if len(selected) != 1 || selected[0].ID != "unknown" {
		t.Fatal("started rows selected or unknown runtime discarded", selected)
	}
}
