package enrichment

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"

	"messeances/api/internal/tmdb"
)

// Public parsed TMDB FR release pages, frozen 2026-09-13; selected dates from
// schedule:37;enrichment:1002;paris:2026-09-13, generated 2026-09-13T14:03:40Z.
// These are measured hint counts, not precision estimates or exclusion labels.
func TestUpcomingReviewGolden312(t *testing.T) {
	data, err := os.ReadFile("testdata/upcoming_review_2026-09-13.json")
	if err != nil {
		t.Fatal(err)
	}
	var movies []struct {
		ID   int64                   `json:"tmdb_id"`
		Date string                  `json:"selected_date"`
		Rows []tmdb.FrenchReleaseRow `json:"french_releases"`
	}
	if err := json.Unmarshal(data, &movies); err != nil {
		t.Fatal(err)
	}
	seen, reasonsByID := map[int64]bool{}, map[int64][]string{}
	counts, union, tv := map[string]int{}, 0, 0
	for _, m := range movies {
		if seen[m.ID] {
			t.Fatalf("duplicate %d", m.ID)
		}
		seen[m.ID] = true
		evidence, err := tmdb.NormalizeFrenchReleases(m.Rows)
		if err != nil || evidence.FrenchReleaseDate != m.Date || !reflect.DeepEqual(evidence.Rows, m.Rows) {
			t.Fatalf("evidence mismatch %d: %+v %v", m.ID, evidence, err)
		}
		reasons := AssessUpcoming(m.Rows, m.Date)
		reasonsByID[m.ID] = reasons
		if len(reasons) > 0 {
			union++
		}
		for _, r := range reasons {
			counts[r]++
		}
		for _, row := range m.Rows {
			if row.Type == 6 && row.Date <= m.Date {
				tv++
				break
			}
		}
	}
	want := map[string]int{ReasonLimitedOnly: 21, ReasonNonTheatrical: 3, ReasonBroadcaster: 1, ReasonSingleScreening: 1}
	if len(seen) != 312 || !reflect.DeepEqual(counts, want) || union != 23 || tv != 2 {
		t.Fatalf("records=%d counts=%v union=%d tv=%d", len(seen), counts, union, tv)
	}
	for id, want := range map[int64][]string{1240889: {ReasonLimitedOnly}, 1491788: {ReasonNonTheatrical}, 1765345: {ReasonNonTheatrical, ReasonBroadcaster}, 1739423: {ReasonLimitedOnly, ReasonNonTheatrical}, 1769097: {ReasonLimitedOnly, ReasonSingleScreening}, 1191818: {}, 1488459: {}} {
		if !seen[id] || !slices.Equal(reasonsByID[id], want) {
			t.Fatalf("counterexample %d: %v", id, reasonsByID[id])
		}
	}
}

func TestUpcomingReviewPredicates(t *testing.T) {
	for _, test := range []struct {
		name string
		rows []tmdb.FrenchReleaseRow
		want []string
	}{
		{"national and limited", []tmdb.FrenchReleaseRow{{Type: 2}, {Type: 3}}, []string{}},
		{"premiere not TV", []tmdb.FrenchReleaseRow{{Type: 1, Date: "2000-01-01", Note: "France 2 séance unique"}, {Type: 3}}, []string{}},
		{"later TV", []tmdb.FrenchReleaseRow{{Type: 6, Date: "2027-01-01"}, {Type: 3}}, []string{}},
		{"digital same day", []tmdb.FrenchReleaseRow{{Type: 4, Date: "2026-10-01"}, {Type: 3}}, []string{ReasonNonTheatrical}},
		{"physical same day", []tmdb.FrenchReleaseRow{{Type: 5, Date: "2026-10-01"}, {Type: 3}}, []string{ReasonNonTheatrical}},
		{"TV same day", []tmdb.FrenchReleaseRow{{Type: 6, Date: "2026-10-01"}, {Type: 3}}, []string{ReasonNonTheatrical}},
		{"boundaries", []tmdb.FrenchReleaseRow{{Type: 3, Note: "cartel France 200 France festival programme rediffusion séance uniquement"}}, []string{}},
		{"html is plain text", []tmdb.FrenchReleaseRow{{Type: 3, Note: "<script>alert('France 2')</script> SÉANCE UNIQUE"}}, []string{ReasonBroadcaster, ReasonSingleScreening}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := AssessUpcoming(test.rows, "2026-10-01"); !slices.Equal(got, test.want) {
				t.Fatalf("%v != %v", got, test.want)
			}
			if len(AssessUpcoming(test.rows, "")) != 0 {
				t.Fatal("no date has reasons")
			}
		})
	}
	for _, name := range []string{"France 2", "FRANCE\t24", "France.tv", "France télévisions", "France television", "Franceinfo", "Arte", "TF1", "TMC", "M6", "W9", "Canal+", "C8", "CStar", "BFM", "RTL", "Europe 1", "RFI", "Radio France", "Public Senat", "LCP", "TV5"} {
		if !broadcasterNote.MatchString("("+name+")") || broadcasterNote.MatchString("x"+name+"x") {
			t.Errorf("boundary %s", name)
		}
	}
}
