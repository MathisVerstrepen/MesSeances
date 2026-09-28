package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"messeances/api/internal/accounts"
)

const validWatchlistTagCreate = `{"expected_username":"owner","expected_revision":"0","name":"Action"}`
const validWatchlistTagRename = `{"expected_username":"owner","expected_revision":"0","tag_id":"1","name":"Action"}`
const validWatchlistTagDelete = `{"expected_username":"owner","expected_revision":"0","tag_id":"1"}`
const validWatchlistTagAssign = `{"expected_username":"owner","expected_revision":"0","movie_slug":"film-1","tag_id":"1","assigned":"true"}`

func TestWatchlistTagsStrictTransport(t *testing.T) {
	for _, route := range []struct{ path, body string }{{"/tags", validWatchlistTagCreate}, {"/tags/rename", validWatchlistTagRename}, {"/tags/delete", validWatchlistTagDelete}, {"/tags/assign", validWatchlistTagAssign}} {
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
		})
	}
}

func watchlistTagRouteDenied(t *testing.T, p *browserProbe, status int) {
	t.Helper()
	for _, route := range []struct{ path, body string }{{"/tags", validWatchlistTagCreate}, {"/tags/rename", validWatchlistTagRename}, {"/tags/delete", validWatchlistTagDelete}, {"/tags/assign", validWatchlistTagAssign}} {
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
	}
	create := map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "name": " Action "}
	p.request("POST", watchlistRoute+"/tags", create, 200, &view)
	assertArrays()
	if len(view.Tags) != 1 || view.Tags[0].Name != "Action" {
		t.Fatal("create wire")
	}
	id := view.Tags[0].ID
	p.request("POST", watchlistRoute+"/tags", create, 409, nil)
	create["expected_revision"] = view.Revision
	p.request("POST", watchlistRoute+"/tags", create, 409, nil)
	assign := map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "movie_slug": slug, "tag_id": id, "assigned": "true"}
	p.request("POST", watchlistRoute+"/tags/assign", assign, 404, nil)
	p.request("POST", watchlistRoute, map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "movie_slug": slug, "saved": "true"}, 200, &view)
	assertArrays()
	assign["expected_revision"] = view.Revision
	p.request("POST", watchlistRoute+"/tags/assign", assign, 200, &view)
	assertArrays()
	if !reflect.DeepEqual(view.Items[0].TagIDs, []string{id}) {
		t.Fatal("assign wire")
	}
	rename := map[string]string{"expected_username": "watchlist_http", "expected_revision": view.Revision, "tag_id": id, "name": "<b>Été</b>"}
	p.request("POST", watchlistRoute+"/tags/rename", rename, 200, &view)
	if view.Tags[0].Name != "<b>Été</b>" || len(view.Items[0].TagIDs) != 1 {
		t.Fatal("rename wire")
	}
	p.request("GET", watchlistRoute, nil, 200, &view)
	assign["expected_revision"] = view.Revision
	assign["assigned"] = "false"
	p.request("POST", watchlistRoute+"/tags/assign", assign, 200, &view)
	if len(view.Items[0].TagIDs) != 0 {
		t.Fatal("unassign wire")
	}
	assign["expected_revision"] = view.Revision
	assign["assigned"] = "true"
	p.request("POST", watchlistRoute+"/tags/assign", assign, 200, &view)
	del := map[string]string{"expected_username": "other_owner", "expected_revision": view.Revision, "tag_id": id}
	p.request("POST", watchlistRoute+"/tags/delete", del, 401, nil)
	del["expected_username"] = "watchlist_http"
	p.request("POST", watchlistRoute+"/tags/delete", del, 200, &view)
	assertArrays()
	if len(view.Tags) != 0 || len(view.Items) != 1 || len(view.Items[0].TagIDs) != 0 {
		t.Fatal("tag delete removed film")
	}
	del["expected_revision"] = view.Revision
	p.request("POST", watchlistRoute+"/tags/delete", del, 404, nil)
}
