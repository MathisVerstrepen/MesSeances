package shortlink

import (
	"strings"
	"testing"
)

func TestValidTargetAcceptsSupportedRoutesAndQueries(t *testing.T) {
	for _, target := range []string{
		"/",
		"/planning?date=2026-08-22&language=VOSTFR&format=IMAX&mode=map&zoom=12",
		"/recherche?theaters=ugc-25%2Cugc-26&date=2026-08-22&start_after=18%3A00&finish_before=23%3A00&language=VF&format=2D&include_ads=true&buffer_ads=20&grouping=movie&layout=grid",
		"/films?q=Am%C3%A9lie+Poulain&sort=title&page=2&genres=Com%C3%A9die%2CDrame&duration=medium&date=2026-08-22&date_to=2026-08-23",
		"/credits",
		"/film/ugc-film-42?date=2026-08-22&language=VF&format=2D&sort=time",
		"/cinema/ugc-lille?date=2026-08-22",
		"/cinema/ugc-lille?view=films",
		"/cinema/ugc-lille?date=2026-08-22&grouping=movie&layout=lines&view=films&shared_theaters=ugc-25%2Ckinepolis_42",
		"/ville/villeneuve-d_ascq/cinemas",
	} {
		if !ValidTarget(target) {
			t.Errorf("target rejected: %q", target)
		}
	}
}

func TestValidTargetRejectsUnsupportedOrUnsafeTargets(t *testing.T) {
	for _, target := range []string{
		"", "planning", "//evil.example/x", "/\\evil", "https://evil.example/", "/films#x",
		"/films/", "/film//x", "/film/.", "/film/..", "/film/%2e%2e", "/film/caf%C3%A9", "/film/-slug", "/film/slug!",
		"/cinemas", "/cinemas?q=Lille", "/cinemas?%71=Lille", "/cinemas?shared_theaters=ugc-25", "/cinemas?q=Lille&shared_theaters=ugc-25", "/credits?q=x", "/admin", "/ville/lille/cinemas/extra",
		"/?q=x", "/films?unknown=x", "/cinema/ugc-lille?unknown=x", "/films?q=x&q=y", "/films?q=x&%71=y", "/films?", "/films?q=x&",
		"/films?q=%", "/films?q=%0A", "/films?q=%00", "/films?%0A=x", "/films?q=x\r\nInjected:x",
		"/films?unsupported_date=2026-08-22", strings.Repeat("/", 2049), "/films?q=" + strings.Repeat("x", 2048),
		string([]byte{'/', 'f', 'i', 'l', 'm', 's', '?', 'q', '=', 0xff}),
	} {
		if ValidTarget(target) {
			t.Errorf("target accepted: %q", target)
		}
	}
}

func TestValidTargetPreservesBoundaryAndEncodedValues(t *testing.T) {
	target := "/films?q=" + strings.Repeat("x", 2039)
	if len(target) != 2048 || !ValidTarget(target) {
		t.Fatalf("2048-byte target rejected: bytes=%d", len(target))
	}
	if !ValidTarget("/films?%71=a%26b%3Dc") {
		t.Fatal("encoded allowed key or value rejected")
	}
}

func TestValidTargetAcceptsSharedTheatersOnSevenShareRoutes(t *testing.T) {
	for _, target := range []string{
		"/planning?shared_theaters=ugc-25%2Ckinepolis_42",
		"/recherche?date=2026-08-22&shared_theaters=ugc-25,kinepolis_42",
		"/films?shared_theaters=ugc-25,kinepolis_42&q=Film",
		"/credits?shared_theaters=ugc-25,kinepolis_42",
		"/film/ugc-film-42?shared_theaters=ugc-25,kinepolis_42",
		"/cinema/ugc-lille?date=2026-08-22&shared_theaters=ugc-25,kinepolis_42",
		"/ville/lille/cinemas?shared_theaters=ugc-25,kinepolis_42",
	} {
		if !ValidTarget(target) {
			t.Errorf("shared target rejected: %q", target)
		}
	}
}

func TestValidTargetRejectsInvalidSharedTheaters(t *testing.T) {
	for _, target := range []string{
		"/?shared_theaters=ugc-25",
		"/films?shared_theaters=",
		"/films?shared_theaters=ugc-25,",
		"/films?shared_theaters=,ugc-25",
		"/films?shared_theaters=ugc-25,,kinepolis-42",
		"/films?shared_theaters=ugc-25,ugc-25",
		"/films?shared_theaters=ugc-25%2Cugc-25",
		"/films?shared_theaters=ugc-25&shared_theaters=kinepolis-42",
		"/films?shared_theaters=ugc-25%20",
		"/films?shared_theaters=%20ugc-25",
		"/films?shared_theaters=-ugc-25",
		"/films?shared_theaters=ugc.25",
		"/films?shared_theaters=ugc%2F25",
		"/films?shared_theaters=ugc%2525",
		"/films?shared_theaters=%",
		"/films?shared_theaters=ugc-25%0A",
		"/films?shared_theaters=" + strings.Repeat("a", 129),
	} {
		if ValidTarget(target) {
			t.Errorf("invalid shared target accepted: %q", target)
		}
	}
}

func TestValidTargetAcceptsSharedTheaterIDBoundariesAndHistoricalTargets(t *testing.T) {
	if !ValidTarget("/films?shared_theaters=" + strings.Repeat("a", 128)) {
		t.Fatal("128-byte theater ID rejected")
	}
	for _, target := range []string{
		"/planning?date=2026-08-22",
		"/recherche?theaters=ugc-25",
		"/films?q=Film",
		"/credits",
		"/film/ugc-film-42?date=2026-08-22",
		"/cinema/ugc-lille?date=2026-08-22",
		"/ville/lille/cinemas",
	} {
		if !ValidTarget(target) {
			t.Errorf("historical target rejected: %q", target)
		}
	}
}

func TestValidCode(t *testing.T) {
	for _, code := range []string{"AAAAAAAAAAAAAAAAAAAAAA", "Abcdefghijklmnopqr_1-2"} {
		if !ValidCode(code) {
			t.Errorf("code rejected: %q", code)
		}
	}
	for _, code := range []string{"", "short", "AAAAAAAAAAAAAAAAAAAAA!", "éAAAAAAAAAAAAAAAAAAAAA"} {
		if ValidCode(code) {
			t.Errorf("code accepted: %q", code)
		}
	}
}
