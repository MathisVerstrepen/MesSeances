package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/enrichment"
)

const maxAdminMovieBody int64 = 256 << 10

func (a *adminAPI) adminMovies(w http.ResponseWriter, r *http.Request) {
	query, ok := parseAdminMovieQuery(r.URL.RawQuery)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_admin_movie_query", "Filtres de films invalides.")
		return
	}
	if a.movies == nil {
		writeError(w, http.StatusServiceUnavailable, "admin_unavailable", "Service administrateur indisponible.")
		return
	}
	result, err := a.movies.List(r.Context(), query)
	if err != nil {
		if errors.Is(err, enrichment.ErrAdminMovieInvalid) {
			writeError(w, http.StatusBadRequest, "invalid_admin_movie_query", "Filtres de films invalides.")
			return
		}
		writeError(w, http.StatusInternalServerError, "admin_movie_list_failed", "Impossible de charger les films.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *adminAPI) updateAdminMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := parseAdminMovieID(chi.URLParam(r, "id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_admin_movie_id", "Identifiant de film invalide.")
		return
	}
	patch, ok := decodeAdminMoviePatch(w, r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_admin_movie_update", "Modifications de film invalides.")
		return
	}
	if a.movies == nil {
		writeError(w, http.StatusServiceUnavailable, "admin_unavailable", "Service administrateur indisponible.")
		return
	}
	item, err := a.movies.Update(r.Context(), id, patch)
	if err != nil {
		switch {
		case errors.Is(err, enrichment.ErrAdminMovieInvalid):
			writeError(w, http.StatusBadRequest, "invalid_admin_movie_update", "Modifications de film invalides.")
		case errors.Is(err, enrichment.ErrAdminMovieNotFound):
			writeError(w, http.StatusNotFound, "admin_movie_not_found", "Film introuvable.")
		case errors.Is(err, enrichment.ErrAdminMovieConflict):
			writeError(w, http.StatusConflict, "admin_movie_conflict", "Ce film a changé. La liste a été actualisée.")
		default:
			writeError(w, http.StatusInternalServerError, "admin_movie_update_failed", "Impossible d’enregistrer le film.")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func parseAdminMovieQuery(rawQuery string) (enrichment.AdminMovieQuery, bool) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return enrichment.AdminMovieQuery{}, false
	}
	allowed := map[string]bool{
		"limit": true, "offset": true, "search": true, "runtime_min": true, "runtime_max": true,
		"release_date_from": true, "release_date_to": true, "genre": true, "override_status": true,
		"override_field": true, "sort": true, "direction": true,
	}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 || strings.TrimSpace(entries[0]) == "" {
			return enrichment.AdminMovieQuery{}, false
		}
	}
	query := enrichment.AdminMovieQuery{Limit: 50, OverrideStatus: "all", Sort: "title", Direction: "asc"}
	if raw, exists := oneQueryValue(values, "limit"); exists {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return enrichment.AdminMovieQuery{}, false
		}
		query.Limit = int(parsed)
	}
	if raw, exists := oneQueryValue(values, "offset"); exists {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return enrichment.AdminMovieQuery{}, false
		}
		query.Offset = int(parsed)
	}
	query.Search, _ = oneQueryValue(values, "search")
	query.Search = strings.TrimSpace(query.Search)
	query.Genre, _ = oneQueryValue(values, "genre")
	query.Genre = strings.TrimSpace(query.Genre)
	if raw, exists := oneQueryValue(values, "runtime_min"); exists {
		value, ok := parseAdminMovieRuntime(raw)
		if !ok {
			return enrichment.AdminMovieQuery{}, false
		}
		query.RuntimeMin = &value
	}
	if raw, exists := oneQueryValue(values, "runtime_max"); exists {
		value, ok := parseAdminMovieRuntime(raw)
		if !ok {
			return enrichment.AdminMovieQuery{}, false
		}
		query.RuntimeMax = &value
	}
	if raw, exists := oneQueryValue(values, "release_date_from"); exists {
		query.ReleaseDateFrom = &raw
	}
	if raw, exists := oneQueryValue(values, "release_date_to"); exists {
		query.ReleaseDateTo = &raw
	}
	if raw, exists := oneQueryValue(values, "override_status"); exists {
		query.OverrideStatus = raw
	}
	if raw, exists := oneQueryValue(values, "override_field"); exists {
		query.OverrideField = enrichment.AdminMovieField(raw)
	}
	if raw, exists := oneQueryValue(values, "sort"); exists {
		query.Sort = raw
	}
	if raw, exists := oneQueryValue(values, "direction"); exists {
		query.Direction = raw
	}
	return query, validParsedAdminMovieQuery(query)
}

func oneQueryValue(values url.Values, key string) (string, bool) {
	entries, exists := values[key]
	if !exists || len(entries) != 1 {
		return "", false
	}
	return entries[0], true
}

func parseAdminMovieRuntime(raw string) (int, bool) {
	value, err := strconv.ParseInt(raw, 10, 32)
	return int(value), err == nil && value >= 0 && value <= math.MaxInt32
}

