package schedulepg

import (
	"testing"

	"messeances/api/internal/schedule"
)

// TicketingCiné, SCARE and Webedia cinemas rank and filter as one public
// "independants" chain; other providers keep their own chain.
func TestIndependentChainGroupsStatisticsIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	s := NewStore(pool)
	historyPublish(t, s, testDataset(), ticketingcineTestDataset(t), scareTestDataset(t), webediaTestDataset(t))
	all := historyGet(t, s, schedule.StatisticsQuery{})
	var independent *schedule.HistoryChainRank
	for i, row := range all.Chains {
		switch row.Chain {
		case schedule.ProviderTicketingcine, schedule.ProviderScare, schedule.ProviderWebedia:
			t.Fatalf("independent provider ranked alone: %+v", all.Chains)
		case schedule.ChainIndependent:
			independent = &all.Chains[i]
		}
	}
	if independent == nil || independent.TheaterCount != 3 || independent.ShowtimeCount != 7 || len(all.Chains) != 2 {
		t.Fatalf("chains=%+v", all.Chains)
	}
	filtered := historyGet(t, s, schedule.StatisticsQuery{Chain: string(schedule.ChainIndependent)})
	if filtered.Totals.Theaters != 3 || filtered.Totals.Showtimes != 7 || len(filtered.Chains) != 1 {
		t.Fatalf("filtered=%+v", filtered.Totals)
	}
	single := historyGet(t, s, schedule.StatisticsQuery{Chain: string(schedule.ProviderScare)})
	if single.Totals.Theaters != 1 || single.Totals.Showtimes != 3 {
		t.Fatalf("provider filter=%+v", single.Totals)
	}
}
