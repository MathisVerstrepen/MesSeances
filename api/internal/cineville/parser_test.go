package cineville

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

type alternateVADBoundaryCase struct {
	name        string
	raw         json.RawMessage
	decodeError bool
}

// Shared wire cases pin both film classification and Sync's fail-closed behavior.
func alternateVADBoundaryCases(t *testing.T) []alternateVADBoundaryCase {
	t.Helper()
	var tests []alternateVADBoundaryCase
	add := func(name string, decodeError bool, change func(map[string]any, map[string]any, map[string]any)) {
		f := alternateVADFilm()
		delete(f, "visa")
		d := f["dates"].([]any)[0].(map[string]any)
		s := d["showtimes"].([]any)[0].(map[string]any)
		change(f, d, s)
		tests = append(tests, alternateVADBoundaryCase{name, jsonBytes(t, f), decodeError})
	}
	for _, visa := range []string{`""`, `0`, `"1.0"`, `1.0`, `1e3`, `9223372036854775808`, `-9223372036854775809`, `true`, `{}`, `[]`} {
		add("supplied visa "+visa, false, func(f, _, _ map[string]any) { f["visa"] = json.RawMessage(visa) })
	}
	for _, nullVisa := range []bool{false, true} {
		prefix := "omitted visa/"
		if nullVisa {
			prefix = "null visa/"
		}
		for _, key := range []string{"num_visa", "idfilm_cotecine", "places_en_vente_vad", "places_vad_restantes"} {
			add(prefix+"missing "+key, nullVisa, func(f, _, s map[string]any) {
				if nullVisa {
					f["visa"] = nil
				}
				delete(f, key)
				delete(s, key)
			})
		}
	}
	for _, key := range []string{"num_visa", "idfilm_cotecine"} {
		values := []string{`null`, `1`, `{}`, `true`, `[]`}
		if key == "num_visa" {
			values = append(values, `""`, `" \t "`)
		}
		for _, value := range values {
			add(key+"="+value, false, func(f, _, _ map[string]any) { f[key] = json.RawMessage(value) })
		}
	}
	for _, key := range []string{"id_seance", "salle", "ID_SEANCE", "SaLlE"} {
		for _, value := range []string{`null`, `""`, `0`, `{}`} {
			add("supplied "+key+"="+value, value == "null", func(_, _, s map[string]any) { s[key] = json.RawMessage(value) })
		}
	}
	for _, key := range []string{"dates", "showtimes"} {
		for _, value := range []string{"omitted", `null`, `{}`, `"invalid"`, `[null]`, `[1]`} {
			decodeError := value == `{}` || value == `"invalid"` || value == `[1]` || (key == "showtimes" && value == `null`)
			add(key+"="+value, decodeError, func(f, d, _ map[string]any) {
				target := f
				if key == "showtimes" {
					target = d
				}
				if value == "omitted" {
					delete(target, key)
				} else {
					target[key] = json.RawMessage(value)
				}
			})
		}
	}
	add("mixed standard and alternate sessions", false, func(_, d, _ map[string]any) {
		d["showtimes"] = append(d["showtimes"].([]any), fixtureFilm("1").Dates[0].Showtimes[0])
	})
	for _, visa := range []string{"73", "-73"} {
		for _, key := range []string{"id_seance", "salle"} {
			for _, value := range []string{"omitted", `null`, `""`, `0`} {
				add("standard visa "+visa+"/"+key+"="+value, value == "null", func(f, d, _ map[string]any) {
					f["visa"], f["titre_cotecine"], f["movie_data"] = json.RawMessage(visa), "Standard", []any{}
					var s map[string]any
					if err := json.Unmarshal(jsonBytes(t, fixtureFilm("1").Dates[0].Showtimes[0]), &s); err != nil {
						t.Fatal(err)
					}
					s["places_en_vente_vad"], s["places_vad_restantes"] = nil, 0
					if value == "omitted" {
						delete(s, key)
					} else {
						s[key] = json.RawMessage(value)
					}
					d["showtimes"] = []any{s}
				})
			}
		}
	}
	// Duplicate and case-insensitive keys must not hide a supplied standard visa.
	raw := string(jsonBytes(t, alternateVADFilm()))
	for _, replacement := range []string{`"VISA":0`, `"VISA":0,"visa":null`, `"visa":0,"VISA":null`, `"visa":0,"visa":null`} {
		tests = append(tests, alternateVADBoundaryCase{replacement, json.RawMessage(strings.Replace(raw, `"visa":null`, replacement, 1)), strings.Contains(replacement, "null")})
	}
	return tests
}

