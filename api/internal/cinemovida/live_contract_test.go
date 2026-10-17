package cinemovida

import (
	"context"
	"os"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

func TestProxyFullSyncContractIntegration(t *testing.T) {
	proxyFile, enabled := os.LookupEnv("CINEMOVIDA_LIVE_PROXY_FILE")
	if !enabled {
		t.Skip("set CINEMOVIDA_LIVE_PROXY_FILE to run full proxy-backed Ciné-Movida contract")
	}
	if proxyFile != "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt" {
		t.Fatal("Ciné-Movida live contract requires approved proxy file")
	}
	file, err := os.Open(proxyFile)
	if err != nil {
		t.Fatal("open Ciné-Movida live proxy file")
	}
	defer func() { _ = file.Close() }()
	proxies, err := syncproxy.Parse(file)
	if err != nil {
		t.Fatal("parse Ciné-Movida live proxy file")
	}
	client, err := NewClient(ClientConfig{Proxies: proxies, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	t.Logf("full live sync started=%s", started.UTC().Format(time.RFC3339))
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Minute)
	defer cancel()
	dataset, summary, err := Sync(ctx, client, SyncOptions{From: started.In(location).Format(time.DateOnly), Now: started})
	t.Logf("full live sync finished=%s cinemas=%d movies=%d jobs=%d requests=%d showtimes=%d ends_attempted=%d ends_recovered=%d ends_unresolved=%d ends_budget_skipped=%d", time.Now().UTC().Format(time.RFC3339), summary.Cinemas, summary.Movies, summary.Jobs, summary.Requests, summary.Showtimes, summary.EndsAttempted, summary.EndsRecovered, summary.EndsUnresolved, summary.EndsBudgetSkipped)
	if err != nil {
		t.Fatal(err)
	}
	if len(dataset.Theaters) != 4 || len(dataset.Showtimes) == 0 || summary.Showtimes != len(dataset.Showtimes) || summary.EndsAttempted != len(dataset.Showtimes) || summary.EndsRecovered == 0 || summary.EndsBudgetSkipped != 0 {
		t.Fatal("full live contract missing census, sessions or source-end acquisition")
	}
	ends, midnight := 0, 0
	for _, record := range dataset.Showtimes {
		if record.ServiceDate != record.StartTime.In(location).Format(time.DateOnly) {
			t.Fatal("live calendar date ownership mismatch")
		}
		if record.StartTime.Hour() < 3 {
			midnight++
		}
		if record.EndTime.After(record.StartTime) {
			ends++
		}
	}
	if ends != summary.EndsRecovered || schedule.ValidateDataset(dataset, true) != nil {
		t.Fatal("live source ends or dataset validation mismatch")
	}
	t.Logf("validated source_ends=%d missing_ends=%d midnight_sessions=%d", ends, len(dataset.Showtimes)-ends, midnight)
}
