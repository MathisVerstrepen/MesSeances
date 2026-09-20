package megarama

import (
	"strings"
	"testing"

	"messeances/api/internal/schedule"
)

func TestSyncVOF(t *testing.T) {
	for _, tc := range []struct {
		name, version, formats, features string
		want                             schedule.Language
	}{
		{"bare", "VOF", "", `"video_motion","video_3d"`, schedule.LanguageVO},
		{"trimmed", " VOF ", "", `"video_motion","video_3d"`, schedule.LanguageVO},
		{"ST format", "VOF", "ST,OCAP", `"video_motion","video_3d"`, schedule.LanguageVOSTFR},
		{"ST feature", "VOF", "", `"video_motion","ST"`, schedule.LanguageVOSTFR},
		{"subtitle feature", "VOF", "", `"video_motion","subtitle"`, schedule.LanguageVOSTFR},
		{"OCAP alone unchanged", "VOF", "OCAP", `"video_motion"`, schedule.LanguageVO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := syncFixture()
			g.programs["EMS0565"] = strings.NewReplacer(`"version":"VF"`, `"version":"`+tc.version+`"`, `"formats":"ST,OCAP"`, `"formats":"`+tc.formats+`"`, `"video_motion","video_3d"`, tc.features).Replace(programFixture)
			d, _, err := runFixture(t, g)
			if err != nil || len(d.Showtimes) != 1 {
				t.Fatalf("dataset=%+v err=%v", d, err)
			}
			r := d.Showtimes[0]
			if r.Language != tc.want || r.ProviderVersion != "VOF" || r.Format != schedule.Format4DX {
				t.Fatalf("record=%+v", r)
			}
		})
	}
	if _, _, err := attributes(session{Version: "vof"}); err == nil {
		t.Fatal("lowercase version accepted")
	}
}
