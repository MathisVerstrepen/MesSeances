package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/schedule"
)

const (
	internalServiceTokenHeader = "X-Messeances-Internal-Token"
	// Trusted only next to a valid internal token: the web server sets it from
	// the address its reverse proxy reported for the visitor.
	internalVisitorAddressHeader = "X-Messeances-Client-IP"
)

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

// internalVisitorKey returns the canonical visitor address, or empty when the
// header is missing, repeated or not a plain IP address.
func internalVisitorKey(values []string) string {
	if len(values) != 1 {
		return ""
	}
	address, err := netip.ParseAddr(values[0])
	if err != nil || address.Zone() != "" {
		return ""
	}
	return address.Unmap().String()
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
	scopedQuery := schedule.MovieShowtimesQuery{
		Slug: chi.URLParam(r, "slug"), Date: query.Get("date"), City: query.Get("city"),
		Language: schedule.Language(query.Get("language")), Format: schedule.Format(query.Get("format")),
		Sort: schedule.MovieShowtimesSort(query.Get("sort")),
	}
	scoped, nationwide, err := api.schedule.MovieShowtimesBundle(scopedQuery)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, movieShowtimesBundle{Scoped: scoped, Nationwide: nationwide})
}
