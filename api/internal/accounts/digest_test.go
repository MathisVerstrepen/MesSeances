package accounts

import (
	"strings"
	"testing"
	"time"
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

func TestDigestMailEscapesAndLinks(t *testing.T) {
	items := []pushPayload{
		{Title: "Watchlist", Body: `<script>alert(1)</script> est programmé à UGC`, URL: "/film/film-2"},
		{Title: "Nouveautés", Body: "3 nouveaux films à UGC", URL: "/compte/activite"},
	}
	m, err := digestMailMessage("https://messeances.fr", "a@example.com", DigestWeekly, items, 5, "https://messeances.fr/api/v1/notifications/unsubscribe?token=abc", "https://messeances.fr/notifications/desabonnement?token=abc")
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Vos alertes MesSeances : 5 notifications" || m.UnsubscribeURL == "" || m.Recipient != "a@example.com" {
		t.Fatalf("%+v", m)
	}
	if strings.Contains(m.HTML, "<script>") || !strings.Contains(m.HTML, "&lt;script&gt;") {
		t.Fatal("HTML not escaped")
	}
	for _, want := range []string{"https://messeances.fr/film/film-2", "Résumé hebdomadaire", "Et 3 de plus", "desabonnement?token=abc"} {
		if !strings.Contains(m.HTML, want) && !strings.Contains(m.Text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if !strings.Contains(m.Text, "Se désabonner : https://messeances.fr/notifications/desabonnement?token=abc") {
		t.Fatal(m.Text)
	}
}
