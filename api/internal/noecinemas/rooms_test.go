package noecinemas

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestRoomsParserAndBudgets(t *testing.T) {
	for _, tc := range []struct{ body, want string }{{"\xe9{\"auditorium_showtime\":\"salle-2\"}", "Salle 2"}, {`{\"auditorium_showtime\":\"salle-3\"}`, "Salle 3"}, {`{"auditorium_showtime":"salle-1"} {"auditorium_showtime":"salle-2"}`, ""}, {`{"auditorium_showtime":"salle-0002"}`, ""}, {`unknown`, ""}} {
		if got := parseRoom([]byte(tc.body)); got != tc.want {
			t.Fatalf("room=%q want=%q", got, tc.want)
		}
	}
	g := fixture(t)
	d, _, err := syncSnapshot(t.Context(), g, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	var missing schedule.ShowtimeRecord
	for _, r := range d.Showtimes {
		if r.Room == "" {
			missing = r
		}
	}
	rows := []schedule.ShowtimeRecord{missing, missing, missing, missing}
	rows[2].Room = "Source"
	rows[3].TheaterID = "noecinemas-B0181"
	var s SyncSummary
	if err = enrichRooms(t.Context(), g, rows, &s, time.Second, 1); err != nil || s.RoomsAttempted != 1 || rows[0].Room != "Salle 2" || rows[1].Room != "Salle 2" || rows[2].Room != "Source" || rows[3].Room != "" {
		t.Fatalf("rooms=%+v err=%v", s, err)
	}
	rows = []schedule.ShowtimeRecord{missing}
	s = SyncSummary{}
	if err = enrichRooms(t.Context(), g, rows, &s, time.Second, 0); err != nil || s.RoomsBudgetSkipped != 1 || s.RoomsAttempted != 0 {
		t.Fatal("limit")
	}
	g.hook = func(op Operation, _ string, b []byte) ([]byte, error) {
		if op == OperationRoom {
			return nil, context.DeadlineExceeded
		}
		return b, nil
	}
	s = SyncSummary{}
	if err = enrichRooms(t.Context(), g, rows, &s, time.Nanosecond, 1); err != nil || rows[0].Room != "" {
		t.Fatal("budget must be optional")
	}
}

func TestRoomWorkersAndParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows := make([]schedule.ShowtimeRecord, 12)
	for i := range rows {
		rows[i] = schedule.ShowtimeRecord{TheaterID: "noecinemas-P8088", BookingURL: fmt.Sprintf("https://achat.cinema-laigle.com/reserver/r/%d", i+1)}
	}
	entered := make(chan struct{}, 12)
	var active, maximum atomic.Int64
	g := getterFunc(func(ctx context.Context, _ Operation, _ string) ([]byte, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	done := make(chan error, 1)
	var summary SyncSummary
	go func() { done <- enrichRooms(ctx, g, rows, &summary, time.Minute, 12) }()
	for range roomWorkers {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("parent cancellation lost")
		}
	case <-time.After(time.Second):
		t.Fatal("workers leaked")
	}
	if maximum.Load() != 4 || active.Load() != 0 || summary.RoomsAttempted != 4 || summary.RoomsBudgetSkipped != 8 {
		t.Fatalf("workers=%d summary=%+v", maximum.Load(), summary)
	}
}
