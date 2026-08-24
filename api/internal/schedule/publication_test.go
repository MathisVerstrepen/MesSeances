package schedule

import (
	"testing"
	"time"
)

func TestPreparePublicationDetachesMaterializesAndDeduplicates(t *testing.T) {
	data := testDataset()
	publication, err := PreparePublication(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(publication.Movies) != 4 || publication.Dataset.Provider != ProviderUGC || publication.Dataset.Theaters[0].Provider != ProviderUGC || publication.Dataset.Showtimes[0].Provider != ProviderUGC || publication.Dataset.Showtimes[0].Movie.Provider != ProviderUGC {
		t.Fatalf("publication=%+v movies=%d", publication.Dataset, len(publication.Movies))
	}
	data.Theaters[0].AvailableDates[0] = "changed"
	data.Showtimes[0].Movie.Genres = append(data.Showtimes[0].Movie.Genres, "changed")
	if publication.Dataset.Theaters[0].AvailableDates[0] == "changed" || len(publication.Dataset.Showtimes[0].Movie.Genres) != 0 {
		t.Fatal("publication retained caller-owned slices")
	}
}

func TestPreparePublicationRejectsConflictingMovieMetadata(t *testing.T) {
	data := testDataset()
	data.Showtimes[1].Movie.ProviderID = data.Showtimes[0].Movie.ProviderID
	data.Showtimes[1].Movie.Slug = data.Showtimes[0].Movie.Slug
	data.Showtimes[1].Movie.Title = "conflict"
	if _, err := PreparePublication(data); err == nil {
		t.Fatal("conflicting movie metadata accepted")
	}
}

func TestMovieIdentityAndServiceDateOperations(t *testing.T) {
	for _, identity := range []MovieIdentity{{Provider: ProviderUGC, ProviderID: "25"}, {Provider: ProviderKinepolis, ProviderID: "HO200"}, {Provider: ProviderPathe, ProviderID: "film-a"}} {
		if err := identity.Validate(); err != nil {
			t.Fatalf("identity=%+v err=%v", identity, err)
		}
	}
	for _, identity := range []MovieIdentity{{Provider: ProviderUGC, ProviderID: "zero"}, {Provider: ProviderCombined, ProviderID: "25"}, {Provider: ProviderKinepolis, ProviderID: ""}, {Provider: ProviderPathe, ProviderID: "bad id"}} {
		if err := identity.Validate(); err == nil {
			t.Fatalf("identity=%+v accepted", identity)
		}
	}
	date, err := ParseServiceDate("2026-08-15")
	if err != nil || date.Location().String() != Timezone || FormatServiceDate(date) != "2026-08-15" {
		t.Fatalf("date=%v err=%v", date, err)
	}
	if _, err := ParseServiceDate("2026-8-15"); err == nil {
		t.Fatal("noncanonical service date accepted")
	}
	if got := FormatServiceDate(time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)); got != "2026-08-15" {
		t.Fatalf("formatted=%q", got)
	}
}
