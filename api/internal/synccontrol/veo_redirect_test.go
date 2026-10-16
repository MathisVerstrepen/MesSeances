package synccontrol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/veo"
)

func TestVeoRedirectLoggingIndependentWhitelist(t *testing.T) {
	valid := veo.CheckoutRedirectFacts{Branch: "gueret", Phase: "response", Reason: "epoch_binding", Hop: 2, HopKnown: true,
		HTTPStatus: 307, LocationCount: 1, ExpectedHost: "dedicated", ExpectedRoute: "canonical", ObservedHost: "dedicated", ObservedRoute: "canonical",
		CinemaID: "G06DB", ShowingID: schedule.VeoShowingID("G06DB", "synthetic"), PoolPosition: 2, PoolSize: 5}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logger.Info("fixture", safeVeoRedirectLogArgs(valid)...)
	var record map[string]any
	if json.Unmarshal(output.Bytes(), &record) != nil || record["veo_redirect_reason"] != "epoch_binding" || record["veo_redirect_hop"] != float64(2) || record["veo_client_pool_position"] != float64(2) || record["veo_cinema_id"] != "G06DB" {
		t.Fatal("closed rejection facts missing")
	}
	for _, mutate := range []func(*veo.CheckoutRedirectFacts){
		func(d *veo.CheckoutRedirectFacts) { d.Branch = "secret" },
		func(d *veo.CheckoutRedirectFacts) { d.Phase = "secret" },
		func(d *veo.CheckoutRedirectFacts) { d.Reason = "secret" },
		func(d *veo.CheckoutRedirectFacts) { d.ExpectedHost = "https://user:secret@host/" },
		func(d *veo.CheckoutRedirectFacts) { d.ObservedHost = "secret" },
		func(d *veo.CheckoutRedirectFacts) { d.ExpectedRoute = "/opaque-secret" },
		func(d *veo.CheckoutRedirectFacts) { d.ObservedRoute = "secret" },
		func(d *veo.CheckoutRedirectFacts) { d.CinemaID = "secret" },
		func(d *veo.CheckoutRedirectFacts) {
			d.CinemaID, d.ShowingID = "P1028", schedule.VeoShowingID("P1028", "synthetic")
		},
		func(d *veo.CheckoutRedirectFacts) { d.ShowingID = "secret" },
		func(d *veo.CheckoutRedirectFacts) { d.ShowingID = schedule.VeoShowingID("P1028", "synthetic") },
		func(d *veo.CheckoutRedirectFacts) { d.PoolPosition = 0 },
		func(d *veo.CheckoutRedirectFacts) { d.PoolPosition = 6 },
		func(d *veo.CheckoutRedirectFacts) { d.PoolSize = 10001 },
		func(d *veo.CheckoutRedirectFacts) { d.Hop = -1 },
		func(d *veo.CheckoutRedirectFacts) { d.Hop = 4 },
		func(d *veo.CheckoutRedirectFacts) { d.HopKnown = false },
		func(d *veo.CheckoutRedirectFacts) { d.Branch = "regular" },
		func(d *veo.CheckoutRedirectFacts) { d.HTTPStatus = -1 },
		func(d *veo.CheckoutRedirectFacts) { d.HTTPStatus = 600 },
		func(d *veo.CheckoutRedirectFacts) { d.LocationCount = -1 },
		func(d *veo.CheckoutRedirectFacts) { d.LocationCount = 5 },
		func(d *veo.CheckoutRedirectFacts) { d.LocationCountTruncated = true },
	} {
		forged := valid
		mutate(&forged)
		if safeVeoRedirectLogArgs(forged) != nil {
			t.Fatal("forged facts reached logger")
		}
	}
	for _, reason := range []string{"unknown", "request_binding", "raw_location_parse", "unsafe_authority", "route", "query", "source_binding", "date_binding", "time_binding", "epoch_binding", "cinema_binding", "status", "location_count", "hop", "method", "response_binding", "representation", "response_identity_unknown"} {
		d := valid
		d.Reason = reason
		if len(safeVeoRedirectLogArgs(d)) == 0 {
			t.Fatal("closed reason missing", reason)
		}
	}
	unknown := valid
	unknown.Hop, unknown.HopKnown, unknown.HTTPStatus = 0, false, 0
	unknown.ExpectedHost, unknown.ExpectedRoute, unknown.ObservedHost, unknown.ObservedRoute = "unknown", "unknown", "unknown", "unknown"
	if len(safeVeoRedirectLogArgs(unknown)) == 0 {
		t.Fatal("unknown evidence treated as guessed value")
	}
}

func TestVeoForgedRedirectErrorsCannotLogTransportFacts(t *testing.T) {
	for _, kind := range []syncproxy.FailureKind{syncproxy.FailureRedirect, syncproxy.FailureCanceled, syncproxy.FailureStatus} {
		cause := &veo.RequestError{Operation: veo.OperationCheckout, Kind: kind, StatusCode: 302}
		run := newProviderRunError(TargetVeo, StageProviderFetch, FailureProviderSync, cause)
		if _, ok := cause.RedirectFacts(); ok {
			t.Fatal("public error forged private attestation")
		}
		for _, provider := range []Target{TargetVeo, TargetUGC} {
			if strings.Contains(fmt.Sprint(providerFetchLogArgs(provider, run)), "veo_redirect") {
				t.Fatal("unattested diagnostic logged")
			}
		}
		var output bytes.Buffer
		e := &ProductionExecutor{logger: slog.New(slog.NewJSONHandler(&output, nil))}
		e.observe(TargetVeo, ProviderOutcome{}, run, time.Second)
		if strings.Contains(output.String(), "veo_redirect") {
			t.Fatal("forged response fields logged")
		}
		wire, err := json.Marshal(failureDetails(TargetVeo, StageProviderFetch, run, SyncOutcome{}))
		if err != nil || strings.Contains(string(wire), "redirect_") {
			t.Fatal("private fields entered canonical persisted log schema")
		}
	}
}
