package cgr

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const liveProxyFile = "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt"

func TestProxyScheduleContractIntegration(t *testing.T) {
	client := liveContractClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	body, err := client.Get(ctx, OperationCinemas, CinemasURL)
	if err != nil {
		t.Fatal(err)
	}
	cinemas, err := parseCinemas(body)
	if err != nil {
		t.Fatal(err)
	}
	var theater cinema
	for _, candidate := range cinemas {
		if candidate.id == "W8010" {
			theater = candidate
			break
		}
	}
	if theater.id == "" {
		t.Fatal("CGR contract cinema is missing")
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Now().In(location)
	if from.Hour() < 3 {
		from = from.AddDate(0, 0, -1)
	}
	body, err = client.Get(ctx, OperationProgram, programURL(theater.id))
	if err != nil {
		t.Fatal(err)
	}
	program, err := parseProgram(body, from, location)
	if err != nil {
		t.Fatal(err)
	}
	serviceDate := firstProgramDate(program, from.Format("2006-01-02"), "9999-12-31")
	if serviceDate == "" {
		t.Fatal("CGR contract program has no current service date")
	}
	oneDayProgram := filterProgramDates(program, serviceDate, serviceDate)
	ids := make([]string, 0, len(oneDayProgram))
	for id := range oneDayProgram {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 || len(ids) > MovieBatchSize {
		t.Fatalf("CGR contract service-day movie count is outside bounds: %d", len(ids))
	}
	body, err = client.Get(ctx, OperationMovies, moviesURL(ids))
	if err != nil {
		t.Fatal(err)
	}
	movies, err := parseMovies(body)
	if err != nil || len(movies) != len(ids) {
		t.Fatalf("parse CGR contract movies: count=%d err=%v", len(movies), err)
	}
	body, err = client.Get(ctx, OperationSchedule, scheduleURL(theater.id, theater.timeZone, serviceDate, serviceDate))
	if err != nil {
		t.Fatal(err)
	}
	showtimes, err := parseSchedule(body, theater, oneDayProgram, movies, location, serviceDate)
	if err != nil || len(showtimes) == 0 {
		t.Fatalf("parse CGR contract schedule: showtimes=%d err=%v", len(showtimes), err)
	}
}

func TestProxyFullSyncContractIntegration(t *testing.T) {
	client := liveContractClient(t)
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	serviceDay := now.In(location)
	if serviceDay.Hour() < 3 {
		serviceDay = serviceDay.AddDate(0, 0, -1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dataset, summary, err := Sync(ctx, client, SyncOptions{From: serviceDay.Format("2006-01-02"), Now: now})
	if err != nil {
		t.Fatalf("CGR full contract sync failed: cinemas=%d movies=%d jobs=%d requests=%d showtimes=%d err=%v", summary.Cinemas, summary.Movies, summary.Jobs, summary.Requests, summary.Showtimes, err)
	}
	if len(dataset.Theaters) == 0 || len(dataset.Showtimes) == 0 || summary.Showtimes != len(dataset.Showtimes) {
		t.Fatalf("CGR full contract sync is empty: cinemas=%d movies=%d jobs=%d requests=%d showtimes=%d", summary.Cinemas, summary.Movies, summary.Jobs, summary.Requests, summary.Showtimes)
	}
}

func liveContractClient(t *testing.T) *Client {
	t.Helper()
	proxyFile, enabled := os.LookupEnv("CGR_LIVE_PROXY_FILE")
	if !enabled {
		t.Skip("set CGR_LIVE_PROXY_FILE to run proxy-backed CGR contract test")
	}
	if proxyFile != liveProxyFile {
		t.Fatal("CGR live contract test requires approved proxy file")
	}
	file, err := os.Open(proxyFile)
	if err != nil {
		t.Fatal("open CGR live proxy file")
	}
	defer func() { _ = file.Close() }()
	proxies, err := syncproxy.Parse(file)
	if err != nil {
		t.Fatal("parse CGR live proxy file")
	}
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
