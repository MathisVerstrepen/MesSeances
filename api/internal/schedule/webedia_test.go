package schedule

import (
	"testing"
	"time"
)

func TestWebediaIdentitiesAndURLs(t *testing.T) {
	for _, test := range []struct {
		kind, value string
		valid       bool
	}{
		{"theater", "P5385", true}, {"theater", "G0FTD", true}, {"theater", "p5385", false}, {"theater", "P538", false},
		{"movie", "204864", true}, {"movie", "P5385-c42", true}, {"movie", "c42", false}, {"movie", "0123", false},
		{"showing", "P5385-0123456789abcdef0123456789abcdef", true}, {"showing", "P5385-0123", false}, {"other", "P5385", false},
	} {
		if got := ValidWebediaIdentity(test.kind, test.value); got != test.valid {
			t.Fatalf("%s %q valid=%t", test.kind, test.value, got)
		}
	}
	for raw, valid := range map[string]bool{"https://www.cinepolesud.fr/": true, "https://www.cinepolesud.fr": false, "https://www.cinepolesud.fr/films": false, "http://www.cinepolesud.fr/": false, "https://www.cinepolesud.fr/?x=1": false} {
		if ValidWebediaOrigin(raw) != valid {
			t.Fatalf("origin %q", raw)
		}
	}
	for raw, valid := range map[string]bool{"https://all.web.img.acsta.net/img/f6/50/f.jpg": true, "https://acsta.net/a.jpg": true, "https://evil.example/a.jpg": false, "https://all.web.img.acsta.net/a.jpg?x=1": false} {
		if ValidWebediaPosterURL(raw) != valid {
			t.Fatalf("poster %q", raw)
		}
	}
	if recordProvider("", "webedia-P5385") != ProviderWebedia {
		t.Fatal("identity prefix resolution")
	}
}

func TestWebediaShowingRule(t *testing.T) {
	start := time.Date(2026, 11, 25, 14, 0, 0, 0, time.UTC)
	theater := TheaterRecord{ProviderID: "P5385"}
	for movie, valid := range map[string]bool{"204864": true, "P5385-c42": true, "W0001-c42": false} {
		s := ShowtimeRecord{ProviderShowingID: "P5385-0123456789abcdef0123456789abcdef", Movie: MovieRecord{ProviderID: movie}, StartTime: start, EndTime: start}
		if _, err := webediaShowingRule(s, theater, true); (err == nil) != valid {
			t.Fatalf("movie %s err=%v", movie, err)
		}
	}
	if !webediaBooking("", "P5385-x", "P5385") || webediaBooking("https://x.example/", "W0001-x", "P5385") || webediaBooking("http://x.example/", "P5385-x", "P5385") {
		t.Fatal("booking binding")
	}
}
