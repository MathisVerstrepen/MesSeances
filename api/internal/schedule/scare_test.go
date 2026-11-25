package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestScareIdentities(t *testing.T) {
	for _, test := range []struct {
		kind, value string
		valid       bool
	}{
		{"theater", "1854", true},
		{"theater", "0854", false},
		{"theater", "18a", false},
		{"movie", "1854-F1", true},
		{"movie", "1854-t0123456789abcdef", true},
		{"movie", "1854-t0123", true},
		{"movie", "1854-" + strings.Repeat("A", 41), false},
		{"movie", "F1", false},
		{"showing", "1854-a99", true},
		{"showing", "1854-", false},
		{"showing", "1854-a_9", false},
		{"other", "1854", false},
	} {
		if got := ValidScareIdentity(test.kind, test.value); got != test.valid {
			t.Fatalf("%s %q valid=%t", test.kind, test.value, got)
		}
	}
	if recordProvider("", "scare-1854") != ProviderScare {
		t.Fatal("identity prefix resolution")
	}
}

func TestScareBookingURLs(t *testing.T) {
	for raw, valid := range map[string]bool{
		"https://studio.cine.boutique/media/1016?showId=14503":              true,
		"https://latoiledemer.fr/film.html#/media/1582?showId=12560":        true,
		"http://studio.cine.boutique/media/1016":                            false,
		"https://user@studio.cine.boutique/":                                false,
		"https://studio.cine.boutique:8443/":                                false,
		"https://127.0.0.1/":                                                false,
		"https://STUDIO.cine.boutique/":                                     false,
		"https://studio.cine.boutique/a b":                                  false,
		"https://studio.cine.boutique/\\x":                                  false,
		"https://localhost/":                                                false,
		"javascript:alert(1)":                                               false,
		"https://studio.cine.boutique/" + strings.Repeat("a", maxURLLength): false,
	} {
		if got := ValidScareBookingURL(raw); got != valid {
			t.Fatalf("%q valid=%t", raw, got)
		}
	}
	if !scareBooking("", "1854-1", "1854") || scareBooking("", "1855-1", "1854") {
		t.Fatal("booking binding")
	}
}

func TestScareShowingAndVersionRules(t *testing.T) {
	start := time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC)
	theater := TheaterRecord{ProviderID: "1854"}
	s := ShowtimeRecord{ProviderShowingID: "1854-1", Movie: MovieRecord{ProviderID: "1854-F1"}, StartTime: start, EndTime: start}
	if ok, err := scareShowingRule(s, theater, false); err != nil || !ok {
		t.Fatalf("valid showing ok=%t err=%v", ok, err)
	}
	for _, bad := range []ShowtimeRecord{
		{ProviderShowingID: "1855-1", Movie: s.Movie, StartTime: start, EndTime: start},
		{ProviderShowingID: "1854-1", Movie: MovieRecord{ProviderID: "1855-F1"}, StartTime: start, EndTime: start},
		{ProviderShowingID: "1854-1", Movie: s.Movie, StartTime: start, EndTime: start, Room: "Salle 1"},
	} {
		if _, err := scareShowingRule(bad, theater, true); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if ok, _ := scareShowingRule(ShowtimeRecord{ProviderShowingID: "1854-1", Movie: s.Movie, StartTime: start, EndTime: start.Add(-time.Minute)}, theater, true); ok {
		t.Fatal("end before start accepted")
	}
	if silent, unknown, err := scareVersionRule(ShowtimeRecord{ProviderVersion: "VERSION_MUET"}); !silent || unknown || err != nil {
		t.Fatal("silent version")
	}
	if silent, unknown, err := scareVersionRule(ShowtimeRecord{}); silent || !unknown || err != nil {
		t.Fatal("unknown version")
	}
	if _, _, err := scareVersionRule(ShowtimeRecord{ProviderVersion: "VF"}); err == nil {
		t.Fatal("version without language accepted")
	}
}
