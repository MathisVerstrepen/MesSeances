package kinepolis

import (
	"context"
	"time"

	"messeances/api/internal/schedule"
)

type Fetcher interface {
	Fetch(context.Context) ([]byte, error)
}
type SyncOptions struct {
	From string
	Now  time.Time
}
type SyncSummary struct {
	Cinemas, Showtimes int
	GeneratedAt        time.Time
}

func Sync(ctx context.Context, fetcher Fetcher, options SyncOptions) (schedule.Dataset, SyncSummary, error) {
	body, err := fetcher.Fetch(ctx)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	data, err := Parse(body, options.From, options.Now)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, err
	}
	return data, SyncSummary{Cinemas: len(data.Theaters), Showtimes: len(data.Showtimes), GeneratedAt: data.GeneratedAt}, nil
}
