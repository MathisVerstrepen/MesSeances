package ugc

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func fixture(t *testing.T, name string) *os.File {
	t.Helper()
	file, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func TestParseSitemap(t *testing.T) {
	ids, err := ParseSitemap(fixture(t, "sitemap.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "3,25,46" {
		t.Fatalf("ids=%v", ids)
	}
}

func TestParseSitemapFailsWithoutCanonicalCinema(t *testing.T) {
	sitemap := `<?xml version="1.0"?><urlset><url><loc>https://www.ugc.fr/cinema.html?id=invalid</loc></url><url><loc>https://www.ugc.fr/cinema.html?legacy=25</loc></url></urlset>`
	_, err := ParseSitemap(strings.NewReader(sitemap))
	if err == nil || err.Error() != "sitemap contains no cinemas" {
		t.Fatalf("error=%v", err)
	}
}

func TestParseCinema(t *testing.T) {
	cinema, err := ParseCinema(fixture(t, "cinema.html"), "25")
	if err != nil {
		t.Fatal(err)
	}
	if cinema.Name != "UGC Ciné Cité Lille" || cinema.City != "Lille" || cinema.PostalCode != "59000" || len(cinema.AdvertisedDates) != 2 {
		t.Fatalf("cinema=%+v", cinema)
	}
}

func TestParseCinemaFallsBackToAddressLocality(t *testing.T) {
	cinema, err := ParseCinema(fixture(t, "cinema-address-locality.html"), "39")
	if err != nil {
		t.Fatal(err)
	}
	if cinema.ProviderID != "39" || cinema.Name != "UGC Le Majestic" || cinema.Address != "11 Place Henri IV 77100 MEAUX" || cinema.City != "MEAUX" || cinema.PostalCode != "77100" {
		t.Fatalf("cinema=%+v", cinema)
	}
}

func TestParseCinemaUsesAddressLocalityForBrandedTitle(t *testing.T) {
	cinema, err := ParseCinema(fixture(t, "cinema-brand-locality.html"), "6")
	if err != nil {
		t.Fatal(err)
	}
	if cinema.ProviderID != "6" || cinema.Name != "UGC Ciné Cité SQY Ouest" || cinema.Address != "1, avenue de la Source de la Bièvre 78180 MONTIGNY-LE-BRETONNEUX" || cinema.City != "MONTIGNY-LE-BRETONNEUX" || cinema.PostalCode != "78180" {
		t.Fatalf("cinema=%+v", cinema)
	}
}

func TestParseCinemaUsesAddressLocalityForMarketTitle(t *testing.T) {
	cinema, err := ParseCinema(fixture(t, "cinema-market-locality.html"), "29")
	if err != nil {
		t.Fatal(err)
	}
	if cinema.ProviderID != "29" || cinema.Name != "UGC Ciné Cité Ludres" || cinema.Address != "350, rue des Mazurots 54710 LUDRES" || cinema.City != "LUDRES" || cinema.PostalCode != "54710" {
		t.Fatalf("cinema=%+v", cinema)
	}
}

func TestParseCinemaUsesAddressLocalityForMetropolitanTitle(t *testing.T) {
	cinema, err := ParseCinema(fixture(t, "cinema-metropolitan-locality.html"), "31")
	if err != nil {
		t.Fatal(err)
	}
	if cinema.ProviderID != "31" || cinema.Name != "UGC Ciné Cité Atlantis" || cinema.Address != "Pôle commercial Atlantis - Place Jean Bart 44800 SAINT HERBLAIN" || cinema.City != "SAINT HERBLAIN" || cinema.PostalCode != "44800" {
		t.Fatalf("cinema=%+v", cinema)
	}
}

func TestParseCinemaAddressLocalityValidation(t *testing.T) {
	page := func(title, headings string) string {
		return `<html><head><title>` + title + `</title></head><body><section id="cinema-heading">` + headings + `</section><input name="cinemaId" value="39"><button id="nav_date_2026-08-15"></button></body></html>`
	}
	tests := []struct {
		name     string
		title    string
		headings string
		wantCity string
		wantCode string
		wantErr  string
	}{
		{name: "unicode and collapsed whitespace", title: "UGC Test", headings: `<h1>UGC Test</h1><p class="address">1 rue Test&nbsp;94240
 L’Haÿ-les-Roses</p>`, wantCity: "L’Haÿ-les-Roses", wantCode: "94240"},
		{name: "title only remains valid", title: "UGC Test, cinéma à Lille (59000)", headings: `<h1>UGC Test</h1><p class="address">40 rue de Béthune</p>`, wantCity: "Lille", wantCode: "59000"},
		{name: "matching sources use address", title: "UGC Test, cinéma à Meaux (77100)", headings: `<h1>UGC Test</h1><p class="address">11 Place Henri IV 77100 MEAUX</p>`, wantCity: "MEAUX", wantCode: "77100"},
		{name: "title city does not override address city", title: "UGC Test, cinéma à Paris (77100)", headings: `<h1>UGC Test</h1><p class="address">11 Place Henri IV 77100 MEAUX</p>`, wantCity: "MEAUX", wantCode: "77100"},
		{name: "missing", title: "UGC Test", headings: `<h1>UGC Test</h1><p class="address">11 Place Henri IV</p>`, wantErr: "cinema locality missing"},
		{name: "postal without city", title: "UGC Test", headings: `<h1>UGC Test</h1><p class="address">11 Place Henri IV 77100</p>`, wantErr: "cinema locality malformed"},
		{name: "title does not rescue malformed address", title: "UGC Test, cinéma à Meaux (77100)", headings: `<h1>UGC Test</h1><p class="address">11 Place Henri IV 77100</p>`, wantErr: "cinema locality malformed"},
		{name: "numeric city", title: "UGC Test", headings: `<h1>UGC Test</h1><p class="address">11 Place Henri IV 77100 42</p>`, wantErr: "cinema locality malformed"},
		{name: "ambiguous postal suffix", title: "UGC Test", headings: `<h1>UGC Test</h1><p class="address">75001 PARIS 77100 MEAUX</p>`, wantErr: "cinema locality malformed"},
		{name: "four digit postal", title: "UGC Test", headings: `<h1>UGC Test</h1><p class="address">11 Place Henri IV 7710 MEAUX</p>`, wantErr: "cinema locality missing"},
		{name: "six digit postal", title: "UGC Test", headings: `<h1>UGC Test</h1><p class="address">11 Place Henri IV 771000 MEAUX</p>`, wantErr: "cinema locality missing"},
		{name: "postal conflict", title: "UGC Test, cinéma à Paris (75001)", headings: `<h1>UGC Test</h1><p class="address">11 Place Henri IV 77100 MEAUX</p>`, wantErr: "cinema locality conflicting"},
		{name: "branding postal conflict", title: "UGC SQY Ouest, cinéma à SQY Ouest (75001)", headings: `<h1>UGC SQY Ouest</h1><p class="address">1 avenue Test 78180 MONTIGNY-LE-BRETONNEUX</p>`, wantErr: "cinema locality conflicting"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cinema, err := ParseCinema(strings.NewReader(page(test.title, test.headings)), "39")
			if test.wantErr != "" {
				if err == nil || err.Error() != test.wantErr {
					t.Fatalf("error=%v want=%q", err, test.wantErr)
				}
				return
			}
			if err != nil || cinema.City != test.wantCity || cinema.PostalCode != test.wantCode {
				t.Fatalf("cinema=%+v error=%v", cinema, err)
			}
		})
	}
}

func TestParseCinemaPreservesIdentityNameAndAddressChecks(t *testing.T) {
	tests := []struct {
		name     string
		identity string
		headings string
		want     string
	}{
		{name: "identity", identity: `<input name="cinemaId" value="40">`, headings: `<h1>UGC Test</h1><p class="address">1 rue Test 77100 MEAUX</p>`, want: "cinema identity missing or conflicting"},
		{name: "name", identity: `<input name="cinemaId" value="39">`, headings: `<h1>UGC Test</h1><h1>Other</h1><p class="address">1 rue Test 77100 MEAUX</p>`, want: "cinema name missing or conflicting"},
		{name: "address", identity: `<input name="cinemaId" value="39">`, headings: `<h1>UGC Test</h1><p class="address">1 rue Test 77100 MEAUX</p><p class="address">2 rue Test 77100 MEAUX</p>`, want: "cinema address missing or conflicting"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := `<html><head><title>UGC Test</title></head><body><section id="cinema-heading">` + test.headings + `</section>` + test.identity + `<button id="nav_date_2026-08-15"></button></body></html>`
			_, err := ParseCinema(strings.NewReader(body), "39")
			if err == nil || err.Error() != test.want {
				t.Fatalf("error=%v want=%q", err, test.want)
			}
		})
	}
}

