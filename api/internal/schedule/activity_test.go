package schedule

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestActivityCursorValidation(t *testing.T) {
	e := ActivityEvent{EventID: "9223372036854775806", DetectedAt: time.Date(2026, 10, 1, 12, 1, 2, 123456000, time.UTC)}
	good := EncodeActivityCursor("ugc-25", 9223372036854775807, e)
	if p, err := DecodeActivityCursor(good, "ugc-25"); err != nil || p.LastID != e.EventID {
		t.Fatal(p, err)
	}
	base, _ := DecodeActivityCursor(good, "ugc-25")
	for _, change := range []func(*ActivityPosition){
		func(p *ActivityPosition) { p.Version = 2 }, func(p *ActivityPosition) { p.Slug = "ugc-26" }, func(p *ActivityPosition) { p.UpperID = "0" }, func(p *ActivityPosition) { p.UpperID = "9223372036854775808" }, func(p *ActivityPosition) { p.LastID = "01" }, func(p *ActivityPosition) { p.LastID = "-1" }, func(p *ActivityPosition) { p.UpperID = "1" }, func(p *ActivityPosition) { p.LastDetectedAt = "2026-10-01T12:01:02.123456789Z" }, func(p *ActivityPosition) { p.LastDetectedAt = "2026-10-01T12:01:02+00:00" }, func(p *ActivityPosition) { p.LastDetectedAt = "not-a-date" },
	} {
		p := base
		change(&p)
		data, _ := json.Marshal(p)
		if _, err := DecodeActivityCursor(base64.RawURLEncoding.EncodeToString(data), "ugc-25"); err == nil {
			t.Fatal("accepted", p)
		}
	}
	for _, raw := range []string{"", good + "=", good + "\n", "!", strings.Repeat("x", 1025), base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"unknown":true}`)), base64.RawURLEncoding.EncodeToString([]byte{255})} {
		if _, err := DecodeActivityCursor(raw, "ugc-25"); err == nil {
			t.Fatal("accepted malformed cursor")
		}
	}
	data, _ := base64.RawURLEncoding.DecodeString(good)
	for _, raw := range []string{strings.Replace(string(data), `"v":1`, `"v":1,"v":1`, 1), strings.Replace(string(data), `"v":1`, `"V":1`, 1), string(data) + `{}`} {
		if _, err := DecodeActivityCursor(base64.RawURLEncoding.EncodeToString([]byte(raw)), "ugc-25"); err == nil {
			t.Fatal("ambiguous cursor accepted", raw)
		}
	}
	for _, q := range []TheaterActivityQuery{{Slug: "../ugc-25"}, {Slug: "écran"}, {Slug: strings.Repeat("x", 129)}, {Slug: "ugc-25", Limit: -1}, {Slug: "ugc-25", Limit: 101}} {
		if _, err := NormalizeTheaterActivityQuery(q); err == nil {
			t.Fatal("accepted", q)
		}
	}
	if q, err := NormalizeTheaterActivityQuery(TheaterActivityQuery{Slug: "kinepolis-LOM"}); err != nil || q.Limit != 20 {
		t.Fatal(q, err)
	}
}

func TestActivityCoverageValidationAndClone(t *testing.T) {
	d := testDataset()
	d.Coverage = []PublicationCoverage{{TheaterID: d.Theaters[0].ID, ServiceDate: d.Window.From, Status: CoverageComplete, Basis: CoverageDateResponse}}
	if err := ValidateDataset(d, false); err != nil {
		t.Fatal(err)
	}
	copy := cloneDataset(d)
	copy.Coverage[0].Status = CoverageUnknown
	if d.Coverage[0].Status != CoverageComplete {
		t.Fatal("coverage shared across clones")
	}
	for _, c := range []PublicationCoverage{{TheaterID: "ugc-99999", ServiceDate: d.Window.From, Status: CoverageComplete, Basis: CoverageDateResponse}, {TheaterID: d.Theaters[0].ID, ServiceDate: "2026-02-30", Status: CoverageComplete, Basis: CoverageDateResponse}, {TheaterID: d.Theaters[0].ID, ServiceDate: d.Window.From, Status: "guessed", Basis: CoverageDateResponse}, {TheaterID: d.Theaters[0].ID, ServiceDate: d.Window.From, Status: CoverageComplete, Basis: CoverageUnproven}} {
		copy := cloneDataset(d)
		copy.Coverage = []PublicationCoverage{c}
		if err := ValidateDataset(copy, false); err == nil {
			t.Fatal("accepted", c)
		}
	}
	d.Coverage = append(d.Coverage, d.Coverage[0])
	if err := ValidateDataset(d, false); err == nil {
		t.Fatal("duplicate coverage accepted")
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Coverage") || strings.Contains(string(data), CoverageDateResponse) {
		t.Fatal("acquisition evidence leaked to snapshot")
	}
	var loaded Dataset
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Coverage != nil {
		t.Fatal("snapshot reconstructed evidence")
	}
	coverage := ProgramCoverage("ugc-25", map[string][]string{"a": {"2026-10-01", "2026-10-02"}, "b": {"2026-10-01"}}, map[string]bool{"2026-10-01": true})
	if len(coverage) != 2 {
		t.Fatal(coverage)
	}
	for _, c := range coverage {
		if c.ServiceDate == "2026-10-01" && c.Status != CoverageUnknown {
			t.Fatal("mixed positive data certified")
		}
	}
}
