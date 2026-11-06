package trailer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"messeances/api/internal/syncproxy"
)

var ErrUGCBlocked = errors.New("UGC trailer access blocked")

var positiveID = regexp.MustCompile(`^[1-9][0-9]{0,127}$`)

type UGCFetcher struct{ executor *syncproxy.Executor }

func NewUGCFetcher(proxies []syncproxy.Proxy, timeout time.Duration) (*UGCFetcher, error) {
	valid := func(u *url.URL) bool {
		return u != nil && positiveID.MatchString(u.Query().Get("filmId")) && u.String() == "https://www.ugc.fr/filmTrailerAjaxAction?filmId="+u.Query().Get("filmId")
	}
	clients, err := syncproxy.NewHTTPClients(proxies, timeout, func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse })
	if err != nil {
		return nil, fmt.Errorf("trailer transport unavailable")
	}
	executor, err := syncproxy.NewExecutor(syncproxy.ExecutorConfig{Clients: clients, ProxyBacked: true, ValidURL: valid, MaxResponseBytes: 1 << 20, Headers: http.Header{"User-Agent": {"MesSeances-schedule-sync/1.0"}, "Accept": {"application/json"}}, Retry: syncproxy.RetryPolicy{RequestScopedFailures: true}, CancelReadOnContext: true})
	if err != nil {
		return nil, fmt.Errorf("trailer transport unavailable")
	}
	return &UGCFetcher{executor: executor}, nil
}

func (f *UGCFetcher) Fetch(ctx context.Context, id string) (*Media, error) {
	if !positiveID.MatchString(id) {
		return nil, fmt.Errorf("invalid trailer identity")
	}
	body, failure := f.executor.Do(ctx, syncproxy.Request{Method: http.MethodGet, URL: "https://www.ugc.fr/filmTrailerAjaxAction?filmId=" + id, MediaTypes: []string{"application/json"}, NoRedirect: true}, syncproxy.ResponsePolicy{
		BeforeRead: func(status int) (*syncproxy.Failure, bool) {
			if status == 403 || status == 429 {
				return &syncproxy.Failure{Kind: syncproxy.FailureChallenge, StatusCode: status}, false
			}
			return nil, false
		},
		AfterRead: func(status int, body []byte) (*syncproxy.Failure, bool) {
			if syncproxy.IsChallenge(body) {
				return &syncproxy.Failure{Kind: syncproxy.FailureChallenge}, false
			}
			if status >= 500 {
				return &syncproxy.Failure{Kind: syncproxy.FailureServer, StatusCode: status}, true
			}
			if status != 200 {
				return &syncproxy.Failure{Kind: syncproxy.FailureStatus, StatusCode: status}, false
			}
			return nil, false
		},
	})
	if failure != nil {
		if failure.Kind == syncproxy.FailureChallenge {
			return nil, ErrUGCBlocked
		}
		return nil, fmt.Errorf("UGC trailer unavailable")
	}
	return ParseUGC(body, id)
}

func ParseUGC(body []byte, id string) (*Media, error) {
	var d struct {
		Videos []struct {
			FilmID json.RawMessage `json:"filmId"`
			Type   string          `json:"type"`
			Src    string          `json:"src"`
		} `json:"videos"`
	}
	if len(body) > 1<<20 || json.Unmarshal(body, &d) != nil || d.Videos == nil || !positiveID.MatchString(id) {
		return nil, errors.New("invalid UGC trailer response")
	}
	var selected *Media
	for _, v := range d.Videos {
		filmID := string(v.FilmID)
		if len(filmID) > 0 && filmID[0] == '"' {
			if json.Unmarshal(v.FilmID, &filmID) != nil {
				continue
			}
		}
		if v.Type != "FILM" || filmID != id {
			continue
		}
		m := MP4(v.Src, "ugc")
		if m == nil {
			return nil, errors.New("invalid UGC trailer reference")
		}
		if selected == nil {
			selected = m
		}
	}
	return selected, nil
}
