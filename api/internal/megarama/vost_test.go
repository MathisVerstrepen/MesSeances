package megarama

import (
	"strings"
	"testing"

	"messeances/api/internal/schedule"
)

func TestSyncVOST(t *testing.T) {
	for _, flags := range []struct{ name, formats, features string }{
		{"version alone", "", `"video_motion","video_3d"`}, {"ST format", "ST", `"video_motion","video_3d"`}, {"subtitle feature", "", `"video_motion","subtitle"`},
	} {
		t.Run(flags.name, func(t *testing.T) {
			g := syncFixture()
			g.programs["EMS0565"] = strings.NewReplacer(`"version":"VF"`, `"version":"VOST"`, `"formats":"ST,OCAP"`, `"formats":"`+flags.formats+`"`, `"video_motion","video_3d"`, flags.features).Replace(programFixture)
			d, _, err := runFixture(t, g)
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Showtimes) != 1 {
				t.Fatal("missing VOST showing")
			}
			r := d.Showtimes[0]
			if r.Language != schedule.LanguageVOSTFR || r.ProviderVersion != "VOST" || r.Format != schedule.Format4DX {
				t.Fatalf("unexpected VOST mapping: %+v", r)
			}
		})
	}
}
