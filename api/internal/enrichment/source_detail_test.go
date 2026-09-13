package enrichment

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewSourceDetailURLs(t *testing.T) {
	for _, test := range []struct {
		name, provider, movieID, showingID, theaterID, stored, want string
	}{
		{"ugc", SourceUGC, "200", "", "", "https://evil.example/", "https://www.ugc.fr/film.html?id=200"},
		{"kinepolis", SourceKinepolis, "HO00016258", "1234", "FRLIL", "https://kinepolis.fr/direct-vista-redirect/1234/0/FRLIL/0", "https://kinepolis.fr/direct-vista-redirect/1234/0/FRLIL/0"},
		{"pathe", SourcePathe, "film-a", "V3308S135392", "lille", "https://s.pathe.fr/fr/V3308S135392/booking", "https://s.pathe.fr/fr/V3308S135392/booking"},
		{"cgr", SourceCGR, "1001", "W8010-" + strings.Repeat("a", 64), "W8010", "https://achat.cgrcinemas.fr/lille/r/12345", "https://achat.cgrcinemas.fr/lille/r/12345"},
		{"megarama global", SourceMegarama, "ABCDE", "", "", "https://evil.example/", "https://www.ticketingcine.com/film/ABCDE.html"},
		{"megarama local", SourceMegarama, "EMS0565-emsx0565HC123", "emsx056500000001", "EMS0565", "https://bordeaux.megarama.fr/#showsession?id=emsx056500000001", "https://bordeaux.megarama.fr/#showsession?id=emsx056500000001"},
		{"megarama verified alias", SourceMegarama, "EMS0809-emsx0809HC123", "emsx080900000001", "EMS0809", "https://www.royalpalacenogent.fr/#showsession?id=emsx080900000001", "https://www.royalpalacenogent.fr/#showsession?id=emsx080900000001"},
		{"megarama existing root fallback", SourceMegarama, "EMS0565-emsx0565HC123", "emsx056500000001", "EMS0565", "https://bordeaux.megarama.fr/", "https://bordeaux.megarama.fr/"},
		{"missing screening", SourceKinepolis, "HO00016258", "", "", "", ""},
		{"unknown provider", "other", "200", "1234", "FRLIL", "https://kinepolis.fr/direct-vista-redirect/1234/0/FRLIL/0", ""},
		{"invalid movie ID", SourceMegarama, "../ABCDE", "", "", "https://evil.example/", ""},
		{"wrong cinema", SourceMegarama, "EMS0565-emsx0565HC123", "emsx056500000001", "EMS0565", "https://roubaix.megarama.fr/#showsession?id=emsx056500000001", ""},
		{"wrong showing", SourcePathe, "film-a", "V3308S135392", "lille", "https://s.pathe.fr/fr/V3308S135393/booking", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, filter := range []PendingMatchFilter{PendingMatchFilterUnresolved, PendingMatchFilterRejected, PendingMatchFilterMatched} {
				store := &reviewStoreStub{items: []PendingMatch{{SourceProvider: test.provider, SourceMovieID: test.movieID, SourceTitle: "Film (VF)", SourceDetailURL: test.stored, sourceShowingID: test.showingID, sourceTheaterID: test.theaterID}}}
				provider := &reviewProviderStub{}
				items, err := NewReviewService(store, provider, nil).Pending(context.Background(), filter, "", 20, 0)
				if err != nil || len(items) != 1 || items[0].SourceDetailURL != test.want || items[0].SourceTitle != "Film (VF)" || provider.calls != 0 {
					t.Fatalf("items=%+v calls=%d err=%v", items, provider.calls, err)
				}
				raw, err := json.Marshal(items[0])
				if err != nil || strings.Contains(string(raw), "sourceShowingID") || strings.Contains(string(raw), "sourceTheaterID") {
					t.Fatalf("internal screening fields exposed: %s err=%v", raw, err)
				}
			}
		})
	}
}

func TestReviewSourceDetailRejectsUnsafePersistedURLs(t *testing.T) {
	const valid = "https://kinepolis.fr/direct-vista-redirect/1234/0/FRLIL/0"
	for _, raw := range []string{
		"javascript:alert(1)",
		"//kinepolis.fr/direct-vista-redirect/1234/0/FRLIL/0",
		strings.Replace(valid, "https:", "http:", 1),
		strings.Replace(valid, "kinepolis.fr", "kinepolis.fr.evil.example", 1),
		strings.Replace(valid, "kinepolis.fr", "kinepolis.fr:443", 1),
		strings.Replace(valid, "kinepolis.fr", "user@kinepolis.fr", 1),
		strings.Replace(valid, "/1234/", "/%31%32%33%34/", 1),
		strings.Replace(valid, "/1234/", "/../1234/", 1),
		strings.Replace(valid, "/1234/", "/1235/", 1),
		strings.Replace(valid, "/FRLIL/", "/FRNAN/", 1),
		valid + "?redirect=https://evil.example",
		valid + "?",
		valid + "#fragment",
		valid + strings.Repeat("x", 4096),
	} {
		t.Run(raw[:min(len(raw), 100)], func(t *testing.T) {
			store := &reviewStoreStub{items: []PendingMatch{{SourceProvider: SourceKinepolis, SourceMovieID: "HO00016258", SourceDetailURL: raw, sourceShowingID: "1234", sourceTheaterID: "FRLIL"}}}
			items, err := NewReviewService(store, nil, nil).Pending(context.Background(), PendingMatchFilterUnresolved, "", 20, 0)
			if err != nil || items[0].SourceDetailURL != "" {
				t.Fatalf("unsafe URL retained: items=%+v err=%v", items, err)
			}
		})
	}
}
