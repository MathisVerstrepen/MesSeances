package enrichment

import "testing"

func TestMajesticSourceIdentityAndPoster(t *testing.T) {
	for _, id := range []string{"ABCDE", "EMS0700-emsx0700HC12"} {
		if !validSourceIdentity(SourceMajestic, id) {
			t.Fatal(id)
		}
	}
	if validSourceIdentity(SourceMajestic, "EMS0700-emsx0654HC12") || !validSourcePosterURL(SourceMajestic, "") || !validSourcePosterURL(SourceMajestic, "https://images.monnaie-services.com/ems_spectacle/120/0700/HC12.jpg?ts=123") || validSourcePosterURL(SourceMajestic, "https://evil.example/a") {
		t.Fatal("identity/poster policy")
	}
}
