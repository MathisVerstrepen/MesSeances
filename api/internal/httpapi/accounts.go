package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/accounts"
)

// AccountOptions is deliberately separate from admin and internal authorization.
type AccountOptions struct {
	Enabled bool
	Service *accounts.Service
	Origin  string
}

func accountPath(path string) bool {
	return path == "/api/v1/auth" || strings.HasPrefix(path, "/api/v1/auth/") || path == "/api/v1/account" || strings.HasPrefix(path, "/api/v1/account/")
}

// Wrap the whole router so cache/error and CORS policy also cover unknown paths,
// wrong methods and preflight requests, not just successful account handlers.
func accountBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accountPath(r.URL.Path) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			w.Header().Add("Vary", "Cookie")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		}
		next.ServeHTTP(w, r)
	})
}

func accountAwareCORS(corsMiddleware func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		withCORS := corsMiddleware(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if accountPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			withCORS.ServeHTTP(w, r)
		})
	}
}

func registerAccountRoutes(router chi.Router, options AccountOptions) {
	unavailable := func(w http.ResponseWriter, _ *http.Request) {
		code := "accounts_disabled"
		if options.Enabled {
			code = "accounts_unavailable"
		}
		writeError(w, http.StatusServiceUnavailable, code, "Les comptes sont indisponibles.")
	}
	if options.Enabled && options.Service != nil {
		registerAccountLifecycle(router, options, unavailable)
		return
	}
	router.Get("/api/v1/auth/session", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			writeError(w, http.StatusBadRequest, "invalid_input", "Requête invalide.")
			return
		}
		if options.Enabled {
			unavailable(w, r)
			return
		}
		writeJSON(w, http.StatusOK, accounts.SessionView{Enabled: false, State: accounts.StateAnonymous, Account: nil})
	})
	for _, path := range []string{
		"/auth/register", "/auth/login", "/auth/verification/request", "/auth/verification/confirm",
		"/auth/password/reset/request", "/auth/password/reset/confirm", "/auth/logout", "/auth/logout-all", "/auth/google/start",
		"/account/username", "/account/reauth/password", "/account/reauth/email/request", "/account/reauth/email/confirm",
		"/account/password", "/account/email/request", "/account/email/confirm", "/account/email/cancel", "/account/google/unlink",
	} {
		router.Post("/api/v1"+path, unavailable)
	}
	router.Get("/api/v1/auth/google/callback", unavailable)
	router.Get("/api/v1/account", unavailable)
	router.Get("/api/v1/account/theaters", unavailable)
	router.Post("/api/v1/account/theaters", unavailable)
	router.Get("/api/v1/account/reauth/continuation", unavailable)
	router.Delete("/api/v1/account", unavailable)
	router.Post("/api/v1/account/avatar", unavailable)
	router.Delete("/api/v1/account/avatar", unavailable)
	router.Get("/api/v1/account/avatar/{revision}", unavailable)
}
