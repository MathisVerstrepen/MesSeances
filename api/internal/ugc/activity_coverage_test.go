package ugc

import "testing"

func TestActivityAcquisitionCoverage(t *testing.T) {
	for _, tc := range []struct {
		file, cinema, date string
		complete           bool
	}{{"showings.html", "25", "2026-08-15", true}, {"showings-identityless-package.html", "25", "2026-08-15", false}, {"showings-next-session-only.html", "11", "2026-08-14", false}} {
		complete := false
		_, err := parseShowings(fixture(t, tc.file), Cinema{ProviderID: tc.cinema}, tc.date, &complete)
		if err != nil || complete != tc.complete {
			t.Fatal(tc, complete, err)
		}
	}
}
