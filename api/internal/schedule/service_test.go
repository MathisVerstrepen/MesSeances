package schedule

import (
	"testing"
	"time"
)

func testDataset() Dataset {
	location, _ := time.LoadLocation(Timezone)
	showing := func(id, theater, title, clock, language string, runtime int) ShowtimeRecord {
		start, _ := time.ParseInLocation("2006-01-02 15:04", "2026-08-15 "+clock, location)
		if start.Hour() < 8 {
			start = start.AddDate(0, 0, 1)
		}
		return ShowtimeRecord{ID: "ugc-showing-" + id, ProviderShowingID: id, ServiceDate: "2026-08-15", TheaterID: theater, Movie: MovieRecord{ProviderID: id, Slug: "ugc-film-" + id, Title: title, RuntimeMinutes: runtime}, StartTime: start, EndTime: start.Add(time.Duration(runtime) * time.Minute), Language: language, ProviderVersion: language, Format: "2D", Room: "Salle 1", BookingURL: "https://www.ugc.fr/reservationSeances.html?id=" + id}
	}
	return Dataset{SchemaVersion: 1, Provider: ProviderUGC, Scope: ScopeAll, GeneratedAt: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC), Timezone: Timezone, Window: Window{From: "2026-08-15", Through: "2026-08-15"}, Theaters: []TheaterRecord{{ID: "ugc-25", ProviderID: "25", Slug: "ugc-25", Name: "UGC Lille", Address: "Lille", City: "Lille", PostalCode: "59000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}, {ID: "ugc-26", ProviderID: "26", Slug: "ugc-26", Name: "UGC Villeneuve", Address: "Villeneuve", City: "Villeneuve d'Ascq", PostalCode: "59650", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}, {ID: "ugc-99", ProviderID: "99", Slug: "ugc-99", Name: "UGC Lyon", Address: "Lyon", City: "Lyon", PostalCode: "69000", AvailableDates: []string{"2026-08-15"}, AcceptedPasses: []string{"UGC_ILLIMITE"}}}, Showtimes: []ShowtimeRecord{showing("100", "ugc-25", "Film A", "12:00", LanguageVOSTFR, 100), showing("101", "ugc-25", "Film B", "14:30", LanguageVFSME, 95), showing("102", "ugc-26", "Film C", "00:15", LanguageVO, 75), showing("103", "ugc-99", "Film D", "12:30", LanguageVF, 90)}}
}

type testSource struct{ data Dataset }

func (s testSource) Snapshot() Dataset { return cloneDataset(s.data) }

func testService(t *testing.T) *Service {
	t.Helper()
	source := testSource{data: testDataset()}
	if err := ValidateDataset(source.data, true); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(source, ServiceOptions{DefaultCity: "Lille", CityAliases: map[string][]string{"Lille": {"Lille", "Villeneuve d'Ascq"}}})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestTimelineLilleDefaultAndExplicitFrance(t *testing.T) {
	service := testService(t)
	timeline, err := service.Timeline(TimelineQuery{Date: "2026-08-15", Language: LanguageAll})
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline.Theaters) != 2 {
		t.Fatalf("default theaters=%d", len(timeline.Theaters))
	}
	explicit, err := service.Timeline(TimelineQuery{Date: "2026-08-15", Language: LanguageAll, TheaterIDs: []string{"ugc-99"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit.Theaters) != 1 || explicit.Theaters[0].City != "Lyon" {
		t.Fatalf("explicit=%+v", explicit.Theaters)
	}
	if explicit.Theaters[0].Showtimes[0].BookingURL == nil || explicit.Theaters[0].Showtimes[0].StartTime.Location() != time.UTC {
		t.Fatal("booking URL or UTC conversion missing")
	}
}

func TestSearchSlotStrictBoundariesAndFilters(t *testing.T) {
	service := testService(t)
	tests := []struct {
		name  string
		query SlotQuery
		want  []string
	}{{"inclusive", SlotQuery{"Lille", "2026-08-15", "12:00", "13:40", 0, LanguageAll}, []string{"ugc-showing-100"}}, {"ads exclusion", SlotQuery{"Lille", "2026-08-15", "12:00", "13:40", 20, LanguageAll}, nil}, {"VF includes SME", SlotQuery{"Lille", "2026-08-15", "12:00", "17:00", 0, LanguageVF}, []string{"ugc-showing-101"}}, {"post midnight alias", SlotQuery{"LILLE", "2026-08-15", "00:15", "01:30", 0, LanguageAll}, []string{"ugc-showing-102"}}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results, err := service.SearchSlot(test.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != len(test.want) {
				t.Fatalf("count=%d want=%d", len(results), len(test.want))
			}
			for i := range results {
				if results[i].Showtime.ID != test.want[i] {
					t.Fatalf("id=%s", results[i].Showtime.ID)
				}
			}
		})
	}
}
