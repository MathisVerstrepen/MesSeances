package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/cinemaimage"
	"messeances/api/internal/enrichment"
)

type theaterImageFake struct {
	calls         int
	err           error
	revision      int64
	bytes         []byte
	media         string
	limit, offset int
}

func (f *theaterImageFake) List(_ context.Context, limit, offset int) (cinemaimage.Inventory, error) {
	f.calls++
	f.limit, f.offset = limit, offset
	return cinemaimage.Inventory{Items: []cinemaimage.Theater{}, Limit: limit, Offset: offset}, f.err
}
func (f *theaterImageFake) Upload(_ context.Context, _ cinemaimage.Identity, read func() (int64, []byte, string, error)) (cinemaimage.Result, error) {
	f.calls++
	if f.err != nil {
		return cinemaimage.Result{}, f.err
	}
	revision, b, media, e := read()
	f.revision, f.bytes, f.media = revision, b, media
	return cinemaimage.Result{ImageRevision: revision + 1}, e
}
func (f *theaterImageFake) Import(_ context.Context, _ cinemaimage.Identity, revision int64, _ string) (cinemaimage.Result, error) {
	f.calls++
	f.revision = revision
	return cinemaimage.Result{ImageRevision: revision + 1}, f.err
}
func (f *theaterImageFake) Remove(_ context.Context, _ cinemaimage.Identity, revision int64) (cinemaimage.Result, error) {
	f.calls++
	f.revision = revision
	return cinemaimage.Result{ImageRevision: revision + 1}, f.err
}
func (f *theaterImageFake) Read(_ context.Context, _ cinemaimage.Identity, revision int64) ([]byte, error) {
	f.calls++
	f.revision = revision
	return []byte("RIFF-fixture-WEBP"), f.err
}
func cinemaAdminHandler(t *testing.T, f TheaterImageController) http.Handler {
	t.Helper()
	return testHandlerWithAdmin(t, AdminOptions{Password: "password", SessionSecret: "test-secret", Reviews: enrichment.NewReviewService(adminReviewStore{}, adminProvider{}, time.Now), TheaterImages: f})
}

const cinemaTarget = "/api/v1/admin/theaters/ugc/25/image"

type observedBody struct {
	io.Reader
	reads int
}

