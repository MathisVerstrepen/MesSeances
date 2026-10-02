package enrichment

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"messeances/api/internal/tmdb"
)

type metacriticLookupFunc func(context.Context, string) (string, error)

func (f metacriticLookupFunc) MetacriticID(ctx context.Context, qid string) (string, error) {
	return f(ctx, qid)
}

func TestMetacriticProvider(t *testing.T) {
	for _, tc := range []struct {
		name, qid, value string
		err              error
		checked          bool
	}{
		{"success", "Q42", "movie/a", nil, true}, {"negative", "Q42", "", nil, true}, {"failure", "Q42", "", errors.New("private"), false}, {"stop isolated", "Q42", "", tmdb.ErrStop, false}, {"lookup deadline isolated", "Q42", "", context.DeadlineExceeded, false}, {"invalid result", "Q42", "https://evil.example", nil, false}, {"invalid mapping", "Q0", "movie/a", nil, false}, {"no mapping", "", "movie/a", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"id":42,"imdb_id":"tt1234567","title":"Film","original_title":"Film","external_ids":{"imdb_id":"tt1234567","wikidata_id":"`+tc.qid+`"}}`)
			}))
			defer server.Close()
			raw, err := tmdb.NewClientWithConfig("synthetic", tmdb.Config{BaseURL: server.URL, RequestInterval: time.Nanosecond})
			if err != nil {
				t.Fatal(err)
			}
			provider := NewMetacriticProvider(raw, metacriticLookupFunc(func(_ context.Context, qid string) (string, error) {
				calls++
				if qid != tc.qid {
					t.Fatal(qid)
				}
				return tc.value, tc.err
			}))
			got, err := provider.Details(t.Context(), 42)
			wantCalls := 1
			if tc.qid == "" || tc.qid == "Q0" {
				wantCalls = 0
			}
			if err != nil || got.ID != 42 || got.MetacriticChecked != tc.checked || calls != wantCalls || tc.checked && got.MetacriticID != tc.value || !tc.checked && got.MetacriticID != "" {
				t.Fatalf("details=%+v err=%v calls=%d", got, err, calls)
			}
			metadata := metadataFromDetails(got, 0, matcherNow)
			if metadata.MetacriticID != got.MetacriticID || metadata.MetacriticChecked != got.MetacriticChecked {
				t.Fatal("observation lost")
			}
		})
	}
}

func TestMetacriticProviderParentCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":42,"title":"Film","original_title":"Film","external_ids":{"id":42,"wikidata_id":"Q42"}}`)
	}))
	defer server.Close()
	raw, _ := tmdb.NewClientWithConfig("synthetic", tmdb.Config{BaseURL: server.URL})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	provider := NewMetacriticProvider(raw, metacriticLookupFunc(func(context.Context, string) (string, error) { cancel(); return "", context.Canceled }))
	if _, err := provider.Details(ctx, 42); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMetacriticMetadataRefreshComparison(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		checked, updated    bool
	}{
		{"add", "", "movie/a", true, true}, {"remove", "movie/a", "", true, true}, {"replace", "movie/a", "movie/b", true, true}, {"same", "movie/a", "movie/a", true, false}, {"failure preserves", "movie/a", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			details := tmdb.Details{ID: 42, Title: "Film", OriginalTitle: "Film", Runtime: 90}
			cached := metadataFromDetails(details, 0, matcherNow)
			cached.MetacriticID = tc.before
			details.MetacriticID, details.MetacriticChecked = tc.after, tc.checked
			store := &metadataRefreshStore{ids: []int64{42}, metadata: map[int64]Metadata{42: cached}}
			provider := &metadataRefreshProvider{results: map[int64]metadataDetailsResult{42: {details: details}}}
			summary, err := NewMetadataRefreshService(store, provider, func() time.Time { return matcherNow }, nil).Refresh(t.Context())
			want := MetadataRefreshSummary{Processed: 1, Unchanged: 1}
			if tc.updated {
				want.Updated, want.Unchanged = 1, 0
			}
			if err != nil || summary != want || len(store.published) != 1 || store.published[0].MetacriticID != tc.after || store.published[0].MetacriticChecked != tc.checked {
				t.Fatalf("summary=%+v outgoing=%+v err=%v", summary, store.published, err)
			}
		})
	}
	invalid := metadataFromDetails(tmdb.Details{ID: 42, Title: "Film", OriginalTitle: "Film", MetacriticID: "movie/A"}, 0, matcherNow)
	if validateMetadata(invalid) == nil {
		t.Fatal("invalid metadata accepted")
	}
}
