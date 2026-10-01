package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"messeances/api/internal/schedule"
)

type ActivityReader interface {
	TheaterActivity(context.Context, schedule.TheaterActivityQuery) (schedule.TheaterActivity, error)
}

func (api *API) theaterActivity(w http.ResponseWriter, r *http.Request) {
	q, err := parseActivityQuery(chi.URLParam(r, "slug"), r.URL.RawQuery)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if api.activity == nil {
		writeHistoryError(w, schedule.ErrHistoryUnavailable)
		return
	}
	result, err := api.activity.TheaterActivity(r.Context(), q)
	if err != nil {
		writeHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

var activityLimit = regexp.MustCompile(`^[1-9][0-9]{0,2}$`)

func parseActivityQuery(slug, raw string) (schedule.TheaterActivityQuery, error) {
	q := schedule.TheaterActivityQuery{Slug: slug}
	if len(raw) > 2048 || !utf8.ValidString(raw) {
		return q, schedule.InvalidActivityQuery()
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return q, schedule.InvalidActivityQuery()
	}
	for key, entries := range values {
		if (key != "limit" && key != "cursor") || len(entries) != 1 || entries[0] == "" || !utf8.ValidString(entries[0]) {
			return q, schedule.InvalidActivityQuery()
		}
		if key == "limit" {
			if !activityLimit.MatchString(entries[0]) {
				return q, schedule.InvalidActivityQuery()
			}
			q.Limit, _ = strconv.Atoi(entries[0])
		} else {
			q.Cursor = entries[0]
		}
	}
	return schedule.NormalizeTheaterActivityQuery(q)
}
