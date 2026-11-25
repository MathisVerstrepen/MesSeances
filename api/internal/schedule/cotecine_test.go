package schedule

import (
	"testing"
	"time"
)

func TestCotecineIdentitiesAndURLs(t *testing.T) {
	for _, test := range []struct {
		kind, value string
		valid       bool
	}{
		{"theater", "1824", true}, {"theater", "0", false}, {"theater", "18a", false},
		{"movie", "628177", true}, {"movie", "1824-lady-bird-0000", true}, {"movie", "lady-bird", false}, {"movie", "0123", false}, {"movie", "1824-Lady", false},
		{"showing", "1824-628177-1795863600", true}, {"showing", "1824-1824-lady-bird-0000-1795863600", true}, {"showing", "1824-628177-0123", false}, {"showing", "1824-628177", false},
		{"other", "1824", false},
	} {
		if got := ValidCotecineIdentity(test.kind, test.value); got != test.valid {
			t.Fatalf("%s %q valid=%t", test.kind, test.value, got)
		}
	}
	for raw, valid := range map[string]bool{
		"https://www.cinema-arvor.fr/": true, "https://www.cinema-confluences.com/sable-sur-sarthe/": true,
		"https://www.cinema-arvor.fr": false, "https://www.cinema-confluences.com/sable": false, "http://www.cinema-arvor.fr/": false,
		"https://www.cinema-arvor.fr/?x=1": false, "https://www.cinema-arvor.fr/?": false, "https://www.cinema-arvor.fr/#x": false, "https://www.cinema-arvor.fr/Sable/": false, "https://www.cinema-arvor.fr//": false,
	} {
		if ValidCotecineWebsite(raw) != valid {
			t.Fatalf("website %q", raw)
		}
	}
	for raw, valid := range map[string]bool{"https://fr.web.img5.acsta.net/img/0b/e9/a.jpg": true, "https://static.cotecine.fr/tb/Affiches/a.JPG": true, "https://acsta.net.evil.example/a.jpg": false, "https://fr.web.img5.acsta.net/a.jpg?x=1": false} {
		if ValidCotecinePosterURL(raw) != valid {
			t.Fatalf("poster %q", raw)
		}
	}
	if recordProvider("", "cotecine-1824") != ProviderCotecine || PublicChain(ProviderCotecine) != ChainIndependent {
		t.Fatal("provider resolution")
	}
}

func TestCotecineShowingRule(t *testing.T) {
	start := time.Date(2026, 11, 28, 11, 0, 0, 0, time.UTC)
	theater := TheaterRecord{ProviderID: "1824"}
	for movie, valid := range map[string]bool{"628177": true, "1824-lady-bird-0000": true, "4464-lady-bird-0000": false} {
		s := ShowtimeRecord{ProviderShowingID: CotecineShowingID("1824", movie, start), Movie: MovieRecord{ProviderID: movie}, StartTime: start, EndTime: start}
		if _, err := cotecineShowingRule(s, theater, true); (err == nil) != valid {
			t.Fatalf("movie %s err=%v", movie, err)
		}
	}
	s := ShowtimeRecord{ProviderShowingID: CotecineShowingID("1824", "628177", start.Add(time.Minute)), Movie: MovieRecord{ProviderID: "628177"}, StartTime: start, EndTime: start}
	if _, err := cotecineShowingRule(s, theater, true); err == nil {
		t.Fatal("showing bound to another instant")
	}
	if !cotecineBooking("", "1824-x", "1824") || cotecineBooking("https://x.example/", "4464-x", "1824") || cotecineBooking("http://x.example/", "1824-x", "1824") {
		t.Fatal("booking binding")
	}
}
