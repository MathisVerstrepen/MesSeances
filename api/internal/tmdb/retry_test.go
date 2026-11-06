package tmdb

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTemporaryServerErrorsRetryButStopResponsesDoNot(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusNotFound} {
		calls := 0
		client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls == 1 {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("synthetic-secret"))
				return
			}
			_, _ = w.Write([]byte(`{"results":[]}`))
		}, "synthetic-secret")
		client.sleep = func(context.Context, time.Duration) error { return nil }
		_, err := client.Search(t.Context(), "Synthetic")
		if status == http.StatusBadGateway {
			if err != nil || calls != 2 {
				t.Fatal("temporary failure not recovered", calls, err)
			}
		} else {
			if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("permanent failure retried or leaked", calls, err)
			}
		}
	}
}

func TestRetryCeilingAndCancellation(t *testing.T) {
	calls := 0
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(http.StatusServiceUnavailable) }, "secret")
	client.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := client.Search(t.Context(), "Synthetic"); err == nil || calls != 3 {
		t.Fatal("unbounded retry", calls, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	calls = 0
	client.sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	if _, err := client.Search(ctx, "Synthetic"); !errors.Is(err, context.Canceled) || calls > 1 {
		t.Fatal("retry ignored cancellation", calls, err)
	}
}
