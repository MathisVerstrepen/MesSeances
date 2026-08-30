package parallel

import (
	"context"
	"errors"
	"sync"
)

var errInvalidWorkerCount = errors.New("parallel: worker count must be positive")

// Options configures parallel mapping.
type Options struct {
	Workers int
}

type indexedJob[J any] struct {
	index int
	value J
}

type indexedResult[R any] struct {
	index int
	value R
}

// MapOrdered applies work concurrently and returns results in job order.
func MapOrdered[J, R any](ctx context.Context, jobs []J, options Options, work func(context.Context, J) (R, error)) ([]R, error) {
	if len(jobs) == 0 {
		return []R{}, nil
	}
	if options.Workers <= 0 {
		return nil, errInvalidWorkerCount
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	queue := make(chan indexedJob[J])
	results := make(chan indexedResult[R], len(jobs))

	var group sync.WaitGroup
	var captureError sync.Once
	var firstError error

	group.Add(options.Workers)
	for range options.Workers {
		go func() {
			defer group.Done()
			for {
				select {
				case <-workCtx.Done():
					return
				case job, ok := <-queue:
					if !ok {
						return
					}
					if workCtx.Err() != nil {
						return
					}
					value, err := work(workCtx, job.value)
					if err != nil {
						captureError.Do(func() {
							firstError = err
							cancel()
						})
						return
					}
					select {
					case results <- indexedResult[R]{index: job.index, value: value}:
					case <-workCtx.Done():
						return
					}
				}
			}
		}()
	}

	group.Add(1)
	go func() {
		defer group.Done()
		defer close(queue)
		for index, job := range jobs {
			select {
			case <-workCtx.Done():
				return
			case queue <- indexedJob[J]{index: index, value: job}:
			}
		}
	}()

	group.Wait()
	close(results)
	if firstError != nil {
		return nil, firstError
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ordered := make([]R, len(jobs))
	for result := range results {
		ordered[result.index] = result.value
	}
	return ordered, nil
}
