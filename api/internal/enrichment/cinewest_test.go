package enrichment

import "testing"

func TestCinewestSourcePolicy(t *testing.T) {
	for _, id := range []string{"cineoffice-1", "ticketingcine-ABCDE", "ticketingcine-EMS0042-emsx0042HC123", "webediamovies-1"} {
		if !validSourceIdentity(SourceCinewest, id) {
			t.Fatal("namespaced source rejected")
		}
	}
	for _, id := range []string{"1", "ABCDE", "ticketingcine-EMS0042-emsx1185HC123", "webediamovies-0"} {
		if validSourceIdentity(SourceCinewest, id) {
			t.Fatal("invalid source accepted")
		}
	}
	if !validSourcePosterURL(SourceCinewest, "") || !validSourcePosterURL(SourceCinewest, "https://images.monnaie-services.com/movie_poster/600/FRABCDE/12345678.webp") || validSourcePosterURL(SourceCinewest, "https://evil.test/poster.jpg") {
		t.Fatal("poster policy")
	}
}
