package wikidata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIdentifiers(t *testing.T) {
	for _, value := range []string{"movie/a", "movie/mission-impossible_(2026)+!", "movie/" + strings.Repeat("a", 249)} {
		if !ValidMetacriticID(value) {
			t.Fatalf("rejected %q", value)
		}
	}
	for _, value := range []string{"", "movie/", "movie/" + strings.Repeat("a", 250), "movie/A", "game/a", "https://www.metacritic.com/movie/a", "movie/a/b", "movie/..", "movie/a%2f", "movie/a?b", "movie/a#b", "movie/é", "movie/a\n", "movie/a\r\n", "movie/a\\b", " movie/a"} {
		if ValidMetacriticID(value) {
			t.Fatalf("accepted %q", value)
		}
	}
	for _, value := range []string{"Q1", "Q" + strings.Repeat("9", 19)} {
		if !ValidQID(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"Q0", "Q01", "q1", "Q1\n", "Q" + strings.Repeat("1", 20), "Q١", ""} {
		if ValidQID(value) {
			t.Fatal(value)
		}
	}
}

func claim(value, rank string) string {
	b, _ := json.Marshal(value)
	return `{"type":"statement","rank":"` + rank + `","mainsnak":{"property":"P1712","datatype":"external-id","snaktype":"value","datavalue":{"type":"string","value":` + string(b) + `}}}`
}

func item(claims string) string {
	return `{"success":1,"entities":{"Q42":{"id":"Q42","type":"item"` + claims + `}}}`
}
func collection(statements ...string) string {
	return `,"claims":{"P1712":[` + strings.Join(statements, ",") + `]}`
}

func TestClaims(t *testing.T) {
	noValue := `{"type":"statement","rank":"normal","mainsnak":{"property":"P1712","datatype":"external-id","snaktype":"novalue"}}`
	valid := claim("movie/a", "normal")
	for _, tc := range []struct {
		name, body, want string
		bad              bool
	}{
		{"positive", item(collection(valid)), "movie/a", false},
		{"duplicates", item(collection(valid, claim("movie/a", "preferred"))), "movie/a", false},
		{"both ranks", item(collection(valid, claim("movie/b", "preferred"))), "", true},
		{"deprecated", item(collection(valid, claim("movie/b", "deprecated"))), "movie/a", false},
		{"deprecated only", item(collection(claim("malformed", "deprecated"))), "", false},
		{"deprecated malformed snak", item(collection(`{"rank":"deprecated","mainsnak":42}`)), "", false},
		{"non movie", item(collection(claim("game/a", "normal"))), "", false},
		{"non movie and movie", item(collection(claim("tv/a", "preferred"), valid)), "movie/a", false},
		{"no value", item(collection(noValue)), "", false},
		{"unknown alongside valid", item(collection(valid, strings.Replace(noValue, "novalue", "somevalue", 1))), "", true},
		{"bad alongside valid", item(collection(valid, claim("movie/../a", "normal"))), "", true},
		{"missing claims", item(""), "", false},
		{"empty object", item(`,"claims":{}`), "", false},
		{"empty array", item(`,"claims":[]`), "", false},
		{"formatted empty array", item(",\"claims\": [ \n ]"), "", false},
		{"empty statements", item(collection()), "", false},
		{"null claims", item(`,"claims":null`), "", true},
		{"null statements", item(`,"claims":{"P1712":null}`), "", true},
		{"nonempty array", item(`,"claims":[{}]`), "", true},
		{"references ignored", item(`,"claims":{"P1":[` + valid + `]}`), "", false},
		{"wrong entity", strings.Replace(item(collection(valid)), `"id":"Q42"`, `"id":"Q43"`, 1), "", true},
		{"wrong type", strings.Replace(item(collection(valid)), `"type":"item"`, `"type":"property"`, 1), "", true},
		{"missing", item(`,"missing":""`), "", true},
		{"deleted", item(`,"deleted":""`), "", true},
		{"redirect", item(`,"redirect":{}`), "", true},
		{"api error", `{"success":1,"error":{"info":"secret"},"entities":{}}`, "", true},
		{"no success", strings.Replace(item(collection(valid)), `"success":1`, `"success":0`, 1), "", true},
		{"invalid JSON", `{`, "", true},
		{"absent entity", `{"success":1,"entities":{}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extract([]byte(tc.body), "Q42")
			if got != tc.want || (err != nil) != tc.bad {
				t.Fatalf("got=%q err=%v", got, err)
			}
		})
	}
	for _, replacement := range []struct{ old, new string }{
		{`"type":"statement"`, `"type":"other"`}, {`"rank":"normal"`, `"rank":"other"`}, {`"property":"P1712"`, `"property":"P1"`}, {`"datatype":"external-id"`, `"datatype":"string"`}, {`"type":"string"`, `"type":"other"`}, {`"value":"movie/a"`, `"value":42`},
	} {
		if _, err := extract([]byte(item(collection(strings.Replace(valid, replacement.old, replacement.new, 1)))), "Q42"); err == nil {
			t.Fatal(replacement)
		}
	}
	for _, bad := range []string{"https://evil.example", "movie/a\n", "movie/A", "tv/../a", "movie/" + strings.Repeat("a", 250)} {
		if _, err := extract([]byte(item(collection(claim(bad, "normal")))), "Q42"); err == nil {
			t.Fatal(bad)
		}
	}
}

func TestClientTransport(t *testing.T) {
	for _, status := range []int{200, 302, 404, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/w/api.php" || r.URL.Query().Encode() != "action=wbgetentities&format=json&ids=Q42&props=claims&redirects=no" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") != "MesSeancesMetacriticBot/1.0 (https://messeances.fr)" {
					t.Error("unsafe request")
				}
				w.Header().Set("Location", "/redirect")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, item(collection(claim("movie/a", "normal"))))
			}))
			defer server.Close()
			jar, _ := cookiejar.New(nil)
			client, err := NewClientWithConfig(Config{BaseURL: server.URL, HTTPClient: &http.Client{Jar: jar}})
			if err != nil {
				t.Fatal(err)
			}
			jar.SetCookies(client.base, []*http.Cookie{{Name: "secret", Value: "secret"}})
			got, err := client.MetacriticID(t.Context(), "Q42")
			if calls != 1 || (status == 200 && (err != nil || got != "movie/a")) || (status != 200 && err == nil) {
				t.Fatalf("calls=%d got=%q err=%v", calls, got, err)
			}
		})
	}
	for _, body := range []string{strings.Repeat("x", maxResponseBytes+1), `{"error":{"info":"private"}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
		client, _ := NewClientWithConfig(Config{BaseURL: server.URL})
		_, err := client.MetacriticID(t.Context(), "Q42")
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal(err)
		}
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientDeadlineCancellationAndEndpoint(t *testing.T) {
	client, _ := NewClientWithConfig(Config{HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "www.wikidata.org" {
			t.Fatal("wrong production origin")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("unbounded request")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := client.MetacriticID(ctx, "Q42"); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err := client.MetacriticID(t.Context(), "Q0"); err == nil {
		t.Fatal("invalid QID accepted")
	}
	for _, origin := range []string{"https://evil.example", "http://localhost:1234", "http://127.0.0.1/path", "http://user@127.0.0.1", "http://127.0.0.1?x=1", "http://127.0.0.1#x"} {
		if _, err := NewClientWithConfig(Config{BaseURL: origin}); err == nil {
			t.Fatal(origin)
		}
	}
}
