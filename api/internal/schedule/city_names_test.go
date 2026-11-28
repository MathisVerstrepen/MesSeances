package schedule

import (
	"errors"
	"testing"
)

func TestCityDisplayNamePrefersTheBestWrittenSpelling(t *testing.T) {
	for _, test := range []struct {
		labels []string
		want   string
	}{
		{labels: []string{"CRETEIL", "Créteil"}, want: "Créteil"},
		{labels: []string{"Nimes", "Nîmes"}, want: "Nîmes"},
		{labels: []string{"ROSNY SOUS BOIS", "Rosny-sous-Bois"}, want: "Rosny-sous-Bois"},
		{labels: []string{"Châlons en Champagne", "Châlons-en-Champagne"}, want: "Châlons-en-Champagne"},
		{labels: []string{"La-Roche-Sur-Yon", "La Roche-sur-Yon"}, want: "La Roche-sur-Yon"},
		{labels: []string{" Lille ", "LILLE", " lille "}, want: "Lille"},
		{labels: []string{"  Paris   10e arrondissement "}, want: "Paris 10e arrondissement"},
		{labels: []string{"", "  "}, want: ""},
	} {
		if got := cityDisplayName(test.labels); got != test.want {
			t.Errorf("cityDisplayName(%q)=%q want %q", test.labels, got, test.want)
		}
	}
	// The choice never depends on the order providers listed labels in.
	if forward, reverse := cityDisplayName([]string{"Evry", "Évry"}), cityDisplayName([]string{"Évry", "Evry"}); forward != "Évry" || reverse != forward {
		t.Fatalf("forward=%q reverse=%q", forward, reverse)
	}
}

func TestCapitalOnlyCityNamesGetFrenchTitleCase(t *testing.T) {
	for _, test := range []struct{ name, want string }{
		{name: "LILLE", want: "Lille"},
		{name: "PARIS", want: "Paris"},
		{name: "VILLENEUVE D'ASCQ", want: "Villeneuve-d'Ascq"},
		{name: "SAINT GERMAIN EN LAYE", want: "Saint-Germain-en-Laye"},
		{name: "AULNAY SOUS BOIS", want: "Aulnay-sous-Bois"},
		{name: "ENGHIEN-LES-BAINS", want: "Enghien-les-Bains"},
		{name: "LE CHESNAY", want: "Le Chesnay"},
		{name: "L'ISLE ADAM", want: "L'Isle-Adam"},
		{name: "MONTIGNY-LE-BRETONNEUX", want: "Montigny-le-Bretonneux"},
		{name: "PARIS 10E ARRONDISSEMENT", want: "Paris 10e Arrondissement"},
		{name: "ÉVRY", want: "Évry"},
	} {
		if got := cityDisplayName([]string{test.name}); got != test.want {
			t.Errorf("cityDisplayName(%q)=%q want %q", test.name, got, test.want)
		}
	}
}

func TestCitySpellingsMergeIntoOneNormalizedCity(t *testing.T) {
	data := testDataset()
	data.Theaters[0].City = "CRETEIL"
	data.Theaters[1].City = "Créteil"
	data.Theaters[2].City = "LILLE"
	service, err := NewService(newTestSource(data), ServiceOptions{Now: testServiceNow})
	if err != nil {
		t.Fatal(err)
	}

	inventory := service.Cities()
	got := map[string]int{}
	for _, item := range inventory.Items {
		got[item.Name+":"+item.Slug] = len(item.Theaters)
	}
	if len(got) != 2 || got["Créteil:creteil"] != 2 || got["Lille:lille"] != 1 {
		t.Fatalf("cities=%v", got)
	}

	for _, theater := range service.Theaters(TheaterCatalogQuery{City: "CRÉTEIL"}) {
		if theater.City != "Créteil" || theater.CitySlug != "creteil" {
			t.Fatalf("theater=%+v", theater)
		}
	}
	if theaters := service.Theaters(TheaterCatalogQuery{City: "creteil"}); len(theaters) != 2 {
		t.Fatalf("slug filter returned %d theaters", len(theaters))
	}

	// Links indexed before the merge keep working; spellings stay rejected.
	for _, slug := range []string{"creteil", "creteil--3e9b9ea8", "creteil--c7b1ef7a"} {
		detail, err := service.City(slug)
		if err != nil || detail.City != (City{Name: "Créteil", Slug: "creteil"}) || len(detail.Theaters) != 2 {
			t.Fatalf("City(%q)=%+v err=%v", slug, detail.City, err)
		}
	}
	var notFound *NotFoundError
	for _, slug := range []string{"Créteil", "CRETEIL", "creteil--zz", "paris--3e9b9ea8"} {
		if _, err := service.City(slug); !errors.As(err, &notFound) {
			t.Fatalf("City(%q) err=%v", slug, err)
		}
	}

	result, err := service.MovieShowtimes(MovieShowtimesQuery{Slug: "ugc-film-200", Date: "2026-08-15"})
	if err != nil {
		t.Fatal(err)
	}
	for _, theater := range result.Theaters {
		if theater.City != "Créteil" {
			t.Fatalf("movie showtimes theater city=%q", theater.City)
		}
	}
}

func TestNonLatinCityNamesNeverMerge(t *testing.T) {
	identities := TheaterCityIdentities([]TheaterRecord{{City: "東京"}, {City: "大阪"}, {City: "Σ"}, {City: "ς"}})
	if identities[0].Slug == identities[1].Slug || identities[2] != identities[3] {
		t.Fatalf("identities=%v", identities)
	}
}
