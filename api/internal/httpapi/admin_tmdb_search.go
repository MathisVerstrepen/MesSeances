package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/tmdb"
)

const maxAdminTMDBQueryRunes = 200

type adminTMDBSearchItem struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitempty"`
	ReleaseDate   string `json:"release_date,omitempty"`
	PosterURL     string `json:"poster_url,omitempty"`
	DetailURL     string `json:"detail_url"`
}

type adminTMDBMovie struct {
	ID               int64    `json:"id"`
	Title            string   `json:"title"`
	OriginalTitle    string   `json:"original_title,omitempty"`
	OriginalLanguage string   `json:"original_language,omitempty"`
	ReleaseDate      string   `json:"release_date,omitempty"`
	RuntimeMinutes   int      `json:"runtime_minutes,omitempty"`
	Overview         string   `json:"overview,omitempty"`
	Genres           []string `json:"genres"`
	IMDBID           string   `json:"imdb_id,omitempty"`
	Adult            *bool    `json:"adult,omitempty"`
	PosterURL        string   `json:"poster_url,omitempty"`
	DetailURL        string   `json:"detail_url"`
}

// searchTMDB proxies a French TMDB title search through the server token so
// admin clients can find a replacement ID without holding TMDB credentials.
func (a *adminAPI) searchTMDB(w http.ResponseWriter, r *http.Request) {
	values, exists := r.URL.Query()["query"]
	if !exists || len(values) != 1 {
		writeError(w, http.StatusBadRequest, "invalid_query", "Recherche invalide.")
		return
	}
	query := strings.TrimSpace(values[0])
	if query == "" || !utf8.ValidString(query) || utf8.RuneCountInString(query) > maxAdminTMDBQueryRunes {
		writeError(w, http.StatusBadRequest, "invalid_query", "Recherche invalide.")
		return
	}
	if a.tmdbSearch == nil {
		writeError(w, http.StatusServiceUnavailable, "tmdb_unavailable", "Recherche TMDB indisponible.")
		return
	}
	candidates, err := a.tmdbSearch.Search(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusBadGateway, "tmdb_failed", "La recherche TMDB a échoué.")
		return
	}
	items := make([]adminTMDBSearchItem, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, adminTMDBSearchItem{ID: candidate.ID, Title: candidate.Title, OriginalTitle: candidate.OriginalTitle, ReleaseDate: candidate.ReleaseDate, PosterURL: candidate.PosterURL, DetailURL: adminTMDBDetailURL(candidate.ID)})
	}
	writeJSON(w, http.StatusOK, struct {
		Items []adminTMDBSearchItem `json:"items"`
	}{Items: items})
}

func (a *adminAPI) tmdbMovieDetails(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "tmdbID")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		writeError(w, http.StatusBadRequest, "invalid_request", "Identifiant TMDB invalide.")
		return
	}
	if a.tmdbSearch == nil {
		writeError(w, http.StatusServiceUnavailable, "tmdb_unavailable", "Recherche TMDB indisponible.")
		return
	}
	details, err := a.tmdbSearch.Details(r.Context(), id)
	if errors.Is(err, tmdb.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Film TMDB introuvable.")
		return
	}
	if err != nil || details.ID != id {
		writeError(w, http.StatusBadGateway, "tmdb_failed", "La lecture TMDB a échoué.")
		return
	}
	genres := details.Genres
	if genres == nil {
		genres = []string{}
	}
	writeJSON(w, http.StatusOK, adminTMDBMovie{
		ID: details.ID, Title: details.Title, OriginalTitle: details.OriginalTitle, OriginalLanguage: details.OriginalLanguage,
		ReleaseDate: details.ReleaseDate, RuntimeMinutes: details.Runtime, Overview: details.Overview, Genres: genres,
		IMDBID: details.IMDBID, Adult: details.Adult, PosterURL: details.PosterURL, DetailURL: adminTMDBDetailURL(details.ID),
	})
}

func adminTMDBDetailURL(id int64) string {
	return "https://www.themoviedb.org/movie/" + strconv.FormatInt(id, 10) + "?language=fr-FR"
}
