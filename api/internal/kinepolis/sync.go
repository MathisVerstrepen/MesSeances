package kinepolis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"messeances/api/internal/schedule"
)

type Fetcher interface {
	Fetch(context.Context) ([]byte, error)
	FetchCinema(context.Context, string) ([]byte, error)
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
		return schedule.Dataset{}, SyncSummary{}, typedKinepolisError(err, OperationSchedule, CategoryTransport)
	}
	data, inventory, err := parseSchedule(body, options.From, options.Now)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, requestError(OperationSchedule, CategoryInvalidPayload, 0, err)
	}
	definitions, err := resolveCinemaDefinitions(inventory, data.Theaters)
	if err != nil {
		return schedule.Dataset{}, SyncSummary{}, requestError(OperationSchedule, CategoryInvalidPayload, 0, err)
	}
	for index := range data.Theaters {
		definition := definitions[data.Theaters[index].ProviderID]
		body, err := fetcher.FetchCinema(ctx, definition.path)
		if err != nil {
			return schedule.Dataset{}, SyncSummary{}, typedKinepolisError(err, OperationCinema, CategoryTransport)
		}
		address, err := parseCinemaDetail(body, definition.detailNames)
		if err != nil {
			return schedule.Dataset{}, SyncSummary{}, requestError(OperationCinema, CategoryInvalidPayload, 0, err)
		}
		data.Theaters[index].Address = address.address
		data.Theaters[index].City = address.city
		data.Theaters[index].PostalCode = address.postalCode
	}
	if err := schedule.ValidateDataset(data, true); err != nil {
		return schedule.Dataset{}, SyncSummary{}, fmt.Errorf("%w: %s", schedule.ErrDatasetValidation, err.Error())
	}
	return data, SyncSummary{Cinemas: len(data.Theaters), Showtimes: len(data.Showtimes), GeneratedAt: data.GeneratedAt}, nil
}

func typedKinepolisError(err error, operation Operation, category ErrorCategory) error {
	var requestErr *RequestError
	if errors.As(err, &requestErr) {
		return err
	}
	return requestError(operation, category, 0, err)
}
