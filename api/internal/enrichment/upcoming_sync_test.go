package enrichment

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/tmdb"
)

type upcomingTestStore struct {
	metadataRefreshStore
	publication UpcomingPublication
}

func (s *upcomingTestStore) ActiveUpcomingIDs(context.Context) ([]int64, error) {
	return s.ids, s.idsErr
}
func (s *upcomingTestStore) PublishUpcoming(ctx context.Context, p UpcomingPublication) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.publishCalls++
	if s.publishErr != nil {
		return s.publishErr
	}
	s.publication = p
	return nil
}

type upcomingTestProvider struct {
	metadataRefreshProvider
	pages      []tmdb.DiscoverPage
	pageErr    int
	pageCalls  []int
	dates      map[int64]string
	dateErrors map[int64]error
	verified   []int64
	discover   func(context.Context) error
}

func (p *upcomingTestProvider) DiscoverMovies(ctx context.Context, _, _ string, page int) (tmdb.DiscoverPage, error) {
	if p.discover != nil {
		if err := p.discover(ctx); err != nil {
			return tmdb.DiscoverPage{}, err
		}
	}
	p.pageCalls = append(p.pageCalls, page)
	if page == p.pageErr {
		return tmdb.DiscoverPage{}, errors.New("private page failure")
	}
	if len(p.pages) == 0 {
		return tmdb.DiscoverPage{Page: 1, IDs: []int64{}}, nil
	}
	return p.pages[page-1], nil
}
func (p *upcomingTestProvider) FrenchTheatricalReleaseDate(_ context.Context, id int64) (string, error) {
	p.verified = append(p.verified, id)
	return p.dates[id], p.dateErrors[id]
}

func upcomingFixture() (*upcomingTestStore, *upcomingTestProvider, time.Time) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	store := &upcomingTestStore{metadataRefreshStore: metadataRefreshStore{ids: []int64{99}, metadata: map[int64]Metadata{99: metadataFromDetails(tmdb.Details{ID: 99, Title: "Cached", OriginalTitle: "Cached"}, 0, now.Add(-time.Hour))}}}
	provider := &upcomingTestProvider{dates: map[int64]string{1: "2026-09-14", 2: "2027-09-13", 3: "2026-09-13", 4: "2027-09-14", 5: "1990-01-01", 99: "2026-11-01"}, dateErrors: map[int64]error{}, metadataRefreshProvider: metadataRefreshProvider{results: map[int64]metadataDetailsResult{1: {details: tmdb.Details{ID: 1, Title: "Tomorrow", OriginalTitle: "Tomorrow", ReleaseDate: "2000-01-01"}}, 2: {details: tmdb.Details{ID: 2, Title: "Anniversary", OriginalTitle: "Anniversary"}}}}}
	ids := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	provider.pages = []tmdb.DiscoverPage{{Page: 1, TotalPages: 2, TotalResults: 22, IDs: ids}, {Page: 2, TotalPages: 2, TotalResults: 22, IDs: []int64{1, 21}}}
	return store, provider, now
}

func TestUpcomingCompletePublicationAndCache(t *testing.T) {
	store, provider, now := upcomingFixture()
	if err := NewUpcomingService(store, provider, func() time.Time { return now }, nil).sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.publishCalls != 1 || !reflect.DeepEqual(provider.pageCalls, []int{1, 2}) || len(provider.verified) != 22 || provider.verified[21] != 99 || !reflect.DeepEqual(provider.calls, []int64{1, 2}) {
		t.Fatalf("pages=%v verified=%v details=%v writes=%d", provider.pageCalls, provider.verified, provider.calls, store.publishCalls)
	}
	p := store.publication
	if p.Window != (schedule.Window{From: "2026-09-14", Through: "2027-09-13"}) || !p.CompletedAt.Equal(now) || len(p.Metadata) != 2 || p.Metadata[0].ReleaseDate != "2000-01-01" || !p.Metadata[0].RefreshAfter.Equal(now.Add(metadataTTL)) {
		t.Fatalf("publication=%+v", p)
	}
	var active []int64
	for _, release := range p.Releases {
		if release.Active {
			active = append(active, release.TMDBID)
		}
	}
	if !reflect.DeepEqual(active, []int64{1, 2, 99}) {
		t.Fatalf("active=%v", active)
	}
}

func TestUpcomingFailuresNeverPublishPartialResults(t *testing.T) {
	for _, name := range []string{"middle page", "inconsistent pages", "over limit", "incomplete page", "invalid ID", "release failure", "malformed date", "details failure", "invalid details", "cache read", "canceled"} {
		t.Run(name, func(t *testing.T) {
			store, p, now := upcomingFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "middle page":
				p.pageErr = 2
			case "inconsistent pages":
				p.pages[1].TotalResults = 21
			case "over limit":
				p.pages[0].TotalPages = 501
			case "incomplete page":
				p.pages[0].IDs = p.pages[0].IDs[:19]
			case "invalid ID":
				p.pages[0].IDs[0] = 0
			case "release failure":
				p.dateErrors[2] = tmdb.ErrStop
			case "malformed date":
				p.dates[2] = "2027-02-29"
			case "details failure":
				p.results[2] = metadataDetailsResult{err: errors.New("private details")}
			case "invalid details":
				p.results[2] = metadataDetailsResult{details: tmdb.Details{ID: 3}}
			case "cache read":
				store.readErr = errors.New("private database")
			case "canceled":
				cancel()
			}
			if err := NewUpcomingService(store, p, func() time.Time { return now }, nil).sync(ctx); err == nil || store.publishCalls != 0 {
				t.Fatalf("err=%v publications=%d", err, store.publishCalls)
			}
		})
	}
}

func TestUpcomingEmptyWithdrawalAndExpiredCache(t *testing.T) {
	for _, name := range []string{"omitted eligible", "empty evidence", "release 404", "details 404", "expired metadata"} {
		t.Run(name, func(t *testing.T) {
			store, p, now := upcomingFixture()
			p.pages = nil
			wantActive := true
			switch name {
			case "empty evidence":
				p.dates[99] = ""
				wantActive = false
			case "release 404":
				p.dateErrors[99] = tmdb.ErrNotFound
				wantActive = false
			case "details 404":
				store.metadata = nil
				p.results[99] = metadataDetailsResult{err: tmdb.ErrNotFound}
				wantActive = false
			case "expired metadata":
				m := store.metadata[99]
				m.RefreshAfter = now
				store.metadata[99] = m
				p.results[99] = metadataDetailsResult{details: tmdb.Details{ID: 99, Title: "Fresh", OriginalTitle: "Fresh"}}
			}
			if err := NewUpcomingService(store, p, func() time.Time { return now }, nil).sync(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(store.publication.Releases) != 1 || store.publication.Releases[0].Active != wantActive {
				t.Fatalf("release=%+v", store.publication.Releases)
			}
			if name == "expired metadata" && len(store.publication.Metadata) != 1 {
				t.Fatal("expired cache not refreshed")
			}
		})
	}
	store := &upcomingTestStore{}
	if err := NewUpcomingService(store, &upcomingTestProvider{}, nil, nil).sync(context.Background()); err != nil || store.publishCalls != 1 || store.publication.CompletedAt.IsZero() {
		t.Fatalf("empty publication=%+v err=%v", store.publication, err)
	}
}
