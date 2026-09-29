package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"messeances/api/internal/accounts"
	"messeances/api/internal/tmdb"
)

const validWatchlistTagCreate = `{"expected_username":"owner","expected_revision":"0","name":"Action","color":"neutral"}`
const validWatchlistTagUpdate = `{"expected_username":"owner","expected_revision":"0","tag_id":"1","name":"Action","color":"neutral"}`
const validWatchlistTagDelete = `{"expected_username":"owner","expected_revision":"0","tag_id":"1"}`
const validWatchlistTagAssign = `{"expected_username":"owner","expected_revision":"0","movie_slug":"film-1","tag_id":"1","assigned":"true"}`

func TestWatchlistTagsStrictTransport(t *testing.T) {
	for _, route := range []struct{ path, body string }{{"/tags", validWatchlistTagCreate}, {"/tags/update", validWatchlistTagUpdate}, {"/tags/delete", validWatchlistTagDelete}, {"/tags/assign", validWatchlistTagAssign}} {
		t.Run(route.path, func(t *testing.T) {
			for _, method := range []string{"GET", "PUT", "DELETE", "PATCH"} {
				h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
				w := httptest.NewRecorder()
				h.ServeHTTP(w, theaterPreferenceRequest(t, method, watchlistRoute+route.path, route.body))
				if w.Code != 405 {
					t.Fatal("unsupported tag method accepted", method, w.Code)
				}
				assertAccountHeaders(t, w)
			}
			check := func(body string, status int) {
				t.Helper()
				h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: lifecycleHTTPOptions(t)})
				w := httptest.NewRecorder()
				h.ServeHTTP(w, theaterPreferenceRequest(t, "POST", watchlistRoute+route.path, body))
				if w.Code != status {
					t.Fatalf("status %d want %d body %s", w.Code, status, w.Body.String())
				}
				if status == 400 && !strings.Contains(w.Body.String(), `"code":"invalid_request"`) {
					t.Fatal("wrong validation code")
				}
				assertAccountHeaders(t, w)
			}
			check(route.body, 503)
			var input map[string]string
			if err := json.Unmarshal([]byte(route.body), &input); err != nil {
				t.Fatal(err)
			}
			for field, value := range input {
				for _, replacement := range []string{`null`, `[]`, `true`, `1`, `{}`} {
					check(strings.Replace(route.body, `"`+field+`":"`+value+`"`, `"`+field+`":`+replacement, 1), 400)
				}
				missing := map[string]string{}
				for k, v := range input {
					if k != field {
						missing[k] = v
					}
				}
				body, err := json.Marshal(missing)
				if err != nil {
					t.Fatal(err)
				}
				check(string(body), 400)
				check(strings.Replace(route.body, `"`+field+`"`, `"X`+field+`"`, 1), 400)
				check(strings.TrimSuffix(route.body, "}")+`,"`+field+`":"`+value+`"}`, 400)
				check(strings.Replace(route.body, `"`+field+`":"`+value+`"`, `"`+field+`":"\ud800"`, 1), 400)
			}
			for _, body := range []string{`{}`, route.body + `{}`, "{\xff}", route.body + strings.Repeat(" ", 8192), strings.TrimSuffix(route.body, "}") + `,"account_id":"1"}`, strings.TrimSuffix(route.body, "}") + `,"\u0065xpected_revision":"0"}`} {
				check(body, 400)
			}
			for _, revision := range []string{"01", "-1", "9007199254740992", " 1", ""} {
				check(strings.Replace(route.body, `"expected_revision":"0"`, `"expected_revision":"`+revision+`"`, 1), 400)
			}
			if _, ok := input["tag_id"]; ok {
				for _, id := range []string{"0", "01", "-1", "+1", "9223372036854775808", "1.0", ""} {
					check(strings.Replace(route.body, `"tag_id":"1"`, `"tag_id":"`+id+`"`, 1), 400)
				}
			}
			if _, ok := input["name"]; ok {
				for _, name := range []string{"", "   ", strings.Repeat("é", 41), `a\nb`, `a\u2028b`, `a\u2029b`, `a\u0000b`} {
					check(strings.Replace(route.body, "Action", name, 1), 400)
				}
			}
			if route.path == "/tags/assign" {
				for _, flag := range []string{"TRUE", "yes", "0", ""} {
					check(strings.Replace(route.body, `"true"`, `"`+flag+`"`, 1), 400)
				}
			}
			if _, ok := input["color"]; ok {
				for _, color := range []string{"", "purple", "BLUE", " blue", "blue ", "#2563eb", "var(--blue)", "rgb(0,0,0)"} {
					check(strings.Replace(route.body, "neutral", color, 1), 400)
				}
				for _, color := range []string{"neutral", "red", "amber", "green", "teal", "blue", "violet", "rose"} {
					check(strings.Replace(route.body, "neutral", color, 1), 503)
				}
			}
		})
	}
}

