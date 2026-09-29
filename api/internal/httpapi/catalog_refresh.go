package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// Runs after internal authentication and expensive-read admission. Normal
// catalog/slug resolution retains its existing safe error response on failure.
func (api *API) refreshMissingMovie(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_ = api.schedule.RefreshMovie(ctx, chi.URLParam(r, "slug"))
		next.ServeHTTP(w, r)
	})
}
