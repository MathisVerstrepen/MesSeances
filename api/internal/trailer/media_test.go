package trailer

import (
	"encoding/json"
	"strings"
	"testing"
)

const testMP4 = "https://fr.vid.web.acsta.net/nmedia/33/26/08/25/15/00/20642715_hd_013.mp4"

func TestMediaValidationAndNormalization(t *testing.T) {
	m := MP4(testMP4+"?source=ugc&amp;platform=proxy", "ugc")
	if m == nil || m.URL != testMP4 || !Valid(m) {
		t.Fatal("tracking parameters not normalized")
	}
	for _, raw := range []string{strings.Replace(testMP4, "https:", "http:", 1), strings.Replace(testMP4, "acsta.net/", "acsta.net:443/", 1), strings.Replace(testMP4, "https://", "https://user@", 1), strings.Replace(testMP4, "/nmedia/", "/nmedia/../", 1), strings.Replace(testMP4, "/nmedia/", "/nmedia//", 1), testMP4 + "?", testMP4 + "#", testMP4 + "?token=x", testMP4 + "?source=a&source=b", testMP4 + "\n", "https://evil.example/a.mp4"} {
		if MP4(raw, "ugc") != nil {
			t.Errorf("unsafe reference accepted: %q", raw)
		}
	}
	if YouTube("bad", "cineville") != nil || !Valid(YouTube("AbC-12_xYz9", "cineville")) {
		t.Fatal("YouTube validation")
	}
}

func TestOptionalObservationsDistinguishMissingInvalidAndAbsent(t *testing.T) {
	for _, tc := range []struct {
		raw             string
		observed, found bool
	}{
		{`{"HD":"` + testMP4 + `","SD":"https://evil.example/a.mp4"}`, true, true},
		{`{"HD":null,"SD":"` + testMP4 + `"}`, true, true},
		{`{"HD":null,"SD":null}`, true, false}, {`null`, true, false}, {``, false, false}, {`false`, false, false}, {`{}`, false, false}, {`{"error":"unknown"}`, false, false},
		{`{"HD":123}`, false, false}, {`{"HD":"https://evil.example/a.mp4"}`, false, false},
	} {
		m, observed := CGR(json.RawMessage(tc.raw))
		if observed != tc.observed || (m != nil) != tc.found {
			t.Errorf("CGR %s: %v %v", tc.raw, m, observed)
		}
	}
	for _, tc := range []struct {
		id, show        string
		observed, found bool
	}{
		{`"AbC-12_xYz9"`, `1`, true, true}, {`"AbC-12_xYz9"`, `"1"`, true, true}, {`""`, `0`, true, false}, {`"bad"`, `1`, false, false}, {`null`, `1`, false, false}, {`"AbC-12_xYz9"`, `true`, false, false}, {`"AbC-12_xYz9"`, `2`, false, false}, {`"AbC-12_xYz9"`, `null`, false, false},
	} {
		m, observed := Cineville(json.RawMessage(tc.id), json.RawMessage(tc.show))
		if observed != tc.observed || (m != nil) != tc.found {
			t.Errorf("Cineville %s/%s: %v %v", tc.id, tc.show, m, observed)
		}
	}
}

func TestUGCFiltersAdvertisementsAndOtherMovies(t *testing.T) {
	body := `{"videos":[{"type":"ADVERTISING","filmId":17967,"src":"https://evil.example/ad.mp4"},{"type":"FILM","filmId":123,"src":"https://evil.example/other.mp4"},{"type":"FILM","filmId":17967,"src":"` + testMP4 + `?source=ugc&amp;platform=proxy"}]}`
	m, err := ParseUGC([]byte(body), "17967")
	if err != nil || m == nil || m.URL != testMP4 {
		t.Fatalf("film trailer not selected: %v %v", m, err)
	}
	for _, raw := range []string{`{}`, `{"videos":null}`, `{"videos":false}`, `{"videos":[{"type":"FILM","filmId":17967,"src":"https://evil.example/a.mp4"}]}`} {
		if _, err := ParseUGC([]byte(raw), "17967"); err == nil {
			t.Errorf("invalid response treated as missing: %s", raw)
		}
	}
	m, err = ParseUGC([]byte(`{"videos":[]}`), "17967")
	if err != nil || m != nil {
		t.Fatal("explicit absence rejected")
	}
}
