package enrichment

import "testing"

func TestVeoSourcePolicy(t *testing.T) {
	for _, id := range []string{"123", "cEvent_1", "ticketingcine-A12B3"} {
		if !validSourceIdentity(SourceVeo, id) {
			t.Fatal(id)
		}
	}
	for _, id := range []string{"0", "01", "c", "ticketingcine-bad"} {
		if validSourceIdentity(SourceVeo, id) {
			t.Fatal(id)
		}
	}
	for _, raw := range []string{"", "https://acsta.net/test.jpg"} {
		if !validSourcePosterURL(SourceVeo, raw) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"https://evil.test/test.jpg", "https://acsta.net/../test.jpg", "https://acsta.net:443/test.jpg"} {
		if validSourcePosterURL(SourceVeo, raw) {
			t.Fatal(raw)
		}
	}
}
