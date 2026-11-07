package schedulepg

import (
	"reflect"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestTheaterTechnologiesHistoryIntegration(t *testing.T) {
	counter := &historyQueryCounter{}
	pool := newHistoryPool(t, counter)
	store := NewStore(pool)
	var today time.Time
	if err := pool.QueryRow(t.Context(), `SELECT (clock_timestamp() AT TIME ZONE 'Europe/Paris')::date`).Scan(&today); err != nil {
		t.Fatal(err)
	}
	data := testDataset()
	base, _ := time.Parse("2006-01-02", data.Window.From)
	days := int(today.Sub(base).Hours() / 24)
	day := today.Format("2006-01-02")
	data.Window.From, data.Window.Through = day, day
	for i := range data.Theaters {
		data.Theaters[i].AvailableDates = []string{day}
	}
	for i := range data.Showtimes {
		data.Showtimes[i].ServiceDate = day
		data.Showtimes[i].StartTime = data.Showtimes[i].StartTime.AddDate(0, 0, days)
		data.Showtimes[i].EndTime = data.Showtimes[i].EndTime.AddDate(0, 0, days)
	}
	data.Showtimes[0].Format = schedule.FormatIMAX
	if _, err := store.Replace(t.Context(), []Dataset{data}); err != nil {
		t.Fatal(err)
	}
	// Cold history stays out of snapshots; current programming remains usable.
	loaded, _, err := store.Load(t.Context())
	if err != nil || len(loaded.Theaters[0].Technologies) != 0 {
		t.Fatal("pending history leaked", err)
	}
	historyProcess(t, store)
	historyCount(t, pool, `SELECT count(*) FROM theater_technology_summary WHERE theater_id='ugc-25' AND format='IMAX'`, 1)
	// Remove IMAX from the current programme without deleting retained evidence.
	data.Showtimes = data.Showtimes[1:]
	if _, err := store.Replace(t.Context(), []Dataset{data}); err != nil {
		t.Fatal(err)
	}
	historyProcess(t, store)
	source, err := schedule.NewPostgresSource(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}
	service, err := schedule.NewService(source, schedule.ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertFormats := func(want []schedule.Format) {
		t.Helper()
		counter.count.Store(0)
		detail, err := service.TheaterShowtimes(schedule.TheaterShowtimesQuery{Slug: "ugc-25"})
		if err != nil || !reflect.DeepEqual(detail.AvailableFormats, []schedule.Format{schedule.Format2D}) || !reflect.DeepEqual(detail.Technologies, want) {
			t.Fatalf("detail=%+v err=%v", detail, err)
		}
		for _, row := range service.Theaters(schedule.TheaterCatalogQuery{}) {
			if row.ID == "ugc-25" && !reflect.DeepEqual(row.Technologies, want) {
				t.Fatal("directory/detail mismatch")
			}
		}
		if counter.count.Load() != 0 {
			t.Fatal("request queried database")
		}
	}
	assertFormats([]schedule.Format{schedule.FormatIMAX})
	before, err := store.CurrentRevision(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := store.ProcessHistoryStatistics(t.Context()); err != nil || changed {
		t.Fatal("idle worker rewrote summary", changed, err)
	}
	// Exact 12-month boundary is included; it expires the next day without sync.
	var boundary time.Time
	if err := pool.QueryRow(t.Context(), `SELECT ($1::date + interval '12 months')::date`, today).Scan(&boundary); err != nil {
		t.Fatal(err)
	}
	refreshAt := func(date time.Time) bool {
		t.Helper()
		tx, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer rollbackScheduleTx(tx)
		if _, err := tx.Exec(t.Context(), `SELECT 1 FROM screening_statistics_state WHERE singleton FOR UPDATE`); err != nil {
			t.Fatal(err)
		}
		changed, err := refreshTheaterTechnologies(t.Context(), tx, date, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(t.Context()); err != nil {
			t.Fatal(err)
		}
		return changed
	}
	if refreshAt(boundary) {
		t.Fatal("inclusive boundary expired early")
	}
	if !refreshAt(boundary.AddDate(0, 0, 1)) {
		t.Fatal("daily expiry did not publish")
	}
	if err := source.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertFormats([]schedule.Format{})
	after, err := store.CurrentRevision(t.Context())
	if err != nil || after.ScheduleVersion != before.ScheduleVersion || after.EnrichmentVersion != before.EnrichmentVersion || after.TheaterTechnologyVersion <= before.TheaterTechnologyVersion {
		t.Fatal("expiry revision", before, after, err)
	}
	execWithoutProcessing := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), sql); err != nil {
			t.Fatal(err)
		}
	}
	// A correction removes the old classification. A failed publication must
	// preserve the prior summary and leave its counter work pending for retry.
	execWithoutProcessing(`UPDATE screening_history_showtimes SET format='ICE' WHERE theater_id='ugc-25' AND provider_showing_id='100'`)
	execWithoutProcessing(`ALTER TABLE theater_technology_summary ADD CONSTRAINT fail_technology_publish CHECK(format<>'ICE') NOT VALID`)
	if _, err := store.ProcessHistoryStatistics(t.Context()); err == nil {
		t.Fatal("fault injection failed")
	}
	failed, err := store.CurrentRevision(t.Context())
	if err != nil || failed != after {
		t.Fatal("failed summary changed revision", err)
	}
	historyCount(t, pool, `SELECT count(*) FROM theater_technology_summary`, 0)
	var pending int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM screening_statistics_pending`).Scan(&pending); err != nil || pending == 0 {
		t.Fatal("failed summary lost queue", err)
	}
	execWithoutProcessing(`ALTER TABLE theater_technology_summary DROP CONSTRAINT fail_technology_publish`)
	historyProcess(t, store)
	if err := source.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertFormats([]schedule.Format{schedule.FormatICE})
	execWithoutProcessing(`DELETE FROM screening_history_showtimes WHERE theater_id='ugc-25' AND provider_showing_id='100'`)
	historyProcess(t, store)
	if err := source.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertFormats([]schedule.Format{})
}
