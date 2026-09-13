package megarama

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

// Minimal synthetic fixtures encode verified shapes, never saved responses.
const configFixture = `var gl_config = {"site_id":"CHN0042","chain":{"sites":[{"id":"EMS0565","prog_id":"123","time_zone":"Europe/Paris","website_url":"https://bordeaux.megarama.fr/","name":"Cinéma {test}","address":{"street":"1 rue","city":"Bordeaux","zip_code":"33000"}}]}}; start();`
const programFixture = `{"jsonrpc":"2.0","id":1,"result":{"schedule":{"id":"123","ems":{"id":"0565"},"events":[{"id":"ABCDE","title":"Film synthétique","duration":"100","bill_url":"https://images.monnaie-services.com/movie_poster/120/FRABCDE/ABCDEFG1.webp","sessions":[{"id":"emsx056500000001","date":"202609121800","hall_name":" Salle  1 ","version":"VF","features":["video_motion","video_3d"],"formats":"ST,OCAP","first_part_duration":10}]}]}}}`

func fixtureCinema(t *testing.T) cinema {
	t.Helper()
	cs, err := parseConfig([]byte(configFixture))
	if err != nil {
		t.Fatal(err)
	}
	return cs[0]
}

func TestConfigAndProgramShapes(t *testing.T) {
	c := fixtureCinema(t)
	p, err := parseProgram([]byte(programFixture), c)
	if err != nil || len(p.Events) != 1 || p.Events[0].Duration != 100 {
		t.Fatalf("synthetic program: %v", err)
	}
	for _, replacement := range []struct{ from, to string }{
		{`"events":[`, `"other":[`}, {`"id":"123"`, `"id":"EMS0565"`}, {`"id":1`, `"id":2`}, {`"result":`, `"error":`}, {`"VF"`, `"UNKNOWN"`}, {`"ST,OCAP"`, `"ST,UNKNOWN"`}, {`"emsx056500000001"`, `"emsx131500000001"`}, {`"duration":"100"`, `"duration":-1`}, {`"first_part_duration":10`, `"first_part_duration":1.5`},
	} {
		if _, err := parseProgram([]byte(strings.Replace(programFixture, replacement.from, replacement.to, 1)), c); err == nil {
			t.Errorf("accepted invalid synthetic field %s", replacement.from)
		}
	}
	for _, body := range []string{`{"jsonrpc":"2.0","id":1,"result":null}`, `{"jsonrpc":"2.0","id":1,"result":{"schedule":{"id":"123","events":null}}}`} {
		if _, err := parseProgram([]byte(body), c); err == nil {
			t.Error("accepted missing program")
		}
	}
	if p, err := parseProgram([]byte(`{"jsonrpc":"2.0","id":1,"result":{"schedule":{"id":"123","events":[]}}}`), c); err != nil || p.Events == nil {
		t.Fatal("explicit empty events rejected")
	}
	for _, body := range []string{strings.Replace(configFixture, "https://bordeaux.megarama.fr/", "https://evil.test/", 1), strings.Replace(configFixture, "Europe/Paris", "UTC", 1), strings.Replace(configFixture, "CHN0042", "CHN9999", 1)} {
		if _, err := parseConfig([]byte(body)); err == nil {
			t.Error("accepted invalid config")
		}
	}
}

func TestMinutes(t *testing.T) {
	for _, raw := range []string{`null`, `0`, `100`, `"100"`} {
		var value minutes
		if json.Unmarshal([]byte(raw), &value) != nil {
			t.Errorf("rejected %s", raw)
		}
	}
	for _, raw := range []string{`-1`, `1.5`, `""`, `"1.5"`, `true`, `2147483648`} {
		var value minutes
		if json.Unmarshal([]byte(raw), &value) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestAttributesAndParisTimes(t *testing.T) {
	for _, test := range []struct {
		version, formats string
		features         []string
		language         schedule.Language
		format           schedule.Format
	}{
		{"VF", "", nil, schedule.LanguageVF, schedule.Format2D}, {"VO", "", nil, schedule.LanguageVO, schedule.Format2D}, {"VO", "ST", nil, schedule.LanguageVOSTFR, schedule.Format2D}, {"VF", "ST,OCAP", nil, schedule.LanguageVFSTF, schedule.Format2D}, {"VO", "", []string{"subtitle", "video_imax", "video_motion"}, schedule.LanguageVOSTFR, schedule.FormatIMAX}, {"VF", "3D", []string{"video_motion"}, schedule.LanguageVF, schedule.Format4DX}, {"VF", "3D", nil, schedule.LanguageVF, schedule.Format3D},
	} {
		language, format, err := attributes(session{Version: test.version, Formats: test.formats, Features: test.features})
		if err != nil || language != test.language || format != test.format {
			t.Errorf("attribute mapping %s/%s", test.version, test.formats)
		}
	}
	location, _ := time.LoadLocation(schedule.Timezone)
	for _, test := range []struct {
		wall   string
		offset int
	}{{"202609121800", 7200}, {"202701011800", 3600}, {"202609130030", 7200}, {"202607010330", 7200}} {
		start, err := parseStart(test.wall, location)
		_, offset := start.Zone()
		if err != nil || offset != test.offset {
			t.Errorf("Paris time %s", test.wall)
		}
	}
	for _, wall := range []string{"202603290230", "202610250230", "202613011800", "202609121860", "000001010000", "20260101"} {
		if _, err := parseStart(wall, location); err == nil {
			t.Errorf("accepted unsafe time %s", wall)
		}
	}
}

func TestPosters(t *testing.T) {
	const small = "https://images.monnaie-services.com/movie_poster/120/FRABCDE/ABCDEFG1.webp"
	const local = "https://images.monnaie-services.com/ems_spectacle/120/0565/HC12.jpg?ts=123"
	if got := posterURL(small, "ABCDE"); got != strings.Replace(small, "/120/", "/600/", 1) {
		t.Fatal("global resize")
	}
	if posterURL(local, "emsx0565HC12") != local {
		t.Fatal("local poster resized or rejected")
	}
	if posterURL(small, "ZZZZZ") != "" || posterURL(local, "emsx1315HC12") != "" || posterURL(small+"?x=1", "ABCDE") != "" {
		t.Fatal("unsafe poster relationship")
	}
	if got := parsePoster([]byte(`<html><meta property="og:image" content="`+small+`"></html>`), "ABCDE"); got == "" {
		t.Fatal("og:image missing")
	}
	if parsePoster([]byte(`<meta property="og:image" content="https://evil.test/a.jpg">`), "ABCDE") != "" {
		t.Fatal("unsafe fallback")
	}
}
