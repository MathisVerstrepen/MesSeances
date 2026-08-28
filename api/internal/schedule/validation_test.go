package schedule

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestValidateDatasetCoordinatesAndClonePointers(t *testing.T) {
	validLatitude, validLongitude := 50.6321, 3.0612
	data := testDataset()
	data.Theaters[0].Latitude, data.Theaters[0].Longitude = &validLatitude, &validLongitude
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("valid coordinates rejected: %v", err)
	}
	clone := cloneDataset(data)
	*clone.Theaters[0].Latitude = 1
	if *data.Theaters[0].Latitude != validLatitude {
		t.Fatal("dataset clone shared coordinate pointer")
	}
	for _, test := range []struct {
		name      string
		latitude  *float64
		longitude *float64
	}{
		{name: "latitude only", latitude: &validLatitude},
		{name: "longitude only", longitude: &validLongitude},
		{name: "latitude range", latitude: floatPointer(91), longitude: &validLongitude},
		{name: "longitude range", latitude: &validLatitude, longitude: floatPointer(-181)},
		{name: "nan", latitude: floatPointer(math.NaN()), longitude: &validLongitude},
		{name: "infinity", latitude: &validLatitude, longitude: floatPointer(math.Inf(1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := testDataset()
			candidate.Theaters[0].Latitude, candidate.Theaters[0].Longitude = test.latitude, test.longitude
			if err := ValidateDataset(candidate, true); err == nil || err.Error() != "invalid theater coordinates" {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func floatPointer(value float64) *float64 { return &value }

func TestValidInclusiveDateWindowAcrossParisDST(t *testing.T) {
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, location)
	if !ValidInclusiveDateWindow(from, from.AddDate(0, 0, 90)) {
		t.Fatal("long ordered window rejected")
	}
	if ValidInclusiveDateWindow(from, from.AddDate(0, 0, -1)) {
		t.Fatal("reversed window accepted")
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
	data.Window.Through = "2027-02-01"
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("long valid dataset window rejected: %v", err)
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
	showing.EndTime = showing.StartTime
	if err := ValidateDataset(data, true); err == nil || err.Error() != "invalid showing times" {
		t.Fatalf("error=%v", err)
	}
}

func TestValidateDatasetProviderTimingRules(t *testing.T) {
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		t.Fatal(err)
	}
	canonicalEnd := time.Date(2026, 8, 15, 15, 23, 0, 0, location)
	ugc := testDataset()
	ugc.Showtimes[0].EndTime = canonicalEnd
	if err := ValidateDataset(ugc, true); err != nil {
		t.Fatalf("canonical UGC end rejected: %v", err)
	}
	pathe := patheTestDataset()
	pathe.Showtimes[0].EndTime = pathe.Showtimes[0].StartTime.Add(time.Duration(pathe.Showtimes[0].Movie.RuntimeMinutes+20) * time.Minute)
	if err := ValidateDataset(pathe, true); err != nil {
		t.Fatalf("provider Pathé end rejected: %v", err)
	}
	for _, test := range []struct {
		name string
		end  func(time.Time) time.Time
	}{
		{name: "zero", end: func(time.Time) time.Time { return time.Time{} }},
		{name: "equal", end: func(start time.Time) time.Time { return start }},
		{name: "reversed", end: func(start time.Time) time.Time { return start.Add(-time.Minute) }},
	} {
		t.Run("UGC "+test.name, func(t *testing.T) {
			data := testDataset()
			data.Showtimes[0].EndTime = test.end(data.Showtimes[0].StartTime)
			if err := ValidateDataset(data, true); err == nil || err.Error() != "invalid showing times" {
				t.Fatalf("error=%v", err)
			}
		})
	}
	kinepolis := kinepolisTestDataset()
	kinepolis.Showtimes[0].EndTime = kinepolis.Showtimes[0].EndTime.Add(time.Minute)
	if err := ValidateDataset(kinepolis, true); err == nil || err.Error() != "invalid showing times" {
		t.Fatalf("Kinepolis mismatch error=%v", err)
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

func TestValidCGRBookingURL(t *testing.T) {
	if !validBookingURL(ProviderCGR, "https://achat.cgrcinemas.fr/lille-synthetique/r/123456", "ignored", "W8010") {
		t.Fatal("valid CGR booking URL rejected")
	}
	for _, raw := range []string{
		"http://achat.cgrcinemas.fr/lille/r/123",
		"https://www.cgrcinemas.fr/lille/r/123",
		"https://achat.cgrcinemas.fr:443/lille/r/123",
		"https://achat.cgrcinemas.fr/Lille/r/123",
		"https://achat.cgrcinemas.fr/lille/r/0",
		"https://achat.cgrcinemas.fr/lille/r/123/",
		"https://achat.cgrcinemas.fr/lille/r/123?source=test",
		"https://achat.cgrcinemas.fr/lille/r/123#test",
		"https://user@achat.cgrcinemas.fr/lille/r/123",
		"https://achat.cgrcinemas.fr/lille/../r/123",
	} {
		if validBookingURL(ProviderCGR, raw, "ignored", "W8010") {
			t.Fatalf("unsafe CGR booking URL accepted: %q", raw)
		}
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

func TestValidateDatasetRejectsMalformedPublicMovieTrailerYouTubeKey(t *testing.T) {
	data := combinedTestDataset()
	for index := range data.Showtimes {
		if data.Showtimes[index].Movie.ProviderID == "200" || data.Showtimes[index].Movie.ProviderID == "HO200" {
			data.Showtimes[index].Movie.PublicMovieID = 1
		}
	}
	data.PublicMovies = []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderUGC, IdentityAnchorSourceID: "200", Title: "Film", RuntimeMinutes: 100, TMDBID: 42, TrailerVFYouTubeKey: "FRoff123456", TrailerVOYouTubeKey: "ENoff123456", UpdatedAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)}}
	data.MovieSources = []PublicMovieSourceRecord{
		{Provider: ProviderUGC, SourceMovieID: "200", PublicMovieID: 1, SourceSlug: "ugc-film-200", Title: "Film", RuntimeMinutes: 100},
		{Provider: ProviderKinepolis, SourceMovieID: "HO200", PublicMovieID: 1, SourceSlug: "kinepolis-film-HO200", Title: "Film", RuntimeMinutes: 100},
	}
	data.MovieAliases = []MovieSlugAliasRecord{}
	filtered := data.Showtimes[:0]
	for _, showing := range data.Showtimes {
		if showing.Movie.ProviderID == "200" || showing.Movie.ProviderID == "HO200" {
			filtered = append(filtered, showing)
		}
	}
	data.Showtimes = filtered
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("valid trailer key rejected: %v", err)
	}
	data.PublicMovies[0].TrailerVOYouTubeKey = "invalid"
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("malformed trailer key accepted")
	}
	data.PublicMovies[0].TrailerVOYouTubeKey = data.PublicMovies[0].TrailerVFYouTubeKey
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("duplicate trailer keys accepted")
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

func TestValidatePatheDatasetIdentityPassesAndURLs(t *testing.T) {
	data := patheTestDataset()
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("valid Pathé dataset rejected: %v", err)
	}
	data = patheTestDataset()
	longID := "V1S" + strings.Repeat("9", maxIdentityLength-len("pathe-showing-")-len("V1S"))
	data.Showtimes[0].ProviderShowingID = longID
	data.Showtimes[0].ID = "pathe-showing-" + longID
	data.Showtimes[0].BookingURL = "https://s.pathe.fr/fr/" + longID + "/booking"
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("maximum Pathé showing identity rejected: %v", err)
	}
	data.Showtimes[0].ProviderShowingID += "9"
	data.Showtimes[0].ID = "pathe-showing-" + data.Showtimes[0].ProviderShowingID
	data.Showtimes[0].BookingURL = "https://s.pathe.fr/fr/" + data.Showtimes[0].ProviderShowingID + "/booking"
	if err := ValidateDataset(data, true); err == nil {
		t.Fatal("oversized Pathé showing identity accepted")
	}
	for _, showingID := range []string{"S135392", "V0S1", "V01S1", "V1S0", "V1S01", "V1S-1", "V1S1_2"} {
		data := patheTestDataset()
		data.Showtimes[0].ProviderShowingID = showingID
		data.Showtimes[0].ID = "pathe-showing-" + showingID
		if err := ValidateDataset(data, true); err == nil {
			t.Fatalf("invalid Pathé showing identity accepted: %q", showingID)
		}
	}
	for _, bookingURL := range []string{
		"http://s.pathe.fr/fr/V3308S135392/booking",
		"https://www.pathe.fr/fr/V3308S135392/booking",
		"https://s.pathe.fr/fr/V3308S135393/booking",
		"https://s.pathe.fr/fr/V9999S135392/booking",
		"https://s.pathe.fr/fr/V3308S135392/booking?source=test",
		"https://s.pathe.fr/fr/V3308S135392/booking?",
		"https://s.pathe.fr/fr/./booking",
		"https://s.pathe.fr/fr/a/V3308S135392/booking",
	} {
		data := patheTestDataset()
		data.Showtimes[0].BookingURL = bookingURL
		if err := ValidateDataset(data, true); err == nil {
			t.Fatalf("unsafe Pathé booking URL accepted: %q", bookingURL)
		}
	}
	for _, posterURL := range []string{
		"http://www.pathe.fr/media/poster.jpg",
		"https://evil.example/media/poster.jpg",
		"https://www.pathe.fr:443/media/poster.jpg",
		"https://www.pathe.fr/media/../poster.jpg",
		"https://www.pathe.fr/media/poster.jpg?source=test",
		"https://www.pathe.fr/media/poster.jpg?",
		"https://www.pathe.fr/media/./poster.jpg",
		"https://www.pathe.fr/",
	} {
		data := patheTestDataset()
		data.Showtimes[0].Movie.PosterURL = posterURL
		if err := ValidateDataset(data, true); err == nil {
			t.Fatalf("unsafe Pathé poster URL accepted: %q", posterURL)
		}
	}
	data = patheTestDataset()
	data.Showtimes[0].Movie.PosterURL = "https://media.pathe.fr/posters/a.jpg"
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("valid Pathé subdomain poster rejected: %v", err)
	}
	for _, mutate := range []func(*Dataset){
		func(data *Dataset) { data.Theaters[0].Address = "" },
		func(data *Dataset) { data.Theaters[0].PostalCode = "" },
		func(data *Dataset) { data.Theaters[0].AcceptedPasses = []string{"UGC_ILLIMITE"} },
	} {
		data := patheTestDataset()
		mutate(&data)
		if err := ValidateDataset(data, true); err == nil {
			t.Fatal("invalid Pathé theater accepted")
		}
	}
}

func TestValidatePatheDerivedIdentityLengthBoundaries(t *testing.T) {
	t.Run("cinema", func(t *testing.T) {
		data := patheTestDataset()
		providerID := strings.Repeat("c", maxIdentityLength-len("pathe-"))
		data.Theaters[0].ProviderID = providerID
		data.Theaters[0].ID = "pathe-" + providerID
		data.Theaters[0].Slug = data.Theaters[0].ID
		data.Showtimes[0].TheaterID = data.Theaters[0].ID
		if err := ValidateDataset(data, true); err != nil {
			t.Fatalf("maximum cinema identity rejected: %v", err)
		}
		data.Theaters[0].ProviderID += "c"
		data.Theaters[0].ID = "pathe-" + data.Theaters[0].ProviderID
		data.Theaters[0].Slug = data.Theaters[0].ID
		data.Showtimes[0].TheaterID = data.Theaters[0].ID
		if err := ValidateDataset(data, true); err == nil {
			t.Fatal("oversized cinema source identity accepted")
		}
	})
	t.Run("movie", func(t *testing.T) {
		data := patheTestDataset()
		providerID := strings.Repeat("m", maxIdentityLength-len("pathe-film-"))
		data.Showtimes[0].Movie.ProviderID = providerID
		data.Showtimes[0].Movie.Slug = "pathe-film-" + providerID
		if err := ValidateDataset(data, true); err != nil {
			t.Fatalf("maximum movie identity rejected: %v", err)
		}
		data.Showtimes[0].Movie.ProviderID += "m"
		data.Showtimes[0].Movie.Slug = "pathe-film-" + data.Showtimes[0].Movie.ProviderID
		if err := ValidateDataset(data, true); err == nil {
			t.Fatal("oversized movie source identity accepted")
		}
	})
}

func TestValidatePathePublicMovieIdentityContracts(t *testing.T) {
	data := patheTestDataset()
	data.Showtimes[0].Movie.PublicMovieID = 1
	data.PublicMovies = []PublicMovieRecord{{ID: 1, IdentityAnchorProvider: ProviderPathe, IdentityAnchorSourceID: "film-a", Title: "Film Pathé", RuntimeMinutes: 110, UpdatedAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)}}
	data.MovieSources = []PublicMovieSourceRecord{{Provider: ProviderPathe, SourceMovieID: "film-a", PublicMovieID: 1, SourceSlug: "pathe-film-film-a", Title: "Film Pathé", RuntimeMinutes: 110, PosterURL: "https://www.pathe.fr/media/poster.jpg"}}
	data.MovieAliases = []MovieSlugAliasRecord{{Slug: "pathe-film-film-a", PublicMovieID: 1, Kind: "source", Provider: ProviderPathe, SourceMovieID: "film-a"}}
	if err := ValidateDataset(data, true); err != nil {
		t.Fatalf("valid Pathé public identity rejected: %v", err)
	}
	for _, mutate := range []func(*Dataset){
		func(data *Dataset) { data.PublicMovies[0].IdentityAnchorSourceID = "bad id" },
		func(data *Dataset) { data.MovieSources[0].Provider = "other" },
		func(data *Dataset) { data.MovieSources[0].SourceSlug = "ugc-film-film-a" },
		func(data *Dataset) { data.MovieAliases[0].SourceMovieID = "bad id" },
	} {
		invalid := cloneDataset(data)
		mutate(&invalid)
		if err := ValidateDataset(invalid, true); err == nil {
			t.Fatal("invalid Pathé public identity accepted")
		}
	}
}

func TestValidateDatasetCanonicalFormats(t *testing.T) {
	for _, format := range []Format{Format2D, Format3D, FormatIMAX, FormatDolby, FormatScreenX, FormatLaserUltra, Format4DX, FormatICE} {
		data := testDataset()
		data.Showtimes[0].Format = format
		if err := ValidateDataset(data, true); err != nil {
			t.Errorf("canonical format %q rejected: %v", format, err)
		}
	}
	for _, format := range []Format{FormatAll, "screenx", "LASER ULTRA"} {
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
