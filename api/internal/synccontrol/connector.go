package synccontrol

import (
	"context"
	"errors"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncsource"
)

// Connector binds one provider's client factory to its synchronizer. The
// executor treats every provider through this interface; provider-specific
// behaviour lives in the connector definition (connectors.go).
type Connector interface {
	Target() Target
	// NewClient builds a provider client. It exists so wiring can be checked
	// without running a synchronization.
	NewClient() (any, error)
	fetch(ctx context.Context, e *ProductionExecutor, window Window, lines *[]string) (schedule.Dataset, SyncOutcome, error)
}

// fetchBudget selects the context deadline applied to a provider fetch.
type fetchBudget uint8

const (
	// fetchBudgetCaller runs under the caller's context only.
	fetchBudgetCaller fetchBudget = iota
	// fetchBudgetOperation applies the executor operation timeout.
	fetchBudgetOperation
	// fetchBudgetCrawl applies crawlFetchTimeout for per-session crawls.
	fetchBudgetCrawl
)

var errMissingClientFactory = errors.New("sync client factory is not configured")

// ErrNoSources reports a provider without enabled sync sources. Runs skip it
// instead of failing.
var ErrNoSources = errors.New("provider has no enabled sync source")

// SourceLister reads the enabled sources of a source-configured provider.
type SourceLister interface {
	Enabled(ctx context.Context, provider schedule.Provider) ([]syncsource.Source, error)
}

// crawlFetchTimeout bounds providers that resolve per-session pages; they need
// a crawl budget, not the short database/metadata operation timeout.
const crawlFetchTimeout = 25 * time.Minute

// providerTraits are the static, executor-independent properties of a
// provider: failure classification and run policy. They are looked up by
// target so logging and validation never need a live connector.
type providerTraits struct {
	target Target
	budget fetchBudget
	// enrichUnknownRuntime submits movies without a source runtime to TMDB
	// enrichment instead of skipping them.
	enrichUnknownRuntime bool
	// retainLogs attaches the provider journal to successful outcomes.
	retainLogs bool
	// typedFetchError marks errors that stay provider-fetch failures even when
	// they also wrap cancellation or dataset validation.
	typedFetchError func(error) bool
	// classify fills operation, category and HTTP facts for a fetch failure.
	classify func(error, *logFailure)
	// logArgs adds provider-specific, pre-sanitized structured log fields.
	logArgs func(error, logFailure) []any
}

func traitsFor(target Target) (providerTraits, bool) {
	for _, traits := range providerRegistry {
		if traits.target == target {
			return traits, true
		}
	}
	return providerTraits{}, false
}

// SourceConnector adapts a provider package (client factory, Sync function,
// options and summary types) to the Connector interface.
type SourceConnector[C, O, S any] struct {
	target    Target
	newClient func() (C, error)
	sync      func(context.Context, C, O) (schedule.Dataset, S, error)
	options   func(from string, now time.Time) O
	outcome   func(S) SyncOutcome
	// report emits provider diagnostics after the fetch, successful or not.
	report func(e *ProductionExecutor, summary S, err error, lines *[]string)
	// sources, when set, lists the provider's configured cinemas before any
	// request; withSources hands them to the synchronizer options.
	sources     SourceLister
	withSources func(O, []syncsource.Source) O
}

func (c *SourceConnector[C, O, S]) Target() Target { return c.target }

func (c *SourceConnector[C, O, S]) NewClient() (any, error) {
	if c.newClient == nil {
		return nil, errMissingClientFactory
	}
	return c.newClient()
}

func (c *SourceConnector[C, O, S]) fetch(ctx context.Context, e *ProductionExecutor, window Window, lines *[]string) (schedule.Dataset, SyncOutcome, error) {
	var data schedule.Dataset
	var outcome SyncOutcome
	if c.newClient == nil || c.sync == nil {
		return data, outcome, newProviderRunError(c.target, StageClientCreation, FailureInternal, nil)
	}
	var configured []syncsource.Source
	if c.withSources != nil {
		if c.sources == nil {
			return data, outcome, newProviderRunError(c.target, StageClientCreation, FailureInternal, nil)
		}
		loadCtx, cancel := context.WithTimeout(ctx, e.operationTimeout)
		loaded, err := c.sources.Enabled(loadCtx, schedule.Provider(c.target))
		cancel()
		if err != nil {
			return data, outcome, newProviderRunError(c.target, StageClientCreation, FailureClientCreation, err)
		}
		if len(loaded) == 0 {
			return data, outcome, ErrNoSources
		}
		configured = loaded
	}
	client, err := c.newClient()
	if err != nil {
		return data, outcome, newProviderRunError(c.target, StageClientCreation, FailureClientCreation, err)
	}
	*lines = append(*lines, lifecycleLog(e.now().UTC(), c.target, eventClientReady), lifecycleLog(e.now().UTC(), c.target, eventFetchStarted))
	fetchCtx, cancel := ctx, context.CancelFunc(func() {})
	traits, _ := traitsFor(c.target)
	switch traits.budget {
	case fetchBudgetOperation:
		fetchCtx, cancel = context.WithTimeout(ctx, e.operationTimeout)
	case fetchBudgetCrawl:
		fetchCtx, cancel = context.WithTimeout(ctx, crawlFetchTimeout)
	}
	options := c.options(window.From, e.now())
	if c.withSources != nil {
		options = c.withSources(options, configured)
	}
	data, summary, err := c.sync(fetchCtx, client, options)
	cancel()
	if c.report != nil {
		c.report(e, summary, err, lines)
	}
	outcome = c.outcome(summary)
	outcome.Requests = max(outcome.Requests, requestCount(client))
	return data, outcome, err
}

// requestCount reads the transport request counter when the client exposes
// one. Summaries can under-count requests made by failed jobs.
func requestCount(client any) int {
	if counter, ok := client.(interface{ RequestCount() int }); ok {
		return counter.RequestCount()
	}
	return 0
}
