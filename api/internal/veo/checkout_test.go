package veo

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

type retryCheckoutGetter struct {
	Getter
	mu    sync.Mutex
	calls map[string]int
	fetch func(context.Context, string, time.Time, int) (CheckoutResult, error)
}

func (g *retryCheckoutGetter) Checkout(ctx context.Context, raw string, start time.Time) (CheckoutResult, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]int)
	}
	g.calls[raw]++
	n := g.calls[raw]
	g.mu.Unlock()
	return g.fetch(ctx, raw, start, n)
}

func (g *retryCheckoutGetter) RequestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, count := range g.calls {
		n += count
	}
	if g.Getter != nil {
		n += g.Getter.RequestCount()
	}
	return n
}

func retryTestRow(i int) schedule.ShowtimeRecord {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 10, 23, 17, 30, 0, 0, loc)
	id := schedule.VeoShowingID("G0699", strconv.Itoa(i))
	return schedule.ShowtimeRecord{TheaterID: "veo-G0699", ProviderShowingID: id, ID: "veo-showing-" + id, StartTime: start, EndTime: start, BookingURL: fmt.Sprintf("https://achat.veocinemas.fr/toulouse/r/%d", i+1)}
}

func retryTestPage(start time.Time, mismatch bool) CheckoutResult {
	canonical := fmt.Sprintf("https://achat.veocinemas.fr/toulouse/reserver/F123/D%d/VF/81/", start.Unix())
	action := canonical
	if mismatch {
		action = strings.Replace(action, "F123", "F124", 1)
	}
	return CheckoutResult{Body: []byte(bookingFormFixture(action) + `<div id=s_fin>fin prévue à 20h15</div>`), canonicalURL: canonical, pool: clientPoolPosition{2, 5}}
}

