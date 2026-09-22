package schedulepg

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"messeances/api/internal/schedule"
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

func TestHistoryDailyShowtimesSafeCounts(t *testing.T) {
	for _, count := range []string{"9007199254740992", "-1", "9223372036854775808", "1.5"} {
		t.Run(count, func(t *testing.T) {
			raw := fmt.Sprintf(`{"daily_showtimes":[{"date":"2026-08-15","showtime_count":%s}]}`, count)
			var r schedule.HistoryStatistics
			if err := decodeHistoryJSON([]byte(raw), &r); err == nil {
				t.Fatal("unsafe daily count accepted", raw)
			}
		})
	}
}

func TestHistoryDailyShowtimesJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want []schedule.HistoryDailyShowtimes
	}{
		{"empty", `[]`, []schedule.HistoryDailyShowtimes{}},
		{"counts", `[{"date":"2026-08-15","showtime_count":9007199254740991},{"date":"2026-08-16","showtime_count":0},{"date":"2026-08-17","showtime_count":2}]`, []schedule.HistoryDailyShowtimes{
			{Date: "2026-08-15", ShowtimeCount: 9007199254740991},
			{Date: "2026-08-16", ShowtimeCount: 0},
			{Date: "2026-08-17", ShowtimeCount: 2},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r schedule.HistoryStatistics
			if err := decodeHistoryJSON([]byte(`{"daily_showtimes":`+tc.raw+`}`), &r); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r.DailyShowtimes, tc.want) {
				t.Fatalf("daily showtimes=%+v want=%+v", r.DailyShowtimes, tc.want)
			}
			raw, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["daily_showtimes"]) != tc.raw {
				t.Fatalf("serialized daily showtimes=%s want=%s", fields["daily_showtimes"], tc.raw)
			}
		})
	}
}

func TestHistoryChainsSafeCounts(t *testing.T) {
	for _, field := range []string{"showtime_count", "movie_count", "theater_count"} {
		for _, count := range []string{"9007199254740992", "-1", "9223372036854775808", "1.5"} {
			t.Run(field+"/"+count, func(t *testing.T) {
				raw := fmt.Sprintf(`{"chains":[{"chain":"ugc",%q:%s}]}`, field, count)
				var r schedule.HistoryStatistics
				if err := decodeHistoryJSON([]byte(raw), &r); err == nil {
					t.Fatal("unsafe chain count accepted", raw)
				}
			})
		}
	}
}

func TestHistoryChainsJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want []schedule.HistoryChainRank
	}{
		{"empty", `[]`, []schedule.HistoryChainRank{}},
		{"counts", `[{"chain":"ugc","showtime_count":9007199254740991,"movie_count":9007199254740991,"theater_count":9007199254740991},{"chain":"kinepolis","showtime_count":2,"movie_count":1,"theater_count":1}]`, []schedule.HistoryChainRank{
			{Chain: schedule.ProviderUGC, ShowtimeCount: 9007199254740991, MovieCount: 9007199254740991, TheaterCount: 9007199254740991},
			{Chain: schedule.ProviderKinepolis, ShowtimeCount: 2, MovieCount: 1, TheaterCount: 1},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r schedule.HistoryStatistics
			if err := decodeHistoryJSON([]byte(`{"chains":`+tc.raw+`}`), &r); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r.Chains, tc.want) {
				t.Fatalf("chains=%+v want=%+v", r.Chains, tc.want)
			}
			raw, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["chains"]) != tc.raw {
				t.Fatalf("serialized chains=%s want=%s", fields["chains"], tc.raw)
			}
		})
	}
}
