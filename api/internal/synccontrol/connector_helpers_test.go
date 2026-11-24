package synccontrol

import (
	"context"

	"messeances/api/internal/cinemovida"
	"messeances/api/internal/cineode"
	"messeances/api/internal/cineville"
	"messeances/api/internal/cinewest"
	"messeances/api/internal/grandecran"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/majestic"
	"messeances/api/internal/megarama"
	"messeances/api/internal/mk2"
	"messeances/api/internal/noecinemas"
	"messeances/api/internal/ocine"
	"messeances/api/internal/pathe"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncsource"
	"messeances/api/internal/ticketingcine"
	"messeances/api/internal/ugc"
	"messeances/api/internal/veo"
)

// testConnectors builds the connector map for executors constructed as
// struct literals in tests.
func testConnectors(connectors ...Connector) map[Target]Connector {
	result := make(map[Target]Connector, len(connectors))
	for _, connector := range connectors {
		result[connector.Target()] = connector
	}
	return result
}

// withSync replaces the synchronizer so tests can return fixture datasets.
func (c *SourceConnector[C, O, S]) withSync(sync func(context.Context, C, O) (schedule.Dataset, S, error)) *SourceConnector[C, O, S] {
	c.sync = sync
	return c
}

// connectorOf returns the executor's typed connector for fresh's target,
// installing fresh when the executor has none yet.
func connectorOf[C, O, S any](e *ProductionExecutor, fresh *SourceConnector[C, O, S]) *SourceConnector[C, O, S] {
	if e.connectors == nil {
		e.connectors = map[Target]Connector{}
	}
	if existing, ok := e.connectors[fresh.target]; ok {
		return existing.(*SourceConnector[C, O, S])
	}
	e.connectors[fresh.target] = fresh
	return fresh
}

func ugcOf(e *ProductionExecutor) *SourceConnector[ugc.Getter, ugc.SyncOptions, ugc.SyncSummary] {
	return connectorOf(e, UGCConnector(nil))
}

func kinepolisOf(e *ProductionExecutor) *SourceConnector[kinepolis.Fetcher, kinepolis.SyncOptions, kinepolis.SyncSummary] {
	return connectorOf(e, KinepolisConnector(nil))
}

func patheOf(e *ProductionExecutor) *SourceConnector[pathe.Getter, pathe.SyncOptions, pathe.SyncSummary] {
	return connectorOf(e, PatheConnector(nil))
}

func megaramaOf(e *ProductionExecutor) *SourceConnector[megarama.Getter, megarama.SyncOptions, megarama.SyncSummary] {
	return connectorOf(e, MegaramaConnector(nil))
}

func cinevilleOf(e *ProductionExecutor) *SourceConnector[cineville.Fetcher, cineville.SyncOptions, cineville.SyncSummary] {
	return connectorOf(e, CinevilleConnector(nil))
}

func mk2Of(e *ProductionExecutor) *SourceConnector[mk2.Fetcher, mk2.SyncOptions, mk2.SyncSummary] {
	return connectorOf(e, MK2Connector(nil))
}

func cinewestOf(e *ProductionExecutor) *SourceConnector[cinewest.Fetcher, cinewest.SyncOptions, cinewest.SyncSummary] {
	return connectorOf(e, CinewestConnector(nil))
}

func grandEcranOf(e *ProductionExecutor) *SourceConnector[grandecran.Getter, grandecran.SyncOptions, grandecran.SyncSummary] {
	return connectorOf(e, GrandEcranConnector(nil))
}

func noeCinemasOf(e *ProductionExecutor) *SourceConnector[noecinemas.Getter, noecinemas.SyncOptions, noecinemas.SyncSummary] {
	return connectorOf(e, NoeCinemasConnector(nil))
}

func veoOf(e *ProductionExecutor) *SourceConnector[veo.Getter, veo.SyncOptions, veo.SyncSummary] {
	return connectorOf(e, VeoConnector(nil))
}

func majesticOf(e *ProductionExecutor) *SourceConnector[majestic.Getter, majestic.SyncOptions, majestic.SyncSummary] {
	return connectorOf(e, MajesticConnector(nil))
}

func cineodeOf(e *ProductionExecutor) *SourceConnector[cineode.Getter, cineode.SyncOptions, cineode.SyncSummary] {
	return connectorOf(e, CineodeConnector(nil))
}

func cinemovidaOf(e *ProductionExecutor) *SourceConnector[cinemovida.Getter, cinemovida.SyncOptions, cinemovida.SyncSummary] {
	return connectorOf(e, CinemovidaConnector(nil))
}

func ocineOf(e *ProductionExecutor) *SourceConnector[ocine.Getter, ocine.SyncOptions, ocine.SyncSummary] {
	return connectorOf(e, OcineConnector(nil))
}

func ticketingcineOf(e *ProductionExecutor) *SourceConnector[ticketingcine.Getter, ticketingcine.SyncOptions, ticketingcine.SyncSummary] {
	return connectorOf(e, TicketingcineConnector(nil, nil))
}

// staticSources lists fixed sources, or fails with err.
type staticSources struct {
	sources []syncsource.Source
	err     error
}

func (s staticSources) Enabled(context.Context, schedule.Provider) ([]syncsource.Source, error) {
	return s.sources, s.err
}
