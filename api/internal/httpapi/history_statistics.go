package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"messeances/api/internal/schedule"
)

type HistoryReader interface {
	HistoryStatistics(context.Context, schedule.StatisticsQuery) (schedule.HistoryStatistics, error)
	HistoryOptions(context.Context, schedule.HistoryOptionsQuery) (schedule.HistoryOptions, error)
}

func (api *API) historyStatistics(w http.ResponseWriter, r *http.Request) {
	query, err := parseHistoryStatisticsQuery(r.URL.RawQuery)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if api.history == nil {
		writeHistoryError(w, schedule.ErrHistoryUnavailable)
		return
	}
	result, err := api.history.HistoryStatistics(r.Context(), query)
	if err != nil {
		writeHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) historyOptions(w http.ResponseWriter, r *http.Request) {
	query, err := parseHistoryOptionsQuery(r.URL.RawQuery)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if api.history == nil {
		writeHistoryError(w, schedule.ErrHistoryUnavailable)
		return
	}
	result, err := api.history.HistoryOptions(r.Context(), query)
	if err != nil {
		writeHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeHistoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, schedule.ErrHistoryUnavailable):
		writeError(w, http.StatusServiceUnavailable, "history_unavailable", "L’historique est indisponible. Réessayez plus tard.")
	case errors.Is(err, schedule.ErrHistoryBusy):
		writeError(w, http.StatusServiceUnavailable, "history_busy", "L’historique est occupé. Réessayez dans quelques instants.")
	case errors.Is(err, schedule.ErrHistoryQueryTimeout), errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusServiceUnavailable, "history_query_timeout", "La recherche historique a pris trop de temps. Réessayez ou réduisez la période.")
	default:
		writeServiceError(w, err)
	}
}

func parseHistoryStatisticsQuery(raw string) (schedule.StatisticsQuery, error) {
	if !utf8.ValidString(raw) {
		return schedule.StatisticsQuery{}, &schedule.ValidationError{Message: "Les filtres statistiques sont invalides."}
	}
	query, err := parseStatisticsQuery(raw)
	if err != nil {
		return query, err
	}
	return schedule.NormalizeHistoryQuery(query)
}

func parseHistoryOptionsQuery(raw string) (schedule.HistoryOptionsQuery, error) {
	var query schedule.HistoryOptionsQuery
	invalid := func() (schedule.HistoryOptionsQuery, error) {
		return query, &schedule.ValidationError{Message: "Les options statistiques sont invalides."}
	}
	if len(raw) > 4096 || !utf8.ValidString(raw) {
		return invalid()
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return invalid()
	}
	for key, entries := range values {
		if key != "kind" && key != "q" && key != "selected" || key != "selected" && len(entries) != 1 || key == "selected" && len(entries) > 50 {
			return invalid()
		}
		for _, value := range entries {
			if len(value) > 200 || !utf8.ValidString(value) || strings.TrimSpace(value) == "" {
				return invalid()
			}
			switch key {
			case "kind":
				query.Kind = strings.TrimSpace(value)
			case "q":
				query.Q = value
			case "selected":
				query.Selected = append(query.Selected, value)
			}
		}
	}
	return schedule.NormalizeHistoryOptionsQuery(query)
}

func noStoreHistory(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
