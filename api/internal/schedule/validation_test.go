package schedule

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestValidInclusiveDateWindowAcrossParisDST(t *testing.T) {
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, location)
	if !ValidInclusiveDateWindow(from, from.AddDate(0, 0, 13)) {
		t.Fatal("14 inclusive calendar days rejected")
	}
	if ValidInclusiveDateWindow(from, from.AddDate(0, 0, 14)) {
		t.Fatal("15 inclusive calendar days accepted")
	}
	data := testDataset()
	data.Window = Window{From: "2026-10-18", Through: "2026-10-31"}
	for i := range data.Theaters {
		data.Theaters[i].AvailableDates = []string{"2026-10-18"}
	}
	for i := range data.Showtimes {
		local := data.Showtimes[i].StartTime.In(location)
		startDate := from
		if local.Hour() <= 2 {
			startDate = from.AddDate(0, 0, 1)
		}
		start := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), local.Hour(), local.Minute(), 0, 0, location)
		data.Showtimes[i].ServiceDate = "2026-10-18"
		data.Showtimes[i].StartTime = start
		data.Showtimes[i].EndTime = start.Add(time.Duration(data.Showtimes[i].Movie.RuntimeMinutes) * time.Minute)
	}
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("valid dataset rejected: %v", err)
	}
	data.Window.Through = "2026-11-01"
	if err := ValidateDataset(data, true); err == nil || err.Error() != "invalid dataset window" {
		t.Fatalf("error=%v", err)
	}
}

func TestValidateDatasetResourceAndRuntimeBounds(t *testing.T) {
	data := testDataset()
	data.Theaters[0].Name = strings.Repeat("x", maxNameAndTitleLength+1)
	if err := ValidateDataset(data, true); err == nil || err.Error() != "theater field limit exceeded" {
		t.Fatalf("error=%v", err)
	}
	data = testDataset()
	showing := &data.Showtimes[0]
	showing.Movie.RuntimeMinutes = 12 * 60
	showing.EndTime = showing.StartTime.Add(12 * time.Hour)
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("long runtime rejected: %v", err)
	}
	showing.EndTime = showing.StartTime.Add(11*time.Hour + 59*time.Minute)
	if err := ValidateDataset(data, true); err == nil || err.Error() != "invalid showing times" {
		t.Fatalf("error=%v", err)
	}
}

func TestValidateDatasetAcceptsTheatersAboveFormerRecordCap(t *testing.T) {
	data := testDataset()
	template := data.Theaters[0]
	data.Theaters = make([]TheaterRecord, 257)
	for index := range data.Theaters {
		id := strconv.Itoa(index + 1)
		theater := template
		theater.ID = "ugc-" + id
		theater.ProviderID = id
		theater.Slug = theater.ID
		data.Theaters[index] = theater
	}
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("257-theater dataset rejected: %v", err)
	}
}

func TestRuntimeDurationRepresentationBounds(t *testing.T) {
	maxMinutes := int(int64(^uint64(0)>>1) / int64(time.Minute))
	for _, test := range []struct {
		name    string
		minutes int
		valid   bool
	}{
		{name: "negative", minutes: -1},
		{name: "zero", minutes: 0},
		{name: "long runtime", minutes: 12 * 60, valid: true},
		{name: "largest representable", minutes: maxMinutes, valid: true},
		{name: "duration overflow", minutes: maxMinutes + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			duration, ok := RuntimeDuration(test.minutes)
			if ok != test.valid {
				t.Fatalf("RuntimeDuration(%d) valid=%v want=%v", test.minutes, ok, test.valid)
			}
			if ok && duration/time.Minute != time.Duration(test.minutes) {
				t.Fatalf("RuntimeDuration(%d)=%v", test.minutes, duration)
			}
		})
	}
}

func TestValidateDatasetScopeIdentityAndURLs(t *testing.T) {
	data := testDataset()
	data.Scope = ScopeSingle
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("single scope accepted as complete")
	}
	if err := ValidateDataset(data, false); err != nil {
		t.Fatalf("single scope rejected for diagnostics: %v", err)
	}
	data = testDataset()
	data.Showtimes[0].BookingURL = "https://evil.example/reservationSeances.html?id=100"
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("untrusted URL accepted")
	}
}

func TestValidateDatasetRejectsUnsafeBackdropURLs(t *testing.T) {
	valid := testDataset()
	valid.Showtimes[0].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, BackdropURL: "https://image.tmdb.org/t/p/w780/a.jpg"}
	if err := ValidateDataset(valid, true); err != nil {
		t.Fatalf("valid backdrop rejected: %v", err)
	}
	for _, raw := range []string{
		"http://image.tmdb.org/t/p/w780/a.jpg",
		"https://evil.example/t/p/w780/a.jpg",
		"https://image.tmdb.org/t/p/w500/a.jpg",
		"https://image.tmdb.org:443/t/p/w780/a.jpg",
		"https://image.tmdb.org/t/p/w780/../a.jpg",
		"https://image.tmdb.org/t/p/w780/a.jpg?x=1",
		"https://image.tmdb.org/t/p/w780/",
		"https://image.tmdb.org/t/p/w780//a.jpg",
		"https://image.tmdb.org/t/p/w780/%2e%2e/a.jpg",
	} {
		data := testDataset()
		data.Showtimes[0].Movie.Enrichment = &MovieEnrichment{TMDBID: 42, BackdropURL: raw}
		if err := ValidateDataset(data, true); err == nil {
			t.Fatalf("unsafe backdrop accepted: %q", raw)
		}
	}
}

