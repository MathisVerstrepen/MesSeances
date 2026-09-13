package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestFrenchReleaseEvidenceBounds(t *testing.T) {
	row := `{"type":3,"release_date":"2026-10-01T00:00:00Z","note":"<b>France 2</b>"}`
	for _, test := range []struct {
		name, rows string
		valid      bool
	}{
		{"normalized duplicates", `[{"type":2,"release_date":"2026-10-01T00:00:00Z","note":"  séance\t unique  "},{"type":2,"release_date":"2026-10-01T00:00:00Z","note":"séance unique"}]`, true},
		{"null note", `[{"type":3,"release_date":"2026-10-01T00:00:00Z","note":null}]`, true},
		{"64 rows before dedup", "[" + strings.Repeat(row+",", 63) + row + "]", true},
		{"65 rows before dedup", "[" + strings.Repeat(row+",", 64) + row + "]", false},
		{"unknown type", `[{"type":7,"release_date":"2026-10-01T00:00:00Z"}]`, false},
		{"null type", `[{"type":null,"release_date":"2026-10-01T00:00:00Z"}]`, false},
		{"NUL", `[{"type":3,"release_date":"2026-10-01T00:00:00Z","note":"\u0000"}]`, false},
		{"invalid UTF8", strings.Replace(row, "France", string([]byte{0xff}), 1), false},
		{"wrong note type", `[{"type":3,"release_date":"2026-10-01T00:00:00Z","note":42}]`, false},
		{"1024 runes", "[" + strings.Replace(row, "<b>France 2</b>", strings.Repeat("é", 1024), 1) + "]", true},
		{"1025 whitespace runes", "[" + strings.Replace(row, "<b>France 2</b>", strings.Repeat(" ", 1025), 1) + "]", false},
		{"body limit", "[" + strings.Replace(row, "<b>France 2</b>", strings.Repeat("x", 2<<20), 1) + "]", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"id":42,"results":[{"iso_3166_1":"FR","release_dates":%s}]}`, test.rows)
			}, "fixture-token")
			evidence, err := client.FrenchReleaseEvidence(context.Background(), 42)
			if (err == nil) != test.valid {
				t.Fatalf("%+v %v", evidence, err)
			}
			if test.valid && len(evidence.Rows) != 1 {
				t.Fatalf("not deduplicated: %+v", evidence)
			}
		})
	}
}
