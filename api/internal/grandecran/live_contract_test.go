package grandecran

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

const liveProxyFile = "/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt"

func liveClient(t *testing.T) *Client {
	t.Helper()
	path, ok := os.LookupEnv("GRANDECRAN_LIVE_PROXY_FILE")
	if !ok {
		t.Skip("set GRANDECRAN_LIVE_PROXY_FILE for opt-in proxy contract")
	}
	if path != liveProxyFile {
		t.Fatal("live contract requires approved proxy path")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("approved proxy file unavailable")
	}
	defer func() { _ = f.Close() }()
	p, err := syncproxy.Parse(f)
	if err != nil {
		t.Fatal("invalid proxy configuration")
	}
	c, err := NewClient(ClientConfig{Proxies: p, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func liveOptions(t *testing.T) SyncOptions {
	t.Helper()
	l, err := time.LoadLocation(schedule.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	from := now.In(l)
	if from.Hour() < 3 {
		from = from.AddDate(0, 0, -1)
	}
	return SyncOptions{From: from.Format(time.DateOnly), Now: now}
}
func TestProxyScheduleContractIntegration(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	d, s, err := syncSnapshot(ctx, diagnosticGetter{c, t}, liveOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	// Eight distinct missing-room URLs maximum, no database publication.
	if err := enrichRooms(ctx, c, d.Showtimes, &s, time.Minute, 8); err != nil {
		t.Fatal(err)
	}
	events := map[string]bool{}
	missingRuntime, missingPoster := 0, 0
	for _, r := range d.Showtimes {
		if strings.HasPrefix(r.Movie.ProviderID, "c") {
			events[r.Movie.ProviderID] = true
		}
		if r.Movie.RuntimeMinutes == 0 {
			missingRuntime++
		}
		if r.Movie.PosterURL == "" {
			missingPoster++
		}
	}
	t.Logf("cinemas=%d movies=%d showtimes=%d requests=%d through=%s event_movies=%d unknown_runtime_showtimes=%d missing_poster_showtimes=%d room_attempted=%d room_recovered=%d room_unresolved=%d room_budget_skipped=%d", s.Cinemas, s.Movies, s.Showtimes, c.RequestCount(), d.Window.Through, len(events), missingRuntime, missingPoster, s.RoomsAttempted, s.RoomsRecovered, s.RoomsUnresolved, s.RoomsBudgetSkipped)
}

type diagnosticGetter struct {
	*Client
	t *testing.T
}

func (g diagnosticGetter) Get(ctx context.Context, op Operation, raw string) ([]byte, error) {
	body, err := g.Client.Get(ctx, op, raw)
	if err == nil && op == OperationMovies {
		var movies []movieResponse
		if json.Unmarshal(body, &movies) == nil {
			for _, m := range movies {
				if m.Poster != "" && !schedule.ValidGrandEcranPosterURL(m.Poster) {
					u, parseErr := url.Parse(m.Poster)
					if parseErr != nil {
						g.t.Log("poster drift: malformed URL")
						continue
					}
					g.t.Logf("poster drift: https=%t acsta_host=%t userinfo=%t port=%t query=%t fragment=%t escapes=%t whitespace=%t path_traversal=%t", u.Scheme == "https", u.Hostname() == "acsta.net" || strings.HasSuffix(u.Hostname(), ".acsta.net"), u.User != nil, u.Port() != "", u.RawQuery != "", u.Fragment != "", strings.Contains(m.Poster, "%"), strings.ContainsAny(m.Poster, " \t\r\n"), strings.Contains(u.Path, ".."))
				}
			}
		}
	}
	return body, err
}
func TestProxyFullSyncContractIntegration(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Minute)
	defer cancel()
	d, s, err := Sync(ctx, c, liveOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cinemas=%d movies=%d showtimes=%d requests=%d through=%s room_attempted=%d room_recovered=%d room_unresolved=%d room_budget_skipped=%d", s.Cinemas, s.Movies, s.Showtimes, s.Requests, d.Window.Through, s.RoomsAttempted, s.RoomsRecovered, s.RoomsUnresolved, s.RoomsBudgetSkipped)
}
