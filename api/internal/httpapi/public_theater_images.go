package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/cinemaimage"
)

func noStorePublicTheaterImages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (api *API) publicTheaterImage(w http.ResponseWriter, r *http.Request) {
	id, ok := cinemaPath(r)
	revision, valid := cinemaRevision(chi.URLParam(r, "revision"))
	if !ok || !valid || revision == 0 || api.publicTheaterImages == nil {
		writePublicTheaterImageError(w, cinemaimage.ErrImageNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	b, err := api.publicTheaterImages.Read(ctx, id, revision)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		writePublicTheaterImageError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Content-Disposition", `inline; filename="cinema.webp"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func writePublicTheaterImageError(w http.ResponseWriter, err error) {
	if errors.Is(err, cinemaimage.ErrNotFound) || errors.Is(err, cinemaimage.ErrImageNotFound) || errors.Is(err, cinemaimage.ErrRequest) {
		writeError(w, http.StatusNotFound, "cinema_image_not_found", "Image introuvable.")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "cinema_images_unavailable", "Images des cinémas indisponibles.")
}
