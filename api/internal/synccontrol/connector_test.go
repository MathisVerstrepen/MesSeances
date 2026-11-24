package synccontrol

import (
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"messeances/api/internal/majestic"
	"messeances/api/internal/schedule"
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
	want := []Target{TargetUGC, TargetKinepolis, TargetPathe, TargetCGR, TargetMegarama, TargetCineville, TargetMK2, TargetCinewest, TargetGrandEcran, TargetNoeCinemas, TargetVeo, TargetMajestic, TargetCineode, TargetCinemovida, TargetOcine}
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
	unknownRuntime := []Target{TargetMegarama, TargetCineville, TargetMK2, TargetCinewest, TargetGrandEcran, TargetNoeCinemas, TargetVeo, TargetMajestic, TargetCineode, TargetCinemovida, TargetOcine}
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
