package synccontrol

import (
	"context"
	"fmt"
	"time"

	"movieflow/api/internal/enrichment"
	"movieflow/api/internal/kinepolis"
	"movieflow/api/internal/schedule"
	"movieflow/api/internal/syncproxy"
	"movieflow/api/internal/ugc"
)

const (
	requestInterval  = 2 * time.Second
	requestTimeout   = 20 * time.Second
	operationTimeout = 2 * time.Minute
)

type ProductionExecutor struct {
	proxies            []syncproxy.Proxy
	writer             schedule.SnapshotWriter
	enrichmentStore    enrichment.Store
	enrichmentProvider enrichment.Provider
	now                func() time.Time
	newUGC             func([]syncproxy.Proxy) (ugc.Getter, error)
	newKinepolis       func([]syncproxy.Proxy) (kinepolis.Fetcher, error)
	syncUGC            func(context.Context, ugc.Getter, ugc.SyncOptions) (schedule.Dataset, ugc.SyncSummary, error)
	syncKinepolis      func(context.Context, kinepolis.Fetcher, kinepolis.SyncOptions) (schedule.Dataset, kinepolis.SyncSummary, error)
	enrich             func(context.Context, enrichment.Store, enrichment.Provider, []enrichment.Movie) error
}

func NewProductionExecutor(proxies []syncproxy.Proxy, writer schedule.SnapshotWriter, store enrichment.Store, provider enrichment.Provider, now func() time.Time) (*ProductionExecutor, error) {
	if len(proxies) == 0 || writer == nil {
		return nil, fmt.Errorf("sync executor dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	executor := &ProductionExecutor{
		proxies: append([]syncproxy.Proxy(nil), proxies...), writer: writer,
		enrichmentStore: store, enrichmentProvider: provider, now: now,
		newUGC: func(proxies []syncproxy.Proxy) (ugc.Getter, error) {
			return ugc.NewClient(ugc.ClientConfig{Proxies: proxies, RequestInterval: requestInterval, Timeout: requestTimeout})
		},
		newKinepolis: func(proxies []syncproxy.Proxy) (kinepolis.Fetcher, error) {
			return kinepolis.NewClient(kinepolis.ClientConfig{Proxies: proxies, RequestInterval: requestInterval, Timeout: requestTimeout})
		},
		syncUGC: ugc.Sync, syncKinepolis: kinepolis.Sync,
		enrich: func(ctx context.Context, store enrichment.Store, provider enrichment.Provider, movies []enrichment.Movie) error {
			_, err := enrichment.NewMatcher(store, provider, time.Now).Run(ctx, movies)
			return err
		},
	}
	return executor, nil
}

func (e *ProductionExecutor) Run(ctx context.Context, provider Target, window Window) error {
	var data schedule.Dataset
	var err error
	switch provider {
	case TargetUGC:
		client, clientErr := e.newUGC(e.proxies)
		if clientErr != nil {
			return fmt.Errorf("create UGC client")
		}
		data, _, err = e.syncUGC(ctx, client, ugc.SyncOptions{From: window.From, Through: window.Through, Now: e.now()})
	case TargetKinepolis:
		client, clientErr := e.newKinepolis(e.proxies)
		if clientErr != nil {
			return fmt.Errorf("create Kinepolis client")
		}
		data, _, err = e.syncKinepolis(ctx, client, kinepolis.SyncOptions{From: window.From, Through: window.Through, Now: e.now()})
	default:
		return ErrInvalidTarget
	}
	if err != nil {
		return fmt.Errorf("provider sync failed")
	}
	if data.Scope != schedule.ScopeAll || data.Provider != string(provider) || schedule.ValidateDataset(data, true) != nil {
		return fmt.Errorf("provider dataset rejected")
	}
	writeCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	_, err = e.writer.Replace(writeCtx, data)
	cancel()
	if err != nil {
		return fmt.Errorf("provider replacement failed")
	}
	if e.enrichmentStore != nil && e.enrichmentProvider != nil {
		enrichCtx, enrichCancel := context.WithTimeout(ctx, operationTimeout)
		_ = e.enrich(enrichCtx, e.enrichmentStore, e.enrichmentProvider, enrichmentMovies(provider, data))
		enrichCancel()
	}
	return nil
}

func enrichmentMovies(provider Target, data schedule.Dataset) []enrichment.Movie {
	unique := make(map[string]enrichment.Movie)
	for _, showing := range data.Showtimes {
		unique[showing.Movie.ProviderID] = enrichment.Movie{SourceProvider: string(provider), ProviderID: showing.Movie.ProviderID, Title: showing.Movie.Title, RuntimeMinutes: showing.Movie.RuntimeMinutes}
	}
	movies := make([]enrichment.Movie, 0, len(unique))
	for _, movie := range unique {
		movies = append(movies, movie)
	}
	return movies
}
