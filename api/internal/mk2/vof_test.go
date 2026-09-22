package mk2

import (
	"errors"
	"strings"
	"testing"

	"messeances/api/internal/schedule"
)

func TestSyncVOF(t *testing.T) {
	for _, tc := range []struct {
		tokens  []string
		want    schedule.Language
		version string
	}{
		{[]string{"2D", "VOF"}, schedule.LanguageVO, "VOF"},
		{[]string{"2D", "VOF", "STFR"}, schedule.LanguageVOSTFR, "VOF+STFR"},
		{[]string{"2D", "VOF", "VF"}, "", ""},
		{[]string{"2D", "VOF", "VO"}, "", ""},
		{[]string{"2D", "VOF", "Muet"}, schedule.LanguageVO, "VOF"},
		{[]string{"2D", "VOF", "Muet", "STFR"}, schedule.LanguageVOSTFR, "VOF+STFR"},
		{[]string{"2D"}, "", ""},
		{[]string{"2D", "vof"}, "", ""},
	} {
		t.Run(strings.Join(tc.tokens, "+"), func(t *testing.T) {
			f, p, options := fixture(t)
			s := &p.Types[0].Groups[0].Sessions[0]
			s.Attributes = nil
			for _, token := range tc.tokens {
				s.Attributes = append(s.Attributes, attribute{ShortName: token})
			}
			f.pages[p.Slug] = encode(t, p)
			d, _, err := Sync(t.Context(), f, options)
			if tc.version == "" {
				if !errors.Is(err, schedule.ErrDatasetValidation) {
					t.Fatalf("expected dataset validation error, got %v", err)
				}
				return
			}
			if err != nil || len(d.Showtimes) != 1 {
				t.Fatalf("dataset=%+v err=%v", d, err)
			}
			r := d.Showtimes[0]
			if r.Language != tc.want || r.ProviderVersion != tc.version || r.Format != schedule.Format2D {
				t.Fatalf("record=%+v", r)
			}
		})
	}
}
