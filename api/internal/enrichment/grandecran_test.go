package enrichment

import (
	"strings"
	"testing"
)

func TestGrandEcranSourcePolicy(t *testing.T) {
	for _, id := range []string{"1", "cEvent_1-x", strings.Repeat("1", 112), "c" + strings.Repeat("a", 111)} {
		if !validSourceIdentity(SourceGrandEcran, id) {
			t.Fatal("valid identity")
		}
	}
	for _, id := range []string{"0", "01", "c", "Cevent", "c/x", strings.Repeat("1", 113)} {
		if validSourceIdentity(SourceGrandEcran, id) {
			t.Fatal("invalid identity")
		}
	}
	for _, raw := range []string{"", "https://acsta.net/test.jpg", "https://fr.web.img6.acsta.net/test.jpg"} {
		if !validSourcePosterURL(SourceGrandEcran, raw) {
			t.Fatal("valid poster")
		}
	}
	for _, raw := range []string{"https://acsta.net/../x", "https://evil.test/x", "https://acsta.net/x?", "https://acsta.net:443/x"} {
		if validSourcePosterURL(SourceGrandEcran, raw) {
			t.Fatal("invalid poster")
		}
	}
}
