package httpapi

import (
	"context"
	"net/http"
	"time"

	"messeances/api/internal/schedule"
)

type SitemapObserver interface {
	ObserveSitemapData(context.Context) (schedule.SitemapData, error)
}

const sitemapReadTimeout = 10 * time.Second

func (api *API) sitemapData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "invalid_query", "Cette ressource n’accepte aucun paramètre.")
		return
	}
	if api.sitemap == nil {
		writeError(w, http.StatusServiceUnavailable, "sitemap_unavailable", "Les données du sitemap ne sont pas disponibles.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), sitemapReadTimeout)
	defer cancel()
	result, err := api.sitemap.ObserveSitemapData(ctx)
	if err != nil || ctx.Err() != nil {
		writeError(w, http.StatusServiceUnavailable, "sitemap_unavailable", "Les données du sitemap ne sont pas disponibles.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
