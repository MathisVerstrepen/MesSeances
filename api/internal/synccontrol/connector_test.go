package synccontrol

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"messeances/api/internal/majestic"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/syncsource"
	"messeances/api/internal/ticketingcine"
	"messeances/api/internal/veo"
)

func TestProviderRegistryCoversEverySourceProviderOnce(t *testing.T) {
	seen := map[Target]bool{}
	for _, traits := range providerRegistry {
		if seen[traits.target] {
			t.Fatalf("duplicate traits for %s", traits.target)
		}
		seen[traits.target] = true
		if !isProviderTarget(traits.target) {
			t.Fatalf("traits for unknown provider %s", traits.target)
		}
		if traits.classify == nil {
			t.Fatalf("missing failure classification for %s", traits.target)
		}
	}
	for _, provider := range schedule.SourceProviders() {
		if !seen[Target(provider)] {
			t.Fatalf("missing traits for %s", provider)
		}
	}
}

func TestProviderTargetsFollowCanonicalOrder(t *testing.T) {
	want := []Target{TargetUGC, TargetKinepolis, TargetPathe, TargetCGR, TargetMegarama, TargetCineville, TargetMK2, TargetCinewest, TargetGrandEcran, TargetNoeCinemas, TargetVeo, TargetMajestic, TargetCineode, TargetCinemovida, TargetOcine, TargetTicketingcine}
	if got := ProviderTargets(); !slices.Equal(got, want) {
		t.Fatalf("targets=%v", got)
	}
	if !slices.Equal(runTargets(TargetAll), want) || !slices.Equal(runTargets(TargetMK2), []Target{TargetMK2}) {
		t.Fatal("run targets")
	}
	if !ValidTarget(TargetAll) || !ValidTarget(TargetOcine) || ValidTarget("combined") || ValidTarget("") {
		t.Fatal("target validity")
	}
}

func TestProviderTraitsPreserveRunPolicies(t *testing.T) {
	unknownRuntime := []Target{TargetMegarama, TargetCineville, TargetMK2, TargetCinewest, TargetGrandEcran, TargetNoeCinemas, TargetVeo, TargetMajestic, TargetCineode, TargetCinemovida, TargetOcine, TargetTicketingcine}
	budgets := map[Target]fetchBudget{TargetCineode: fetchBudgetOperation, TargetCinemovida: fetchBudgetCrawl, TargetOcine: fetchBudgetCrawl}
	for _, target := range ProviderTargets() {
		traits, ok := traitsFor(target)
		if !ok {
			t.Fatalf("missing traits for %s", target)
		}
		if traits.enrichUnknownRuntime != slices.Contains(unknownRuntime, target) {
			t.Fatalf("%s unknown-runtime enrichment=%t", target, traits.enrichUnknownRuntime)
		}
		want, ok := budgets[target]
		if !ok {
			want = fetchBudgetCaller
		}
		if traits.budget != want {
			t.Fatalf("%s budget=%d", target, traits.budget)
		}
		if traits.retainLogs != (target == TargetVeo) || (traits.logArgs != nil) != (target == TargetVeo) {
			t.Fatalf("%s log policy", target)
		}
		if (traits.typedFetchError != nil) != (target == TargetKinepolis) {
			t.Fatalf("%s typed fetch error policy", target)
		}
	}
}

func TestNewProductionExecutorValidatesConnectors(t *testing.T) {
	base := ProductionExecutorOptions{Writer: writerFunc(nil), Now: time.Now, Logger: slog.New(slog.DiscardHandler), OperationTimeout: time.Second}
	newMajestic := func() (majestic.Getter, error) { return unusedMajesticGetter{}, nil }
	for name, connectors := range map[string][]Connector{
		"empty":     nil,
		"nil":       {nil},
		"duplicate": {MajesticConnector(newMajestic), MajesticConnector(newMajestic)},
		"unknown":   {&SourceConnector[majestic.Getter, majestic.SyncOptions, majestic.SyncSummary]{target: "combined"}},
	} {
		options := base
		options.Connectors = connectors
		if _, err := NewProductionExecutor(options); err == nil {
			t.Fatalf("%s connectors accepted", name)
		}
	}
	options := base
	options.Connectors = []Connector{MajesticConnector(newMajestic)}
	executor, err := NewProductionExecutor(options)
	if err != nil || len(executor.connectors) != 1 {
		t.Fatalf("executor=%v err=%v", executor, err)
	}
	if _, err := MajesticConnector(nil).NewClient(); err == nil {
		t.Fatal("missing factory produced a client")
	}
}

func TestProductionExecutorMissingConnectorFailsClientCreation(t *testing.T) {
	executor := &ProductionExecutor{now: time.Now, logger: slog.New(slog.DiscardHandler), operationTimeout: time.Second, writer: writerFunc(nil)}
	_, err := executor.Run(t.Context(), TargetMajestic, Window{From: "2026-08-17"})
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Stage != StageClientCreation || runErr.Code != FailureInternal {
		t.Fatalf("err=%v", err)
	}
	_, err = executor.Run(t.Context(), "combined", Window{From: "2026-08-17"})
	if !errors.As(err, &runErr) || runErr.Stage != StageOrchestration {
		t.Fatalf("invalid target err=%v", err)
	}
}

