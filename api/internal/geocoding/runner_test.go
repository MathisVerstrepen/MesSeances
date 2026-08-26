package geocoding

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryStore struct {
	theaters  []Theater
	saved     []Location
	selectErr error
	saveErr   error
}

func (s *memoryStore) Select(context.Context, Filters) ([]Theater, error) {
	return s.theaters, s.selectErr
}
func (s *memoryStore) Save(_ context.Context, _ *Location, location Location) (bool, error) {
	if s.saveErr != nil {
		return false, s.saveErr
	}
	s.saved = append(s.saved, location)
	return true, nil
}

type fakeProvider struct {
	candidates []Candidate
	err        error
	calls      int
}

func (p *fakeProvider) Search(context.Context, Query) ([]Candidate, error) {
	p.calls++
	return p.candidates, p.err
}

func TestRunnerMatchDryRunSkipsAndAddressChanges(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	base := Theater{Provider: "ugc", ProviderID: "25", ID: "ugc-25", Address: "40 rue de Béthune", PostalCode: "59000", City: "Lille"}
	manual := base
	manual.ProviderID, manual.ID = "26", "ugc-26"
	manual.Location = &Location{Status: StatusManual}
	unchanged := base
	unchanged.ProviderID, unchanged.ID = "27", "ugc-27"
	unchanged.Location = &Location{Status: StatusMatched, AddressHash: AddressHash(unchanged.Address, unchanged.PostalCode, unchanged.City)}
	stale := base
	stale.ProviderID, stale.ID = "28", "ugc-28"
	stale.Location = &Location{Status: StatusNotFound, AddressHash: AddressHash("old", stale.PostalCode, stale.City)}
	store := &memoryStore{theaters: []Theater{manual, unchanged, base, stale}}
	provider := &fakeProvider{candidates: []Candidate{{Longitude: 3.0612, Latitude: 50.6321, HasCoordinates: true, Label: "40 Rue de Béthune 59000 Lille", Score: .91, HasScore: true, PostalCode: "59000", City: "LILLE", Type: "housenumber"}}}
	runner, err := NewRunner(store, provider, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	summary, err := runner.Run(context.Background(), RunOptions{DryRun: true})
	if err != nil || summary.Selected != 2 || summary.Skipped != 2 || summary.Matched != 2 || summary.Written != 0 || len(store.saved) != 0 || provider.calls != 2 {
		t.Fatalf("summary=%+v saved=%d calls=%d err=%v", summary, len(store.saved), provider.calls, err)
	}
}

func TestRunnerAmbiguousNotFoundLimitAndPartialFailure(t *testing.T) {
	theaters := []Theater{{Provider: "ugc", ProviderID: "1", Address: "", PostalCode: "59000", City: "Lille"}, {Provider: "ugc", ProviderID: "2", Address: "2 rue", PostalCode: "59000", City: "Lille"}}
	store := &memoryStore{theaters: theaters}
	provider := &fakeProvider{candidates: []Candidate{{Label: "2 Rue", Score: .95, HasScore: true, PostalCode: "59100", City: "Lille", Type: "housenumber"}}}
	runner, _ := NewRunner(store, provider, func() time.Time { return time.Now().UTC() })
	summary, err := runner.Run(context.Background(), RunOptions{})
	if err != nil || summary.NotFound != 1 || summary.Ambiguous != 1 || summary.Written != 2 || provider.calls != 1 {
		t.Fatalf("summary=%+v calls=%d err=%v", summary, provider.calls, err)
	}
	provider.err = errors.New("secret provider failure")
	store.saved = nil
	summary, err = runner.Run(context.Background(), RunOptions{Limit: 1})
	if err != nil || summary.Selected != 1 || summary.NotFound != 1 || summary.Failed != 0 {
		t.Fatalf("limited summary=%+v err=%v", summary, err)
	}
	store.theaters = theaters[1:]
	store.saved = nil
	summary, err = runner.Run(context.Background(), RunOptions{})
	if err == nil || summary.Failed != 1 || len(store.saved) != 0 {
		t.Fatalf("failure summary=%+v saved=%d err=%v", summary, len(store.saved), err)
	}
}

func TestEvaluateUsesHighestAcceptedCandidateAndStableTie(t *testing.T) {
	theater := Theater{Provider: "ugc", ProviderID: "25", PostalCode: "59000", City: "Béthune"}
	candidates := []Candidate{
		{Longitude: 2, Latitude: 50, HasCoordinates: true, Label: "first", Score: .8, HasScore: true, PostalCode: "59000", City: "Bethune", Type: "housenumber"},
		{Longitude: 3, Latitude: 51, HasCoordinates: true, Label: "second", Score: .8, HasScore: true, PostalCode: "59000", City: "Béthune", Type: "housenumber"},
	}
	location, err := evaluate(theater, "hash", candidates, time.Now())
	if err != nil || location.Status != StatusMatched || location.MatchedLabel != "first" || *location.Longitude != 2 {
		t.Fatalf("location=%+v err=%v", location, err)
	}
}

func TestEvaluateStreetAcceptanceBoundaries(t *testing.T) {
	valid := Candidate{Longitude: 2, Latitude: 50, HasCoordinates: true, Label: "Rue du cinéma", Score: .70, HasScore: true, PostalCode: "59000", City: "Lille", Type: "street"}
	tests := []struct {
		name      string
		theater   Theater
		candidate Candidate
		want      Status
	}{
		{name: "CGR numberless", theater: Theater{Provider: "cgr", Address: "Rue du cinéma", PostalCode: "59000", City: "Lille"}, candidate: valid, want: StatusMatched},
		{name: "Pathe numberless", theater: Theater{Provider: "pathe", Address: "Rue du cinéma", PostalCode: "59000", City: "Lille"}, candidate: valid, want: StatusMatched},
		{name: "Kinepolis numberless", theater: Theater{Provider: "kinepolis", Address: "Rue du cinéma", PostalCode: "59000", City: "Lille"}, candidate: valid, want: StatusMatched},
		{name: "UGC excluded", theater: Theater{Provider: "ugc", Address: "Rue du cinéma", PostalCode: "59000", City: "Lille"}, candidate: valid, want: StatusAmbiguous},
		{name: "ASCII number excluded", theater: Theater{Provider: "cgr", Address: "2 Rue du cinéma", PostalCode: "59000", City: "Lille"}, candidate: valid, want: StatusAmbiguous},
		{name: "Unicode number excluded", theater: Theater{Provider: "pathe", Address: "Rue du cinéma ２", PostalCode: "59000", City: "Lille"}, candidate: valid, want: StatusAmbiguous},
		{name: "below score", theater: Theater{Provider: "cgr", Address: "Rue du cinéma", PostalCode: "59000", City: "Lille"}, candidate: withCandidate(valid, func(candidate *Candidate) { candidate.Score = .699999 }), want: StatusAmbiguous},
		{name: "invalid coordinates", theater: Theater{Provider: "cgr", Address: "Rue du cinéma", PostalCode: "59000", City: "Lille"}, candidate: withCandidate(valid, func(candidate *Candidate) { candidate.Latitude = 91 }), want: StatusAmbiguous},
		{name: "postcode mismatch", theater: Theater{Provider: "cgr", Address: "Rue du cinéma", PostalCode: "59000", City: "Lille"}, candidate: withCandidate(valid, func(candidate *Candidate) { candidate.PostalCode = "59100" }), want: StatusAmbiguous},
		{name: "city alias", theater: Theater{Provider: "cgr", Address: "Rue du cinéma", PostalCode: "91000", City: "Évry"}, candidate: withCandidate(valid, func(candidate *Candidate) { candidate.PostalCode, candidate.City = "91000", "Évry-Courcouronnes" }), want: StatusMatched},
		{name: "cross commune", theater: Theater{Provider: "cgr", Address: "Rue du cinéma", PostalCode: "13000", City: "Marseille"}, candidate: withCandidate(valid, func(candidate *Candidate) { candidate.PostalCode, candidate.City = "13000", "Les Pennes-Mirabeau" }), want: StatusAmbiguous},
		{name: "unsupported type", theater: Theater{Provider: "cgr", Address: "Rue du cinéma", PostalCode: "59000", City: "Lille"}, candidate: withCandidate(valid, func(candidate *Candidate) { candidate.Type = "municipality" }), want: StatusAmbiguous},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location, err := evaluate(test.theater, "hash", []Candidate{test.candidate}, time.Now())
			if err != nil || location.Status != test.want {
				t.Fatalf("status=%q want=%q location=%+v err=%v", location.Status, test.want, location, err)
			}
		})
	}
}