func TestFilmAlternateVADBoundaries(t *testing.T) {
	for _, test := range alternateVADBoundaryCases(t) {
		t.Run(test.name, func(t *testing.T) {
			var f film
			err := json.Unmarshal(test.raw, &f)
			if (err != nil) != test.decodeError || f.alternateVAD {
				t.Fatalf("standard film bypassed decoding: skip=%t err=%v", f.alternateVAD, err)
			}
		})
	}
}

func TestFilmAlternateVADClassificationAndReuse(t *testing.T) {
	for _, name := range []string{"null visa", "omitted visa", "nonempty alternate ID", "whitespace alternate ID", "empty date alongside sessions", "arbitrary stock values", "case-insensitive fields"} {
		t.Run(name, func(t *testing.T) {
			candidate := alternateVADFilm()
			switch name {
			case "omitted visa":
				delete(candidate, "visa")
			case "nonempty alternate ID":
				candidate["idfilm_cotecine"] = "alternate-film"
			case "whitespace alternate ID":
				candidate["idfilm_cotecine"] = " \t "
			case "empty date alongside sessions":
				candidate["dates"] = append(candidate["dates"].([]any), map[string]any{"showtimes": []any{}})
			case "arbitrary stock values":
				s := candidate["dates"].([]any)[0].(map[string]any)["showtimes"].([]any)[0].(map[string]any)
				s["places_en_vente_vad"], s["places_vad_restantes"] = map[string]any{}, "unknown"
			}
			raw := jsonBytes(t, candidate)
			if name == "case-insensitive fields" {
				raw = []byte(strings.NewReplacer(`"visa"`, `"ViSa"`, `"dates"`, `"DATES"`, `"showtimes"`, `"SHOWTIMES"`).Replace(string(raw)))
			}
			f := fixtureFilm("73")
			if err := json.Unmarshal(raw, &f); err != nil || !f.alternateVAD || f.Visa != "" || f.Dates != nil || f.Metadata != nil || f.Title != "" {
				t.Fatalf("alternate decode retained standard data: err=%v", err)
			}
			standard := fixtureFilm("-73")
			if err := json.Unmarshal(jsonBytes(t, standard), &f); err != nil || !reflect.DeepEqual(f, standard) {
				t.Fatalf("alternate state leaked into standard film: err=%v", err)
			}
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(`{"visa":null}`), &f); err == nil || !reflect.DeepEqual(f, film{}) {
				t.Fatal("failed decode retained alternate state")
			}
		})
	}
	for _, dates := range []any{[]any{}, []any{map[string]any{"showtimes": []any{}}}} {
		candidate := alternateVADFilm()
		delete(candidate, "visa")
		candidate["dates"] = dates
		var f film
		if err := json.Unmarshal(jsonBytes(t, candidate), &f); err != nil || f.alternateVAD {
			t.Fatalf("sessionless film classified as VAD: err=%v", err)
		}
	}
}