func TestWatchlistTagRenameRouteRemoved(t *testing.T) {
	for _, options := range []AccountOptions{{}, lifecycleHTTPOptions(t)} {
		h := NewHandlerWithOptions(nil, "https://messeances.fr", HandlerOptions{Accounts: options})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, theaterPreferenceRequest(t, "POST", watchlistRoute+"/tags/rename", validWatchlistTagUpdate))
		if w.Code != 404 {
			t.Fatal("legacy rename route still registered", w.Code)
		}
		assertAccountHeaders(t, w)
	}
}

func watchlistTagRouteDenied(t *testing.T, p *browserProbe, status int) {
	t.Helper()
	for _, route := range []struct{ path, body string }{{"/tags", validWatchlistTagCreate}, {"/tags/update", validWatchlistTagUpdate}, {"/tags/delete", validWatchlistTagDelete}, {"/tags/assign", validWatchlistTagAssign}} {
		var input map[string]string
		if err := json.Unmarshal([]byte(route.body), &input); err != nil {
			t.Fatal(err)
		}
		input["expected_username"] = "watchlist_http"
		p.request("POST", watchlistRoute+route.path, input, status, nil)
	}
}

func watchlistTagRegisteredCRUD(t *testing.T, p *browserProbe, view accounts.WatchlistView, slug string) {
	t.Helper()
	wantColor := "blue"
	assertArrays := func() {
		t.Helper()
		if view.Tags == nil || view.Items == nil {
			t.Fatal("null snapshot arrays")
		}
		for _, item := range view.Items {
			if item.TagIDs == nil {
				t.Fatal("null tag_ids")
			}
		}
		for _, tag := range view.Tags {
			if tag.Color != wantColor {
				t.Fatal("snapshot lost committed color")
			}
		}
	}
	snapshot := func(method, path string, body any) {
		t.Helper()
		// Decode into a new value, so omitted fields cannot inherit old data.
		view = accounts.WatchlistView{}
		response := p.request(method, path, body, 200, &view)
		if response.header.Get("Cache-Control") != "no-store" || response.header.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(response.header.Get("Vary"), "Cookie") {
			t.Fatal("snapshot missing privacy headers")
		}
		assertArrays()
	}
	create := map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "name": " Action ", "color": "blue"}
	snapshot("POST", watchlistRoute+"/tags", create)
	assertArrays()
	if len(view.Tags) != 1 || view.Tags[0].Name != "Action" || view.Tags[0].Color != "blue" {
		t.Fatal("create wire")
	}
	id := view.Tags[0].ID
	p.request("POST", watchlistRoute+"/tags", create, 409, nil)
	create["expected_revision"] = view.Revision
	p.request("POST", watchlistRoute+"/tags", create, 409, nil)
	assign := map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "movie_slug": slug, "tag_id": id, "assigned": "true"}
	p.request("POST", watchlistRoute+"/tags/assign", assign, 404, nil)
	snapshot("POST", watchlistRoute, map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "movie_slug": slug, "saved": "true"})
	assertArrays()
	assign["expected_revision"] = view.Revision
	snapshot("POST", watchlistRoute+"/tags/assign", assign)
	assertArrays()
	if !reflect.DeepEqual(view.Items[0].TagIDs, []string{id}) {
		t.Fatal("assign wire")
	}
	update := map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "tag_id": id, "name": "<b>Été</b>", "color": "rose"}
	p.request("POST", watchlistRoute+"/tags/rename", update, 404, nil)
	wantColor = "rose"
	snapshot("POST", watchlistRoute+"/tags/update", update)
	if view.Tags[0].Name != "<b>Été</b>" || view.Tags[0].Color != "rose" || len(view.Items[0].TagIDs) != 1 {
		t.Fatal("update wire")
	}
	snapshot("GET", watchlistRoute, nil)
	snapshot("POST", watchlistRoute+"/sort", map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "sort_order": "title_asc"})
	// Color-only, name-only, normalized no-op and neutral reset use one route.
	for _, change := range []struct{ name, color string }{{"<b>Été</b>", "blue"}, {"Renamed", "blue"}, {" Renamed ", "blue"}, {"Renamed", "neutral"}} {
		wantColor = change.color
		snapshot("POST", watchlistRoute+"/tags/update", map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "tag_id": id, "name": change.name, "color": change.color})
	}
	view = watchlistTagRegisteredImport(t, p, view)
	assertArrays()
	// Remove the imported item again without losing reusable definitions.
	for _, item := range view.Items {
		if item.Slug != slug {
			snapshot("POST", watchlistRoute, map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "movie_slug": item.Slug, "saved": "false"})
		}
	}
	assign["expected_revision"] = view.Revision
	assign["assigned"] = "false"
	snapshot("POST", watchlistRoute+"/tags/assign", assign)
	if len(view.Items[0].TagIDs) != 0 {
		t.Fatal("unassign wire")
	}
	assign["expected_revision"] = view.Revision
	assign["assigned"] = "true"
	snapshot("POST", watchlistRoute+"/tags/assign", assign)
	// Delete one definition while another remains, checking nonempty color output.
	snapshot("POST", watchlistRoute+"/tags", map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "name": "Remaining", "color": "neutral"})
	del := map[string]string{"expected_username": "other_owner", "expected_revision": view.Revision, "tag_id": id}
	p.request("POST", watchlistRoute+"/tags/delete", del, 401, nil)
	del["expected_username"] = "watchlist_http"
	snapshot("POST", watchlistRoute+"/tags/delete", del)
	assertArrays()
	if len(view.Tags) != 1 || len(view.Items) != 1 || len(view.Items[0].TagIDs) != 0 {
		t.Fatal("tag delete removed film")
	}
	del["expected_revision"] = view.Revision
	p.request("POST", watchlistRoute+"/tags/delete", del, 404, nil)
}

