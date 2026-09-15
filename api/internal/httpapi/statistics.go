package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"messeances/api/internal/schedule"
)

func (api *API) statistics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query, err := parseStatisticsQuery(r.URL.RawQuery)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	result, err := api.schedule.Statistics(r.Context(), query)
	if errors.Is(err, schedule.ErrNoCompleteSnapshot) {
		writeError(w, http.StatusServiceUnavailable, "schedule_unavailable", "Les horaires ne sont pas encore disponibles.")
		return
	}
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseStatisticsQuery(raw string) (schedule.StatisticsQuery, error) {
	var query schedule.StatisticsQuery
	invalid := func() (schedule.StatisticsQuery, error) {
		return query, &schedule.ValidationError{Message: "Les filtres statistiques sont invalides."}
	}
	if len(raw) > 4096 {
		return invalid()
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return invalid()
	}
	owned := map[string]*string{"date": &query.Date, "date_to": &query.DateTo, "chain": &query.Chain, "language": &query.Language, "format": &query.Format, "genre": &query.Genre, "pass": &query.Pass}
	selections := map[string]*[]string{"city": &query.City, "theater": &query.Theater}
	for key, entries := range values {
		if target, ok := selections[key]; ok {
			if len(entries) > 50 {
				return invalid()
			}
			seen := make(map[string]bool, len(entries))
			for _, value := range entries {
				if len(value) > 200 {
					return invalid()
				}
				value = strings.TrimSpace(value)
				if value == "" {
					return invalid()
				}
				if !seen[value] {
					*target = append(*target, value)
					seen[value] = true
				}
			}
			continue
		}
		target, ok := owned[key]
		if !ok || len(entries) != 1 {
			return invalid()
		}
		value := entries[0]
		if key != "date" && key != "date_to" {
			if len(value) > 200 {
				return invalid()
			}
			value = strings.TrimSpace(value)
		}
		if value == "" {
			return invalid()
		}
		*target = value
	}
	return query, nil
}