func TestEvaluateKeepsHousenumberAcceptanceForEveryProvider(t *testing.T) {
	for _, provider := range []string{"ugc", "kinepolis", "pathe", "cgr"} {
		theater := Theater{Provider: provider, Address: "2 Rue du cinéma", PostalCode: "59000", City: "Lille"}
		candidate := Candidate{Longitude: 2, Latitude: 50, HasCoordinates: true, Label: "2 Rue du cinéma", Score: .70, HasScore: true, PostalCode: "59000", City: "Lille", Type: "housenumber"}
		location, err := evaluate(theater, "hash", []Candidate{candidate}, time.Now())
		if err != nil || location.Status != StatusMatched {
			t.Fatalf("provider=%q location=%+v err=%v", provider, location, err)
		}
	}
}

func TestEvaluateStoresSanitizedHighestRejectedSuggestion(t *testing.T) {
	theater := Theater{Provider: "ugc", Address: "Rue du cinéma", PostalCode: "59000", City: "Lille"}
	candidates := []Candidate{
		{Longitude: 2, Latitude: 50, HasCoordinates: true, Label: " first ", Score: .8, HasScore: true, PostalCode: " 59100 ", City: " Lille ", Type: " street "},
		{Longitude: 181, Latitude: 50, HasCoordinates: true, Label: " best ", Score: .9, HasScore: true, PostalCode: " 59200 ", City: " Tourcoing ", Type: " municipality "},
		{Longitude: 3, Latitude: 51, HasCoordinates: true, Label: "tie", Score: .9, HasScore: true, PostalCode: "59300", City: "Valenciennes", Type: "street"},
	}
	location, err := evaluate(theater, "hash", candidates, time.Now())
	if err != nil || location.Status != StatusAmbiguous || location.MatchedLabel != "best" || location.MatchScore == nil || *location.MatchScore != .9 || location.Suggestion == nil {
		t.Fatalf("location=%+v err=%v", location, err)
	}
	if location.Suggestion.Latitude != nil || location.Suggestion.Longitude != nil || location.Suggestion.PostalCode != "59200" || location.Suggestion.City != "Tourcoing" || location.Suggestion.Type != "municipality" {
		t.Fatalf("suggestion=%+v", location.Suggestion)
	}
}

func withCandidate(candidate Candidate, change func(*Candidate)) Candidate {
	change(&candidate)
	return candidate
}
