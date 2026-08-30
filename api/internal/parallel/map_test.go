package parallel

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestMapOrderedReturnsNonNilEmptyResult(t *testing.T) {
	called := false
	results, err := MapOrdered(context.Background(), []int{}, Options{}, func(context.Context, int) (int, error) {
		called = true
		return 0, nil
	})
	if err != nil || results == nil || len(results) != 0 || called {
		t.Fatalf("results=%v called=%v err=%v", results, called, err)
	}
}

func TestMapOrderedRejectsNonPositiveWorkerCount(t *testing.T) {
	for name, workers := range map[string]int{"zero": 0, "negative": -1} {
		t.Run(name, func(t *testing.T) {
			results, err := MapOrdered(context.Background(), []int{1}, Options{Workers: workers}, func(context.Context, int) (int, error) {
				t.Fatal("work called")
				return 0, nil
			})
			if results != nil || err == nil || err.Error() != "parallel: worker count must be positive" {
				t.Fatalf("results=%v err=%v", results, err)
			}
		})
	}
}

func TestMapOrderedBoundsConcurrencyAndPreservesOrder(t *testing.T) {
	const workers = 3
	jobs := []int{0, 1, 2, 3}
	started := make(chan int, len(jobs))
	gates := make([]chan struct{}, len(jobs))
	for index := range gates {
		gates[index] = make(chan struct{})
	}
	var active atomic.Int32
	var maximum atomic.Int32
	type outcome struct {
		results []int
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := MapOrdered(context.Background(), jobs, Options{Workers: workers}, func(_ context.Context, job int) (int, error) {
			current := active.Add(1)
			for {
				previous := maximum.Load()
				if current <= previous || maximum.CompareAndSwap(previous, current) {
					break
				}
			}
			started <- job
			<-gates[job]
			active.Add(-1)
			return job * 10, nil
		})
		done <- outcome{results: results, err: err}
	}()

	firstWave := make([]int, 0, workers)
	for range workers {
		firstWave = append(firstWave, <-started)
	}
	if maximum.Load() != workers {
		t.Fatalf("maximum=%d", maximum.Load())
	}
	select {
	case extra := <-started:
		t.Fatalf("job %d started above worker bound", extra)
	default:
	}
	close(gates[firstWave[0]])
	last := <-started
	for _, job := range firstWave[1:] {
		close(gates[job])
	}
	close(gates[last])

	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if !reflect.DeepEqual(result.results, []int{0, 10, 20, 30}) {
		t.Fatalf("results=%v", result.results)
	}
}

func TestMapOrderedFirstErrorCancelsQueuedAndSiblingWork(t *testing.T) {
	const workers = 3
	original := errors.New("original failure")
	started := make(chan int, workers)
	exited := make(chan struct{}, workers-1)
	releaseFailure := make(chan struct{})
	var queuedStarted atomic.Bool
	type outcome struct {
		results []int
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := MapOrdered(context.Background(), []int{0, 1, 2, 3}, Options{Workers: workers}, func(ctx context.Context, job int) (int, error) {
			if job == 3 {
				queuedStarted.Store(true)
				return job, nil
			}
			started <- job
			if job == 0 {
				<-releaseFailure
				return 0, original
			}
			<-ctx.Done()
			exited <- struct{}{}
			return 0, ctx.Err()
		})
		done <- outcome{results: results, err: err}
	}()

	for range workers {
		<-started
	}
	close(releaseFailure)
	result := <-done
	if result.results != nil || !errors.Is(result.err, original) || queuedStarted.Load() {
		t.Fatalf("results=%v err=%v queued=%v", result.results, result.err, queuedStarted.Load())
	}
	for range workers - 1 {
		select {
		case <-exited:
		default:
			t.Fatal("MapOrdered returned before sibling worker exited")
		}
	}
}

func TestMapOrderedParentCancellationWaitsForWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 2)
	exited := make(chan struct{}, 2)
	type outcome struct {
		results []int
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := MapOrdered(ctx, []int{0, 1, 2}, Options{Workers: 2}, func(ctx context.Context, _ int) (int, error) {
			started <- struct{}{}
			<-ctx.Done()
			exited <- struct{}{}
			return 0, nil
		})
		done <- outcome{results: results, err: err}
	}()

	for range 2 {
		<-started
	}
	cancel()
	result := <-done
	if result.results != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("results=%v err=%v", result.results, result.err)
	}
	for range 2 {
		select {
		case <-exited:
		default:
			t.Fatal("MapOrdered returned before canceled worker exited")
		}
	}
}