func TestParseShowings(t *testing.T) {
	cinema := Cinema{ProviderID: "25"}
	records, err := ParseShowings(fixture(t, "showings.html"), cinema, "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	location, locationErr := time.LoadLocation(schedule.Timezone)
	if locationErr != nil {
		t.Fatal(locationErr)
	}
	movie := schedule.MovieRecord{ProviderID: "700", Slug: "ugc-film-700", Title: "L'Été & nous", RuntimeMinutes: 83, PosterURL: "https://www.ugc.fr/posters/700.jpg"}
	firstStart := time.Date(2026, 8, 15, 12, 0, 0, 0, location)
	secondStart := time.Date(2026, 8, 16, 0, 15, 0, 0, location)
	want := []schedule.ShowtimeRecord{
		{ID: "ugc-showing-900", ProviderShowingID: "900", ServiceDate: "2026-08-15", TheaterID: "ugc-25", Movie: movie, StartTime: firstStart, EndTime: firstStart.Add(83 * time.Minute), Language: schedule.LanguageVOSTFR, ProviderVersion: "VOSTF", Format: "3D", Room: "Salle 4", BookingURL: "https://www.ugc.fr/reservationSeances.html?id=900"},
		{ID: "ugc-showing-901", ProviderShowingID: "901", ServiceDate: "2026-08-15", TheaterID: "ugc-25", Movie: movie, StartTime: secondStart, EndTime: secondStart.Add(83 * time.Minute), Language: schedule.LanguageVFSME, ProviderVersion: "VFSTF", Format: "2D", Room: "Salle 4", BookingURL: "https://www.ugc.fr/reservationSeances.html?id=901"},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("records=%+v\nwant=%+v", records, want)
	}
}

func TestParseShowingsDerivesEmptySuffixFilmBlockIdentity(t *testing.T) {
	records, err := ParseShowings(fixture(t, "showings-empty-suffix-derived.html"), Cinema{ProviderID: "25"}, "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records=%+v", records)
	}
	record := records[0]
	if record.ProviderShowingID != "98765" || record.Movie.ProviderID != "18421" || record.Movie.Title != "FILM CAPTURÉ" || record.Movie.RuntimeMinutes != 107 {
		t.Fatalf("record=%+v", record)
	}
}