func validParsedAdminMovieQuery(query enrichment.AdminMovieQuery) bool {
	if query.Limit < 1 || query.Limit > 100 || query.Offset < 0 || len([]rune(query.Search)) > 1024 || len([]rune(query.Genre)) > 256 {
		return false
	}
	if query.RuntimeMin != nil && query.RuntimeMax != nil && *query.RuntimeMin > *query.RuntimeMax {
		return false
	}
	if query.ReleaseDateFrom != nil && !validAdminMovieDate(*query.ReleaseDateFrom) || query.ReleaseDateTo != nil && !validAdminMovieDate(*query.ReleaseDateTo) {
		return false
	}
	if query.ReleaseDateFrom != nil && query.ReleaseDateTo != nil && *query.ReleaseDateFrom > *query.ReleaseDateTo {
		return false
	}
	if query.OverrideStatus != "all" && query.OverrideStatus != "overridden" && query.OverrideStatus != "automatic" {
		return false
	}
	if query.OverrideField != "" && !enrichment.ValidAdminMovieField(query.OverrideField) || query.OverrideStatus == "automatic" && query.OverrideField != "" {
		return false
	}
	if query.Sort != "title" && query.Sort != "runtime_minutes" && query.Sort != "release_date" && query.Sort != "updated_at" && query.Sort != "id" {
		return false
	}
	return query.Direction == "asc" || query.Direction == "desc"
}

func validAdminMovieDate(value string) bool {
	parsed, err := time.Parse(time.DateOnly, value)
	return err == nil && parsed.Format(time.DateOnly) == value
}

func parseAdminMovieID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == raw
}

func decodeAdminMoviePatch(w http.ResponseWriter, r *http.Request) (enrichment.AdminMoviePatch, bool) {
	var input struct {
		ExpectedUpdatedAt string          `json:"expected_updated_at"`
		Overrides         json.RawMessage `json:"overrides"`
		Restore           json.RawMessage `json:"restore"`
	}
	if err := decodeAdminJSONLimit(w, r, &input, maxAdminMovieBody); err != nil {
		return enrichment.AdminMoviePatch{}, false
	}
	expected, err := time.Parse(time.RFC3339Nano, input.ExpectedUpdatedAt)
	if err != nil || expected.IsZero() {
		return enrichment.AdminMoviePatch{}, false
	}
	patch := enrichment.AdminMoviePatch{ExpectedUpdatedAt: expected}
	if len(input.Restore) != 0 {
		var fields []string
		if string(input.Restore) == "null" || json.Unmarshal(input.Restore, &fields) != nil {
			return enrichment.AdminMoviePatch{}, false
		}
		patch.Restore = make([]enrichment.AdminMovieField, len(fields))
		for index, field := range fields {
			patch.Restore[index] = enrichment.AdminMovieField(field)
		}
	}
	if len(input.Overrides) != 0 {
		var overrides map[string]json.RawMessage
		if string(input.Overrides) == "null" || json.Unmarshal(input.Overrides, &overrides) != nil || overrides == nil {
			return enrichment.AdminMoviePatch{}, false
		}
		if !decodeAdminMovieOverrides(overrides, &patch.Overrides) {
			return enrichment.AdminMoviePatch{}, false
		}
	}
	return patch, true
}

func decodeAdminMovieOverrides(raw map[string]json.RawMessage, result *enrichment.AdminMovieOverrides) bool {
	for name, encoded := range raw {
		field := enrichment.AdminMovieField(name)
		if !enrichment.ValidAdminMovieField(field) {
			return false
		}
		switch field {
		case enrichment.AdminMovieFieldTitle:
			result.Title.Present = true
			if json.Unmarshal(encoded, &result.Title.Value) != nil {
				return false
			}
		case enrichment.AdminMovieFieldRuntimeMinutes:
			result.RuntimeMinutes.Present = true
			if json.Unmarshal(encoded, &result.RuntimeMinutes.Value) != nil {
				return false
			}
		case enrichment.AdminMovieFieldReleaseDate:
			result.ReleaseDate.Present = true
			if json.Unmarshal(encoded, &result.ReleaseDate.Value) != nil {
				return false
			}
		case enrichment.AdminMovieFieldGenres:
			result.Genres.Present = true
			if json.Unmarshal(encoded, &result.Genres.Value) != nil {
				return false
			}
		case enrichment.AdminMovieFieldOverview:
			result.Overview.Present = true
			if json.Unmarshal(encoded, &result.Overview.Value) != nil {
				return false
			}
		case enrichment.AdminMovieFieldPosterURL:
			result.PosterURL.Present = true
			if json.Unmarshal(encoded, &result.PosterURL.Value) != nil {
				return false
			}
		case enrichment.AdminMovieFieldBackdropURL:
			result.BackdropURL.Present = true
			if json.Unmarshal(encoded, &result.BackdropURL.Value) != nil {
				return false
			}
		case enrichment.AdminMovieFieldTrailerVFYouTubeKey:
			result.TrailerVFYouTubeKey.Present = true
			if json.Unmarshal(encoded, &result.TrailerVFYouTubeKey.Value) != nil {
				return false
			}
		case enrichment.AdminMovieFieldTrailerVOYouTubeKey:
			result.TrailerVOYouTubeKey.Present = true
			if json.Unmarshal(encoded, &result.TrailerVOYouTubeKey.Value) != nil {
				return false
			}
		}
	}
	return true
}