func invalidShowtimesValues() []string {
	return []string{
		`{"result":true,"message":"synthetic-private-body"}`,
		`{"message":"synthetic-private-body"}`,
		`{"result":false}`,
		`{"result":false,"message":"synthetic-private-body","extra":null}`,
		`{"Result":false,"message":"synthetic-private-body"}`,
		`{"result":false,"Message":"synthetic-private-body"}`,
		`{"result":null,"message":"synthetic-private-body"}`,
		`{"result":"false","message":"synthetic-private-body"}`,
		`{"result":0,"message":"synthetic-private-body"}`,
		`{"result":false,"message":null}`,
		`{"result":false,"message":0}`,
		`{"result":false,"message":false}`,
		`{"result":false,"message":[]}`,
		`{"result":false,"message":{}}`,
		`{}`, `null`, `false`, `42`, `"synthetic-private-body"`, `[1]`,
	}
}

func TestProgramDateShowtimesSentinel(t *testing.T) {
	for _, raw := range []string{
		`{"result":false,"message":"synthetic-private-body"}`,
		`{ "message": "", "result": false }`,
		`[]`,
	} {
		t.Run(raw, func(t *testing.T) {
			date := fixtureFilm("73").Dates[0]
			body := []byte(`{"date":20270914,"showtimes":` + raw + `}`)
			if err := json.Unmarshal(body, &date); err != nil || date.Date != "20270914" || date.Showtimes == nil || len(date.Showtimes) != 0 {
				t.Fatalf("empty date decode: sessions=%d nil=%t err=%v", len(date.Showtimes), date.Showtimes == nil, err)
			}
			standard := fixtureFilm("73").Dates[0]
			if err := json.Unmarshal(jsonBytes(t, standard), &date); err != nil || !reflect.DeepEqual(date, standard) {
				t.Fatalf("standard array changed after reuse: err=%v", err)
			}
		})
	}
}

func TestProgramDateRejectsInvalidShowtimes(t *testing.T) {
	for _, raw := range invalidShowtimesValues() {
		t.Run(raw, func(t *testing.T) {
			date := fixtureFilm("73").Dates[0]
			if err := json.Unmarshal([]byte(`{"date":20260914,"showtimes":`+raw+`}`), &date); err == nil || date.Showtimes != nil {
				t.Fatalf("invalid showtimes accepted or stale sessions retained: err=%v", err)
			}
		})
	}
}

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
		{"VF", "0", "10008", "2D", schedule.LanguageVF, schedule.FormatInfinityVision},
		{"VO", "0", "10008", "3D", schedule.LanguageVOSTFR, schedule.FormatInfinityVision},
		{"VF", "true", "32,25;21|10008", "3D", schedule.LanguageVFSTF, schedule.FormatInfinityVision},
		{"VF", "0", " 10008\t21\n25\r32 ", "2D", schedule.LanguageVF, schedule.FormatInfinityVision},
		{"VF", "0", "110008,100081", "2D", schedule.LanguageVF, schedule.Format2D},
	} {
		l, f, err := parseAttributes(session{Version: v.version, Subtitles: v.flag, Attributes: v.attrs, Relief: v.relief})
		if err != nil || l != v.lang || f != v.format {
			t.Fatalf("attributes=%+v got=%s/%s err=%v", v, l, f, err)
		}
	}
	for _, attrs := range []string{"x21", "21x", "-21", "[21]", "2.1", "10008x", "10008,invalid", "010008"} {
		if _, _, err := parseAttributes(session{Version: "VF", Subtitles: "0", Attributes: attrs, Relief: "2D"}); err == nil {
			t.Fatal("malformed attribute accepted")
		}
	}
	for _, s := range []session{
		{Version: "invalid", Subtitles: "0", Relief: "2D", Attributes: "10008"},
		{Version: "VF", Subtitles: "invalid", Relief: "2D", Attributes: "10008"},
		{Version: "VF", Subtitles: "0", Relief: "invalid", Attributes: "10008"},
	} {
		if _, _, err := parseAttributes(s); err == nil {
			t.Fatal("certification bypassed validation", s)
		}
	}
}
