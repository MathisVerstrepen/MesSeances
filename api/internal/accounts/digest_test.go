package accounts

import (
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestNextDigestSlots(t *testing.T) {
	for _, tc := range []struct {
		frequency, now, want string
	}{
		{DigestDaily, "2026-11-26T06:59:59Z", "2026-11-26T07:00:00Z"}, // 08:00 CET
		{DigestDaily, "2026-11-26T07:00:00Z", "2026-11-27T07:00:00Z"},
		{DigestDaily, "2026-07-01T05:00:00Z", "2026-07-01T06:00:00Z"},  // 08:00 CEST
		{DigestDaily, "2026-03-28T08:00:00Z", "2026-03-29T06:00:00Z"},  // spring forward
		{DigestDaily, "2026-10-24T08:00:00Z", "2026-10-25T07:00:00Z"},  // fall back
		{DigestWeekly, "2026-11-26T10:00:00Z", "2026-12-02T07:00:00Z"}, // Thursday -> Wednesday
		{DigestWeekly, "2026-12-02T06:00:00Z", "2026-12-02T07:00:00Z"},
		{DigestWeekly, "2026-12-02T07:00:00Z", "2026-12-09T07:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		got, err := nextDigest(tc.frequency, now)
		if err != nil || got.Format(time.RFC3339) != tc.want {
			t.Fatalf("%s at %s: got %s want %s (%v)", tc.frequency, tc.now, got.Format(time.RFC3339), tc.want, err)
		}
	}
}

func strPtr(s string) *string { return &s }

func TestEmailPosterSizing(t *testing.T) {
	for raw, want := range map[string]string{
		"https://image.tmdb.org/t/p/w500/abc.jpg":     "https://image.tmdb.org/t/p/w185/abc.jpg",
		"https://image.tmdb.org/t/p/original/abc.jpg": "https://image.tmdb.org/t/p/w185/abc.jpg",
		"https://media.pathe.fr/movie/poster.jpg":     "https://media.pathe.fr/movie/poster.jpg",
		"http://example.com/p.jpg":                    "",
		"javascript:alert(1)":                         "",
	} {
		if got := emailPoster(strPtr(raw)); got != want {
			t.Fatalf("%s: got %q want %q", raw, got, want)
		}
	}
	if emailPoster(nil) != "" || initials("Le Comte de Monte-Cristo") != "LC" || initials("2001 : l'odyssée") != "2L" || initials("Anora") != "AN" || initials("Ça") != "ÇA" {
		t.Fatal("fallbacks")
	}
}

func TestDigestMailFilmFirst(t *testing.T) {
	poster := strPtr("https://image.tmdb.org/t/p/w500/dune.jpg")
	theater := []NotificationTheater{{ID: "ugc-1", Name: "UGC Les Halles"}}
	items := []NotificationView{
		{Kind: NotificationKindWatchlist, MovieCount: 1, TheaterCount: 3, Theaters: theater, Movies: []schedule.ActivityMovie{{Slug: "film-1", Title: `<script>alert(1)</script>`, PosterURL: poster}}},
		{Kind: NotificationKindActivity, MovieCount: 6, TheaterCount: 1, Theaters: theater, Movies: []schedule.ActivityMovie{
			{Slug: "film-2", Title: "Wicked", PosterURL: poster}, {Slug: "film-3", Title: "Anora"}, {Slug: "film-4", Title: "Flow", PosterURL: poster}, {Slug: "film-5", Title: "Conclave", PosterURL: poster},
		}},
		{Kind: NotificationKindActivity, MovieCount: 1, TheaterCount: 1, Theaters: theater, Movies: []schedule.ActivityMovie{{Slug: "film-6", Title: "Nosferatu", PosterURL: poster}}},
	}
	m, err := digestMailMessage("https://messeances.fr", "a@example.com", DigestWeekly, items, 5, "https://messeances.fr/api/v1/notifications/unsubscribe?token=abc", "https://messeances.fr/notifications/desabonnement?token=abc")
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "<script>alert(1)</script>, Wicked et plus encore" || m.UnsubscribeURL == "" || m.Recipient != "a@example.com" {
		t.Fatalf("%+v", m.Subject)
	}
	if strings.Contains(m.HTML, "<script>") || !strings.Contains(m.HTML, "&lt;script&gt;") {
		t.Fatal("HTML not escaped")
	}
	for _, want := range []string{
		"https://image.tmdb.org/t/p/w185/dune.jpg", `alt="Affiche : Wicked"`, ">AN</td>", // poster, alt text, initials fallback
		"https://messeances.fr/film/film-2", "De votre watchlist", "Première séance à UGC Les Halles et 2 autres cinémas",
		"6 nouveaux films", "Et 2 de plus", "Nouveau à l’affiche", "Arrive à UGC Les Halles", "Résumé hebdomadaire",
		"Et 2 de plus dans votre boîte", "desabonnement?token=abc",
	} {
		if !strings.Contains(m.HTML, want) {
			t.Fatalf("HTML missing %q", want)
		}
	}
	if strings.Count(m.HTML, "<img") != 1+3+1+1 { // logo + 3 row posters + 2 featured
		t.Fatal("poster count", strings.Count(m.HTML, "<img"))
	}
	for _, want := range []string{"6 nouveaux films — À UGC Les Halles", "· Wicked https://messeances.fr/film/film-2", "· et 2 autres", "Se désabonner : https://messeances.fr/notifications/desabonnement?token=abc"} {
		if !strings.Contains(m.Text, want) {
			t.Fatalf("text missing %q:\n%s", want, m.Text)
		}
	}
	single, _ := digestMailMessage("https://messeances.fr", "a@example.com", DigestDaily, items[2:], 1, "u", "p")
	if single.Subject != "Nosferatu · MesSeances" {
		t.Fatal(single.Subject)
	}
}
