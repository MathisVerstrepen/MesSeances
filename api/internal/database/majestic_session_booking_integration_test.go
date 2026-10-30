package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestMajesticSessionBookingMigrationIntegration(t *testing.T) {
	for _, baseline := range []string{"fresh", "upgrade071"} {
		t.Run(baseline, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool, _ := newMigrationTestPool(t, ctx, "majestic_session_booking_")
			const booking = "https://www.ticketingcine.com?EMS0700#showsession?id=emsx070000000001"
			if baseline == "upgrade071" {
				installMigrationPrefix(t, ctx, pool, 71, "071_incremental_history_statistics.sql")
				var accepted bool
				if err := pool.QueryRow(ctx, `SELECT majestic_booking_valid($1,'EMS0700','emsx070000000001','ABCDE')`, booking).Scan(&accepted); err != nil || accepted {
					t.Fatal("071 unexpectedly accepts global site/session booking", err)
				}
			}
			for range 2 {
				if err := RunMigrations(ctx, pool); err != nil {
					t.Fatal(err)
				}
			}
			assertCompleteMigrationHistory(t, ctx, pool, mustEmbeddedMigrations(t))
			for _, movie := range []string{"ABCDE", "EMS0700-emsx0700HC12", "EMS0654-emsx0654HC12", "invalid"} {
				for _, raw := range []string{booking, booking + "&ps=ticketingcine", booking + "&extra=1", booking + "&id=emsx070000000002", strings.Replace(booking, "?EMS0700", "?EMS0654", 1), strings.Replace(booking, "?EMS0700", "/?EMS0700", 1), strings.Replace(booking, "?EMS0700", "?EMS0700=", 1), strings.Replace(booking, "https:", "http:", 1), strings.Replace(booking, ".com?", ".com:443?", 1), "https://www.ticketingcine.com/EMS0700/FRABCDE#showsession?id=emsx070000000001", schedule.MajesticWebsite("EMS0700")} {
					var got bool
					if err := pool.QueryRow(ctx, `SELECT majestic_booking_valid($1,'EMS0700','emsx070000000001',$2)`, raw, movie).Scan(&got); err != nil {
						t.Fatal(err)
					}
					if want := schedule.MajesticBookingMatchesMovie(raw, "EMS0700", "emsx070000000001", movie); got != want {
						t.Fatalf("booking parity: movie=%s accepted=%t want=%t", movie, got, want)
					}
				}
			}
		})
	}
}
