package enrichment

import (
	"strings"
	"testing"

	"messeances/api/internal/schedule"
)

func TestDelegatedSourcePolicies(t *testing.T) {
	for _, test := range []struct {
		provider                                  string
		validIDs, invalidIDs, posters, badPosters []string
	}{
		{SourceMK2,
			[]string{"HO00006568", "HO0", "HO" + strings.Repeat("1", 117)},
			[]string{"1", "HO", "ho1", "HO1/", "HO" + strings.Repeat("1", 118)},
			[]string{"", schedule.MK2PosterPrefix + "HO00006568"},
			[]string{schedule.MK2PosterPrefix + "HO1?", schedule.MK2PosterPrefix + "HO1#", schedule.MK2PosterPrefix + "../HO1", "https://evil.test/HO1"}},
		{SourceGrandEcran,
			[]string{"1", "cEvent_1-x", strings.Repeat("1", 112), "c" + strings.Repeat("a", 111)},
			[]string{"0", "01", "c", "Cevent", "c/x", strings.Repeat("1", 113)},
			[]string{"", "https://acsta.net/test.jpg", "https://fr.web.img6.acsta.net/test.jpg"},
			[]string{"https://acsta.net/../x", "https://evil.test/x", "https://acsta.net/x?", "https://acsta.net:443/x"}},
		{SourceNoeCinemas,
			[]string{"1", "cEvent_1-x", strings.Repeat("1", 112), "c" + strings.Repeat("a", 111)},
			[]string{"0", "01", "c", "Cevent", "c/x", strings.Repeat("1", 113)},
			[]string{"", "https://acsta.net/test.jpg", "https://all.web.img.acsta.net/test.jpg"},
			[]string{"https://acsta.net/../x", "https://evil.test/x", "https://acsta.net/x?", "https://acsta.net:443/x"}},
		{SourceCinewest,
			[]string{"cineoffice-1", "ticketingcine-ABCDE", "ticketingcine-EMS0042-emsx0042HC123", "ticketingcine-EMS1378-emsx1378HC123", "webediamovies-1"},
			[]string{"1", "ABCDE", "ticketingcine-EMS0042-emsx1185HC123", "webediamovies-0"},
			[]string{"", "https://images.monnaie-services.com/movie_poster/600/FRABCDE/12345678.webp"},
			[]string{"https://evil.test/poster.jpg"}},
		{SourceVeo,
			[]string{"123", "cEvent_1", "ticketingcine-A12B3"},
			[]string{"0", "01", "c", "ticketingcine-bad"},
			[]string{"", "https://acsta.net/test.jpg"},
			[]string{"https://evil.test/test.jpg", "https://acsta.net/../test.jpg", "https://acsta.net:443/test.jpg"}},
	} {
		t.Run(test.provider, func(t *testing.T) {
			for _, group := range []struct {
				name     string
				values   []string
				validate func(string, string) bool
				want     bool
			}{
				{"valid identity", test.validIDs, validSourceIdentity, true},
				{"invalid identity", test.invalidIDs, validSourceIdentity, false},
				{"valid poster", test.posters, validSourcePosterURL, true},
				{"invalid poster", test.badPosters, validSourcePosterURL, false},
			} {
				for _, value := range group.values {
					if got := group.validate(test.provider, value); got != group.want {
						t.Errorf("%s %q: got %t", group.name, value, got)
					}
				}
			}
		})
	}
}
