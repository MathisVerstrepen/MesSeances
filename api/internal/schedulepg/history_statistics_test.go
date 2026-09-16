package schedulepg

import (
	"messeances/api/internal/schedule"
	"testing"
)

func TestHistorySafeCounts(t *testing.T) {
	for _, raw := range []string{`{"totals":{"showtimes":9007199254740992}}`, `{"totals":{"showtimes":-1}}`, `{"totals":{"showtimes":9223372036854775808}}`, `{"totals":{"showtimes":1.5}}`} {
		var r schedule.HistoryStatistics
		if err := decodeHistoryJSON([]byte(raw), &r); err == nil {
			t.Fatal("unsafe count accepted", raw)
		}
	}
	var r schedule.HistoryStatistics
	if err := decodeHistoryJSON([]byte(`{"totals":{"showtimes":9007199254740991}}`), &r); err != nil || r.Totals.Showtimes != 9007199254740991 {
		t.Fatal("safe integer boundary", err)
	}
}
