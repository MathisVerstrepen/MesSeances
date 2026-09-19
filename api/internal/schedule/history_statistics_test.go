package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestHistoryDirectValidation(t *testing.T) {
	for _, format := range []string{"INFINITY_VISION", "ICE"} {
		q, err := NormalizeHistoryQuery(StatisticsQuery{Format: format})
		if err != nil || q.Format != format {
			t.Fatal(q, err)
		}
	}
	for _, format := range []string{"infinity_vision", "Infinity Vision", "invented", "ALL"} {
		if _, err := NormalizeHistoryQuery(StatisticsQuery{Format: format}); err == nil {
			t.Fatal("accepted", format)
		}
	}
	for _, q := range []StatisticsQuery{{Date: "0001-01-01", DateTo: "9999-12-31"}, {Date: "1900-01-01"}, {Genre: strings.Repeat("é", 100)}, {Language: "VF_SME"}, {Pass: "unknown"}} {
		if _, err := NormalizeHistoryQuery(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, q := range []StatisticsQuery{{Date: "0000-01-01"}, {DateTo: "2026-01-01"}, {Date: "2026-02-30"}, {Date: " 2026-01-01"}, {Date: "2026-02-01", DateTo: "2026-01-01"}, {City: []string{""}}, {City: strings.Fields(strings.Repeat("x ", 51))}, {Theater: []string{strings.Repeat("x", 201)}}, {Genre: strings.Repeat("é", 100) + " "}, {Pass: "\xff"}, {Chain: "all"}, {Language: "VFSME"}, {Format: "imax"}} {
		if _, err := NormalizeHistoryQuery(q); err == nil {
			t.Fatal("accepted", q)
		}
	}
	for _, q := range []HistoryOptionsQuery{{Kind: "bad"}, {Kind: "city", Q: " "}, {Kind: "city", Q: "\xff"}, {Kind: "city", Selected: []string{""}}, {Kind: "city", Selected: strings.Fields(strings.Repeat("x ", 51))}} {
		if _, err := NormalizeHistoryOptionsQuery(q); err == nil {
			t.Fatal("accepted", q)
		}
	}
}

func TestHistoryEmptyPublicationValidation(t *testing.T) {
	d := testDataset()
	d.Theaters = nil
	d.Showtimes = nil
	if _, err := PreparePublication(d); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDataset(d, true); err == nil {
		t.Fatal("ordinary ingestion nonempty guard relaxed")
	}
	for _, modify := range []func(*Dataset){func(d *Dataset) { d.Scope = ScopeSingle }, func(d *Dataset) { d.Window.From = "bad" }, func(d *Dataset) { d.GeneratedAt = time.Time{} }, func(d *Dataset) { d.Provider = "bad" }} {
		invalid := d
		modify(&invalid)
		if _, err := PreparePublication(invalid); err == nil {
			t.Fatal("invalid empty publication accepted")
		}
	}
}

func TestHistoryTheaterCityIdentities(t *testing.T) {
	theaters := []TheaterRecord{{City: "Évreux"}, {City: " evreux "}, {City: "LILLE"}, {City: "lille"}, {City: "\u2003Lille\u00a0"}, {City: "Σ"}, {City: "ς"}}
	identities := TheaterCityIdentities(theaters)
	if len(identities) != len(theaters) || identities[0].Slug == identities[1].Slug || identities[2] != identities[3] || identities[2] != identities[4] || identities[5] != identities[6] {
		t.Fatal(identities)
	}
	reversed := make([]TheaterRecord, len(theaters))
	for i := range theaters {
		reversed[len(theaters)-1-i] = theaters[i]
	}
	other := TheaterCityIdentities(reversed)
	for i := range identities {
		if identities[i] != other[len(other)-1-i] {
			t.Fatal("city identities depend on input order")
		}
	}
	data := testDataset()
	data.Theaters[0].City = "Lille"
	data.Theaters[1].City = "\u2003LILLE\u00a0"
	data.Theaters[2].City = "lille"
	view := NewSnapshotView(data)
	if len(view.cityBuckets) != 1 || view.cityBuckets[0].slug != "lille" || len(view.cityBuckets[0].catalogPositions) != 3 {
		t.Fatal("whitespace/case variants created duplicate city slugs", view.cityBuckets)
	}
}