type watchlistColorHTTPProvider struct{}

func (watchlistColorHTTPProvider) Search(context.Context, string) ([]tmdb.Candidate, error) {
	return []tmdb.Candidate{}, nil
}

func (watchlistColorHTTPProvider) Details(_ context.Context, id int64) (tmdb.Details, error) {
	adult := false
	return tmdb.Details{ID: id, Adult: &adult, Title: "Imported color fixture", OriginalTitle: "Imported color fixture", Genres: []string{}}, nil
}

func watchlistTagRegisteredImport(t *testing.T, p *browserProbe, view accounts.WatchlistView) accounts.WatchlistView {
	t.Helper()
	// Reuse the guarded isolated database and complete cookie session, but use
	// a separate registered router with a fake provider. The browser fixture's
	// normal missing-provider behavior remains unchanged.
	service, err := accounts.NewService(accounts.NewPostgresStore(p.h.pool), accounts.ServiceOptions{
		Hasher: unavailableAccountHasher{}, Origin: p.h.origin, AddressHMACKey: []byte(strings.Repeat("c", 32)),
		WatchlistProvider: watchlistColorHTTPProvider{}, WatchlistRefresh: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandlerWithOptions(nil, p.h.origin, HandlerOptions{Accounts: AccountOptions{Enabled: true, Service: service, Origin: p.h.origin}})
	u, err := url.Parse(p.h.apiURL)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, watchlistRoute+"/import", strings.NewReader(`{"expected_username":"watchlist_http","expected_revision":"`+view.Revision+`","tmdb_id":"42"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", p.h.origin)
	r.Header.Set("X-Messeances-CSRF", "1")
	for _, cookie := range p.client.Jar.Cookies(u) {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("registered fake import failed", w.Code)
	}
	assertAccountHeaders(t, w)
	var result accounts.WatchlistImportView
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !reflect.DeepEqual(result.Watchlist.Tags, view.Tags) || len(result.Watchlist.Items) != len(view.Items)+1 {
		t.Fatal("nested import lost tag colors", err)
	}
	return result.Watchlist
}
