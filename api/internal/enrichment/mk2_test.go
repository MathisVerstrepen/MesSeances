package enrichment

import (
	"strings"
	"testing"

	"messeances/api/internal/schedule"
)

func TestMK2SourcePolicy(t *testing.T) {
	for _, id := range []string{"HO00006568", "HO0", "HO" + strings.Repeat("1", 117)} {
		if !validSourceIdentity(SourceMK2, id) {
			t.Fatal("valid identity", id)
		}
	}
	for _, id := range []string{"1", "HO", "ho1", "HO1/", "HO" + strings.Repeat("1", 118)} {
		if validSourceIdentity(SourceMK2, id) {
			t.Fatal("invalid identity", id)
		}
	}
	if !validSourcePosterURL(SourceMK2, "") || !validSourcePosterURL(SourceMK2, schedule.MK2PosterPrefix+"HO00006568") {
		t.Fatal("valid poster")
	}
	for _, url := range []string{schedule.MK2PosterPrefix + "HO1?", schedule.MK2PosterPrefix + "HO1#", schedule.MK2PosterPrefix + "../HO1", "https://evil.test/HO1"} {
		if validSourcePosterURL(SourceMK2, url) {
			t.Fatal("invalid poster", url)
		}
	}
}
