package synccontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/veo"
)

func TestVeoActualPathLoggingWhitelist(t *testing.T) {
	valid := veo.CheckoutActionPathFacts{Parsed: true, CanonicalValid: true, Shape: "source_alias", SegmentCount: 3,
		Classes: []string{"expected_slug", "route_source_r", "decimal"}, ExpectedPresentMask: 1, ExpectedPositionMask: 1,
		ExpectedAtPosition: [6]bool{true}, ExpectedPositions: [6]int{0, -1, -1, -1, -1, -1}}
	args := safeVeoActionPathLogArgs(valid)
	var output bytes.Buffer
	slog.New(slog.NewJSONHandler(&output, nil)).Info("safe-path-test", args...)
	if !strings.Contains(output.String(), `"veo_action_path_shape":"source_alias"`) || !strings.Contains(output.String(), `"veo_action_path_present_mask":1`) {
		t.Fatal("closed path diagnostics missing")
	}
	for _, forge := range []func(*veo.CheckoutActionPathFacts){
		func(d *veo.CheckoutActionPathFacts) { d.Shape = "https://user:secret@host/raw" },
		func(d *veo.CheckoutActionPathFacts) { d.Classes = []string{"opaque-secret"} },
		func(d *veo.CheckoutActionPathFacts) { d.Classes = make([]string, 33) },
		func(d *veo.CheckoutActionPathFacts) { d.SegmentCount = 1025 },
		func(d *veo.CheckoutActionPathFacts) { d.EmptySegments = -1 },
		func(d *veo.CheckoutActionPathFacts) { d.ExpectedPresentMask = 128 },
		func(d *veo.CheckoutActionPathFacts) { d.ExpectedPositionMask = 128 },
		func(d *veo.CheckoutActionPathFacts) { d.ExpectedPositions[0] = 1025 },
		func(d *veo.CheckoutActionPathFacts) { d.ExpectedPositions[0] = -2 },
		func(d *veo.CheckoutActionPathFacts) { d.ExpectedAtPosition[0] = false },
		func(d *veo.CheckoutActionPathFacts) { d.Parsed = false },
	} {
		forged := valid
		forge(&forged)
		if safeVeoActionPathLogArgs(forged) != nil {
			t.Fatal("forged or unbounded structural diagnostic accepted")
		}
	}
	// Public callers cannot attach private path observations to a typed error.
	forged := &veo.CheckoutParseError{Reason: veo.RejectBookingForm, CinemaID: "G0699", ShowingID: schedule.VeoShowingID("G0699", "fixture"), Cause: errors.New("opaque-secret")}
	if strings.Contains(fmt.Sprint(providerFetchLogArgs(TargetVeo, newProviderRunError(TargetVeo, StageProviderFetch, FailureProviderSync, forged))), "veo_action_path") {
		t.Fatal("public error gained private response facts")
	}
}

