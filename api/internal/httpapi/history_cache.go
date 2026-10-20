package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"

	"messeances/api/internal/schedule"
)

type historySnapshot struct {
	json []byte
}

// HistoryCache holds independent immutable, pre-encoded preset responses. Its zero
// value is cold. Requests never populate it or expose its shared bytes.
type HistoryCache struct {
	snapshots  [4]atomic.Pointer[historySnapshot]
	refreshing atomic.Bool
}

// Refresh replaces the snapshot only after a complete successful calculation
// and encoding. Concurrent refreshes are rejected without waiting.
func (c *HistoryCache) Refresh(ctx context.Context, period schedule.HistoryPeriod, load func(context.Context) (schedule.HistoryStatistics, error)) error {
	index := historyPeriodIndex(period)
	if index < 0 {
		return &schedule.ValidationError{Message: "La période est invalide."}
	}
	if !c.refreshing.CompareAndSwap(false, true) {
		return schedule.ErrHistoryBusy
	}
	defer c.refreshing.Store(false)
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := load(ctx)
	if err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.snapshots[index].Store(&historySnapshot{json: append(data, '\n')})
	return nil
}

func (c *HistoryCache) write(w http.ResponseWriter, period schedule.HistoryPeriod) {
	index := historyPeriodIndex(period)
	if c == nil || index < 0 {
		writeHistoryError(w, schedule.ErrHistoryUnavailable)
		return
	}
	snapshot := c.snapshots[index].Load()
	if snapshot == nil {
		writeHistoryError(w, schedule.ErrHistoryUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(snapshot.json)
}

func isUnfilteredHistory(query schedule.StatisticsQuery) bool {
	return len(query.City) == 0 && len(query.Theater) == 0 &&
		query.Chain == "" && query.Language == "" && query.Format == "" && query.Genre == "" && query.Pass == "" && query.Film == ""
}

func historyPeriodIndex(period schedule.HistoryPeriod) int {
	for i, candidate := range schedule.HistoryPeriods() {
		if period == candidate {
			return i
		}
	}
	return -1
}
