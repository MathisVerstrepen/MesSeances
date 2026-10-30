package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestMajesticBookingIdentityPolicy(t *testing.T) {
	base := "https://www.ticketingcine.com/EMS0700/FRABCDE#showsession?id=emsx070000000001"
	local := "https://www.ticketingcine.com?EMS0700#showsession?id=emsx070000000001"
	for _, raw := range []string{base, base + "&ps=ticketingcine", local, local + "&ps=ticketingcine", MajesticWebsite("EMS0700")} {
		if !ValidMajesticBookingURL(raw, "EMS0700", "emsx070000000001") {
			t.Error("valid source rejected", raw)
		}
	}
	for _, raw := range []string{strings.Replace(base, "https:", "http:", 1), strings.Replace(base, ".com/", ".com:443/", 1), strings.Replace(base, ".com/", ".com./", 1), strings.Replace(base, "www.", "user@www.", 1), strings.Replace(base, "/FR", "/%46R", 1), strings.Replace(base, "EMS0700", "EMS0654", 1), strings.Replace(base, "EMS0700", "EMS9999", 1), base + "&id=emsx070000000001", base + "&ps=ticketingcine&ps=ticketingcine", base + "&extra=1", strings.Replace(base, "#", "?x=1#", 1), local + "&extra=1", strings.Replace(local, "?EMS0700", "/?EMS0700", 1), strings.Replace(local, "?EMS0700", "?EMS0700=", 1), "https://majestic-douai.com/"} {
		if ValidMajesticBookingURL(raw, "EMS0700", "emsx070000000001") {
			t.Error("unsafe source accepted", raw)
		}
	}
	if MajesticBookingMatchesMovie(base, "EMS0700", "emsx070000000001", "ZZZZZ") || MajesticBookingMatchesMovie(MajesticWebsite("EMS0700"), "EMS0700", "emsx070000000001", "EMS0654-emsx0654HC12") {
		t.Fatal("cross-film/site accepted")
	}
	for _, raw := range []string{local, local + "&ps=ticketingcine"} {
		if !MajesticBookingMatchesMovie(raw, "EMS0700", "emsx070000000001", "ABCDE") {
			t.Fatal("global film site/session route rejected")
		}
	}
	for _, id := range []string{"EMS0700-emsx0700HC12", "ABCDE"} {
		if !ValidMajesticIdentity("movie", id) {
			t.Fatal(id)
		}
	}
	for _, id := range []string{"EMS0700-emsx0654HC12", "EMS9999-emsx9999HC12", strings.Repeat("A", 115)} {
		if ValidMajesticIdentity("movie", id) {
			t.Fatal(id)
		}
	}
}

func TestMajesticEffectiveEndIncludesFirstPartWithoutInventingRuntime(t *testing.T) {
	start := time.Date(2026, 10, 16, 22, 30, 0, 0, time.UTC)
	for _, test := range []struct {
		source, tmdb int
		want         int
	}{{100, 120, 110}, {0, 120, 130}, {0, 0, 0}} {
		r := ShowtimeRecord{Provider: ProviderMajestic, StartTime: start, EndTime: start, FirstPartDurationMinutes: 10, Movie: MovieRecord{RuntimeMinutes: test.source, PublicMovieID: 1}}
		view := NewSnapshotView(Dataset{PublicMovies: []PublicMovieRecord{{ID: 1, TMDBID: 42, TMDBRuntimeMinutes: test.tmdb, RuntimeMinutes: 999}}}, SnapshotRevision{})
		if got := effectiveRecordEnd(view, r); got.Sub(start) != time.Duration(test.want)*time.Minute {
			t.Fatal("source/TMDB/unknown precedence lost", got)
		}
	}
}
