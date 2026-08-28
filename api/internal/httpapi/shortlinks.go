package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/shortlink"
)

const maxShortlinkBody = 4096

func (api *API) noStoreShortlink(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (api *API) requireShortlinkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != api.origin {
			writeError(w, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (api *API) createShortlink(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Target string `json:"target"`
	}
	if r.Header.Get("Content-Type") != "application/json" || decodeShortlinkJSON(w, r, &input) != nil {
		writeInvalidShortlinkTarget(w)
		return
	}
	if api.shortlinks == nil {
		writeShortlinkUnavailable(w)
		return
	}
	link, err := api.shortlinks.Create(r.Context(), input.Target)
	if err != nil {
		writeShortlinkServiceError(w, err)
		return
	}
	writeShortlinkJSON(w, http.StatusCreated, link)
}

func (api *API) resolveShortlink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if api.shortlinks == nil {
		writeShortlinkUnavailable(w)
		return
	}
	link, err := api.shortlinks.Resolve(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		writeShortlinkServiceError(w, err)
		return
	}
	writeShortlinkJSON(w, http.StatusOK, link)
}

func decodeShortlinkJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxShortlinkBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

func writeShortlinkServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, shortlink.ErrInvalidTarget):
		writeInvalidShortlinkTarget(w)
	case errors.Is(err, shortlink.ErrNotFound), errors.Is(err, shortlink.ErrInvalidCode):
		writeError(w, http.StatusNotFound, "not_found", "Ce lien de partage est introuvable.")
	case errors.Is(err, shortlink.ErrUnavailable), errors.Is(err, shortlink.ErrCollision):
		writeShortlinkUnavailable(w)
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "Une erreur interne est survenue.")
	}
}

func writeInvalidShortlinkTarget(w http.ResponseWriter) {
	writeError(w, http.StatusBadRequest, "invalid_request", "La cible de partage est invalide.")
}

func writeShortlinkUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "shortlink_unavailable", "Le service de partage est temporairement indisponible.")
}

func writeShortlinkJSON(w http.ResponseWriter, status int, link shortlink.Link) {
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(link)
}