func (b *observedBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func TestAdminTheaterImagesAuthOriginBeforeAnyIO(t *testing.T) {
	f := &theaterImageFake{}
	handler := cinemaAdminHandler(t, f)
	cookie := loginAdmin(t, handler, "password")
	for _, tc := range []struct{ method, path string }{{http.MethodPost, cinemaTarget}, {http.MethodPost, cinemaTarget + "/import"}, {http.MethodDelete, cinemaTarget}} {
		for _, origin := range []string{"", "https://evil.example", "http://localhost:3000"} {
			body := &observedBody{Reader: strings.NewReader("not parsed")}
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, body)
			req.Header.Set("Origin", origin)
			req.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != 401 || body.reads != 0 || f.calls != 0 {
				t.Fatal("unauthorized performed IO", w.Code, body.reads, f.calls)
			}
			if origin == "http://localhost:3000" {
				continue
			}
			req = httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, body)
			req.AddCookie(cookie)
			req.Header.Set("Origin", origin)
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != 403 || body.reads != 0 || f.calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("wrong Origin performed IO", w.Code, body.reads, f.calls)
			}
		}
	}
	for _, path := range []string{"/api/v1/admin/theaters", cinemaTarget + "/1"} {
		if response := adminRequest(handler, http.MethodGet, path, "", "", nil); response.Code != 401 {
			t.Fatal(response.Code)
		}
	}
}
func TestAdminTheaterImagesPaginationAndAvailability(t *testing.T) {
	f := &theaterImageFake{}
	h := cinemaAdminHandler(t, f)
	cookie := loginAdmin(t, h, "password")
	w := adminRequest(h, http.MethodGet, "/api/v1/admin/theaters", "", "", cookie)
	if w.Code != 200 || f.limit != 20 || f.offset != 0 || strings.TrimSpace(w.Body.String()) != `{"items":[],"limit":20,"offset":0,"total":0,"imports_enabled":false}` {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?offset=-1", "?offset=+1", "?limit=1&limit=2", "?offset=", "?unknown=1", "?offset=1;limit=2", "?offset=%zz"} {
		if w := adminRequest(h, http.MethodGet, "/api/v1/admin/theaters"+query, "", "", cookie); w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	h = cinemaAdminHandler(t, nil)
	cookie = loginAdmin(t, h, "password")
	if w := adminRequest(h, http.MethodGet, "/api/v1/admin/theaters", "", "", cookie); w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func cinemaMultipart(t *testing.T, fields []string, imageSize int) ([]byte, string) {
	t.Helper()
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	for _, field := range fields {
		switch field {
		case "image":
			h := make(textproto.MIMEHeader)
			h.Set("Content-Disposition", `form-data; name="image"; filename="../../unsafe.png"`)
			h.Set("Content-Type", "image/png")
			p, e := writer.CreatePart(h)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = p.Write(make([]byte, imageSize)); e != nil {
				t.Fatal(e)
			}
		case "expected_revision":
			if writer.WriteField(field, "0") != nil {
				t.Fatal("field")
			}
		default:
			if writer.WriteField(field, "unexpected") != nil {
				t.Fatal("field")
			}
		}
	}
	if writer.Close() != nil {
		t.Fatal("multipart close")
	}
	return b.Bytes(), writer.FormDataContentType()
}
func TestAdminTheaterImagesMultipartStrictAndBounded(t *testing.T) {
	f := &theaterImageFake{}
	h := cinemaAdminHandler(t, f)
	cookie := loginAdmin(t, h, "password")
	for _, tc := range []struct {
		name         string
		fields       []string
		size, status int
	}{
		{"revision_first", []string{"expected_revision", "image"}, 10, 200}, {"image_first", []string{"image", "expected_revision"}, 10, 200},
		{"duplicate_image", []string{"image", "expected_revision", "image"}, 10, 400}, {"duplicate_revision", []string{"expected_revision", "image", "expected_revision"}, 10, 400},
		{"extra", []string{"expected_revision", "image", "extra"}, 10, 400}, {"missing_revision", []string{"image"}, 10, 400}, {"missing_image", []string{"expected_revision"}, 10, 400},
		{"too_large", []string{"expected_revision", "image"}, cinemaimage.MaxInput + 1, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, media := cinemaMultipart(t, tc.fields, tc.size)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, cinemaTarget, bytes.NewReader(b))
			req.AddCookie(cookie)
			req.Header.Set("Origin", "http://localhost:3000")
			req.Header.Set("Content-Type", media)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	f.err = cinemaimage.ErrBusy
	body := &observedBody{Reader: strings.NewReader("unread")}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, cinemaTarget, body)
	req.AddCookie(cookie)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" || body.reads != 0 {
		t.Fatal("busy body IO", w.Code, body.reads)
	}
}
func TestAdminTheaterImagesStrictJSONAndRevisionReads(t *testing.T) {
	f := &theaterImageFake{}
	h := cinemaAdminHandler(t, f)
	cookie := loginAdmin(t, h, "password")
	for _, body := range []string{`{}`, `{"expected_revision":null}`, `{"expected_revision":-1}`, `{"expected_revision":9007199254740992}`, `{"expected_revision":1.1}`, `{"expected_revision":"0"}`, `{"expected_revision":0,"extra":1}`, `{"expected_revision":0,"expected_revision":0}`, `{"Expected_Revision":0}`, `{"expected_revision":0} {}`, `null`, strings.Repeat("x", 4097)} {
		w := adminRequest(h, http.MethodDelete, cinemaTarget, body, "http://localhost:3000", cookie)
		if w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
	for _, body := range []string{`{"expected_revision":0}`, `{"expected_revision":0,"url":null}`, `{"expected_revision":0,"url":1}`, `{"expected_revision":0,"url":"x","extra":0}`} {
		w := adminRequest(h, http.MethodPost, cinemaTarget+"/import", body, "http://localhost:3000", cookie)
		if w.Code != 400 {
			t.Fatal(body, w.Code)
		}
	}
	for _, revision := range []string{"0", "01", "-1", "9007199254740992", "x"} {
		w := adminRequest(h, http.MethodGet, cinemaTarget+"/"+revision, "", "", cookie)
		if w.Code != 404 {
			t.Fatal(revision, w.Code)
		}
	}
	w := adminRequest(h, http.MethodGet, cinemaTarget+"/1", "", "", cookie)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/webp" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Disposition") != `inline; filename="cinema.webp"` || w.Header().Get("Content-Length") == "" || w.Header().Get("ETag") != "" {
		t.Fatal(w.Code, w.Header())
	}
	if w := adminRequest(h, http.MethodGet, "/cinema-images/unsafe.webp", "", "", cookie); w.Code != 404 {
		t.Fatal("public media mount exists", w.Code)
	}
}

func TestAdminTheaterImagesEnvelopeFraming(t *testing.T) {
	f := &theaterImageFake{}
	h := cinemaAdminHandler(t, f)
	cookie := loginAdmin(t, h, "password")
	valid, media := cinemaMultipart(t, []string{"expected_revision", "image"}, 10)
	for _, tc := range []struct {
		name   string
		body   []byte
		media  string
		size   int64
		status int
	}{
		{"epilogue", append(append([]byte(nil), valid...), []byte("hidden trailing body")...), media, -1, 400},
		{"truncated", valid[:len(valid)-10], media, -1, 400},
		{"preamble", append([]byte("unexpected\r\n"), valid...), media, -1, 400},
		{"mixed_type", valid, strings.Replace(media, "form-data", "mixed", 1), -1, 400},
		{"oversize_unknown_length", append(append([]byte(nil), valid...), make([]byte, cinemaimage.MaxBody)...), media, -1, 413},
		{"oversize_declared", valid, media, cinemaimage.MaxBody + 1, 413},
		{"noncanonical_revision", bytes.Replace(valid, []byte("\r\n0\r\n"), []byte("\r\n01\r\n"), 1), media, -1, 400},
		{"field_cap", bytes.Replace(valid, []byte("\r\n0\r\n"), []byte("\r\n00000000000000000\r\n"), 1), media, -1, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, cinemaTarget, bytes.NewReader(tc.body))
			req.ContentLength = tc.size
			req.AddCookie(cookie)
			req.Header.Set("Origin", "http://localhost:3000")
			req.Header.Set("Content-Type", tc.media)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
func TestAdminTheaterImagesErrorMappings(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{cinemaimage.ErrURL, 400, "invalid_image_url"}, {cinemaimage.ErrNotFound, 404, "theater_not_found"}, {cinemaimage.ErrImageNotFound, 404, "cinema_image_not_found"}, {cinemaimage.ErrConflict, 409, "cinema_image_conflict"},
		{cinemaimage.ErrTooLarge, 413, "cinema_image_too_large"}, {cinemaimage.ErrUnsupported, 415, "cinema_image_unsupported"}, {cinemaimage.ErrInvalid, 422, "cinema_image_invalid"}, {cinemaimage.ErrDownload, 502, "cinema_image_download_failed"},
		{cinemaimage.ErrBusy, 503, "cinema_image_busy"}, {cinemaimage.ErrStorage, 503, "cinema_images_unavailable"}, {cinemaimage.ErrImportUnavailable, 503, "cinema_image_import_unavailable"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			f := &theaterImageFake{err: tc.err}
			h := cinemaAdminHandler(t, f)
			cookie := loginAdmin(t, h, "password")
			w := adminRequest(h, http.MethodPost, cinemaTarget+"/import", `{"expected_revision":0,"url":"https://private-secret.example/photo"}`, "http://localhost:3000", cookie)
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				}
			}
			if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || w.Code != tc.status || envelope.Error.Code != tc.code || strings.Contains(w.Body.String(), "private-secret") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