func TestVeoCheckoutRetrySummaryIsBoundedAndBackendOnly(t *testing.T) {
	if veoCheckoutRetryLogArgs(veo.SyncSummary{CheckoutTransientFailures: -1}) != nil || veoCheckoutRetryLogArgs(veo.SyncSummary{CheckoutBudgetSkipped: -1}) != nil {
		t.Fatal("negative optional enrichment counters logged")
	}
	if veoCheckoutRetryLogArgs(veo.SyncSummary{ToulouseUnavailableCheckouts: -1}) != nil {
		t.Fatal("negative unavailable count logged")
	}
	if veoCheckoutRetryLogArgs(veo.SyncSummary{GueretUnavailableCheckouts: -1}) != nil {
		t.Fatal("negative Guéret unavailable count logged")
	}
	if veoCheckoutRetryLogArgs(veo.SyncSummary{RegularFinal404UnavailableCheckouts: -1}) != nil {
		t.Fatal("negative regular final404 unavailable count logged")
	}
	for _, counts := range [][2]int{{-1, 0}, {9, 0}, {1, 2}, {1, -1}} {
		if veoCheckoutRetryLogArgs(veo.SyncSummary{CheckoutRetryAttempts: counts[0], CheckoutRecoveries: counts[1]}) != nil {
			t.Fatal("unbounded retry stats logged")
		}
	}
	var output bytes.Buffer
	e := &ProductionExecutor{now: time.Now, logger: slog.New(slog.NewJSONHandler(&output, nil)), operationTimeout: time.Second, writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { return 1, nil })}
	w := Window{From: "2026-08-17"}
	configureVeoTestExecutor(t, e, w)
	base := e.syncVeo
	e.syncVeo = func(ctx context.Context, g veo.Getter, o veo.SyncOptions) (schedule.Dataset, veo.SyncSummary, error) {
		d, s, err := base(ctx, g, o)
		s.CheckoutRetryAttempts = 1
		s.CheckoutRecoveries = 1
		s.ToulouseUnavailableCheckouts = 7
		s.GueretUnavailableCheckouts = 3
		s.RegularFinal404UnavailableCheckouts = 5
		s.FirstCheckoutRetryRecovered = true
		s.FirstCheckoutRetry = &veo.CheckoutParseError{Reason: veo.CheckoutRejection("secret-value"), CinemaID: "secret-value", ShowingID: "secret-value", Cause: errors.New("secret-value")}
		return d, s, err
	}
	out, err := e.Run(t.Context(), TargetVeo, w)
	if err != nil {
		t.Fatal(err)
	}
	log := output.String()
	if !strings.Contains(log, `"msg":"veo_checkout_retry_summary"`) || !strings.Contains(log, `"checkout_retry_attempts":1`) || !strings.Contains(log, `"checkout_recoveries":1`) || !strings.Contains(log, `"toulouse_unavailable_checkouts":7`) || !strings.Contains(log, `"gueret_unavailable_checkouts":3`) || !strings.Contains(log, `"regular_final404_unavailable_checkouts":5`) || strings.Contains(log, "secret-value") || strings.Contains(log, "provider_failed") {
		t.Fatal("unsafe or misleading recovery log", log)
	}
	wire, err := json.Marshal(out)
	if err != nil || strings.Contains(strings.ToLower(string(wire)), "retry") || strings.Contains(strings.ToLower(string(wire)), "recover") || strings.Contains(strings.ToLower(string(wire)), "toulouse") || strings.Contains(strings.ToLower(string(wire)), "gueret") || strings.Contains(strings.ToLower(string(wire)), "final404") || strings.Contains(strings.ToLower(string(wire)), "unavailable") {
		t.Fatal("private retry fields leaked")
	}
	zero := fmt.Sprint(veoCheckoutRetryLogArgs(veo.SyncSummary{}))
	if !strings.Contains(zero, "checkout_retry_attempts 0") || !strings.Contains(zero, "checkout_recoveries 0") || !strings.Contains(zero, "gueret_unavailable_checkouts 0") || !strings.Contains(zero, "regular_final404_unavailable_checkouts 0") {
		t.Fatal("unexercised mitigation not observable")
	}
}

func TestVeoOptionalCheckoutWarningsSurviveSuccessfulPublication(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC) }
	var backend bytes.Buffer
	writes := 0
	e := &ProductionExecutor{now: now, logger: slog.New(slog.NewJSONHandler(&backend, nil)), operationTimeout: time.Second,
		writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) { writes++; return 1, nil })}
	w := Window{From: "2026-08-17"}
	configureVeoTestExecutor(t, e, w)
	base := e.syncVeo
	e.syncVeo = func(ctx context.Context, g veo.Getter, o veo.SyncOptions) (schedule.Dataset, veo.SyncSummary, error) {
		d, s, err := base(ctx, g, o)
		s.CheckoutTransientFailures, s.CheckoutBudgetSkipped = 7, 3
		return d, s, err
	}
	m, err := newTestManager(t.Context(), now, e)
	if err != nil {
		t.Fatal(err)
	}
	status := m.executeProvider(TargetVeo, w)
	if status.State != ProviderSucceeded || status.Outcome == nil || writes != 1 || status.Outcome.logs != nil {
		t.Fatal("optional enrichment blocked publication", status)
	}
	log := strings.Join(status.Log, "\n")
	for _, want := range []string{"event=checkout_enrichment_incomplete", "category=transport skipped=7", "category=canceled skipped=3", "fin inconnue"} {
		if !strings.Contains(log, want) {
			t.Fatal("journal warning lost", want, log)
		}
	}
	if strings.Contains(log, "provider_failed") || strings.Contains(log, "log_truncated") {
		t.Fatal("successful warnings were rejected or marked as failure", log)
	}
	if !strings.Contains(backend.String(), `"checkout_transient_failures":7`) || !strings.Contains(backend.String(), `"checkout_budget_skipped":3`) {
		t.Fatal("backend counters missing")
	}
	// Status persistence uses JSON; the safe journal must survive that boundary.
	wire, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var restored ProviderStatus
	if err := json.Unmarshal(wire, &restored); err != nil {
		t.Fatal(err)
	}
	finished := now()
	if len(normalizeProviderLog(TargetVeo, restored.Log, now(), &finished)) != len(status.Log) {
		t.Fatal("journal did not survive persistence")
	}
}

