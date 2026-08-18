package synccontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"messeances/api/internal/enrichment"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/tmdb"
	"messeances/api/internal/ugc"
)

type writerFunc func(context.Context, schedule.Dataset) (int64, error)

func (f writerFunc) Replace(ctx context.Context, data schedule.Dataset) (int64, error) {
	return f(ctx, data)
}

type unusedGetter struct{}

func (unusedGetter) Get(context.Context, string, string) (ugc.FetchResult, error) {
	return ugc.FetchResult{}, nil
}
func (unusedGetter) RequestCount() int { return 0 }

type unusedFetcher struct{}

func (unusedFetcher) Fetch(context.Context) ([]byte, error) { return nil, nil }

type fakeEnrichmentStore struct{}

func (fakeEnrichmentStore) IsLocallyMerged(context.Context, string, string) (bool, error) {
	return false, nil
}
func (fakeEnrichmentStore) Match(context.Context, string, string, string) (enrichment.Match, bool, error) {
	return enrichment.Match{}, false, nil
}
func (fakeEnrichmentStore) Metadata(context.Context, string, int64, string) (enrichment.Metadata, bool, error) {
	return enrichment.Metadata{}, false, nil
}
func (fakeEnrichmentStore) SaveDecision(context.Context, enrichment.Match) error { return nil }
func (fakeEnrichmentStore) Publish(context.Context, enrichment.Match, enrichment.Metadata) error {
	return nil
}

type fakeEnrichmentProvider struct{}

func (fakeEnrichmentProvider) Search(context.Context, string) ([]tmdb.Candidate, error) {
	return nil, nil
}
func (fakeEnrichmentProvider) Details(context.Context, int64) (tmdb.Details, error) {
	return tmdb.Details{}, nil
}

func TestProductionExecutorCommitsBeforeNonFatalEnrichment(t *testing.T) {
	window := Window{From: "2026-08-17", Through: "2026-08-24"}
	events := []string{}
	executor := &ProductionExecutor{
		proxies: []syncproxy.Proxy{{}}, now: time.Now,
		writer: writerFunc(func(_ context.Context, data schedule.Dataset) (int64, error) {
			events = append(events, "commit:"+data.Provider)
			return 1, nil
		}),
		enrichmentStore: fakeEnrichmentStore{}, enrichmentProvider: fakeEnrichmentProvider{},
		newUGC:       func([]syncproxy.Proxy) (ugc.Getter, error) { return unusedGetter{}, nil },
		newKinepolis: func([]syncproxy.Proxy) (kinepolis.Fetcher, error) { return unusedFetcher{}, nil },
		syncUGC: func(_ context.Context, _ ugc.Getter, options ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
			if options.From != window.From || options.Through != window.Through {
				t.Fatalf("UGC options=%+v", options)
			}
			return validDataset(t, schedule.ProviderUGC, window), ugc.SyncSummary{}, nil
		},
		syncKinepolis: func(_ context.Context, _ kinepolis.Fetcher, options kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error) {
			return validDataset(t, schedule.ProviderKinepolis, window), kinepolis.SyncSummary{}, nil
		},
		enrich: func(_ context.Context, _ enrichment.Store, _ enrichment.Provider, movies []enrichment.Movie) error {
			events = append(events, "enrich:"+movies[0].SourceProvider)
			return errors.New("degraded")
		},
	}
	for _, provider := range []Target{TargetUGC, TargetKinepolis} {
		if err := executor.Run(context.Background(), provider, window); err != nil {
			t.Fatalf("provider=%s err=%v", provider, err)
		}
	}
	want := []string{"commit:ugc", "enrich:ugc", "commit:kinepolis", "enrich:kinepolis"}
	if len(events) != len(want) {
		t.Fatalf("events=%v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events=%v", events)
		}
	}
}

func TestProductionExecutorRejectsInvalidDataAndCommitFailure(t *testing.T) {
	writes := 0
	executor := &ProductionExecutor{
		proxies: []syncproxy.Proxy{{}}, now: time.Now,
		writer: writerFunc(func(context.Context, schedule.Dataset) (int64, error) { writes++; return 0, errors.New("db secret") }),
		newUGC: func([]syncproxy.Proxy) (ugc.Getter, error) { return unusedGetter{}, nil },
		syncUGC: func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
			return schedule.Dataset{}, ugc.SyncSummary{}, nil
		},
	}
	if err := executor.Run(context.Background(), TargetUGC, Window{}); err == nil || writes != 0 {
		t.Fatalf("invalid data err=%v writes=%d", err, writes)
	}
	executor.syncUGC = func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error) {
		return validDataset(t, schedule.ProviderUGC, Window{From: "2026-08-17", Through: "2026-08-24"}), ugc.SyncSummary{}, nil
	}
	if err := executor.Run(context.Background(), TargetUGC, Window{From: "2026-08-17", Through: "2026-08-24"}); err == nil || writes != 1 {
		t.Fatalf("commit err=%v writes=%d", err, writes)
	}
}

func validDataset(t *testing.T, provider string, window Window) schedule.Dataset {
	t.Helper()
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 17, 12, 0, 0, 0, location)
	theaterID, theaterProviderID, movieID, showingID := provider+"-cinema", "cinema", "movie", "showing"
	address, postal, passes := "", "", []string{}
	booking := "https://kinepolis.fr/direct-vista-redirect/showing/0/cinema/0"
	if provider == schedule.ProviderUGC {
		theaterID, theaterProviderID, movieID, showingID = "ugc-25", "25", "200", "100"
		address, postal, passes = "1 rue", "59000", []string{"UGC_ILLIMITE"}
		booking = "https://www.ugc.fr/reservationSeances.html?id=100"
	}
	return schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: provider, Scope: schedule.ScopeAll, GeneratedAt: time.Now().UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: window.From, Through: window.Through},
		Theaters:  []schedule.TheaterRecord{{ID: theaterID, ProviderID: theaterProviderID, Slug: theaterID, Name: "Cinéma", Address: address, City: "Lille", PostalCode: postal, AvailableDates: []string{window.From}, AcceptedPasses: passes}},
		Showtimes: []schedule.ShowtimeRecord{{ID: provider + "-showing-" + showingID, ProviderShowingID: showingID, ServiceDate: window.From, TheaterID: theaterID, Movie: schedule.MovieRecord{ProviderID: movieID, Slug: provider + "-film-" + movieID, Title: "Film", RuntimeMinutes: 100}, StartTime: start, EndTime: start.Add(100 * time.Minute), Language: schedule.LanguageVF, ProviderVersion: "VF", Format: "2D", BookingURL: booking}},
	}
}