func TestCheckoutValidatedRetryRecovery(t *testing.T) {
	for _, room := range []string{"", "API room"} {
		t.Run(room, func(t *testing.T) {
			row := retryTestRow(1)
			row.Room = room
			g := &retryCheckoutGetter{fetch: func(_ context.Context, raw string, start time.Time, n int) (CheckoutResult, error) {
				if raw != row.BookingURL || !start.Equal(row.StartTime) || n > 2 {
					t.Error("retry changed source/start or exceeded one call")
				}
				page := retryTestPage(start, n == 1)
				if n == 1 {
					page.Body = []byte(strings.ReplaceAll(string(page.Body), "20h15", "21h59"))
				} else {
					page.Body = append(page.Body, []byte(`{"auditorium_showtime":"salle-8"}`)...)
				}
				return page, nil
			}}
			rows, summary := []schedule.ShowtimeRecord{row}, SyncSummary{}
			if err := enrichCheckouts(t.Context(), g, rows, &summary, row.StartTime.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			wantRoom := room
			if wantRoom == "" {
				wantRoom = "Salle 8"
			}
			if g.RequestCount() != 2 || summary.CheckoutRetryAttempts != 1 || summary.CheckoutRecoveries != 1 || !summary.FirstCheckoutRetryRecovered || summary.ExactEnds != 1 || rows[0].EndTime.Format("15:04") != "20:15" || rows[0].Room != wantRoom || rows[0].BookingURL != row.BookingURL || summary.FirstCheckoutRetry.actionDiff != actionFilm || summary.FirstCheckoutRetry.ShowingID != row.ProviderShowingID || summary.FirstCheckoutRetry.pool != (clientPoolPosition{2, 5}) {
				t.Fatal("retry facts/diagnostics were not isolated")
			}
		})
	}
}

func TestCheckoutRetryRequiresSoleFailureAndStrictSecondPage(t *testing.T) {
	row := retryTestRow(2)
	first := retryTestPage(row.StartTime, true)
	valid := retryTestPage(row.StartTime, false)
	for _, tc := range []struct {
		name                string
		first, second       CheckoutResult
		firstErr, secondErr error
		calls               int
	}{
		{name: "login plus mismatch", first: CheckoutResult{Body: append(append([]byte{}, first.Body...), []byte(`<input type=password>`)...), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "method plus mismatch", first: CheckoutResult{Body: []byte(strings.ReplaceAll(string(first.Body), `method="POST"`, `method="GET"`)), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "missing method plus mismatch", first: CheckoutResult{Body: []byte(strings.ReplaceAll(string(first.Body), `method="POST"`, "")), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "missing action", first: CheckoutResult{Body: []byte(`<form method=POST></form>`), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "duplicate action", first: CheckoutResult{Body: []byte(strings.Replace(string(first.Body), `action="`, `action="duplicate" action="`, 1)), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "duplicate boolean action", first: CheckoutResult{Body: []byte(strings.Replace(string(first.Body), `<form `, `<form action `, 1)), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "adjacent duplicate action", first: CheckoutResult{Body: []byte(strings.Replace(string(first.Body), `action="`, `action="duplicate"action="`, 1)), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "duplicate method", first: CheckoutResult{Body: []byte(strings.Replace(string(first.Body), `method="POST"`, `method="POST" method="GET"`, 1)), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "nested forms", first: CheckoutResult{Body: append(append([]byte{}, first.Body...), []byte(`<form></form>`)...), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "template", first: CheckoutResult{Body: []byte(`<template>` + string(first.Body) + `</template>`), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "canonical absent", first: CheckoutResult{Body: first.Body}, calls: 1},
		{name: "canonical invalid", first: CheckoutResult{Body: first.Body, canonicalURL: first.canonicalURL + "?"}, calls: 1},
		{name: "room invalid", first: CheckoutResult{Body: append(append([]byte{}, first.Body...), []byte(`{"auditorium_showtime":"other"}`)...), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "clock invalid", first: CheckoutResult{Body: []byte(strings.ReplaceAll(string(first.Body), "20h15", "29h15")), canonicalURL: first.canonicalURL}, calls: 1},
		{name: "getter forged eligibility", firstErr: &CheckoutParseError{Reason: RejectBookingForm, formReason: formActionMismatch, actionOnly: true, CinemaID: "G0699", ShowingID: row.ProviderShowingID, Cause: errShape}, calls: 1},
		{name: "getter transport", firstErr: errors.New("synthetic transport"), calls: 1},
		{name: "first challenge", firstErr: &RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureChallenge, StatusCode: 403}, calls: 1},
		{name: "second mismatch", first: first, second: first, calls: 2},
		{name: "second numeric wrong action", first: first, second: CheckoutResult{Body: append(append([]byte{}, first.Body...), []byte(`{"auditorium_showtime":"salle-2"}`)...), canonicalURL: first.canonicalURL}, calls: 2},
		{name: "second null no form", first: first, second: CheckoutResult{Body: []byte(`{"auditorium_showtime":null}`), canonicalURL: first.canonicalURL}, calls: 2},
		{name: "second unavailable", first: first, second: CheckoutResult{unavailableEnrichment: true}, calls: 2},
		{name: "second network", first: first, secondErr: errors.New("synthetic second transport"), calls: 2},
		{name: "second challenge", first: first, secondErr: &RequestError{Operation: OperationCheckout, Kind: syncproxy.FailureChallenge, StatusCode: 429}, calls: 2},
		{name: "second canceled", first: first, secondErr: context.Canceled, calls: 2},
		{name: "second malformed clock", first: first, second: CheckoutResult{Body: []byte(strings.ReplaceAll(string(valid.Body), "20h15", "29h15")), canonicalURL: valid.canonicalURL}, calls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &retryCheckoutGetter{fetch: func(_ context.Context, _ string, _ time.Time, n int) (CheckoutResult, error) {
				if n == 1 {
					return tc.first, tc.firstErr
				}
				return tc.second, tc.secondErr
			}}
			rows, summary := []schedule.ShowtimeRecord{row}, SyncSummary{}
			err := enrichCheckouts(t.Context(), g, rows, &summary, row.StartTime.Add(-time.Hour))
			if err == nil || g.RequestCount() != tc.calls || summary.CheckoutRetryAttempts != tc.calls-1 || summary.CheckoutRecoveries != 0 || !rows[0].EndTime.Equal(row.StartTime) || rows[0].Room != "" {
				t.Fatal("unexpected retry or partial facts", g.RequestCount(), err)
			}
			if tc.secondErr != nil && !errors.Is(err, tc.secondErr) {
				t.Fatal("final cause replaced", err)
			}
		})
	}
}

func TestCheckoutRetryUnknownMetadataCancellationAndEndResolution(t *testing.T) {
	row := retryTestRow(61)
	for _, marker := range []string{"", `{"auditorium_showtime":null}`, `{"auditorium_showtime":"salle-3"}`} {
		g := &retryCheckoutGetter{fetch: func(_ context.Context, _ string, start time.Time, n int) (CheckoutResult, error) {
			p := retryTestPage(start, n == 1)
			if n == 2 {
				p.Body = []byte(bookingFormFixture(p.canonicalURL) + marker)
			}
			return p, nil
		}}
		rows, s := []schedule.ShowtimeRecord{row}, SyncSummary{}
		if err := enrichCheckouts(t.Context(), g, rows, &s, row.StartTime.Add(-time.Hour)); err != nil || s.CheckoutRecoveries != 1 || s.ExactEnds != 0 || !rows[0].EndTime.Equal(row.StartTime) {
			t.Fatal("valid unpublished metadata", err)
		}
	}
	for _, second := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		g := &retryCheckoutGetter{fetch: func(_ context.Context, _ string, start time.Time, n int) (CheckoutResult, error) {
			if n == 2 || !second {
				cancel()
			}
			return retryTestPage(start, n == 1), nil
		}}
		if err := enrichCheckouts(ctx, g, []schedule.ShowtimeRecord{row}, &SyncSummary{}, row.StartTime.Add(-time.Hour)); !errors.Is(err, context.Canceled) || g.RequestCount() != 1+btoi(second) {
			t.Fatal("cancellation retry", err)
		}
	}
	loc, _ := time.LoadLocation(schedule.Timezone)
	row.StartTime = time.Date(2026, 10, 24, 23, 30, 0, 0, loc)
	row.EndTime = row.StartTime
	g := &retryCheckoutGetter{fetch: func(_ context.Context, _ string, start time.Time, _ int) (CheckoutResult, error) {
		p := retryTestPage(start, true)
		p.Body = []byte(strings.ReplaceAll(string(p.Body), "20h15", "2h30"))
		return p, nil
	}}
	if err := enrichCheckouts(t.Context(), g, []schedule.ShowtimeRecord{row}, &SyncSummary{}, row.StartTime.Add(-time.Hour)); err == nil || g.RequestCount() != 1 {
		t.Fatal("ambiguous first end retried", err)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestCheckoutRetryPreservesFirstFinalCallbackFailure(t *testing.T) {
	rows := []schedule.ShowtimeRecord{retryTestRow(70), retryTestRow(71)}
	started := make(chan struct{})
	g := &retryCheckoutGetter{fetch: func(ctx context.Context, raw string, start time.Time, n int) (CheckoutResult, error) {
		if raw == rows[0].BookingURL {
			close(started)
			<-ctx.Done()
			return CheckoutResult{}, ctx.Err()
		}
		select {
		case <-started:
		case <-ctx.Done():
			return CheckoutResult{}, ctx.Err()
		}
		p := retryTestPage(start, n == 1)
		if n == 2 {
			p.Body = []byte(strings.ReplaceAll(string(p.Body), "20h15", "29h15"))
			p.pool = clientPoolPosition{3, 5}
		}
		return p, nil
	}}
	s := SyncSummary{}
	err := enrichCheckouts(t.Context(), g, rows, &s, rows[0].StartTime.Add(-time.Hour))
	var rejected *CheckoutParseError
	if !errors.As(err, &rejected) || rejected.Reason != RejectClock || rejected.ShowingID != rows[1].ProviderShowingID || rejected.pool != (clientPoolPosition{3, 5}) || errors.Is(err, context.Canceled) || s.CheckoutRetryAttempts != 1 || s.CheckoutRecoveries != 0 {
		t.Fatal("first final callback replaced", err)
	}
	for _, r := range rows {
		if !r.EndTime.Equal(r.StartTime) {
			t.Fatal("partial row facts")
		}
	}
}

func TestCheckoutRetryBudgetConcurrencyResetAndDuplicateIdentity(t *testing.T) {
	for _, count := range []int{8, 9} {
		for run := 0; run < 2; run++ {
			rows := make([]schedule.ShowtimeRecord, count)
			for i := range rows {
				rows[i] = retryTestRow(i)
			}
			g := &retryCheckoutGetter{fetch: func(_ context.Context, _ string, start time.Time, n int) (CheckoutResult, error) {
				return retryTestPage(start, n == 1), nil
			}}
			summary := SyncSummary{}
			err := enrichCheckouts(t.Context(), g, rows, &summary, rows[0].StartTime.Add(-time.Hour))
			if count == 8 && (err != nil || summary.CheckoutRetryAttempts != 8 || summary.CheckoutRecoveries != 8) {
				t.Fatal("shared/reset budget", summary, err)
			}
			if count == 9 && (err == nil || summary.CheckoutRetryAttempts > 8 || summary.CheckoutRecoveries > 8) {
				t.Fatal("budget exceeded", summary, err)
			}
		}
	}
	row := retryTestRow(44)
	response := retryTestPage(row.StartTime, true)
	_, err := resolveCheckout(row, response, false)
	var rejected *CheckoutParseError
	if !errors.As(err, &rejected) {
		t.Fatal(err)
	}
	budget := checkoutRetryBudget{}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() { budget.reserve(t.Context(), rejected, row) })
	}
	wg.Wait()
	if budget.attempts != 1 {
		t.Fatal("duplicate identity reserved twice")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	other := retryTestRow(45)
	_, err = resolveCheckout(other, response, false)
	if !errors.As(err, &rejected) || budget.reserve(ctx, rejected, other) {
		t.Fatal("canceled reservation")
	}
}

func TestSyncValidatedCheckoutRetryIsAtomic(t *testing.T) {
	for _, fail := range []bool{false, true} {
		g := &retryCheckoutGetter{Getter: &fixtureGetter{}, fetch: func(_ context.Context, _ string, start time.Time, n int) (CheckoutResult, error) {
			return retryTestPage(start, n == 1 || fail), nil
		}}
		d, s, err := Sync(t.Context(), g, fixtureOptions())
		if fail {
			if err == nil || len(d.Showtimes) != 0 || s.CheckoutRetryAttempts == 0 || s.CheckoutRecoveries != 0 {
				t.Fatal("failed retry published", err)
			}
		} else if err != nil || len(d.Showtimes) != 5 || s.CheckoutRetryAttempts != 1 || s.CheckoutRecoveries != 1 || s.ExactEnds != 4 {
			t.Fatal("whole sync retry", s, err)
		}
	}
}

func TestCheckoutActionDifferenceDiagnostics(t *testing.T) {
	const canonical = "https://achat.veocinemas.fr/toulouse/reserver/F123/D1792166400/VF/81/"
	for _, tc := range []struct {
		name, action string
		want         actionDifference
	}{
		{"scheme", strings.Replace(canonical, "https:", "http:", 1), actionScheme},
		{"authority", strings.Replace(canonical, "achat.", "other.", 1), actionAuthority},
		{"slug", strings.Replace(canonical, "toulouse", "fronton", 1), actionSlug},
		{"film", strings.Replace(canonical, "F123", "F124", 1), actionFilm},
		{"epoch", strings.Replace(canonical, "1792166400", "1792166401", 1), actionEpoch},
		{"version", strings.Replace(canonical, "/VF/", "/VO/", 1), actionVersion},
		{"tail", strings.Replace(canonical, "/81/", "/82/", 1), actionTail},
		{"slash", strings.TrimSuffix(canonical, "/"), actionTrailingSlash},
		{"query", canonical + "?secret=value", actionQuery},
		{"bare query", canonical + "?", actionQuery},
		{"fragment", canonical + "#private", actionFragment},
		{"bare fragment", canonical + "#", actionFragment},
		{"relative", strings.TrimPrefix(canonical, "https://achat.veocinemas.fr"), actionRelative | actionScheme | actionAuthority},
		{"userinfo", strings.Replace(canonical, "https://", "https://user:credential@", 1), actionUserinfo},
		{"escaping", strings.Replace(canonical, "toulouse", "%74oulouse", 1), actionEscaping},
		{"whitespace", canonical + " ", actionWhitespace | actionPathShape | actionTrailingSlash},
		{"unparseable", "https://user:credential@host/%invalid", actionUnparseable | actionEscaping},
		{"representation", strings.Replace(canonical, "https:", "HTTPS:", 1), actionRepresentation},
		{"shape", strings.Replace(canonical, "/reserver/", "/other/", 1), actionPathShape},
		{"multiple", strings.Replace(strings.Replace(canonical, "F123", "F124", 1), "/81/", "/82/", 1), actionFilm | actionTail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := compareAction(tc.action, canonical); got != tc.want {
				t.Fatalf("difference bits got=%d want=%d", got, tc.want)
			}
			body := `<form method="post" action="` + html.EscapeString(tc.action) + `"></form>`
			_, err := parseCheckout([]byte(body), canonical)
			var rejected *CheckoutParseError
			if !errors.As(err, &rejected) || rejected.FormSubreason() != "action_mismatch" || rejected.ActionDifferences() != uint32(tc.want) || !errors.Is(err, errShape) {
				t.Fatal("missing/redacted diagnostic", err)
			}
			if strings.Contains(fmt.Sprint(err), tc.action) || strings.Contains(fmt.Sprint(errors.Unwrap(err)), "credential") {
				t.Fatal("action value leaked")
			}
			// Earlier form predicates retain priority, without action flags.
			_, err = parseCheckout([]byte("<template>"+body+"</template>"), canonical)
			if !errors.As(err, &rejected) || rejected.FormSubreason() != "template_inert" || rejected.ActionDifferences() != 0 {
				t.Fatal("first subreason precedence changed")
			}
		})
	}
	if compareAction(canonical, canonical) != 0 {
		t.Fatal("equal action flagged")
	}
	row := schedule.ShowtimeRecord{TheaterID: "veo-G0699", ProviderShowingID: schedule.VeoShowingID("G0699", "synthetic")}
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		e := &CheckoutParseError{Reason: RejectBookingForm, formReason: formActionMismatch, actionDiff: actionFilm, Cause: cancellation}
		var identified *CheckoutParseError
		if !errors.As(identifyCheckoutError(e, row, CheckoutResult{pool: clientPoolPosition{2, 5}}), &identified) || !errors.Is(identified, cancellation) || identified.actionDiff != actionFilm || identified.pool != (clientPoolPosition{2, 5}) {
			t.Fatal("cause/diagnostic changed")
		}
	}
}

func TestCheckoutClosedFormSubreasons(t *testing.T) {
	const canonical = "https://achat.veocinemas.fr/toulouse/reserver/F123/D1792166400/VF/81/"
	valid := bookingFormFixture(canonical)
	for _, tc := range []struct {
		name, body, canonical, theater string
		reason                         formSubreason
	}{
		{"body", "", canonical, "", formBody},
		{"canonical missing", valid, "", "", formCanonicalMissing},
		{"canonical invalid", valid, canonical + "?", "", formCanonicalInvalid},
		{"raw zero", "<p>not a booking form</p>", canonical, "", formRawCount},
		{"raw multiple", valid + valid, canonical, "", formRawCount},
		{"inert", "<template>" + valid + "</template>", canonical, "", formInert},
		{"action count", "<form method=post></form>", canonical, "", formActionCount},
		{"action mismatch", strings.ReplaceAll(valid, "F123", "F124"), canonical, "", formActionMismatch},
		{"method count", "<form action=\"" + canonical + "\"></form>", canonical, "", formMethodCount},
		{"method nonpost", strings.ReplaceAll(valid, "POST", "GET"), canonical, "", formMethodNotPOST},
		{"login", valid + "<input type=password>", canonical, "", formLogin},
		{"fronton scope", valid + `<script>{"auditorium_showtime":"cine-fronton"}</script>`, canonical, "veo-G0699", formFronton},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCheckoutForTheater([]byte(tc.body), tc.canonical, tc.theater)
			var rejected *CheckoutParseError
			if !errors.As(err, &rejected) || !errors.Is(err, errShape) || rejected.Reason != RejectBookingForm || rejected.FormSubreason() != string(tc.reason) || rejected.Error() != "véo checkout payload rejected" {
				t.Fatal("wrong closed classification", err)
			}
			row := schedule.ShowtimeRecord{TheaterID: "veo-G0699", ProviderShowingID: schedule.VeoShowingID("G0699", "synthetic")}
			var identified *CheckoutParseError
			if !errors.As(identifyCheckoutError(err, row), &identified) || identified.FormSubreason() != string(tc.reason) || identified.ShowingID != row.ProviderShowingID {
				t.Fatal("diagnostic identity lost")
			}
		})
	}
	// DOM-count and parser failures are defensive branches: the normal tree
	// builder is forgiving. Exercise the former with an independently empty DOM,
	// and the latter's closed wrapping without changing parser input semantics.
	if bookingFormSubreason(&xhtml.Node{Type: xhtml.DocumentNode}, []byte(valid), canonical) != formDOMCount {
		t.Fatal("DOM count not distinguished")
	}
	var parseError *CheckoutParseError
	if !errors.As(formRejection(formHTML), &parseError) || !errors.Is(parseError, errShape) || parseError.FormSubreason() != "html_parse" {
		t.Fatal("HTML classification lost")
	}
	if _, err := parseCheckout([]byte(valid), canonical); err != nil {
		t.Fatal("valid legacy form rejected", err)
	}
	for _, r := range []formSubreason{formBody, formHTML, formCanonicalMissing, formCanonicalInvalid, formRawCount, formDOMCount, formInert, formActionCount, formActionMismatch, formMethodCount, formMethodNotPOST, formLogin, formFronton} {
		var e *CheckoutParseError
		if !errors.As(formRejection(r), &e) {
			t.Fatal("typed rejection missing")
		}
		e.Cause = context.Canceled
		if !errors.Is(e, context.Canceled) {
			t.Fatal("cancellation hidden")
		}
	}
}

func TestCheckoutExactFrontonRoomLiteral(t *testing.T) {
	canonical := "https://achat.veocinemas.fr/fronton/reserver/F123/D1792166400/VF/2729/"
	marker := `<script>{"auditorium_showtime":"cine-fronton"}</script>`
	body := marker + bookingFormFixture(canonical) + `<div id=s_fin>fin prévue à 20h15</div>`
	f, err := parseCheckoutForTheater([]byte(body), canonical, "veo-P3863")
	if err != nil || f.room != "cine-fronton" || !f.hasEnd || f.hour != 20 || f.minute != 15 {
		t.Fatal("literal not retained", f, err)
	}
	for _, tc := range []struct{ name, body, canonical, theater string }{
		{"wrong row", body, canonical, "veo-G0699"},
		{"no row", body, canonical, ""},
		{"no trusted target", body, "", "veo-P3863"},
		{"foreign cinema", strings.ReplaceAll(body, "/fronton/", "/toulouse/"), strings.ReplaceAll(canonical, "/fronton/", "/toulouse/"), "veo-P3863"},
		{"no form", marker, canonical, "veo-P3863"},
		{"wrong film", strings.ReplaceAll(body, "/F123/", "/F124/"), canonical, "veo-P3863"},
		{"wrong start", strings.ReplaceAll(body, "/D1792166400/", "/D1792166401/"), canonical, "veo-P3863"},
		{"wrong tail", strings.ReplaceAll(body, "/VF/2729/", "/VF/2730/"), canonical, "veo-P3863"},
		{"login", body + `<input type=password>`, canonical, "veo-P3863"},
		{"conflicting forms", body + bookingFormFixture(canonical), canonical, "veo-P3863"},
		{"null mix", body + `{"auditorium_showtime":null}`, canonical, "veo-P3863"},
		{"numeric mix", body + `{"auditorium_showtime":"salle-1"}`, canonical, "veo-P3863"},
		{"escaped", strings.ReplaceAll(body, marker, `<script>{\"auditorium_showtime\":\"cine-fronton\"}</script>`), canonical, "veo-P3863"},
		{"encoded", strings.ReplaceAll(body, marker, `<script>{&quot;auditorium_showtime&quot;:&quot;cine-fronton&quot;}</script>`), canonical, "veo-P3863"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseCheckoutForTheater([]byte(tc.body), tc.canonical, tc.theater); !errors.Is(err, errShape) {
				t.Fatal("unsafe literal accepted", err)
			}
		})
	}
	for _, value := range []string{`"cine-fronton "`, `"Cine-fronton"`, `"cine-other"`, `"salle-unique"`, `""`, `42`, `true`, `"cine-fronton"junk`, `"cine-fronton\u0000"`} {
		t.Run(value, func(t *testing.T) {
			b := strings.Replace(body, `"cine-fronton"`, value, 1)
			if _, err := parseCheckoutForTheater([]byte(b), canonical, "veo-P3863"); !errors.Is(err, errShape) {
				t.Fatal("unsupported literal accepted", err)
			}
		})
	}
}

func TestCheckoutClosedDiagnosticsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		body   string
		reason CheckoutRejection
	}{
		{`{"auditorium_showtime":"unsupported"}`, RejectRoom},
		{`maintenance`, RejectBookingForm},
		{`<div id=s_fin>fin prévue à 25h00</div>{"auditorium_showtime":null}`, RejectClock},
	} {
		_, err := parseCheckout([]byte(tc.body), "")
		var rejected *CheckoutParseError
		if !errors.As(err, &rejected) || rejected.Reason != tc.reason || !errors.Is(err, errShape) || strings.Contains(err.Error(), tc.body) {
			t.Fatal("diagnostic lost", err)
		}
	}
	loc, _ := time.LoadLocation(schedule.Timezone)
	_, err := exactEnd(time.Date(2026, 10, 24, 23, 30, 0, 0, loc), checkoutFacts{hasEnd: true, hour: 2, minute: 30})
	var rejected *CheckoutParseError
	if !errors.As(err, &rejected) || rejected.Reason != RejectEndResolution || !errors.Is(err, errShape) {
		t.Fatal(err)
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("https://user:password@host/raw-body-token")} {
		err := checkoutRejection(RejectRoom, cause)
		if !errors.Is(err, cause) || strings.Contains(err.Error(), cause.Error()) {
			t.Fatal("unsafe error rendering")
		}
	}
	unsafe := "https://user:password@host/raw-body-token"
	err = identifyCheckoutError(&CheckoutParseError{Reason: RejectRoom, CinemaID: unsafe, ShowingID: unsafe, Cause: errShape}, schedule.ShowtimeRecord{TheaterID: unsafe, ProviderShowingID: unsafe})
	if !errors.As(err, &rejected) || rejected.CinemaID != "" || rejected.ShowingID != "" {
		t.Fatal("invalid identities retained")
	}
}

func TestFrontonEnrichmentAndConcurrentFirstError(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 10, 16, 18, 0, 0, 0, loc)
	row := schedule.ShowtimeRecord{TheaterID: "veo-P3863", ProviderShowingID: schedule.VeoShowingID("P3863", "fixture"), StartTime: start, EndTime: start, BookingURL: "https://achat.veocinemas.fr/fronton/r/2729"}
	for _, room := range []string{"", "API room"} {
		rows := []schedule.ShowtimeRecord{row}
		rows[0].Room = room
		g := getterFunc(func(_ context.Context, _ Operation, _ string) ([]byte, error) { return nil, nil })
		canonical := fmt.Sprintf("https://achat.veocinemas.fr/fronton/reserver/F123/D%d/VF/2729/", start.Unix())
		bound := frontonGetter{Getter: g, result: CheckoutResult{Body: []byte(`<script>{"auditorium_showtime":"cine-fronton"}</script>` + bookingFormFixture(canonical) + `<div id=s_fin>fin prévue à 20h15</div>`), canonicalURL: canonical}}
		summary := SyncSummary{}
		if err := enrichCheckouts(t.Context(), bound, rows, &summary, start.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		wantRoom := room
		if room == "" {
			wantRoom = "cine-fronton"
		}
		if rows[0].Room != wantRoom || rows[0].EndTime.Sub(start) != 135*time.Minute || summary.ExactEnds != 1 {
			t.Fatal(rows, summary)
		}
	}
	// The second row actually fails first; cancellation of its blocked peer must
	// not replace that parser error or attribute it to the first input row.
	for iteration := range 20 {
		rows := []schedule.ShowtimeRecord{row, row}
		rows[0].BookingURL = "blocked"
		rows[1].ProviderShowingID = schedule.VeoShowingID("P3863", "actual-first")
		g := firstErrorGetter{started: make(chan struct{}), form: iteration%2 == 1}
		err := enrichCheckouts(t.Context(), g, rows, &SyncSummary{}, start.Add(-time.Hour))
		var rejected *CheckoutParseError
		want := RejectRoom
		if g.form {
			want = RejectBookingForm
		}
		if !errors.As(err, &rejected) || rejected.Reason != want || rejected.CinemaID != "P3863" || rejected.ShowingID != rows[1].ProviderShowingID || errors.Is(err, context.Canceled) || g.form && (rejected.FormSubreason() != "action_mismatch" || rejected.actionDiff != actionFilm || rejected.pool != (clientPoolPosition{2, 5})) {
			t.Fatal("first error replaced", err)
		}
	}
}

type frontonGetter struct {
	Getter
	result CheckoutResult
}

func (g frontonGetter) Checkout(context.Context, string, time.Time) (CheckoutResult, error) {
	return g.result, nil
}

type firstErrorGetter struct {
	started chan struct{}
	form    bool
}

func (g firstErrorGetter) Get(context.Context, Operation, string) ([]byte, error) { panic("unused") }
func (g firstErrorGetter) RequestCount() int                                      { return 2 }
func (g firstErrorGetter) Checkout(ctx context.Context, raw string, _ time.Time) (CheckoutResult, error) {
	if raw == "blocked" {
		close(g.started)
		<-ctx.Done()
		return CheckoutResult{}, ctx.Err()
	}
	select {
	case <-g.started:
		if g.form {
			canonical := "https://achat.veocinemas.fr/fronton/reserver/F123/D1792166400/VF/81/"
			return CheckoutResult{Body: []byte(bookingFormFixture(strings.Replace(canonical, "F123", "F124", 1))), canonicalURL: canonical, pool: clientPoolPosition{2, 5}}, nil
		}
		return CheckoutResult{Body: []byte(`{"auditorium_showtime":"unsupported"}`)}, nil
	case <-ctx.Done():
		return CheckoutResult{}, ctx.Err()
	}
}

func TestEnrichCheckoutsBindsSourceSessionStart(t *testing.T) {
	for _, epoch := range []string{"1792065000", "1792065001"} {
		t.Run(epoch, func(t *testing.T) {
			calls := 0
			c := testClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					r := fixtureResponse(307, "", "text/html")
					r.Header.Set("Location", "https://achat.veocinemas.fr/andernos/reserver/F604912/D"+epoch+"/VF/271793/")
					return r, nil
				}
				return fixtureResponse(200, `<div id=s_fin>fin prévue à 15h45</div>{"auditorium_showtime":"salle-2"}`, "text/html"), nil
			})
			loc, _ := time.LoadLocation(schedule.Timezone)
			start := time.Unix(1792065000, 0).In(loc)
			rows := []schedule.ShowtimeRecord{{TheaterID: "veo-W3351", StartTime: start, EndTime: start, BookingURL: "https://achat.veocinemas.fr/andernos/r/55880", Room: "API room"}}
			summary := SyncSummary{}
			err := enrichCheckouts(t.Context(), c, rows, &summary, start.Add(-time.Minute))
			if epoch == "1792065000" {
				if err != nil || !rows[0].EndTime.After(start) || rows[0].Room != "API room" || calls != 2 || summary.Checkouts != 1 {
					t.Fatal("bound enrichment failed", err, calls)
				}
			} else if err == nil || !rows[0].EndTime.Equal(start) || summary.Checkouts != 0 || calls != 1 {
				t.Fatal("wrong session destination was used", err, calls)
			}
		})
	}
}

func TestCheckoutScopedClockCharsetAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid, end bool
		room       string
	}{
		{"scoped", `<aside>fin prévue à 22h22</aside><div class="s_fin">fin prévue à <b>00h15</b></div><script>{"auditorium_showtime":"salle-12"}</script>`, true, true, "Salle 12"},
		{"latin1", "<meta charset=iso-8859-1><div id=s_fin>fin pr\xe9vue \xe0 00h15</div><script>{\"auditorium_showtime\":\"salle-2\"}</script>", true, true, "Salle 2"},
		{"unknown", `<div>fin prévue à 22h22</div>{"auditorium_showtime":null}`, true, false, ""},
		{"missing identity", `<html>maintenance</html>`, false, false, ""},
		{"malformed clock", `<div id=s_fin>fin prévue à 29h22</div>{"auditorium_showtime":null}`, false, false, ""},
		{"conflicting", `<div id=s_fin>fin prévue à 21h00</div><div class=s_fin>fin prévue à 22h00</div>{"auditorium_showtime":null}`, false, false, ""},
		{"malformed room", `{"auditorium_showtime":"salle-0"}`, false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parseCheckout([]byte(tc.body), "")
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if tc.valid && (f.hasEnd != tc.end || f.room != tc.room) {
				t.Fatalf("%+v", f)
			}
		})
	}
	if _, err := parseCheckout([]byte(strings.Repeat("x", MaxHTMLBytes+1)), ""); err == nil {
		t.Fatal("oversized html")
	}
}
func TestExactEndMidnightAndDST(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	for _, tc := range []struct {
		start        string
		hour, minute int
		end          string
		valid        bool
	}{
		{"2026-10-15T23:30:00+02:00", 1, 15, "2026-10-16T01:15:00+02:00", true},
		{"2026-03-28T23:30:00+01:00", 3, 30, "2026-03-29T03:30:00+02:00", true},
		{"2026-03-28T23:30:00+01:00", 2, 30, "", false},
		{"2026-10-24T23:30:00+02:00", 2, 30, "", false},
	} {
		start, _ := time.Parse(time.RFC3339, tc.start)
		end, err := exactEnd(start.In(loc), checkoutFacts{hasEnd: true, hour: tc.hour, minute: tc.minute})
		if (err == nil) != tc.valid || tc.valid && end.Format(time.RFC3339) != tc.end {
			t.Fatal(tc, end, err)
		}
		unknown, err := exactEnd(start.In(loc), checkoutFacts{})
		if err != nil || !unknown.Equal(start) {
			t.Fatal(unknown, err)
		}
	}
	for _, raw := range []string{"2026-03-29T02:30:00", "2026-10-25T02:30:00", "2026-10-15T20:00:00Z"} {
		if _, err := parseStart(raw, loc); err == nil {
			t.Fatal(raw)
		}
	}
}

