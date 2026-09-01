package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/schedule"
)

const internalServiceTokenHeader = "X-Messeances-Internal-Token"

type internalServiceAuthenticator struct {
	configured bool
	expected   [sha256.Size]byte
}

type movieShowtimesBundle struct {
	Scoped     schedule.MovieSchedule `json:"scoped"`
	Nationwide schedule.MovieSchedule `json:"nationwide"`
}

func newInternalServiceAuthenticator(sharedSecret string) internalServiceAuthenticator {
	if !lowerHexString(sharedSecret, 64) {
		return internalServiceAuthenticator{}
	}
	return internalServiceAuthenticator{configured: true, expected: sha256.Sum256([]byte(sharedSecret))}
}

func (a internalServiceAuthenticator) authenticate(r *http.Request) bool {
	values := r.Header.Values(internalServiceTokenHeader)
	if !a.configured || len(values) != 1 || !lowerHexString(values[0], 64) {
		return false
	}
	presented := sha256.Sum256([]byte(values[0]))
	return subtle.ConstantTimeCompare(a.expected[:], presented[:]) == 1
}

func (api *API) requireInternalService(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := requestIdentityFromContext(r.Context())
		if !identity.internalServiceConfigured {
			w.Header().Set("Cache-Control", "no-store")
			writeError(w, http.StatusServiceUnavailable, "internal_service_unavailable", "Service interne indisponible.")
			return
		}
		if !identity.internalService {
			w.Header().Set("Cache-Control", "no-store")
			writeError(w, http.StatusUnauthorized, "unauthorized", "Non autorisé.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (api *API) movieShowtimesBundle(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	base := schedule.MovieShowtimesQuery{Slug: chi.URLParam(r, "slug"), Date: query.Get("date")}
	scopedQuery := base
	scopedQuery.City = query.Get("city")
	scoped, err := api.schedule.MovieShowtimes(scopedQuery)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	nationwide, err := api.schedule.MovieShowtimes(base)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, movieShowtimesBundle{Scoped: scoped, Nationwide: nationwide})
}
