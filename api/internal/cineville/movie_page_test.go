package cineville

import (
	"encoding/json"
	"testing"

	"messeances/api/internal/schedule"
)

func TestSourceMoviePageURL(t *testing.T) {
	for _, tc := range []struct {
		name, id, metadata, want string
	}{
		{"object", "168608", `{"visa":168608,"titre":"Film"}`, "https://www.cineville.fr/programmes/katorzaquimper#168608"},
		{"array", "168608", `[{"visa":"168608"}]`, "https://www.cineville.fr/programmes/katorzaquimper#168608"},
		{"event", "-40370611261", `{"visa":-40370611261,"titre":"Event"}`, "https://www.cineville.fr/programmes/katorzaquimper#-40370611261"},
		{"unsafe JavaScript number", "9223372036854775807", `{"visa":9223372036854775807,"titre":"Film"}`, ""},
		{"exact string visa", "9223372036854775807", `{"visa":"9223372036854775807","titre":"Film"}`, "https://www.cineville.fr/programmes/katorzaquimper#9223372036854775807"},
		{"missing metadata", "168608", `null`, ""},
		{"missing anchor", "168608", `{"titre":"Film"}`, ""},
		{"different anchor", "168608", `{"visa":168609,"titre":"Film"}`, ""},
		{"later array entry", "168608", `[{}, {"visa":168608}]`, ""},
		{"malformed optional anchor", "168608", `{"visa":{},"titre":"Film"}`, ""},
		{"null anchor", "168608", `{"visa":null,"titre":"Film"}`, ""},
		{"empty metadata", "168608", ``, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := film{Visa: scalar(tc.id), Metadata: json.RawMessage(tc.metadata)}
			c := fixtureCinema()
			if got := sourceMoviePageURL(f, c); got != tc.want {
				t.Fatalf("URL=%q want=%q", got, tc.want)
			}
			c.Route = "changed-source-route"
			if sourceMoviePageURL(f, c) != "" {
				t.Fatal("unverified route must remain unlinked")
			}
		})
	}
}

func TestSyncMovieAnchorsAcrossCinemas(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "missing sibling anchor"}[missing], func(t *testing.T) {
			c1 := fixtureCinema()
			c2 := c1
			c2.ID, c2.Route = "4037", "bruz"
			catalog := []cinema{c1, c2}
			pages := map[string]pageProps{}
			for _, c := range catalog {
				p := fixturePage(c)
				p.Cinemas = catalog
				p.Program[0].Metadata = json.RawMessage(`{"visa":167934,"titre":"Source title","duree":"1h33"}`)
				if missing && c.ID == c2.ID {
					p.Program[0].Metadata = json.RawMessage(`null`)
				}
				pages[c.Route] = p
			}
			f := &fixtureFetcher{t: t, builds: []string{"build-1"}, catalog: catalog, pages: pages}
			data, _, err := Sync(t.Context(), f, fixtureOptions())
			if err != nil || len(data.Showtimes) != 2 {
				t.Fatalf("sync failed or lost sessions: %v", err)
			}
			want := "https://www.cineville.fr/programmes/bruz#167934"
			if missing {
				want = ""
			}
			for _, s := range data.Showtimes {
				if s.Movie.SourcePageURL != want {
					t.Fatalf("inconsistent shared source URL: %q", s.Movie.SourcePageURL)
				}
			}
			if schedule.ValidateDataset(data, true) != nil {
				t.Fatal("invalid captured URL dataset")
			}
		})
	}
}

func TestSyncMovieAnchorsRequireDefaultProgramme(t *testing.T) {
	for _, inDefault := range []bool{false, true} {
		t.Run(map[bool]string{false: "event filter only", true: "default programme"}[inDefault], func(t *testing.T) {
			p := fixturePage(fixtureCinema())
			p.Program[0].Metadata = json.RawMessage(`{"visa":167934,"titre":"Film","duree":"1h33"}`)
			p.Events = append([]film(nil), p.Program...)
			if !inDefault {
				p.Program = []film{}
			} else {
				// An alternate event representation must not hide a verified
				// anchor already present in the default film card.
				p.Events[0].Metadata = json.RawMessage(`null`)
			}
			data, _, err := Sync(t.Context(), singleFetcher(t, p), fixtureOptions())
			if err != nil || len(data.Showtimes) != 1 {
				t.Fatalf("sync failed or lost showing: %v", err)
			}
			got := data.Showtimes[0].Movie.SourcePageURL
			if inDefault && got != "https://www.cineville.fr/programmes/katorzaquimper#167934" || !inDefault && got != "" {
				t.Fatalf("wrong default programme availability: %q", got)
			}
		})
	}
}