func bookingFormFixture(action string) string {
	return `<form method="POST" action="` + action + `"><input type="number" name="tickets"><button type="submit">Réserver</button></form>`
}

func TestCheckoutPublishedClockHourWidths(t *testing.T) {
	loc, _ := time.LoadLocation(schedule.Timezone)
	start := time.Date(2026, 10, 15, 23, 30, 0, 0, loc)
	// These clocks are synthetic grammar fixtures, not investigator estimates.
	canonical := "https://achat.veocinemas.fr/toulouse/reserver/F123/D" + strconv.FormatInt(start.Unix(), 10) + "/VF/81/"
	for _, tc := range []struct {
		clock, end string
	}{
		{"0h15", "2026-10-16T00:15:00+02:00"},
		{"00h15", "2026-10-16T00:15:00+02:00"},
		{"1h07", "2026-10-16T01:07:00+02:00"},
		{"01h07", "2026-10-16T01:07:00+02:00"},
		{"9h59", "2026-10-16T09:59:00+02:00"},
		{"23h59", "2026-10-15T23:59:00+02:00"},
	} {
		t.Run(tc.clock, func(t *testing.T) {
			body := bookingFormFixture(canonical) + `<div id=s_fin>(fin prévue à ` + tc.clock + `)</div>`
			facts, err := parseCheckout([]byte(body), canonical)
			if err != nil || !facts.hasEnd || facts.room != "" {
				t.Fatal("published clock rejected or room fabricated", facts, err)
			}
			end, err := exactEnd(start, facts)
			if err != nil || end.Format(time.RFC3339) != tc.end {
				t.Fatal("published clock did not resolve exactly", end, err)
			}
		})
	}
	for _, clock := range []string{"24h15", "25h15", "26h15", "27h15", "28h15", "29h15", "99h15", "000h15", "123h15", "1h", "1h5", "1h60", "1h99", "1h001", "1h150", "1hab", "1h1a", "h15", "-1h15"} {
		t.Run("invalid-"+clock, func(t *testing.T) {
			body := bookingFormFixture(canonical) + `<div id=s_fin>fin prévue à ` + clock + `</div>`
			if _, err := parseCheckout([]byte(body), canonical); err == nil {
				t.Fatal("invalid clock accepted")
			}
		})
	}
	for _, blocks := range []string{
		`<div id=s_fin>fin prévue à 0h15 puis fin prévue à 1h07</div>`,
		`<div id=s_fin>fin prévue à 0h15</div><div class=s_fin>fin prévue à 01h07</div>`,
	} {
		if _, err := parseCheckout([]byte(bookingFormFixture(canonical)+blocks), canonical); err == nil {
			t.Fatal("conflicting clocks accepted")
		}
	}
}

