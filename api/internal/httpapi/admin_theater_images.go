package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"messeances/api/internal/cinemaimage"
)

func (a *adminAPI) adminTheaters(w http.ResponseWriter, r *http.Request) {
	query, ok := cinemaListQuery(r)
	if !ok {
		a.writeCinemaImageError(w, cinemaimage.ErrRequest)
		return
	}
	if a.theaterImages == nil {
		a.writeCinemaImageError(w, cinemaimage.ErrStorage)
		return
	}
	list, err := a.theaterImages.List(r.Context(), query)
	if err != nil {
		a.writeCinemaImageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
func cinemaListQuery(r *http.Request) (cinemaimage.ListQuery, bool) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return cinemaimage.ListQuery{}, false
	}
	query := cinemaimage.ListQuery{Limit: 20}
	for k, vs := range q {
		if len(vs) != 1 {
			return cinemaimage.ListQuery{}, false
		}
		switch k {
		case "limit", "offset":
			if !decimalCinema(vs[0]) {
				return cinemaimage.ListQuery{}, false
			}
			n, e := strconv.Atoi(vs[0])
			if e != nil {
				return cinemaimage.ListQuery{}, false
			}
			if k == "limit" {
				query.Limit = n
			} else {
				query.Offset = n
			}
		case "q", "provider":
			if !utf8.ValidString(vs[0]) || strings.ContainsFunc(vs[0], unicode.IsControl) {
				return cinemaimage.ListQuery{}, false
			}
			if k == "q" {
				query.Search = strings.TrimSpace(vs[0])
			} else {
				query.Provider = strings.TrimSpace(vs[0])
			}
		default:
			return cinemaimage.ListQuery{}, false
		}
	}
	return query, query.Valid()
}
func decimalCinema(raw string) bool {
	if len(raw) == 0 {
		return false
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func cinemaRevision(raw string) (int64, bool) {
	if len(raw) > 16 || !decimalCinema(raw) || len(raw) > 1 && raw[0] == '0' {
		return 0, false
	}
	n, e := strconv.ParseInt(raw, 10, 64)
	return n, e == nil && n <= cinemaimage.MaxRevision
}
func cinemaPath(r *http.Request) (cinemaimage.Identity, bool) {
	p, id, ok := theaterLocationPath(r)
	return cinemaimage.Identity{Provider: p, ProviderTheaterID: id}, ok
}
func (a *adminAPI) uploadTheaterImage(w http.ResponseWriter, r *http.Request) {
	id, ok := cinemaPath(r)
	if !ok {
		a.writeCinemaImageError(w, cinemaimage.ErrRequest)
		return
	}
	if a.theaterImages == nil {
		a.writeCinemaImageError(w, cinemaimage.ErrStorage)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	out, err := a.theaterImages.Upload(ctx, id, func() (int64, []byte, string, error) { return readCinemaMultipart(w, r) })
	if err != nil {
		a.writeCinemaImageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func readCinemaMultipart(w http.ResponseWriter, r *http.Request) (int64, []byte, string, error) {
	controller := http.NewResponseController(w)
	if err := controller.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, nil, "", cinemaimage.ErrRequest
	}
	defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	boundary := params["boundary"]
	if err != nil || media != "multipart/form-data" || len(boundary) == 0 || len(boundary) > 70 {
		return 0, nil, "", cinemaimage.ErrRequest
	}
	if r.ContentLength > cinemaimage.MaxBody {
		return 0, nil, "", cinemaimage.ErrTooLarge
	}
	r.Body = http.MaxBytesReader(w, r.Body, cinemaimage.MaxBody)
	// Buffer the bounded envelope, not just each part: multipart.Reader otherwise
	// ignores arbitrary epilogues, including bytes beyond the overall body cap.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return 0, nil, "", multipartCinemaError(err)
	}
	closing := []byte("\r\n--" + boundary + "--")
	if !bytes.HasPrefix(body, []byte("--"+boundary+"\r\n")) || bytes.Count(body, closing) != 1 || !bytes.HasSuffix(body, closing) && !bytes.HasSuffix(body, append(append([]byte(nil), closing...), '\r', '\n')) {
		return 0, nil, "", cinemaimage.ErrRequest
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	reader, err := r.MultipartReader()
	if err != nil {
		return 0, nil, "", cinemaimage.ErrRequest
	}
	var expected int64
	var b []byte
	var contentType string
	seenRevision, seenImage := false, false
	for {
		part, err := reader.NextRawPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, nil, "", multipartCinemaError(err)
		}
		disposition, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if err != nil || disposition != "form-data" || len(part.Header.Values("Content-Disposition")) != 1 || len(part.Header.Values("Content-Transfer-Encoding")) != 0 {
			return 0, nil, "", cinemaimage.ErrRequest
		}
		_, hasFilename := params["filename"]
		switch params["name"] {
		case "expected_revision":
			if seenRevision || hasFilename {
				return 0, nil, "", cinemaimage.ErrRequest
			}
			seenRevision = true
			v, e := io.ReadAll(io.LimitReader(part, 17))
			if e != nil {
				return 0, nil, "", multipartCinemaError(e)
			}
			var ok bool
			expected, ok = cinemaRevision(string(v))
			if !ok {
				return 0, nil, "", cinemaimage.ErrRequest
			}
		case "image":
			if seenImage || !hasFilename || len(part.Header.Values("Content-Type")) != 1 {
				return 0, nil, "", cinemaimage.ErrRequest
			}
			seenImage = true
			contentType = part.Header.Get("Content-Type")
			b, err = cinemaimage.ReadInput(part)
			if err != nil {
				return 0, nil, "", multipartCinemaError(err)
			}
		default:
			return 0, nil, "", cinemaimage.ErrRequest
		}
		if part.Close() != nil {
			return 0, nil, "", cinemaimage.ErrRequest
		}
	}
	if !seenRevision || !seenImage {
		return 0, nil, "", cinemaimage.ErrRequest
	}
	return expected, b, contentType, nil
}
func multipartCinemaError(err error) error {
	var large *http.MaxBytesError
	if errors.Is(err, cinemaimage.ErrTooLarge) || errors.As(err, &large) {
		return cinemaimage.ErrTooLarge
	}
	return cinemaimage.ErrRequest
}

// Exact case-sensitive fields, no duplicate-key or null last-value ambiguity.
func readCinemaJSON(w http.ResponseWriter, r *http.Request, importing bool) (int64, string, bool) {
	var raw json.RawMessage
	if decodeAdminJSON(w, r, &raw) != nil || !utf8.Valid(raw) {
		return 0, "", false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return 0, "", false
	}
	seen := map[string]bool{}
	var expected int64
	var source string
	for d.More() {
		token, e := d.Token()
		if e != nil {
			return 0, "", false
		}
		key, ok := token.(string)
		if !ok || seen[key] || key != "expected_revision" && (!importing || key != "url") {
			return 0, "", false
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || missingOrNull(value) {
			return 0, "", false
		}
		if key == "expected_revision" {
			if json.Unmarshal(value, &expected) != nil || expected < 0 || expected > cinemaimage.MaxRevision {
				return 0, "", false
			}
		} else if json.Unmarshal(value, &source) != nil {
			return 0, "", false
		}
	}
	want := 1
	if importing {
		want = 2
	}
	return expected, source, len(seen) == want
}
func (a *adminAPI) importTheaterImage(w http.ResponseWriter, r *http.Request) {
	id, ok := cinemaPath(r)
	if !ok {
		a.writeCinemaImageError(w, cinemaimage.ErrRequest)
		return
	}
	expected, source, valid := readCinemaJSON(w, r, true)
	if !valid {
		a.writeCinemaImageError(w, cinemaimage.ErrRequest)
		return
	}
	if a.theaterImages == nil {
		a.writeCinemaImageError(w, cinemaimage.ErrStorage)
		return
	}
	out, err := a.theaterImages.Import(r.Context(), id, expected, source)
	if err != nil {
		a.writeCinemaImageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (a *adminAPI) removeTheaterImage(w http.ResponseWriter, r *http.Request) {
	id, ok := cinemaPath(r)
	if !ok {
		a.writeCinemaImageError(w, cinemaimage.ErrRequest)
		return
	}
	expected, _, valid := readCinemaJSON(w, r, false)
	if !valid {
		a.writeCinemaImageError(w, cinemaimage.ErrRequest)
		return
	}
	if a.theaterImages == nil {
		a.writeCinemaImageError(w, cinemaimage.ErrStorage)
		return
	}
	out, err := a.theaterImages.Remove(r.Context(), id, expected)
	if err != nil {
		a.writeCinemaImageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (a *adminAPI) theaterImage(w http.ResponseWriter, r *http.Request) {
	id, ok := cinemaPath(r)
	revision, valid := cinemaRevision(chi.URLParam(r, "revision"))
	if !ok || !valid || revision == 0 {
		a.writeCinemaImageError(w, cinemaimage.ErrImageNotFound)
		return
	}
	if a.theaterImages == nil {
		a.writeCinemaImageError(w, cinemaimage.ErrStorage)
		return
	}
	b, err := a.theaterImages.Read(r.Context(), id, revision)
	if err != nil {
		a.writeCinemaImageError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Content-Disposition", `inline; filename="cinema.webp"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
func (a *adminAPI) writeCinemaImageError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusServiceUnavailable, "cinema_images_unavailable", "Images des cinémas indisponibles."
	switch {
	case errors.Is(err, cinemaimage.ErrRequest):
		status, code, message = 400, "invalid_request", "Requête invalide."
	case errors.Is(err, cinemaimage.ErrURL):
		status, code, message = 400, "invalid_image_url", "Lien d’image invalide."
	case errors.Is(err, cinemaimage.ErrNotFound):
		status, code, message = 404, "theater_not_found", "Cinéma introuvable."
	case errors.Is(err, cinemaimage.ErrImageNotFound):
		status, code, message = 404, "cinema_image_not_found", "Image introuvable."
	case errors.Is(err, cinemaimage.ErrConflict):
		status, code, message = 409, "cinema_image_conflict", "Cette image a changé. Actualisez le cinéma."
	case errors.Is(err, cinemaimage.ErrTooLarge):
		status, code, message = 413, "cinema_image_too_large", "Image trop volumineuse."
	case errors.Is(err, cinemaimage.ErrUnsupported):
		status, code, message = 415, "cinema_image_unsupported", "Format d’image non pris en charge."
	case errors.Is(err, cinemaimage.ErrInvalid):
		status, code, message = 422, "cinema_image_invalid", "Image invalide."
	case errors.Is(err, cinemaimage.ErrDownload):
		status, code, message = 502, "cinema_image_download_failed", "Téléchargement de l’image impossible."
	case errors.Is(err, cinemaimage.ErrBusy):
		status, code, message = 503, "cinema_image_busy", "Traitement d’image en cours. Réessayez."
		w.Header().Set("Retry-After", "1")
	case errors.Is(err, cinemaimage.ErrImportUnavailable):
		status, code, message = 503, "cinema_image_import_unavailable", "Import par lien indisponible. Utilisez un fichier."
	}
	writeError(w, status, code, message)
}