func TestVeoCheckoutDiagnosticsAreBackendOnlyAndAllowlisted(t *testing.T) {
	showing := schedule.VeoShowingID("P3863", "fixture")
	secret := "https://user:credential@host/raw-body-token?Location=secret"
	for _, reason := range []veo.CheckoutRejection{veo.RejectRoom, veo.RejectBookingForm, veo.RejectClock, veo.RejectEndResolution} {
		cause := &veo.CheckoutParseError{Reason: reason, CinemaID: "P3863", ShowingID: showing, Cause: errors.New(secret)}
		run := newProviderRunError(TargetVeo, StageProviderFetch, FailureProviderSync, cause)
		details := failureDetails(TargetVeo, StageProviderFetch, run, SyncOutcome{})
		if details.Operation != operationShowings || details.Category != categoryInvalidPayload || details.ParseReason != "" {
			t.Fatal(details)
		}
		var output bytes.Buffer
		e := &ProductionExecutor{logger: slog.New(slog.NewJSONHandler(&output, nil))}
		e.observe(TargetVeo, ProviderOutcome{}, run, time.Second)
		log := output.String()
		for _, want := range []string{`"veo_checkout_reason":"` + string(reason) + `"`, `"veo_cinema_id":"P3863"`, showing, `"fetch_category":"invalid_payload"`} {
			if !strings.Contains(log, want) {
				t.Fatal("diagnostic absent", log)
			}
		}
		for _, unwanted := range []string{secret, "credential", "raw-body-token", "Location=", "parse_reason"} {
			if strings.Contains(log, unwanted) {
				t.Fatal("private cause leaked")
			}
		}
		if errors.Unwrap(cause) == nil || !errors.Is(run, cause.Cause) || strings.Contains(cause.Error(), secret) {
			t.Fatal("cause semantics lost")
		}
	}
	for _, forged := range []*veo.CheckoutParseError{
		{Reason: veo.CheckoutRejection(secret), CinemaID: "P3863", ShowingID: showing},
		{Reason: veo.RejectRoom, CinemaID: secret, ShowingID: showing},
		{Reason: veo.RejectRoom, CinemaID: "P3863", ShowingID: secret},
		{Reason: veo.RejectRoom, CinemaID: "P3863", ShowingID: schedule.VeoShowingID("G0699", "fixture")},
	} {
		run := newProviderRunError(TargetVeo, StageProviderFetch, FailureProviderSync, forged)
		args := fmt.Sprint(providerFetchLogArgs(TargetVeo, run))
		if strings.Contains(args, secret) || strings.Contains(args, "veo_cinema_id") || strings.Contains(args, "veo_showing_id") {
			t.Fatal("forgery logged", args)
		}
		if forged.Reason == veo.CheckoutRejection(secret) && strings.Contains(args, "veo_checkout_reason") {
			t.Fatal("unknown enum logged")
		}
		if strings.Contains(fmt.Sprint(providerFetchLogArgs(TargetUGC, run)), "veo_checkout") {
			t.Fatal("cross provider diagnostic")
		}
	}
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		err := &veo.CheckoutParseError{Reason: veo.RejectRoom, CinemaID: "P3863", ShowingID: showing, Cause: cancellation}
		run := newProviderRunError(TargetVeo, StageProviderFetch, FailureProviderSync, err)
		if failureDetails(TargetVeo, StageProviderFetch, run, SyncOutcome{}).Category != categoryCanceled || !errors.Is(run, cancellation) || strings.Contains(fmt.Sprint(providerFetchLogArgs(TargetVeo, run)), "veo_checkout_reason") {
			t.Fatal("cancellation lost priority")
		}
	}
}

type unusedVeoGetter struct{}

func TestVeoFormSubreasonLoggingWhitelist(t *testing.T) {
	for _, reason := range []string{"body_encoding", "html_parse", "canonical_missing", "canonical_invalid", "raw_form_count", "dom_form_count", "template_inert", "action_count", "action_mismatch", "method_count", "method_non_post", "login_password", "fronton_scope"} {
		if safeVeoFormSubreason(reason) != reason {
			t.Fatal("closed reason omitted")
		}
	}
	for _, forged := range []string{"", "https://user:secret@example.test/", "<form action=secret>", "canonical_missing\npassword", "CANONICAL_MISSING", "other"} {
		if safeVeoFormSubreason(forged) != "" {
			t.Fatal("provider text accepted")
		}
	}
	// External callers cannot supply the private form enum. A forged public
	// parse error must never gain a subreason from its cause or public fields.
	args := providerFetchLogArgs(TargetVeo, &veo.CheckoutParseError{Reason: veo.RejectBookingForm, Cause: errors.New("action=secret")})
	if strings.Contains(fmt.Sprint(args), "veo_form_subreason") || strings.Contains(fmt.Sprint(args), "secret") {
		t.Fatal("forged diagnostic emitted")
	}
}

