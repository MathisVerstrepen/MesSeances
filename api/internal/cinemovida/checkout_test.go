package cinemovida

import (
	"context"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestSourceEndClockAndRollover(t *testing.T) {
	for _, tc := range []struct{ start, body, want string }{
		{"2026-10-17T20:00:00+02:00", `<span id="s_fin">(fin prévue à 21h45)</span>`, "2026-10-17T21:45:00+02:00"},
		{"2026-10-17T23:55:00+02:00", `<span class="x s_fin">(fin prévue à 01h10)</span>`, "2026-10-18T01:10:00+02:00"},
		{"2026-03-28T23:30:00+01:00", `<span id="s_fin">(fin prévue à 03h15)</span>`, "2026-03-29T03:15:00+02:00"},
		{"2026-03-29T01:00:00+01:00", `<span id="s_fin">(fin prévue à 02h30)</span>`, ""},
		{"2026-10-25T00:30:00+02:00", `<span id="s_fin">(fin prévue à 02h30)</span>`, ""},
		{"2026-10-25T02:45:00+02:00", `<span id="s_fin">(fin prévue à 02h30)</span>`, "2026-10-25T02:30:00+01:00"},
	} {
		start, _ := time.Parse(time.RFC3339, tc.start)
		got, ok := parseEnd([]byte(tc.body), start)
		if ok != (tc.want != "") || ok && got.Format(time.RFC3339) != tc.want {
			t.Fatal("end resolution", tc.start, got, ok)
		}
	}
	start, _ := time.Parse(time.RFC3339, "2026-10-17T20:00:00+02:00")
	for _, body := range []string{
		`<p>Séance passée, achat impossible</p>`, `<p>Plus de places disponibles en ligne</p>`, `<p>La séance sélectionnée n'est pas disponible à la réservation</p>`,
		`<span id="s_fin">fin prévue à 24h00</span>`, `<span id="s_fin">fin prévue à 22h600</span>`, `<span id="s_fin">fin prévue à 22h300</span>`,
		`<span id="s_fin">fin prévue à 21h00 et fin prévue à 22h00</span>`, `<span id="s_fin">fin prévue à 21h00</span><span class="s_fin">fin prévue à 22h00</span>`,
		`<script>fin prévue à 21h00</script>`, `<template><span id="s_fin">fin prévue à 21h00</span></template>`,
	} {
		if _, ok := parseEnd([]byte(body), start); ok {
			t.Fatal("invalid or absent source end invented", body)
		}
	}
}

func TestSourceEndEnrichmentBudgetAndCount(t *testing.T) {
	start, _ := time.Parse(time.RFC3339, "2026-10-17T20:00:00+02:00")
	for _, budget := range []time.Duration{time.Second, 0} {
		records := []schedule.ShowtimeRecord{{StartTime: start, EndTime: start}, {StartTime: start, EndTime: start}}
		f := &fixtureGetter{checkout: func(ctx context.Context, _ string, _ time.Time) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return []byte(`<span id="s_fin">fin prévue à 21h00</span>`), nil
		}}
		var summary SyncSummary
		if err := enrichEnds(t.Context(), f, records, &summary, budget, 1); err != nil {
			t.Fatal("optional budget became acquisition failure", err)
		}
		want := 0
		if budget > 0 {
			want = 1
		}
		if f.RequestCount() != want || summary.EndsAttempted != want || summary.EndsRecovered != want || summary.EndsBudgetSkipped != 2-want || !records[1].EndTime.Equal(start) {
			t.Fatal("source-end bound not respected", summary)
		}
	}
}
