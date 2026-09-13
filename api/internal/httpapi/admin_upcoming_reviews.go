package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"messeances/api/internal/enrichment"
)

func (a *adminAPI) upcomingReviews(w http.ResponseWriter, r *http.Request) {
	q, ok := parseUpcomingReviewQuery(r.URL.RawQuery)
	if !ok {
		writeError(w, 400, "invalid_upcoming_review_query", "Filtres de revue invalides.")
		return
	}
	if a.upcomingReview == nil {
		writeError(w, 503, "admin_unavailable", "Service administrateur indisponible.")
		return
	}
	result, err := a.upcomingReview.List(r.Context(), q)
	if err != nil {
		writeError(w, 500, "upcoming_review_list_failed", "Impossible de charger les sorties à venir.")
		return
	}
	writeJSON(w, 200, result)
}

func parseUpcomingReviewQuery(raw string) (enrichment.UpcomingReviewQuery, bool) {
	q := enrichment.UpcomingReviewQuery{Filter: "needs_review", Limit: 50}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return q, false
	}
	for key, entries := range values {
		if len(entries) != 1 || strings.TrimSpace(entries[0]) == "" {
			return q, false
		}
		switch key {
		case "filter":
			q.Filter = entries[0]
		case "search":
			q.Search = strings.TrimSpace(entries[0])
		case "limit", "offset":
			if strings.IndexFunc(entries[0], func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				return q, false
			}
			n, err := strconv.ParseInt(entries[0], 10, 32)
			if err != nil {
				return q, false
			}
			if key == "limit" {
				q.Limit = int(n)
			} else {
				q.Offset = int(n)
			}
		default:
			return q, false
		}
	}
	return q, enrichment.ValidUpcomingReviewQuery(q)
}

func parseUpcomingReviewID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0 && id <= enrichment.MaxReviewInteger && strconv.FormatInt(id, 10) == raw
}

func decodeUpcomingDecision(w http.ResponseWriter, r *http.Request) (enrichment.UpcomingDecisionUpdate, bool) {
	var result enrichment.UpcomingDecisionUpdate
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return result, false
	}
	var raw json.RawMessage
	if decodeAdminJSON(w, r, &raw) != nil || !utf8.Valid(raw) {
		return result, false
	}
	// Exactly two unique, case-sensitive fields. Reject duplicate keys rather than last-value wins.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return result, false
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return result, false
		}
		key, ok := token.(string)
		if !ok || seen[key] || (key != "decision" && key != "expected_revision") {
			return result, false
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || missingOrNull(value) {
			return result, false
		}
		if key == "decision" {
			if json.Unmarshal(value, &result.Decision) != nil {
				return result, false
			}
		} else if json.Unmarshal(value, &result.ExpectedRevision) != nil {
			return result, false
		}
	}
	return result, len(seen) == 2 && enrichment.ValidUpcomingDecision(1, result)
}

func (a *adminAPI) setUpcomingDecision(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUpcomingReviewID(chi.URLParam(r, "tmdbID"))
	if !ok {
		writeError(w, 400, "invalid_upcoming_review_id", "Identifiant TMDB invalide.")
		return
	}
	input, ok := decodeUpcomingDecision(w, r)
	if !ok {
		writeError(w, 400, "invalid_upcoming_review_update", "Décision de revue invalide.")
		return
	}
	if a.upcomingReview == nil {
		writeError(w, 503, "admin_unavailable", "Service administrateur indisponible.")
		return
	}
	item, err := a.upcomingReview.Update(r.Context(), id, input)
	if err != nil {
		switch {
		case errors.Is(err, enrichment.ErrUpcomingReviewNotFound):
			writeError(w, 404, "upcoming_review_not_found", "Sortie à venir introuvable.")
		case errors.Is(err, enrichment.ErrUpcomingReviewConflict):
			writeError(w, 409, "upcoming_review_conflict", "Cette évaluation a changé. La liste a été actualisée.")
		case errors.Is(err, enrichment.ErrUpcomingReviewInvalid):
			writeError(w, 400, "invalid_upcoming_review_update", "Décision de revue invalide.")
		default:
			writeError(w, 500, "upcoming_review_update_failed", "Impossible d’enregistrer la décision.")
		}
		return
	}
	writeJSON(w, 200, item)
}