func TestVeoActionAndClientPoolLoggingWhitelist(t *testing.T) {
	names := []string{"scheme", "authority", "path_shape", "slug", "film", "epoch", "version", "tail", "trailing_slash", "query", "fragment", "relative", "whitespace", "escaping", "userinfo", "unparseable", "representation"}
	for i, name := range names {
		if safeVeoActionDifferences(1<<i) != name {
			t.Fatal("closed action difference omitted")
		}
	}
	if safeVeoActionDifferences(1|16|128) != "scheme,film,tail" {
		t.Fatal("combined action flags changed")
	}
	for _, bits := range []uint32{0, 1 << 17, (1 << 31) | 16, ^uint32(0)} {
		if safeVeoActionDifferences(bits) != "" {
			t.Fatal("forged action bits logged")
		}
	}
	for _, pair := range [][2]int{{0, 0}, {-1, 5}, {6, 5}, {1, 10001}, {0, 5}, {1, -1}} {
		if validVeoClientPoolPosition(pair[0], pair[1]) {
			t.Fatal("forged pool index accepted")
		}
	}
	if !validVeoClientPoolPosition(5, 5) || !validVeoClientPoolPosition(1, 10000) {
		t.Fatal("valid bounded pool index rejected")
	}
	// External errors cannot forge these private diagnostics, even with valid IDs.
	forged := &veo.CheckoutParseError{Reason: veo.RejectBookingForm, CinemaID: "G0699", ShowingID: schedule.VeoShowingID("G0699", "fixture"), Cause: errors.New("https://user:credential@host/?private")}
	run := newProviderRunError(TargetVeo, StageProviderFetch, FailureProviderSync, forged)
	args := fmt.Sprint(providerFetchLogArgs(TargetVeo, run))
	for _, forbidden := range []string{"veo_action", "veo_client_pool", "credential", "private"} {
		if strings.Contains(args, forbidden) {
			t.Fatal("forged metadata emitted")
		}
	}
}

func (unusedVeoGetter) Get(context.Context, veo.Operation, string) ([]byte, error) { panic("unused") }
func (unusedVeoGetter) Checkout(context.Context, string, time.Time) (veo.CheckoutResult, error) {
	panic("unused")
}
func (unusedVeoGetter) RequestCount() int { return 29 }
func configureVeoTestExecutor(t *testing.T, e *ProductionExecutor, w Window) {
	t.Helper()
	e.newVeo = func() (veo.Getter, error) { return unusedVeoGetter{}, nil }
	e.syncVeo = func(context.Context, veo.Getter, veo.SyncOptions) (schedule.Dataset, veo.SyncSummary, error) {
		d := validDataset(t, schedule.ProviderUGC, w)
		d.Provider = schedule.ProviderVeo
		d.Theaters = []schedule.TheaterRecord{{Provider: schedule.ProviderVeo, ID: "veo-P1028", ProviderID: "P1028", Slug: "veo-P1028", Name: "Véo fixture", Address: "1 rue", City: "Paris", PostalCode: "75001", AvailableDates: []string{w.From}, AcceptedPasses: []string{}}}
		r := d.Showtimes[0]
		id := schedule.VeoShowingID("P1028", "opaque")
		r.Provider, r.ID, r.ProviderShowingID, r.TheaterID = schedule.ProviderVeo, "veo-showing-"+id, id, "veo-P1028"
		r.Movie = schedule.MovieRecord{Provider: schedule.ProviderVeo, ProviderID: "cEvent_1", Slug: "veo-film-cEvent_1", Title: "Event"}
		r.EndTime = r.StartTime
		r.FirstPartDurationMinutes = 0
		r.Language, r.ProviderVersion, r.Room, r.BookingURL = schedule.LanguageVFSTF, "VFSTF", "", "https://achat.veocinemas.fr/fixture/r/123"
		d.Showtimes = []schedule.ShowtimeRecord{r}
		return d, veo.SyncSummary{Cinemas: 1, Movies: 1, Showtimes: 1, GeneratedAt: d.GeneratedAt}, nil
	}
}
