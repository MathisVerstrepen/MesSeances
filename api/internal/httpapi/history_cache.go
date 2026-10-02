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

// HistoryCache holds one immutable, pre-encoded all-time response. Its zero
// value is cold. Requests never populate it or expose its shared bytes.
type HistoryCache struct {
	snapshot   atomic.Pointer[historySnapshot]
	refreshing atomic.Bool
}

// Refresh replaces the snapshot only after a complete successful calculation
// and encoding. Concurrent refreshes are rejected without waiting.
func (c *HistoryCache) Refresh(ctx context.Context, load func(context.Context) (schedule.HistoryStatistics, error)) error {
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
	c.snapshot.Store(&historySnapshot{json: append(data, '\n')})
	return nil
}

func (c *HistoryCache) write(w http.ResponseWriter) {
	if c == nil {
		writeHistoryError(w, schedule.ErrHistoryUnavailable)
		return
	}
	snapshot := c.snapshot.Load()
	if snapshot == nil {
		writeHistoryError(w, schedule.ErrHistoryUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(snapshot.json)
}

func isAllTimeHistory(query schedule.StatisticsQuery) bool {
	return query.Date == "" && query.DateTo == "" && len(query.City) == 0 && len(query.Theater) == 0 &&
		query.Chain == "" && query.Language == "" && query.Format == "" && query.Genre == "" && query.Pass == "" && query.Film == ""
}