func TestValidateKinepolisDatasetIdentityPassesAndURLs(t *testing.T) {
	data := kinepolisTestDataset()
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("valid Kinepolis dataset rejected: %v", err)
	}
	data.Showtimes[0].BookingURL = "https://www.ugc.fr/reservationSeances.html?id=VS1"
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("cross-provider booking URL accepted")
	}
	data = kinepolisTestDataset()
	data.Showtimes[0].Movie.PosterURL = "https://evil.example/images/poster.jpg"
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("cross-provider image URL accepted")
	}
	data = kinepolisTestDataset()
	data.Theaters[0].AcceptedPasses = []string{"UGC_ILLIMITE"}
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("unsupported Kinepolis pass accepted")
	}
}

func TestValidateDatasetCanonicalFormats(t *testing.T) {
	for _, format := range []string{Format2D, Format3D, FormatIMAX, FormatDolby, FormatScreenX, FormatLaserUltra, Format4DX} {
		data := testDataset()
		data.Showtimes[0].Format = format
		if err := ValidateDataset(data, true); err != nil {
			t.Errorf("canonical format %q rejected: %v", format, err)
		}
	}
	for _, format := range []string{FormatAll, "screenx", "LASER ULTRA"} {
		data := testDataset()
		data.Showtimes[0].Format = format
		if err := ValidateDataset(data, true); err == nil {
			t.Errorf("non-canonical dataset format %q accepted", format)
		}
	}
}

func TestValidateDatasetLocalMovieInvariants(t *testing.T) {
	valid := testDataset()
	for index := range valid.Showtimes {
		if valid.Showtimes[index].Movie.ProviderID != "200" {
			continue
		}
		valid.Showtimes[index].Movie.LocalMovieID = 9
		valid.Showtimes[index].Movie.LocalMetadataProvider = ProviderUGC
	}
	if err := ValidateDataset(valid, true); err != nil {
		t.Fatalf("valid local identity rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Dataset)
	}{
		{"negative ID", func(data *Dataset) { data.Showtimes[0].Movie.LocalMovieID = -1 }},
		{"metadata provider without ID", func(data *Dataset) { data.Showtimes[2].Movie.LocalMetadataProvider = ProviderUGC }},
		{"invalid metadata provider", func(data *Dataset) { data.Showtimes[0].Movie.LocalMetadataProvider = "tmdb" }},
		{"TMDB overlap", func(data *Dataset) { data.Showtimes[0].Movie.Enrichment = &MovieEnrichment{TMDBID: 42} }},
		{"inconsistent metadata", func(data *Dataset) { data.Showtimes[1].Movie.Title = "Autre titre" }},
		{"inconsistent source identity", func(data *Dataset) { data.Showtimes[1].Movie.LocalMovieID = 10 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := cloneDataset(valid)
			test.mutate(&data)
			if err := ValidateDataset(data, true); err == nil {
				t.Fatal("invalid local movie accepted")
			}
		})
	}

	crossProviderPoster := combinedTestDataset()
	for index := range crossProviderPoster.Showtimes {
		movie := &crossProviderPoster.Showtimes[index].Movie
		if movie.ProviderID != "200" && movie.ProviderID != "HO200" {
			continue
		}
		movie.Enrichment = nil
		movie.LocalMovieID = 10
		movie.LocalMetadataProvider = ProviderKinepolis
		movie.Title = "Fallback Kinepolis"
		movie.RuntimeMinutes = 100
		movie.PosterURL = "https://cdn.kinepolis.fr/images/posters/fallback.jpg"
		movie.Overview = ""
		movie.ReleaseDate = ""
		movie.Genres = nil
	}
	if err := ValidateDataset(crossProviderPoster, true); err != nil {
		t.Fatalf("canonical cross-provider poster rejected: %v", err)
	}
}

func TestValidateKinepolisPosterURLAllowsDotsInUnicodeFilenameAndRejectsTraversal(t *testing.T) {
	data := kinepolisTestDataset()
	data.Showtimes[0].Movie.PosterURL = "https://cdn.kinepolis.fr/images/FR/65459BAD/HO00016344/0000027026/Visite_déquipe_:_Ducobu_et_le_fantôme_de_Sa....jpg"
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("valid unicode Kinepolis poster rejected: %v", err)
	}

	for _, raw := range []string{
		"https://cdn.kinepolis.fr/images/../etc/passwd",
		"https://cdn.kinepolis.fr/images/a/../../b",
	} {
		data := kinepolisTestDataset()
		data.Showtimes[0].Movie.PosterURL = raw
		if err := ValidateDataset(data, true); err == nil {
			t.Fatalf("traversal poster accepted: %q", raw)
		}
	}
}
