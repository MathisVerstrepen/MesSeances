package cinewest

import (
	"encoding/json"
	"strings"
	"testing"

	"messeances/api/internal/schedule"
)

func TestSyncWebVOF(t *testing.T) {
	for _, tc := range []struct {
		tags []string
		want schedule.Language
	}{
		{[]string{"Localization.Language.VOF"}, schedule.LanguageVO},
		{[]string{"Localization.Language.French", "Localization.Language.VOF"}, schedule.LanguageVO},
		{[]string{"Localization.Version.Original", "Localization.Language.VOF"}, schedule.LanguageVO},
		{[]string{"Localization.Language.VOF", "Showtime.Accessibility.Subtitled"}, schedule.LanguageVOSTFR},
		{[]string{"Localization.Language.French", "Localization.Language.VOF", "Showtime.Accessibility.Subtitled"}, schedule.LanguageVOSTFR},
	} {
		t.Run(strings.Join(tc.tags, "+"), func(t *testing.T) {
			f := newFixture()
			tags := jsonFixture(append(tc.tags, "Format.Projection.3d", "Auditorium.Experience.InfinityVision"))
			f.calendar = []byte(strings.Replace(string(f.calendar), `["Localization.Language.French"]`, string(tags), 1))
			d, err := syncFixture(t, f)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range d.Showtimes {
				if strings.HasPrefix(r.ProviderShowingID, "webediamovies-") {
					found = true
					if r.Language != tc.want || r.ProviderVersion != "Localization.Language.VOF" || r.Format != schedule.FormatInfinityVision {
						t.Fatalf("record=%+v", r)
					}
				}
			}
			if !found {
				t.Fatal("missing Webedia showing")
			}
		})
	}
	if _, _, _, err := webAttributes([]string{"Localization.Language.vof"}); err == nil {
		t.Fatal("incorrect tag case accepted")
	}
}

func TestSyncTicketVOF(t *testing.T) {
	for _, tc := range []struct {
		name, formats string
		features      []string
		want          schedule.Language
	}{
		{"bare", "", nil, schedule.LanguageVO},
		{"ST format", "ST", nil, schedule.LanguageVOSTFR},
		{"ST feature", "", []string{"ST"}, schedule.LanguageVOSTFR},
		{"subtitle feature", "", []string{"subtitle"}, schedule.LanguageVOSTFR},
		{"OCAP alone unchanged", "OCAP", nil, schedule.LanguageVO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			var envelope struct {
				Result struct {
					Schedule ticketProgram `json:"schedule"`
				} `json:"result"`
			}
			if err := json.Unmarshal(f.programs["EMS1185"], &envelope); err != nil {
				t.Fatal(err)
			}
			p := envelope.Result.Schedule
			s := &p.Events[0].Sessions[0]
			s.Version, s.Formats, s.Features = "VOF", tc.formats, append(tc.features, "video_imax", "video_3d")
			f.programs["EMS1185"] = jsonFixture(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"schedule": p}})
			d, err := syncFixture(t, f)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range d.Showtimes {
				if r.TheaterID == "cinewest-ticketingcine-EMS1185" {
					found = true
					if r.Language != tc.want || r.ProviderVersion != "VOF" || r.Format != schedule.FormatIMAX {
						t.Fatalf("record=%+v", r)
					}
				}
			}
			if !found {
				t.Fatal("missing ticket showing")
			}
		})
	}
	if _, _, err := ticketAttributes(ticketSession{Version: "vof"}); err == nil {
		t.Fatal("lowercase version accepted")
	}
}

func TestSyncOfficeVOF(t *testing.T) {
	for _, subtitle := range []string{"", "SUBTITLE_NORMAL", "SUBTITLE_OCAP", "SUBTITLE_CCAP"} {
		t.Run(subtitle, func(t *testing.T) {
			f := newFixture()
			const key = "synthetic-royanlelido/"
			options := []officeOption{{ID: "1", CinemaID: "royanlelido", Label: "VOF"}, {ID: "2", CinemaID: "royanlelido", Label: "PICTURE_IMAX"}}
			if subtitle != "" {
				options = append(options, officeOption{ID: "3", CinemaID: "royanlelido", Label: subtitle})
				var shows []officeShow
				if err := json.Unmarshal(f.catalogs[key+"shows"], &shows); err != nil {
					t.Fatal(err)
				}
				shows[0].Options = []struct {
					Option officeRef `json:"mediaoptionsid"`
				}{{Option: officeRef{ID: "3"}}}
				f.catalogs[key+"shows"] = jsonFixture(shows)
			}
			f.catalogs[key+"mediaoptions"] = jsonFixture(options)
			d, err := syncFixture(t, f)
			if err != nil {
				t.Fatal(err)
			}
			want := schedule.LanguageVO
			if subtitle != "" {
				want = schedule.LanguageVOSTFR
			}
			found := false
			for _, r := range d.Showtimes {
				if r.TheaterID == "cinewest-cineoffice-royanlelido" {
					found = true
					if r.Language != want || r.ProviderVersion != "VOF" || r.Format != schedule.FormatIMAX {
						t.Fatalf("record=%+v", r)
					}
				}
			}
			if !found {
				t.Fatal("missing office showing")
			}
		})
	}
}

func TestOfficeVOFVersionConflicts(t *testing.T) {
	for _, other := range []string{"VERSION_LOCAL", "VERSION_ORIGINAL", "VERSION_ORIGINAL_LOCAL", "VERSION_MUET"} {
		for _, reverse := range []bool{false, true} {
			version, conflict := "VOF", other
			if reverse {
				version, conflict = conflict, version
			}
			s := officeShow{Language: "1", Format: "2", Options: []struct {
				Option officeRef `json:"mediaoptionsid"`
			}{{Option: officeRef{ID: "3"}}}}
			if _, _, _, err := officeAttributes(s, map[sourceID]string{"1": version, "2": "PICTURE_2K", "3": conflict}); err == nil {
				t.Fatalf("accepted %s + %s", version, conflict)
			}
		}
	}
	if _, _, _, err := officeAttributes(officeShow{Language: "1", Format: "2"}, map[sourceID]string{"1": "vof", "2": "PICTURE_2K"}); err == nil {
		t.Fatal("lowercase version accepted")
	}
}