func TestCheckoutMissingRoomRequiresTrustedBookingForm(t *testing.T) {
	const canonical = "https://achat.veocinemas.fr/toulouse/reserver/F324508/D1792065000/VF/81/"
	form := bookingFormFixture(canonical)
	for _, end := range []string{"", `<div id="s_fin">fin prévue à 23h15</div>`} {
		f, err := parseCheckout([]byte(form+end), canonical)
		if err != nil || f.room != "" || f.hasEnd != (end != "") {
			t.Fatal("verified missing metadata rejected", f, err)
		}
		start := time.Unix(1792065000, 0)
		resolved, err := exactEnd(start, f)
		if err != nil || end == "" && !resolved.Equal(start) {
			t.Fatal("missing end inferred", err)
		}
	}
	for _, action := range []string{
		strings.Replace(canonical, "/toulouse/", "/other-cinema/", 1),
		strings.Replace(canonical, "1792065000", "1792065001", 1),
		strings.Replace(canonical, "F324508", "F324509", 1),
		strings.Replace(canonical, "/81/", "/82/", 1),
		strings.Replace(canonical, "/VF/", "/VO/", 1),
		strings.Replace(canonical, "achat.", "evil.achat.", 1),
		strings.Replace(canonical, ".fr/", ".fr:443/", 1),
		strings.Replace(canonical, "https://", "https://user@", 1),
		strings.Replace(canonical, "toulouse", "%74oulouse", 1),
		strings.Replace(canonical, "https:", "http:", 1),
		canonical + "?", canonical + "?x=1", canonical + "#", canonical + "extra", " " + canonical,
	} {
		if _, err := parseCheckout([]byte(bookingFormFixture(action)), canonical); err == nil {
			t.Fatal("unbound form action accepted")
		}
	}
	for i, body := range []string{
		`<html>maintenance</html>`, `<p>fin prévue à 23h15</p>`, canonical,
		`<a href="` + canonical + `">Réserver</a>`,
		`<script>` + form + `</script>`, `<p>` + strings.ReplaceAll(strings.ReplaceAll(form, "<", "&lt;"), ">", "&gt;") + `</p>`,
		strings.Replace(form, `method="POST"`, `method="GET"`, 1),
		strings.Replace(form, `method="POST"`, "", 1),
		form + form,
		strings.Replace(form, `</form>`, form+`</form>`, 1),
		`<template>` + form + `</template>`,
		form + `<input type="PASSWORD">`, form + `<input name="login">`, form + `<input autocomplete="current-password">`,
		form + `{"auditorium_showtime":"salle-0"}`, form + `{"auditorium_showtime":false}`,
		form + `{"auditorium_showtime":nulljunk}`, form + `{'auditorium_showtime':null}`,
		form + `{"auditorium_showtime":null,"auditorium_showtime":"salle-1"}`,
		form + `<div id=s_fin>fin prévue à 29h15</div>`,
	} {
		if _, err := parseCheckout([]byte(body), canonical); err == nil {
			t.Fatal("malformed/nonbooking identity accepted", i)
		}
	}
	for _, untrusted := range []string{"", "https://achat.veocinemas.fr/toulouse/r/8106", canonical + "?", canonical + "#"} {
		if _, err := parseCheckout([]byte(bookingFormFixture(untrusted)), untrusted); err == nil {
			t.Fatal("body-chosen or noncanonical identity accepted")
		}
	}
	// The fallback never replaces the established valid present-marker branch.
	if f, err := parseCheckout([]byte(`{"auditorium_showtime":null}`), ""); err != nil || f.room != "" || f.hasEnd {
		t.Fatal("legacy explicit null changed", err)
	}
}
