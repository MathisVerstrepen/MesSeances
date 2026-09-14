package enrichment

import "testing"

func TestCinevilleSourceIdentityAndPosterPolicy(t *testing.T) {
	for _, id := range []string{"1", "-693091020261", "2714300920262", "-9223372036854775808", "9223372036854775807"} {
		if !validSourceIdentity(SourceCineville, id) {
			t.Fatalf("valid visa rejected: %s", id)
		}
	}
	for _, id := range []string{"0", "01", "-0", "+1", "1e3", "1.0", "9223372036854775808", "-9223372036854775809"} {
		if validSourceIdentity(SourceCineville, id) {
			t.Fatalf("invalid visa accepted: %s", id)
		}
	}
	const prefix = "https://storage.googleapis.com/cineville-files-prod/images/"
	if !validSourcePosterURL(SourceCineville, prefix+"poster.webp") {
		t.Fatal("valid source poster rejected")
	}
	for _, url := range []string{prefix, prefix + "../x.jpg", prefix + "x.jpg?token=secret", prefix + "x%2Fy.jpg", "https://storage.googleapis.com/other-bucket/images/x.jpg", "https://www.cineville.fr/images/x.jpg"} {
		if validSourcePosterURL(SourceCineville, url) {
			t.Fatal("unsafe source poster accepted")
		}
	}
}
