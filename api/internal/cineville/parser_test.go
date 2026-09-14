package cineville

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestBootstrapValidation(t *testing.T) {
	c := fixtureCinema()
	for _, build := range []string{"", "../x", "a/b", "x?y", "x#z", "a%2fb", strings.Repeat("a", 129)} {
		if _, _, err := parseBootstrap(bootstrapBytes(t, build, []cinema{c})); err == nil {
			t.Fatal("unsafe build accepted")
		}
	}
	for _, change := range []func(*cinema){func(c *cinema) { c.Route = "../x" }, func(c *cinema) { c.ID = "01" }, func(c *cinema) { c.ID = "9223372036854775808" }, func(c *cinema) { c.City = "" }, func(c *cinema) { c.Postal = "" }} {
		bad := c
		change(&bad)
		if _, _, err := parseBootstrap(bootstrapBytes(t, "build", []cinema{bad})); err == nil {
			t.Fatal("bad catalog accepted")
		}
	}
	for _, catalog := range [][]cinema{nil, {c, c}, {{ID: "4676", Route: "anything"}}, {{ID: "639", Route: "siege"}}} {
		if _, _, err := parseBootstrap(bootstrapBytes(t, "build", catalog)); err == nil {
			t.Fatal("empty/duplicate catalog accepted")
		}
	}
	for _, body := range []string{`<script id="__NEXT_DATA__">{}</script>`, string(bootstrapBytes(t, "build", []cinema{c})) + string(bootstrapBytes(t, "build", []cinema{c})), `<script id="__NEXT_DATA__">{"buildId":"b","props":{"pageProps":{"cines":[]}}}</script>`} {
		if _, _, err := parseBootstrap([]byte(body)); err == nil {
			t.Fatal("malformed bootstrap accepted")
		}
	}
}
func TestRuntimeGrammar(t *testing.T) {
	valid := map[string]int{"1h33": 93, "1 h 37": 97, "1H54": 114, "01:25": 85, "2h3": 123, "1h": 60, "3H": 180, "1h38min": 98, "33 mn": 33, "45mn": 45, "52 min": 52, " 1h30 ": 90, "": 0, " ": 0}
	for raw, want := range valid {
		if got, err := parseRuntime(raw); err != nil || got != want {
			t.Fatalf("%q = %d %v", raw, got, err)
		}
	}
	for _, raw := range []string{"0min", "0h", "1h60", "1:99", "-1h", "1.5h", "90", "999999999999999999999h", "2147483648min", "1h3junk", "1:2"} {
		if _, err := parseRuntime(raw); err == nil {
			t.Fatalf("accepted runtime %q", raw)
		}
	}
}
func TestMetadataFirstEntryAndGaps(t *testing.T) {
	for _, n := range []int{1, 2, 3} {
		f := fixtureFilm("2714300920262")
		f.Title = ""
		entries := []map[string]any{{"titre": "First", "duree": "1h", "affichette": "poster.webp"}}
		for i := 1; i < n; i++ {
			entries = append(entries, map[string]any{"titre": "Ignored", "duree": "malformed"})
		}
		f.Metadata = jsonBytes(t, entries)
		m, err := parseMovie(f)
		if err != nil || m.Title != "First" || m.RuntimeMinutes != 60 || m.ProviderID != "2714300920262" {
			t.Fatalf("metadata array %d err=%v", n, err)
		}
	}
	for _, metadata := range []string{`[]`, `{"result":false,"message":"not a movie title"}`} {
		f := fixtureFilm("-9223372036854775808")
		f.Metadata = json.RawMessage(metadata)
		m, err := parseMovie(f)
		if err != nil || m.Title != "Source title" || m.RuntimeMinutes != 0 || m.PosterURL != "" {
			t.Fatal("gap lost")
		}
	}
	f := fixtureFilm("1")
	f.Metadata = json.RawMessage(`[{"affichette":"../private.png","datedesortie":"not a date"}]`)
	m, err := parseMovie(f)
	if err != nil || m.PosterURL != "" || m.ReleaseDate != "" {
		t.Fatal("unsafe optional metadata retained")
	}
	for _, id := range []scalar{"0", "-0", "01", "+1", "1e3", "1.0", "9223372036854775808", "-9223372036854775809"} {
		f := fixtureFilm(id)
		if _, err := parseMovie(f); err == nil {
			t.Fatalf("visa=%s", id)
		}
	}
	var numeric film
	if json.Unmarshal([]byte(`{"visa":-693091020261}`), &numeric) != nil || numeric.Visa != "-693091020261" {
		t.Fatal("float conversion")
	}
}
func TestParisWallTimes(t *testing.T) {
	loc, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ date, clock string }{{"20260329", "02:30"}, {"20261025", "02:30"}, {"20260230", "12:00"}, {"20260914", "1:00"}, {"2026914", "10:00"}, {"20260914", "12:60"}} {
		if _, err := parseStart(v.date, v.clock, loc); err == nil {
			t.Fatalf("invalid wall time %v", v)
		}
	}
	for _, clock := range []string{"00:00", "01:00", "03:00", "04:00", "07:59"} {
		start, err := parseStart("20260914", clock, loc)
		if err != nil || start.Format(time.DateOnly) != "2026-09-14" {
			t.Fatalf("calendar preservation %s: %v", clock, err)
		}
	}
}
func TestLanguageFormatPrecedence(t *testing.T) {
	for _, v := range []struct {
		version       string
		flag          scalar
		attrs, relief string
		lang          schedule.Language
		format        schedule.Format
	}{
		{"VF", "0", "", "2D", schedule.LanguageVF, schedule.Format2D},
		{"VF", "1", "121,125,132", "2D", schedule.LanguageVFSTF, schedule.Format2D},
		{"VO", "0", "32", "2D", schedule.LanguageVOSTFR, schedule.Format3D},
		{"VO", "false", "", "3D", schedule.LanguageVOSTFR, schedule.Format3D},
		{"VF", "true", "32,25", "3D", schedule.LanguageVFSTF, schedule.FormatDolby},
		{"VF", "0", " 32;25|21 ", "3D", schedule.LanguageVF, schedule.FormatIMAX},
	} {
		l, f, err := parseAttributes(session{Version: v.version, Subtitles: v.flag, Attributes: v.attrs, Relief: v.relief})
		if err != nil || l != v.lang || f != v.format {
			t.Fatalf("attributes=%+v got=%s/%s err=%v", v, l, f, err)
		}
	}
	for _, attrs := range []string{"x21", "21x", "-21", "[21]", "2.1"} {
		if _, _, err := parseAttributes(session{Version: "VF", Subtitles: "0", Attributes: attrs, Relief: "2D"}); err == nil {
			t.Fatal("malformed attribute accepted")
		}
	}
}
