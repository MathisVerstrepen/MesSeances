package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accountavatar"
	"messeances/api/internal/accounts"
)

func avatarMultipart(t *testing.T, b []byte, extra bool) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="avatar"; filename="../../private.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(b); err != nil {
		t.Fatal(err)
	}
	if extra {
		if err = writer.WriteField("account_id", "another"); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), writer.FormDataContentType()
}

type avatarDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *avatarDeadlineWriter) SetReadDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}
func TestAvatarReadDeadlineIntegration(t *testing.T) {
	p := newBrowserProbe(t)
	p.google(map[string]string{"mode": "login"}, "verified", "/finaliser")
	p.request("POST", "/api/v1/account/username", accountUsername{Username: "deadline_photo"}, 200, nil)
	b, media := avatarMultipart(t, browserPicturePNG(), false)
	r := httptest.NewRequestWithContext(t.Context(), "POST", p.h.apiURL+"/api/v1/account/avatar", bytes.NewReader(b))
	r.Header.Set("Content-Type", media)
	r.Header.Set("Origin", p.h.origin)
	r.Header.Set("X-Messeances-CSRF", "1")
	u, err := url.Parse(p.h.apiURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range p.client.Jar.Cookies(u) {
		r.AddCookie(cookie)
	}
	w := &avatarDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	before := time.Now()
	p.h.handler.ServeHTTP(w, r)
	if w.Code != 200 || w.deadline.Before(before.Add(9*time.Second)) || w.deadline.After(time.Now().Add(10*time.Second)) {
		t.Fatal("bounded deadline not propagated through middleware", w.Code, w.Body.String())
	}
}
func TestAvatarErrorEnvelope(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{{accountavatar.ErrTooLarge, 413, "avatar_too_large"}, {accountavatar.ErrInvalid, 422, "avatar_invalid"}, {accountavatar.ErrUnsupported, 415, "avatar_unsupported"}, {accountavatar.ErrBusy, 503, "avatar_busy"}, {accounts.ErrAvatarChanged, 409, "avatar_changed"}, {accounts.ErrAvatarNotFound, 404, "avatar_not_found"}} {
		w := httptest.NewRecorder()
		accountError(w, test.err)
		if w.Code != test.status || !strings.Contains(w.Body.String(), `"code":"`+test.code+`"`) {
			t.Fatal(w.Code, w.Body.String())
		}
		if errors.Is(test.err, accountavatar.ErrBusy) && w.Header().Get("Retry-After") != "1" {
			t.Fatal("retry-after")
		}
	}
}
func TestAvatarRegisteredRoutesIntegration(t *testing.T) {
	p := newBrowserProbe(t)
	p.google(map[string]string{"mode": "login"}, "verified", "/finaliser")
	request := func(method, path string, b []byte, media string, alter func(*http.Request), want int) ([]byte, http.Header) {
		t.Helper()
		r, err := http.NewRequestWithContext(t.Context(), method, p.h.apiURL+path, bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", p.h.origin)
		r.Header.Set("X-Messeances-CSRF", "1")
		r.Header.Set("Content-Type", media)
		if alter != nil {
			alter(r)
		}
		response, err := p.client.Do(r)
		if err != nil {
			t.Fatal("local HTTP failed")
		}
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
		}()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.StatusCode, want, data)
		}
		h := response.Header
		if h.Get("Cache-Control") != "no-store" || !strings.Contains(h.Get("Vary"), "Cookie") || h.Get("Cross-Origin-Resource-Policy") != "same-origin" || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("private headers", h)
		}
		return data, h
	}
	b, media := avatarMultipart(t, browserPicturePNG(), false)
	request("POST", "/api/v1/account/avatar", b, media, nil, 403)
	p.request("POST", "/api/v1/account/username", accountUsername{Username: "avatar_http"}, 200, nil)
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Origin") }, func(r *http.Request) { r.Header.Add("Origin", p.h.origin) }, func(r *http.Request) { r.Header.Del("X-Messeances-CSRF") }, func(r *http.Request) { r.Header.Add("X-Messeances-CSRF", "1") }, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }} {
		request("POST", "/api/v1/account/avatar", b, media, mutate, 403)
	}
	request("POST", "/api/v1/account/username", b, media, nil, 400)
	request("POST", "/api/v1/account/avatar?x=1", b, media, nil, 400)
	data, _ := request("POST", "/api/v1/account/avatar", b, media, nil, 200)
	var result accounts.AvatarResult
	if err := json.Unmarshal(data, &result); err != nil || result.AvatarURL == nil {
		t.Fatal("upload DTO")
	}
	data, h := request("GET", *result.AvatarURL, nil, "", func(r *http.Request) { r.Header.Set("Range", "bytes=0-9"); r.Header.Set("If-None-Match", "*") }, 200)
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) || h.Get("Content-Length") != strconv.Itoa(len(data)) || h.Get("Content-Disposition") != `inline; filename="avatar.png"` || h.Get("Content-Type") != "image/png" || h.Get("ETag") != "" || h.Get("Accept-Ranges") != "" {
		t.Fatal("binary contract")
	}
	for _, version := range []string{"0", "-1", "01", "+1", "9223372036854775808"} {
		request("GET", "/api/v1/account/avatar/"+version, nil, "", nil, 400)
	}
	request("GET", *result.AvatarURL+"?x=1", nil, "", nil, 400)
	extra, typ := avatarMultipart(t, browserPicturePNG(), true)
	request("POST", "/api/v1/account/avatar", extra, typ, nil, 400)
	big, typ := avatarMultipart(t, make([]byte, accountavatar.MaxInput+1), false)
	request("POST", "/api/v1/account/avatar", big, typ, func(r *http.Request) { r.ContentLength = -1 }, 413)
	request("DELETE", "/api/v1/account/avatar", []byte(`{"extra":"field"}`), "application/json", nil, 400)
	data, _ = request("DELETE", "/api/v1/account/avatar", []byte(`{}`), "application/json", nil, 200)
	if string(data) != "{\"avatar_url\":null}\n" {
		t.Fatal("remove DTO", string(data))
	}
	request("GET", *result.AvatarURL, nil, "", nil, 404)
	p.request("POST", "/api/v1/auth/logout", struct{}{}, 204, nil)
	request("GET", *result.AvatarURL, nil, "", nil, 401)
	// Synthetic Google picture is fill-empty only: explicit removal remains empty.
	p.google(map[string]string{"mode": "login"}, "verified", "/compte")
	var details accounts.AccountDetails
	p.request("GET", "/api/v1/account", nil, 200, &details)
	if details.AvatarURL != nil {
		t.Fatal("removed photo refilled")
	}
}