func TestClassifyProxyRequestMapsPhases(t *testing.T) {
	for _, test := range []struct {
		name         string
		classify     func(error, *logFailure)
		err          error
		wantOp       logOperation
		wantCategory logCategory
		wantStatus   int
	}{
		{"cinemas", classifyProxy, &syncproxy.RequestError{Operation: syncproxy.Operation{Phase: syncproxy.PhaseCinemas}, Kind: syncproxy.FailureChallenge, StatusCode: 403}, operationCinemas, categoryChallenge, 403},
		{"movies", classifyProxy, &syncproxy.RequestError{Operation: syncproxy.Operation{Phase: syncproxy.PhaseMovies}, Kind: syncproxy.FailureTransport}, operationMovies, categoryTransport, 0},
		{"showings", classifyProxy, &syncproxy.RequestError{Operation: syncproxy.Operation{Phase: syncproxy.PhaseShowings}, Kind: syncproxy.FailureServer, StatusCode: 503}, operationShowings, categoryHTTPStatus, 503},
		{"unnamed phase stays unknown", classifyProxy, &syncproxy.RequestError{Kind: syncproxy.FailureRedirect}, operationUnknown, categoryRedirect, 0},
		{"parse failure stays unknown", classifyProxy, errors.New("parse"), operationUnknown, categoryUnknown, 0},
		{"default program phase", classifyProgramProxy, &syncproxy.RequestError{Kind: syncproxy.FailureStatus, StatusCode: 404}, operationProgram, categoryHTTPStatus, 404},
		{"program proxy parse failure", classifyProgramProxy, errors.New("parse"), operationUnknown, categoryUnknown, 0},
		{"program pages parse failure", classifyProgramPages, errors.New("parse"), operationProgram, categoryInvalidPayload, 0},
		{"program pages request", classifyProgramPages, &syncproxy.RequestError{Kind: syncproxy.FailureChallenge}, operationProgram, categoryChallenge, 0},
		{"veo errors are not shared errors", classifyProxy, &veo.RequestError{Operation: veo.OperationCheckout, Kind: syncproxy.FailureChallenge}, operationUnknown, categoryUnknown, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			details := logFailure{Operation: operationUnknown, Category: categoryUnknown}
			test.classify(test.err, &details)
			if details.Operation != test.wantOp || details.Category != test.wantCategory || details.HTTPStatus != test.wantStatus {
				t.Fatalf("details=%+v", details)
			}
		})
	}
}

func sourceTestExecutor(t *testing.T, sources SourceLister) (*ProductionExecutor, *int) {
	t.Helper()
	clients := 0
	executor := &ProductionExecutor{now: time.Now, logger: slog.New(slog.DiscardHandler), operationTimeout: time.Second, writer: writerFunc(func(context.Context, []schedule.Dataset) (int64, error) {
		t.Fatal("source-less provider published")
		return 0, nil
	})}
	connector := ticketingcineOf(executor)
	connector.sources = sources
	connector.newClient = func() (ticketingcine.Getter, error) {
		clients++
		return nil, errors.New("synthetic client")
	}
	return executor, &clients
}

func TestSourceConnectorSkipsProvidersWithoutEnabledSources(t *testing.T) {
	window := Window{From: "2026-08-17"}
	executor, clients := sourceTestExecutor(t, staticSources{})
	if _, err := executor.Run(t.Context(), TargetTicketingcine, window); !errors.Is(err, ErrNoSources) || *clients != 0 {
		t.Fatalf("err=%v clients=%d", err, *clients)
	}
	manager, err := newTestManager(t.Context(), time.Now, executor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	if _, err := manager.Start(TargetTicketingcine); err != nil {
		t.Fatal(err)
	}
	status := waitForTerminal(t, manager)
	if status.State != StateSucceeded || status.Providers[string(TargetTicketingcine)].State != ProviderSkipped || status.Providers[string(TargetTicketingcine)].Outcome != nil {
		t.Fatalf("status=%+v", status)
	}
}

func TestSourceConnectorReportsSourceLoadFailures(t *testing.T) {
	executor, clients := sourceTestExecutor(t, staticSources{err: errors.New("database-secret")})
	_, err := executor.Run(t.Context(), TargetTicketingcine, Window{From: "2026-08-17"})
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Stage != StageClientCreation || runErr.Code != FailureClientCreation || *clients != 0 {
		t.Fatalf("err=%v clients=%d", err, *clients)
	}
	executor, _ = sourceTestExecutor(t, nil)
	if _, err := executor.Run(t.Context(), TargetTicketingcine, Window{From: "2026-08-17"}); !errors.As(err, &runErr) || runErr.Code != FailureInternal {
		t.Fatalf("missing source lister err=%v", err)
	}
}

func TestSourceConnectorPassesEnabledSourcesToSync(t *testing.T) {
	executor, _ := sourceTestExecutor(t, staticSources{sources: []syncsource.Source{{Key: "EMS1234"}, {Key: "EMS0042"}}})
	connector := ticketingcineOf(executor)
	connector.newClient = func() (ticketingcine.Getter, error) { return nil, nil }
	var got []ticketingcine.Source
	connector.sync = func(_ context.Context, _ ticketingcine.Getter, options ticketingcine.SyncOptions) (schedule.Dataset, ticketingcine.SyncSummary, error) {
		got = options.Sources
		return schedule.Dataset{}, ticketingcine.SyncSummary{}, errors.New("stop after options")
	}
	if _, err := executor.Run(t.Context(), TargetTicketingcine, Window{From: "2026-08-17"}); err == nil {
		t.Fatal("expected synthetic sync failure")
	}
	if len(got) != 2 || got[0].SiteID != "EMS1234" || got[1].SiteID != "EMS0042" {
		t.Fatalf("sources=%+v", got)
	}
}