func TestParseShowingsRejectsEmptySuffixFilmBlockIdentityConflict(t *testing.T) {
	body := `<div id="bloc-showing-film-"><div class="block--title text-uppercase"><a class="color--dark-blue" href="film_test_1.html?cinemaId=25">Film</a></div><span>(2h)</span><button data-showing="10" data-filmid="1" data-cinemaid="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button><button data-showing="11" data-filmid="2" data-cinemaid="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="13:00"></button></div>`
	_, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err == nil || err.Error() != "showing required attribute missing or conflicting" {
		t.Fatalf("error=%v", err)
	}
}

func TestParseShowingsRejectsEmptySuffixFilmBlockInvalidIdentity(t *testing.T) {
	for _, test := range []struct {
		name     string
		identity string
	}{
		{name: "zero", identity: ` data-filmid="0"`},
		{name: "nonnumeric", identity: ` data-filmid="film"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `<div id="bloc-showing-film-"><button data-showing="10"` + test.identity + ` data-cinemaid="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div>`
			_, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
			if err == nil || err.Error() != "showing required attribute missing or conflicting" {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestParseShowingsSkipsIdentitylessPackageBesideValidFilm(t *testing.T) {
	records, err := ParseShowings(fixture(t, "showings-identityless-package.html"), Cinema{ProviderID: "25"}, "2026-08-15")
	if err != nil || len(records) != 1 || records[0].ProviderShowingID != "10" {
		t.Fatalf("records=%+v error=%v", records, err)
	}
}

func TestParseShowingsAcceptsIdentitylessPackageOnly(t *testing.T) {
	for _, identity := range []string{"", ` data-filmid="" data-film=""`} {
		body := `<div id="bloc-showing-film-"><button data-showing="package-1"` + identity + ` data-cinemaid="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div>`
		records, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
		if err != nil || len(records) != 0 {
			t.Fatalf("identity=%q records=%+v error=%v", identity, records, err)
		}
	}
}

func TestParseShowingsRejectsMalformedIdentitylessPackageIdentity(t *testing.T) {
	validButton := func(identity string) string {
		return `<button data-showing="10" ` + identity + ` data-cinemaid="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button>`
	}
	tests := []struct {
		name    string
		content string
	}{
		{name: "mixed empty and positive", content: validButton(`data-filmid=""`) + validButton(`data-filmid="1"`)},
		{name: "zero", content: validButton(`data-filmid="0"`)},
		{name: "nonnumeric", content: validButton(`data-filmid="film"`)},
		{name: "aliases conflict", content: validButton(`data-filmid="1" data-film-id="2"`)},
		{name: "empty with canonical link", content: `<div class="block--title text-uppercase"><a class="color--dark-blue" href="film_test_1.html?cinemaId=25">Film</a></div>` + validButton(`data-filmid=""`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseShowings(strings.NewReader(`<div id="bloc-showing-film-">`+test.content+`</div>`), Cinema{ProviderID: "25"}, "2026-08-15")
			if err == nil || err.Error() != "showing required attribute missing or conflicting" {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestParseShowingsDoesNotSwallowNestedCanonicalBlock(t *testing.T) {
	body := `<div id="bloc-showing-film-"><button data-showing="package-1" data-filmid="" data-cinemaid="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="11:00"></button><div id="bloc-showing-film-1"><div class="block--title text-uppercase"><a class="color--dark-blue" href="film_test_1.html?cinemaId=25">Film</a></div><span>(2h)</span><button data-showing="10" data-filmid="1" data-cinemaid="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div></div>`
	records, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err != nil || len(records) != 1 || records[0].Movie.ProviderID != "1" {
		t.Fatalf("records=%+v error=%v", records, err)
	}
}

func TestParseShowingsPreservesNearestRoomAndFormatWithSharedNestedStructure(t *testing.T) {
	body := `<article id="bloc-showing-film-1"><div class="block--title text-uppercase"><a class="color--dark-blue" href="film_public_1.html?cinemaId=25">Film</a></div><span>(2h)</span><div class="session"><span class="screening-room">Salle extérieure</span><span class="screening-2D3D">IMAX</span><div class="session"><span class="screening-room">Salle intérieure</span><span class="screening-2D3D">4DX</span><button data-showing="10" data-film="1" data-cinema="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div><button data-showing="11" data-film="1" data-cinema="25" data-version="VO" data-seancedate="15/08/2026" data-seancehour="13:00"></button></div></article>`
	records, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].ProviderShowingID != "10" || records[0].Room != "Salle intérieure" || records[0].Format != "4DX" || records[1].ProviderShowingID != "11" || records[1].Room != "Salle extérieure" || records[1].Format != "IMAX" {
		t.Fatalf("records=%+v", records)
	}
}

func TestParseShowingsRejectsAmbiguousNoncanonicalTitles(t *testing.T) {
	body := `<div id="bloc-showing-film-1"><a data-film="1" title="Film canonique">Film</a><a data-film="1" title="Action différente">Action</a><span>(2h)</span><button data-showing="10" data-film="1" data-cinema="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div>`
	_, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err == nil || err.Error() != "film title conflicting" {
		t.Fatalf("error=%v", err)
	}
}

func TestParseShowingsRejectsCanonicalFilmIdentityConflict(t *testing.T) {
	body := `<div id="bloc-showing-film-1"><div class="block--title text-uppercase"><a class="color--dark-blue" href="film_wrong_2.html?cinemaId=25" title="Wrong film">Wrong film</a></div><span>(2h)</span><button data-showing="10" data-film="1" data-cinema="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div>`
	_, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err == nil || err.Error() != "film identity conflict" {
		t.Fatalf("error=%v", err)
	}
}

func TestParseShowingsRejectsMissingOrInvalidCanonicalHeading(t *testing.T) {
	tests := []struct {
		name    string
		heading string
		want    string
	}{
		{name: "missing", heading: `<a class="see-more" href="film_example_1.html?cinemaId=25" title="Action title">Action</a>`, want: "film title missing"},
		{name: "missing title", heading: `<div class="block--title text-uppercase"><a class="color--dark-blue" href="film_example_1.html?cinemaId=25"></a></div>`, want: "film title missing"},
		{name: "malformed href", heading: `<div class="block--title text-uppercase"><a class="color--dark-blue" href="film_example.html?cinemaId=25">Film</a></div>`, want: "invalid film detail link"},
		{name: "cinema mismatch", heading: `<div class="block--title text-uppercase"><a class="color--dark-blue" href="film_example_1.html?cinemaId=26">Film</a></div>`, want: "film identity conflict"},
		{name: "title conflict", heading: `<div class="block--title text-uppercase"><a class="color--dark-blue" href="film_example_1.html?cinemaId=25" title="Other">Film</a></div>`, want: "film title conflicting"},
		{name: "multiple heading conflict", heading: `<div class="block--title text-uppercase"><a class="color--dark-blue" href="film_example_1.html?cinemaId=25">Film</a></div><div class="block--title text-uppercase"><a class="color--dark-blue" href="film_example_1.html?cinemaId=25">Other</a></div>`, want: "film title conflicting"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := `<div id="bloc-showing-film-1">` + test.heading + `<span>(2h)</span><button data-showing="10" data-film="1" data-cinema="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div>`
			_, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
			if err == nil || err.Error() != test.want {
				t.Fatalf("error=%v want=%q", err, test.want)
			}
		})
	}
}

func TestParseShowingsPreservesUnambiguousLegacyTitle(t *testing.T) {
	body := `<div id="bloc-showing-film-1"><a data-film="1" title="Legacy film">Film</a><span>(2h)</span><button data-showing="10" data-film="1" data-cinema="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div>`
	records, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Movie.Title != "Legacy film" {
		t.Fatalf("records=%+v", records)
	}
}

func TestParseShowingsPrefersNumericButtonIDs(t *testing.T) {
	body := `<div id="bloc-showing-film-1"><div class="block--title text-uppercase"><a class="color--dark-blue" href="film_example_1.html?cinemaId=25">Film</a></div><span>(2h)</span><button data-showing="10" data-film="Human film title" data-filmId="1" data-cinema="Human cinema name" data-cinemaId="25" data-version="VF" data-seanceDate="15/08/2026" data-seanceHour="12:00"></button></div>`
	records, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Movie.ProviderID != "1" || records[0].TheaterID != "ugc-25" {
		t.Fatalf("records=%+v", records)
	}
}

func TestParseShowingsRejectsConflictingPreferredButtonID(t *testing.T) {
	body := `<div id="bloc-showing-film-1"><div class="block--title text-uppercase"><a class="color--dark-blue" href="film_example_1.html?cinemaId=25">Film</a></div><span>(2h)</span><button data-showing="10" data-film="1" data-filmId="2" data-cinema="25" data-cinemaId="25" data-version="VF" data-seanceDate="15/08/2026" data-seanceHour="12:00"></button></div>`
	_, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err == nil || err.Error() != "showing required attribute missing or conflicting" {
		t.Fatalf("error=%v", err)
	}
}

func TestParseShowingsFailsClosed(t *testing.T) {
	body := `<div id="bloc-showing-film-1"><a data-film="1" title="Film">Film</a><span>(2h)</span><button data-showing="synthetic-secret" data-film="1" data-cinema="25" data-version="UNKNOWN" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div>`
	_, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err == nil {
		t.Fatal("unknown version accepted")
	}
	if strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("showing value leaked")
	}
}

func TestParseShowingsRejectsUnrecognizedNonemptyContainer(t *testing.T) {
	bodies := []string{
		`<section id="showings"><div class="new-provider-layout">Séances disponibles</div></section>`,
		`<section id="showings"><div id="bloc-showing-film-1">Structure film sans séance reconnue</div></section>`,
	}
	for _, body := range bodies {
		_, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
		if err == nil || err.Error() != "unrecognized showings document" {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestParseShowingsAcceptsExplicitEmptyMarker(t *testing.T) {
	body := `<section id="showings"><p class="no-result">Aucune séance</p></section>`
	records, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err != nil || len(records) != 0 {
		t.Fatalf("records=%v error=%v", records, err)
	}
}

func TestParseShowingsAcceptsNextSessionOnlyFragment(t *testing.T) {
	records, err := ParseShowings(fixture(t, "showings-next-session-only.html"), Cinema{ProviderID: "11"}, "2026-08-14")
	if err != nil || len(records) != 0 {
		t.Fatalf("records=%v error=%v", records, err)
	}
}

func TestParseShowingsAcceptsAuthenticatedBlocksWithPlaceholders(t *testing.T) {
	records, err := ParseShowings(fixture(t, "showings-next-session-placeholders.html"), Cinema{ProviderID: "30"}, "2026-08-14")
	if err != nil || len(records) != 0 {
		t.Fatalf("records=%v error=%v", records, err)
	}
}

func TestParseShowingsAcceptsAuthenticatedInertBlocks(t *testing.T) {
	records, err := ParseShowings(fixture(t, "showings-next-session-inert.html"), Cinema{ProviderID: "18"}, "2026-08-14")
	if err != nil || len(records) != 0 {
		t.Fatalf("records=%v error=%v", records, err)
	}
}

func TestParseShowingsAcceptsCinema36PackagePlaceholder(t *testing.T) {
	records, err := ParseShowings(fixture(t, "showings-next-session-package-placeholder.html"), Cinema{ProviderID: "36"}, "2026-08-16")
	if err != nil || len(records) != 0 {
		t.Fatalf("records=%v error=%v", records, err)
	}
}

func TestParseShowingsCinema36PlaceholderStillRejectsIdentityLink(t *testing.T) {
	body, err := io.ReadAll(fixture(t, "showings-next-session-package-placeholder.html"))
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.Replace(string(body), `href="javascript:void(0);"`, `href="film_package_999.html?cinemaId=36"`, 1))
	records, err := ParseShowings(strings.NewReader(string(body)), Cinema{ProviderID: "36"}, "2026-08-16")
	if err == nil || err.Error() != "unrecognized showings document" {
		t.Fatalf("records=%v error=%v", records, err)
	}
}

func TestParseShowingsNextSessionOnlyAllowsMarkerlessAuthenticatedBlock(t *testing.T) {
	body := nextSessionBlock("1", "film_test_1.html?cinemaId=11", "Prochaine séance le samedi 15 août 2026") + nextSessionBlock("2", "film_test_2.html?cinemaId=11", "")
	records, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "11"}, "2026-08-14")
	if err != nil || len(records) != 0 {
		t.Fatalf("records=%v error=%v", records, err)
	}
}

func TestParseShowingsNextSessionOnlySupportsFrenchMonths(t *testing.T) {
	months := []string{
		"janvier", "janv", "janv.", "février", "févr", "févr.", "mars", "avril", "avr", "avr.",
		"mai", "juin", "juillet", "juil", "juil.", "août", "septembre", "sept", "sept.",
		"octobre", "oct", "oct.", "novembre", "nov", "nov.", "décembre", "déc", "déc.",
	}
	for _, month := range months {
		t.Run(month, func(t *testing.T) {
			body := nextSessionBlock("1", "film_test_1.html?cinemaId=11", "Prochaine séance le 15 "+month+" 2027")
			records, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "11"}, "2026-12-31")
			if err != nil || len(records) != 0 {
				t.Fatalf("records=%v error=%v", records, err)
			}
		})
	}
}

func TestParseShowingsNextSessionOnlyRejectsInvalidPlaceholders(t *testing.T) {
	validBlock := nextSessionBlock("1", "film_test_1.html?cinemaId=30", "Prochaine séance le 5 sept. 2026")
	placeholder := func(content string) string {
		return `<article id="bloc-showing-film-">` + content + `</article>`
	}
	validMarker := `<p>Prochaine séance le 5 sept. 2026</p>`
	tests := []struct {
		name string
		body string
	}{
		{name: "placeholder only", body: placeholder(validMarker)},
		{name: "canonical heading", body: validBlock + placeholder(`<div class="block--title text-uppercase"><a class="color--dark-blue">Film</a></div>`+validMarker)},
		{name: "detail link", body: validBlock + placeholder(`<a href="film_test_1.html?cinemaId=30">Film</a>`+validMarker)},
		{name: "film identity attribute", body: validBlock + placeholder(`<span data-film="1"></span>`+validMarker)},
		{name: "cinema identity attribute", body: validBlock + placeholder(`<span data-cinema-id="30"></span>`+validMarker)},
		{name: "showing attribute", body: validBlock + placeholder(`<span data-showing="1"></span>`+validMarker)},
		{name: "structural session", body: validBlock + placeholder(`<div class="session"></div>`+validMarker)},
		{name: "missing marker", body: validBlock + placeholder("")},
		{name: "duplicate marker", body: validBlock + placeholder(validMarker+validMarker)},
		{name: "malformed marker", body: validBlock + placeholder(`<p>Prochaine séance le 5 sep. 2026</p>`)},
		{name: "nonfuture marker", body: validBlock + placeholder(`<p>Prochaine séance le vendredi 14 août 2026</p>`)},
		{name: "text suffix", body: validBlock + strings.Replace(placeholder(validMarker), `bloc-showing-film-`, `bloc-showing-film-placeholder`, 1)},
		{name: "zero suffix", body: validBlock + strings.Replace(placeholder(validMarker), `bloc-showing-film-`, `bloc-showing-film-0`, 1)},
		{name: "punctuated suffix", body: validBlock + strings.Replace(placeholder(validMarker), `bloc-showing-film-`, `bloc-showing-film-.`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if records, err := ParseShowings(strings.NewReader(test.body), Cinema{ProviderID: "30"}, "2026-08-14"); err == nil {
				t.Fatalf("accepted records=%v", records)
			}
		})
	}
}

func TestParseShowingsNextSessionOnlyRejectsInvalidFragments(t *testing.T) {
	valid := func(marker string) string {
		return nextSessionBlock("1", "film_test_1.html?cinemaId=11", marker)
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "duplicate marker", body: nextSessionBlock("1", "film_test_1.html?cinemaId=11", "Prochaine séance le samedi 15 août 2026</p><p>Prochaine séance le samedi 15 août 2026")},
		{name: "zero document markers", body: valid("") + nextSessionBlock("2", "film_test_2.html?cinemaId=11", "")},
		{name: "same date", body: valid("Prochaine séance le vendredi 14 août 2026")},
		{name: "earlier date", body: valid("Prochaine séance le jeudi 13 août 2026")},
		{name: "impossible date", body: valid("Prochaine séance le 31 février 2027")},
		{name: "malformed month", body: valid("Prochaine séance le 15 aout 2026")},
		{name: "wrong weekday", body: valid("Prochaine séance le lundi 15 août 2026")},
		{name: "generic text", body: valid("Séance disponible demain")},
		{name: "wrong film identity", body: nextSessionBlock("1", "film_test_2.html?cinemaId=11", "Prochaine séance le samedi 15 août 2026")},
		{name: "wrong cinema identity", body: nextSessionBlock("1", "film_test_1.html?cinemaId=12", "Prochaine séance le samedi 15 août 2026")},
		{name: "unspaced malformed block identity", body: strings.Replace(valid("Prochaine séance le samedi 15 août 2026"), "bloc-showing-film-1", "bloc-showing-film-0", 1)},
		{name: "duplicate block identity", body: valid("Prochaine séance le samedi 15 août 2026") + valid("Prochaine séance le samedi 15 août 2026")},
		{name: "missing canonical heading", body: `<article id="bloc-showing-film-1"><a href="film_test_1.html?cinemaId=11">Film</a><p>Prochaine séance le samedi 15 août 2026</p></article>`},
		{name: "duplicate canonical heading", body: `<article id="bloc-showing-film-1"><div class="block--title text-uppercase"><a class="color--dark-blue" href="film_test_1.html?cinemaId=11">Film</a><a class="color--dark-blue" href="film_test_1.html?cinemaId=11">Film</a></div><p>Prochaine séance le samedi 15 août 2026</p></article>`},
		{name: "mixed showing candidate", body: strings.Replace(valid("Prochaine séance le samedi 15 août 2026"), "</article>", `<div class="session"></div></article>`, 1)},
		{name: "marker outside block", body: nextSessionBlock("1", "film_test_1.html?cinemaId=11", "") + `<p>Prochaine séance le samedi 15 août 2026</p>`},
		{name: "extra marker outside marked block", body: valid("Prochaine séance le samedi 15 août 2026") + `<p>Prochaine séance le samedi 15 août 2026</p>`},
		{name: "malformed marker on inert block", body: valid("Prochaine séance le samedi 15 août 2026") + nextSessionBlock("2", "film_test_2.html?cinemaId=11", "Prochaine séance le 15 aout 2026")},
		{name: "unrecognized nested root", body: `<section id="showings"><div>` + valid("Prochaine séance le samedi 15 août 2026") + `</div></section>`},
		{name: "split recognized roots", body: `<section id="showings">` + valid("Prochaine séance le samedi 15 août 2026") + `</section><section id="showings">` + nextSessionBlock("2", "film_test_2.html?cinemaId=11", "") + `</section>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if records, err := ParseShowings(strings.NewReader(test.body), Cinema{ProviderID: "11"}, "2026-08-14"); err == nil {
				t.Fatalf("accepted records=%v", records)
			}
		})
	}
}

func TestParseShowingsNextSessionOnlyFilmBlockLimit(t *testing.T) {
	var body strings.Builder
	for id := 1; id <= MaxFilmBlocksPerResponse+1; id++ {
		filmID := strconv.Itoa(id)
		body.WriteString(nextSessionBlock(filmID, "film_test_"+filmID+".html?cinemaId=11", "Prochaine séance le samedi 15 août 2026"))
	}
	_, err := ParseShowings(strings.NewReader(body.String()), Cinema{ProviderID: "11"}, "2026-08-14")
	if err == nil || err.Error() != "film block limit exceeded" {
		t.Fatalf("error=%v", err)
	}
}

func TestParseShowingsNextSessionOnlyPlaceholderLimit(t *testing.T) {
	var body strings.Builder
	body.WriteString(nextSessionBlock("1", "film_test_1.html?cinemaId=11", "Prochaine séance le samedi 15 août 2026"))
	for range MaxFilmBlocksPerResponse {
		body.WriteString(`<article id="bloc-showing-film-"><p>Prochaine séance le samedi 15 août 2026</p></article>`)
	}
	_, err := ParseShowings(strings.NewReader(body.String()), Cinema{ProviderID: "11"}, "2026-08-14")
	if err == nil || err.Error() != "film block limit exceeded" {
		t.Fatalf("error=%v", err)
	}
}

func nextSessionBlock(filmID, href, marker string) string {
	markerHTML := ""
	if marker != "" {
		markerHTML = "<p>" + marker + "</p>"
	}
	return `<article id="bloc-showing-film-` + filmID + `"><div class="block--title text-uppercase"><a class="color--dark-blue" href="` + href + `">Film</a></div>` + markerHTML + `</article>`
}

func TestParseShowingsRejectsMixedValidAndPartialCandidates(t *testing.T) {
	_, err := ParseShowings(fixture(t, "showings-mixed-partial.html"), Cinema{ProviderID: "25"}, "2026-08-15")
	if err == nil || err.Error() != "showing required attribute missing or conflicting" {
		t.Fatalf("error=%v", err)
	}
}

func TestParseShowingsRejectsScreeningStructureWithoutAttributes(t *testing.T) {
	body := `<div id="bloc-showing-film-1"><a data-film="1" title="Film">Film</a><span>(2h)</span><button data-showing="10" data-film="1" data-cinema="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button><div><span class="screening-room">Salle 2</span><button>Réserver</button></div></div>`
	_, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err == nil || err.Error() != "showing required attribute missing or conflicting" {
		t.Fatalf("error=%v", err)
	}
}

func TestParseShowingsRejectsCandidateOutsideCanonicalBlock(t *testing.T) {
	_, err := ParseShowings(fixture(t, "showings-changed-owner.html"), Cinema{ProviderID: "25"}, "2026-08-15")
	if err == nil || err.Error() != "unrecognized showing ownership" {
		t.Fatalf("error=%v", err)
	}
}

func TestParseShowingsIgnoresOrdinaryUnrelatedButton(t *testing.T) {
	body := `<main><button class="navigation">Menu</button><div id="bloc-showing-film-1"><a data-film="1" title="Film">Film</a><span>(2h)</span><button data-showing="10" data-film="1" data-cinema="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button></div></main>`
	records, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v error=%v", records, err)
	}
}

func TestParseShowingsAllowsNonShowingFilmBlock(t *testing.T) {
	records, err := ParseShowings(fixture(t, "showings-non-showing-block.html"), Cinema{ProviderID: "25"}, "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ProviderShowingID != "10" {
		t.Fatalf("records=%+v", records)
	}
}

func TestParseShowingsRuntimeBounds(t *testing.T) {
	validButton := `<button data-showing="10" data-film="1" data-cinema="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="12:00"></button>`
	maxDurationMinutes := int(int64(^uint64(0)>>1) / int64(time.Minute))
	runtimeText := func(minutes int) string {
		return fmt.Sprintf("(%dh%02d)", minutes/60, minutes%60)
	}
	tests := []struct {
		name    string
		runtime string
		want    int
	}{
		{name: "long runtime", runtime: "(12h01)", want: 12*60 + 1},
		{name: "very long runtime", runtime: "(999999h)", want: 999999 * 60},
		{name: "largest duration", runtime: runtimeText(maxDurationMinutes), want: maxDurationMinutes},
		{name: "invalid minutes", runtime: "(10h60)"},
		{name: "duration overflow", runtime: runtimeText(maxDurationMinutes + 1)},
		{name: "atoi overflow", runtime: "(999999999999999999999999999999999999999999h)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := `<div id="bloc-showing-film-1"><a data-film="1" title="Film">Film</a><span>` + test.runtime + `</span>` + validButton + `</div>`
			records, err := ParseShowings(strings.NewReader(body), Cinema{ProviderID: "25"}, "2026-08-15")
			if test.want == 0 {
				if err == nil || err.Error() != "invalid film runtime" {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil || len(records) != 1 || records[0].Movie.RuntimeMinutes != test.want || records[0].EndTime.Sub(records[0].StartTime) != time.Duration(test.want)*time.Minute {
				t.Fatalf("records=%+v error=%v", records, err)
			}
		})
	}
}

func TestParserResourceLimits(t *testing.T) {
	t.Run("cinemas", func(t *testing.T) {
		var sitemap strings.Builder
		sitemap.WriteString(`<?xml version="1.0"?><urlset>`)
		for id := 1; id <= schedule.MaxTheaters+1; id++ {
			fmt.Fprintf(&sitemap, `<url><loc>https://www.ugc.fr/cinema.html?id=%d</loc></url>`, id)
		}
		sitemap.WriteString(`</urlset>`)
		if _, err := ParseSitemap(strings.NewReader(sitemap.String())); err == nil || err.Error() != "sitemap cinema limit exceeded" {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("advertised dates", func(t *testing.T) {
		for _, test := range []struct {
			name      string
			dateCount int
			wantError string
		}{
			{name: "maximum", dateCount: schedule.MaxAdvertisedDatesPerTheater},
			{name: "above maximum", dateCount: schedule.MaxAdvertisedDatesPerTheater + 1, wantError: "cinema advertised date limit exceeded"},
		} {
			t.Run(test.name, func(t *testing.T) {
				var page strings.Builder
				page.WriteString(`<html><head><title>UGC Test, cinéma à Lille (59000)</title></head><body><section id="cinema-heading"><h1>UGC Test</h1><p class="address">Adresse</p></section><input name="cinemaId" value="25">`)
				start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				for offset := 0; offset < test.dateCount; offset++ {
					fmt.Fprintf(&page, `<button id="nav_date_%s"></button>`, start.AddDate(0, 0, offset).Format("2006-01-02"))
				}
				page.WriteString(`</body></html>`)
				cinema, err := ParseCinema(strings.NewReader(page.String()), "25")
				if test.wantError != "" {
					if err == nil || err.Error() != test.wantError {
						t.Fatalf("error=%v", err)
					}
					return
				}
				if err != nil || len(cinema.AdvertisedDates) != test.dateCount {
					t.Fatalf("advertised dates=%d error=%v", len(cinema.AdvertisedDates), err)
				}
			})
		}
	})
	t.Run("showings response", func(t *testing.T) {
		var page strings.Builder
		page.WriteString(`<div id="bloc-showing-film-1">`)
		for id := 1; id <= MaxShowingsPerResponse+1; id++ {
			fmt.Fprintf(&page, `<button data-showing="%d"></button>`, id)
		}
		page.WriteString(`</div>`)
		if _, err := ParseShowings(strings.NewReader(page.String()), Cinema{ProviderID: "25"}, "2026-08-15"); err == nil || err.Error() != "showings response limit exceeded" {
			t.Fatalf("error=%v", err)
		}
	})
}
