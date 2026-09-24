package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"messeances/api/internal/accountavatar"
	"messeances/api/internal/accounts"
)

func (h *accountHTTP) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || len(r.Header.Values("Content-Type")) != 1 || media != "multipart/form-data" || len(params) != 1 || params["boundary"] == "" {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	result, err := h.service.UploadAvatar(r.Context(), raw, func() ([]byte, string, error) {
		controller := http.NewResponseController(w)
		if err := controller.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return nil, "", accounts.ErrUnavailable
		}
		// Keep the deadline through net/http's final request-body drain. The server
		// resets it for the next request; clearing it here could unbound that drain.
		r.Body = http.MaxBytesReader(w, r.Body, accountavatar.MaxBody)
		reader, err := r.MultipartReader()
		if err != nil {
			return nil, "", accounts.ErrInvalidInput
		}
		part, err := reader.NextRawPart()
		if err != nil {
			return nil, "", multipartError(err)
		}
		defer func() { _ = part.Close() }() // Request owns the bounded body and read deadline.
		disposition, p, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		_, file := p["filename"]
		if err != nil || disposition != "form-data" || p["name"] != "avatar" || !file || len(p) != 2 || len(part.Header.Values("Content-Disposition")) != 1 || len(part.Header.Values("Content-Type")) != 1 || part.Header.Get("Content-Transfer-Encoding") != "" {
			return nil, "", accounts.ErrInvalidInput
		}
		b, err := accountavatar.ReadInput(part)
		if err != nil {
			return nil, "", multipartError(err)
		}
		contentType := part.Header.Get("Content-Type")
		if _, err = reader.NextRawPart(); !errors.Is(err, io.EOF) {
			if err == nil {
				err = accounts.ErrInvalidInput
			}
			return nil, "", multipartError(err)
		}
		// Drain the bounded envelope, including any unread epilogue. A lying or absent
		// Content-Length cannot bypass MaxBytesReader.
		if _, err = io.Copy(io.Discard, r.Body); err != nil {
			return nil, "", multipartError(err)
		}
		return b, contentType, nil
	})
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func multipartError(err error) error {
	var large *http.MaxBytesError
	if errors.As(err, &large) || errors.Is(err, accountavatar.ErrTooLarge) {
		return accountavatar.ErrTooLarge
	}
	return accounts.ErrInvalidInput
}
func (h *accountHTTP) removeAvatar(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	result, err := h.service.RemoveAvatar(r.Context(), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *accountHTTP) avatar(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "revision")
	revision, err := strconv.ParseInt(version, 10, 64)
	if err != nil || revision <= 0 || strconv.FormatInt(revision, 10) != version {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	b, err := h.service.Avatar(r.Context(), raw, revision)
	if err != nil {
		accountError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Content-Disposition", `inline; filename="avatar.webp"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
