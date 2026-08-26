package geocoding

import "testing"

func TestAddressHashNormalizationAndMaterialChanges(t *testing.T) {
	first := AddressHash("  40  RUE de Béthune ", "59000", "LILLE")
	if got := AddressHash("40 rue de Be\u0301thune", "59000", "lille"); got != first {
		t.Fatalf("equivalent hash=%q want=%q", got, first)
	}
	if got := AddressHash("40 rue de Bethune", "59000", "Lille"); got == first {
		t.Fatal("accent-changing input reused hash")
	}
	if cityKey("Villeneuve-d'Ascq") != cityKey("VILLENEUVE D ASCQ") || cityKey("Béthune") != cityKey("bethune") {
		t.Fatal("city comparison key did not ignore accents and punctuation")
	}
}

func TestCityKeyAcceptsOnlyDocumentedAliases(t *testing.T) {
	aliases := [][2]string{
		{"Évry", "Évry-Courcouronnes"},
		{"Cherbourg-Octeville", "Cherbourg-en-Cotentin"},
		{"Grand-Quevilly", "Le Grand-Quevilly"},
		{"Levallois", "Levallois-Perret"},
		{"Le Chesnay", "Le Chesnay-Rocquencourt"},
	}
	for _, pair := range aliases {
		if cityKey(pair[0]) != cityKey(pair[1]) || cityKey(pair[1]) != cityKey(pair[0]) {
			t.Fatalf("alias %q/%q did not compare bidirectionally", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{
		{"Cergy-le-Haut", "Cergy"},
		{"Marseille", "Les Pennes-Mirabeau"},
		{"Le Grand-Quevilly", "Petit-Quevilly"},
		{"Levallois-Perret", "Levallois Village"},
	} {
		if cityKey(pair[0]) == cityKey(pair[1]) {
			t.Fatalf("unapproved alias %q/%q compared equal", pair[0], pair[1])
		}
	}
}

func TestCityKeyExpandsOnlyLeadingStandaloneSt(t *testing.T) {
	for _, abbreviated := range []string{"St Denis", "St-Denis", "St. Denis", "  ST-DENIS  "} {
		if cityKey(abbreviated) != cityKey("Saint-Denis") {
			t.Fatalf("leading St form %q did not compare with Saint", abbreviated)
		}
	}
	for _, pair := range [][2]string{
		{"Stains", "Saintains"},
		{"Mont-St-Aignan", "Mont-Saint-Aignan"},
		{"Stéphane", "Saintéphane"},
	} {
		if cityKey(pair[0]) == cityKey(pair[1]) {
			t.Fatalf("non-token St form %q/%q compared equal", pair[0], pair[1])
		}
	}
}
