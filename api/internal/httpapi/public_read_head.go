package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Paths that never answer HEAD like GET: they hold private or operator data.
var headExcludedPrefixes = []string{"/api/v1/admin", "/api/v1/account", "/api/v1/auth", "/api/v1/internal"}

// publicReadHead serves HEAD on public read routes with their GET handler.
// Some agent fetchers probe a URL with HEAD before reading it; net/http drops
// the body because the request method stays HEAD.
func publicReadHead(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && publicReadPath(r.URL.Path) {
			rctx := chi.RouteContext(r.Context())
			routePath := r.URL.Path
			if r.URL.RawPath != "" {
				routePath = r.URL.RawPath
			}
			if rctx != nil && !rctx.Routes.Match(chi.NewRouteContext(), http.MethodHead, routePath) && rctx.Routes.Match(chi.NewRouteContext(), http.MethodGet, routePath) {
				rctx.RouteMethod = http.MethodGet
				rctx.RoutePath = routePath
			}
		}
		next.ServeHTTP(w, r)
	})
}

func publicReadPath(path string) bool {
	if !strings.HasPrefix(path, "/api/v1/") {
		return false
	}
	for _, prefix := range headExcludedPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return false
		}
	}
	return true
}
