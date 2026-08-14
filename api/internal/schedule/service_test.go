package schedule

import "testing"

func TestSearchSlotStrictBoundariesAndFilters(t *testing.T) {
	service, err := NewService()
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	tests := []struct {
		name       string
		query      SlotQuery
		wantIDs    []string
		wantBefore []int
		wantAfter  []int
	}{
		{
			name: "inclusive exact boundaries",
			query: SlotQuery{
				City: "Lille", Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "13:40",
				BufferAds: 0, Language: LanguageAll,
			},
			wantIDs: []string{"seance-lumieres-lille-1200"}, wantBefore: []int{0}, wantAfter: []int{0},
		},
		{
			name: "advertising buffer excludes runtime boundary",
			query: SlotQuery{
				City: "Lille", Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "13:40",
				BufferAds: 20, Language: LanguageAll,
			},
			wantIDs: []string{}, wantBefore: []int{}, wantAfter: []int{},
		},
		{
			name: "language filter",
			query: SlotQuery{
				City: "Lille", Date: "2026-08-15", StartAfter: "12:00", FinishBefore: "17:00",
				BufferAds: 0, Language: LanguageVF,
			},
			wantIDs: []string{"seance-ete-lille-1430"}, wantBefore: []int{150}, wantAfter: []int{55},
		},
		{
			name: "post-midnight belongs to following date",
			query: SlotQuery{
				City: "LILLE", Date: "2026-08-15", StartAfter: "00:15", FinishBefore: "01:30",
				BufferAds: 0, Language: LanguageAll,
			},
			wantIDs: []string{"seance-minuit-villeneuve-0015"}, wantBefore: []int{0}, wantAfter: []int{0},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := service.SearchSlot(test.query)
			if err != nil {
				t.Fatalf("SearchSlot() error = %v", err)
			}
			if len(results) != len(test.wantIDs) {
				t.Fatalf("SearchSlot() result count = %d, want %d", len(results), len(test.wantIDs))
			}
			for i, result := range results {
				if result.Showtime.ID != test.wantIDs[i] {
					t.Errorf("result[%d].Showtime.ID = %q, want %q", i, result.Showtime.ID, test.wantIDs[i])
				}
				if result.SlackBeforeMinutes != test.wantBefore[i] {
					t.Errorf("result[%d].SlackBeforeMinutes = %d, want %d", i, result.SlackBeforeMinutes, test.wantBefore[i])
				}
				if result.SlackAfterMinutes != test.wantAfter[i] {
					t.Errorf("result[%d].SlackAfterMinutes = %d, want %d", i, result.SlackAfterMinutes, test.wantAfter[i])
				}
			}
		})
	}
}
