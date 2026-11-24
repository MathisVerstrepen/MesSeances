package httpapi

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"regexp"

	"messeances/api/internal/accounts"
	"messeances/api/internal/schedule"
)

const maxPreviewTheaters = 4096
const maxPreviewBody = maxPreviewTheaters*(128+3) + 2

var previewTheaterID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// A read-only POST keeps cinema preferences out of URLs and supports the full
// saved selection without depending on proxy URL length limits.
func previewTheaters(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPreviewBody))
	var ids []string
	if err := decoder.Decode(&ids); err != nil || ids == nil || len(ids) > maxPreviewTheaters {
		return nil, false
	}
	var extra json.RawMessage
	if decoder.Decode(&extra) != io.EOF {
		return nil, false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !previewTheaterID.MatchString(id) || seen[id] {
			return nil, false
		}
		seen[id] = true
	}
	return ids, true
}

type watchlistScreeningsResponse struct {
	Username string                   `json:"username"`
	Items    []schedule.NextScreening `json:"items"`
}

func (h *accountHTTP) watchlistNextScreenings(w http.ResponseWriter, r *http.Request) {
	theaters, ok := previewTheaters(w, r)
	if !ok {
		watchlistHTTPError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	username, slugs, err := h.service.WatchlistScreeningMembership(r.Context(), raw)
	if err != nil {
		watchlistHTTPError(w, err)
		return
	}
	items, err := h.schedule.NextScreenings(slugs, theaters)
	if err != nil {
		watchlistHTTPError(w, accounts.ErrWatchlistUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, watchlistScreeningsResponse{Username: username, Items: items})
}

func (api *API) theaterActivityPreview(w http.ResponseWriter, r *http.Request) {
	theaters, ok := previewTheaters(w, r)
	if !ok {
		writeServiceError(w, schedule.InvalidActivityQuery())
		return
	}
	if api.activity == nil {
		writeHistoryError(w, schedule.ErrHistoryUnavailable)
		return
	}
	items, err := api.activity.ActivityPreview(r.Context(), theaters)
	if err != nil {
		writeHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
