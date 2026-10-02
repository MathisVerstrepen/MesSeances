package megarama

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const siteConfigFixture = `var gl_config = {"site":{"id":"EMS1379","prog_id":"6708900","website_url":"ems1379.ticketingcine.com","website_full_url":"https://www.ticketingcine.com/?EMS1379"}}; start();`
const emptyProgramFixture = `{"jsonrpc":"2.0","id":1,"result":{"schedule":{"id":"6708900","ems":{"id":"1379"},"events":[]}}}`

func recoveryFixture() *fixtureGetter {
	g := syncFixture()
	g.config = strings.Replace(g.config, `]}};`, `,{"id":"EMS1379","prog_id":"6708900","time_zone":"Europe/Paris","name":"Megarama","address":{"street":"1 rue","city":"Saint Martin d'Hères","zip_code":"38400"}}]}};`, 1)
	g.siteConfigs = map[string]string{"EMS1379": siteConfigFixture}
	g.programs["EMS1379"] = emptyProgramFixture
	return g
}

func TestSyncMissingWebsiteRecoveryAndEmptyCinema(t *testing.T) {
	baseline, _, err := runFixture(t, syncFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"", `,"website_url":""`, `,"website_url":null`} {
		g := recoveryFixture()
		g.config = strings.Replace(g.config, `"id":"EMS1379"`, `"id":"EMS1379"`+field, 1)
		d, summary, err := runFixture(t, g)
		if err != nil || summary.Cinemas != 2 || summary.Showtimes != 1 || len(d.Theaters) != 2 {
			t.Fatalf("complete recovered dataset: %v", err)
		}
		if !reflect.DeepEqual(g.siteCalls, []string{"EMS1379"}) || g.programCalls["EMS1379"] != "https://ems1379.ticketingcine.com/" || g.programCalls["EMS0565"] != "https://bordeaux.megarama.fr/" {
			t.Fatal("recovery or per-cinema root binding")
		}
		if !reflect.DeepEqual(d.Theaters[0], baseline.Theaters[0]) || !reflect.DeepEqual(d.Showtimes, baseline.Showtimes) {
			t.Fatal("known valid chain entry changed")
		}
		if d.Theaters[1].ProviderID != "EMS1379" || d.Theaters[1].City != "Saint Martin d'Hères" || d.Theaters[1].AvailableDates == nil || len(d.Theaters[1].AvailableDates) != 0 {
			t.Fatal("explicit empty cinema not retained")
		}
		if err := schedule.ValidateDataset(d, true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSyncRecoveryFailureAtomicity(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*fixtureGetter)
	}{
		{"bootstrap transport", func(g *fixtureGetter) {
			g.siteErr = &RequestError{Operation: OperationConfig, Kind: syncproxy.FailureTransport}
		}},
		{"bootstrap site identity", func(g *fixtureGetter) {
			g.siteConfigs["EMS1379"] = strings.Replace(siteConfigFixture, "EMS1379", "EMS0565", 1)
		}},
		{"bootstrap program identity", func(g *fixtureGetter) {
			g.siteConfigs["EMS1379"] = strings.Replace(siteConfigFixture, "6708900", "123", 1)
		}},
		{"bootstrap invalid host", func(g *fixtureGetter) {
			g.siteConfigs["EMS1379"] = strings.Replace(siteConfigFixture, "ems1379.ticketingcine.com", "evil.test", 1)
		}},
		{"program identity", func(g *fixtureGetter) {
			g.programs["EMS1379"] = strings.Replace(emptyProgramFixture, "6708900", "123", 1)
		}},
		{"program cinema", func(g *fixtureGetter) {
			g.programs["EMS1379"] = strings.Replace(emptyProgramFixture, `"1379"`, `"0565"`, 1)
		}},
		{"missing events", func(g *fixtureGetter) {
			g.programs["EMS1379"] = strings.Replace(emptyProgramFixture, `"events":[]`, `"events":null`, 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := recoveryFixture()
			test.edit(g)
			d, _, err := runFixture(t, g)
			if err == nil || !reflect.DeepEqual(d, schedule.Dataset{}) {
				t.Fatal("failed acquisition returned publishable partial dataset")
			}
		})
	}
}

func TestSyncPopulatedInvalidWebsiteNeverRecovers(t *testing.T) {
	for _, website := range []string{" ", "ems1379.ticketingcine.com", "http://ems1379.ticketingcine.com/", "https://evil.test/", "https://ems1379.ticketingcine.com/?EMS1379", "https://ems1379.ticketingcine.com/?x=1&x=2"} {
		g := recoveryFixture()
		g.config = strings.Replace(g.config, `"id":"EMS1379"`, `"id":"EMS1379","website_url":"`+website+`"`, 1)
		d, _, err := runFixture(t, g)
		if err == nil || len(g.siteCalls) != 0 || len(g.programCalls) != 0 || !reflect.DeepEqual(d, schedule.Dataset{}) {
			t.Fatal("malformed populated website recovered or dispatched")
		}
	}
	for _, website := range []string{"https://www.ticketingcine.com/?EMS1379", "https://evil.test/", "https://ems1379.ticketingcine.com/"} {
		g := recoveryFixture()
		g.config = strings.Replace(g.config, `"id":"EMS1379"`, `"id":"EMS1379","website_full_url":"`+website+`"`, 1)
		if _, _, err := runFixture(t, g); err == nil || len(g.siteCalls) != 0 || len(g.programCalls) != 0 {
			t.Fatal("populated portal-only chain website incorrectly treated as absent")
		}
	}
	g := syncFixture()
	if _, _, err := runFixture(t, g); err != nil || len(g.siteCalls) != 0 {
		t.Fatal("valid chain entry unnecessarily recovered")
	}
}

func TestSiteWebsiteNormalizationAndGuards(t *testing.T) {
	c := cinema{ID: "EMS1379", ProgramID: "6708900"}
	for _, website := range []string{"ems1379.ticketingcine.com", "ems1379.ticketingcine.com/", "https://ems1379.ticketingcine.com", "https://ems1379.ticketingcine.com/"} {
		body := strings.Replace(siteConfigFixture, "ems1379.ticketingcine.com", website, 1)
		got, err := parseSiteWebsite([]byte(body), c)
		if err != nil || got != "https://ems1379.ticketingcine.com/" {
			t.Fatal("verified root normalization failed")
		}
	}
	for _, website := range []string{"", "evil.ticketingcine.com", "bordeaux.megarama.fr", "saintmartindheres.megarama.fr", "ems1379.ticketingcine.com.evil.test", "ems1379.ticketingcine.com?site_id=EMS1379", "ems1379.ticketingcine.com?x=1&x=2", "https://ems1379.ticketingcine.com/?", "http://ems1379.ticketingcine.com/", "https://user@ems1379.ticketingcine.com/", "https://ems1379.ticketingcine.com:443/", "https://ems1379.ticketingcine.com/path", "https://ems1379.ticketingcine.com/#showsession?id=emsx137900000001", strings.Repeat("x", 4096)} {
		body := strings.Replace(siteConfigFixture, "ems1379.ticketingcine.com", website, 1)
		if _, err := parseSiteWebsite([]byte(body), c); !errors.Is(err, errShape) {
			t.Fatal("unsafe bootstrap root accepted")
		}
	}
	for _, body := range []string{"", `var gl_config = {};`, strings.Replace(siteConfigFixture, `"website_url":`, `"unused":`, 1), strings.Replace(siteConfigFixture, `"id":"EMS1379"`, `"id":"EMS0565"`, 1), strings.Replace(siteConfigFixture, `"prog_id":"6708900"`, `"prog_id":"123"`, 1)} {
		if _, err := parseSiteWebsite([]byte(body), c); !errors.Is(err, errShape) {
			t.Fatal("invalid bootstrap accepted")
		}
	}
}

func TestSiteConfigURLGuards(t *testing.T) {
	for _, raw := range []string{ConfigURL, ProgramURL, "https://ws.ticketingcine.com/config.js?site_id=EMS1379"} {
		u, err := url.Parse(raw)
		if err != nil || !allowedURL(u) {
			t.Fatal("bounded endpoint rejected")
		}
	}
	for _, raw := range []string{"https://ws.ticketingcine.com/config.js", "https://ws.ticketingcine.com/config.js?site_id=EMS1379&site_id=EMS0565", "https://ws.ticketingcine.com/config.js?site_id=EMS1379&x=1", "https://ws.ticketingcine.com/config.js?site_id=%45MS1379", "https://ws.ticketingcine.com/config.js?site_id=EMS13790", "https://evil.test/config.js?site_id=EMS1379", "http://ws.ticketingcine.com/config.js?site_id=EMS1379", "https://ws.ticketingcine.com:443/config.js?site_id=EMS1379", "https://user@ws.ticketingcine.com/config.js?site_id=EMS1379", "https://ws.ticketingcine.com/config.js?site_id=EMS1379#x"} {
		u, err := url.Parse(raw)
		if err == nil && allowedURL(u) {
			t.Fatal("unsafe bootstrap endpoint accepted")
		}
	}
	c := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid site dispatched"); return nil, nil })
	for _, id := range []string{"", "CHN0042", "EMS13790", "EMS1379&site_id=EMS0565", "../EMS1379"} {
		if _, err := c.SiteConfig(t.Context(), id); err == nil || c.RequestCount() != 0 {
			t.Fatal("invalid site requested")
		}
	}
}

func TestClientSyncRecoveryExactReferers(t *testing.T) {
	g := recoveryFixture()
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			switch r.URL.String() {
			case ConfigURL:
				return response(200, "text/javascript", g.config), nil
			case "https://ws.ticketingcine.com/config.js?site_id=EMS1379":
				return response(200, "application/javascript", siteConfigFixture), nil
			default:
				t.Error("unexpected config or poster request")
				return response(404, "text/html", ""), nil
			}
		}
		id := "EMS0565"
		want := "https://bordeaux.megarama.fr/"
		if r.Header.Get("Referer") == "https://ems1379.ticketingcine.com/" {
			id, want = "EMS1379", "https://ems1379.ticketingcine.com/"
		}
		var body struct {
			Params struct {
				SiteID string `json:"site_id"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Params.SiteID != id || r.Header.Get("Referer") != want || r.Method != http.MethodPost || r.URL.String() != ProgramURL {
			t.Error("program request and exact cinema Referer diverged")
		}
		return response(200, "application/json", g.programs[id]), nil
	})
	d, summary, err := Sync(t.Context(), c, SyncOptions{From: "2026-09-12", Now: time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)})
	if err != nil || len(d.Theaters) != 2 || summary.Requests != 4 || summary.Showtimes != 1 {
		t.Fatalf("client recovery sync: %v", err)
	}
}
