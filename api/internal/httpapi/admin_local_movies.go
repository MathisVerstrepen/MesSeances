package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/enrichment"
)

type localMovieGroupsResponse struct {
	Items  []enrichment.LocalMovieGroup `json:"items"`
	Limit  int                          `json:"limit"`
	Offset int                          `json:"offset"`
}

type mergeLocalMoviesRequest struct {
	Members []enrichment.LocalMovieSource `json:"members"`
	Primary enrichment.LocalMovieSource   `json:"primary"`
}

type unmergeLocalMovieResponse struct {
	Status       string `json:"status"`
	LocalMovieID string `json:"local_movie_id"`
}

func (a *adminAPI) localMovieGroups(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parseLocalMoviePagination(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query", "Pagination invalide.")
		return
	}
	if a.locals == nil {
		a.writeLocalMovieError(w, errors.New("local movie service unavailable"))
		return
	}
	items, err := a.locals.Groups(r.Context(), limit, offset)
	if err != nil {
		a.writeLocalMovieError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, localMovieGroupsResponse{Items: items, Limit: limit, Offset: offset})
}

func (a *adminAPI) mergeLocalMovies(w http.ResponseWriter, r *http.Request) {
	var input mergeLocalMoviesRequest
	if err := decodeAdminJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	if a.locals == nil {
		a.writeLocalMovieError(w, errors.New("local movie service unavailable"))
		return
	}
	group, err := a.locals.Merge(r.Context(), input.Members, input.Primary)
	if err != nil {
		a.writeLocalMovieError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, group)
}

func (a *adminAPI) unmergeLocalMovie(w http.ResponseWriter, r *http.Request) {
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	localMovieID := chi.URLParam(r, "localMovieID")
	if a.locals == nil {
		a.writeLocalMovieError(w, errors.New("local movie service unavailable"))
		return
	}
	if err := a.locals.Unmerge(r.Context(), localMovieID); err != nil {
		a.writeLocalMovieError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, unmergeLocalMovieResponse{Status: "unmerged", LocalMovieID: localMovieID})
}

func parseLocalMoviePagination(r *http.Request) (int, int, bool) {
	query := r.URL.Query()
	for key, values := range query {
		if (key != "limit" && key != "offset") || len(values) != 1 || values[0] == "" {
			return 0, 0, false
		}
	}
	limit, offset := 50, 0
	var err error
	if raw, exists := query["limit"]; exists {
		limit, err = strconv.Atoi(raw[0])
	}
	if err == nil {
		if raw, exists := query["offset"]; exists {
			offset, err = strconv.Atoi(raw[0])
		}
	}
	return limit, offset, err == nil && limit >= 1 && limit <= 100 && offset >= 0
}

func (a *adminAPI) writeLocalMovieError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, enrichment.ErrLocalMovieInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	case errors.Is(err, enrichment.ErrLocalMovieNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Groupe de films introuvable.")
	case errors.Is(err, enrichment.ErrLocalMovieConflict):
		writeError(w, http.StatusConflict, "local_movie_conflict", "Ce regroupement ne peut plus être modifié.")
	default:
		writeError(w, http.StatusBadGateway, "local_movie_failed", "Le regroupement de films n'a pas pu être modifié.")
	}
}
