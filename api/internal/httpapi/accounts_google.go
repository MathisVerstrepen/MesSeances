package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"messeances/api/internal/accounts"
)

func (h *accountHTTP) googleCookieName() string {
	if h.secure {
		return "__Host-messeances_google"
	}
	return "messeances_google_dev"
}
func (h *accountHTTP) setGoogleCookie(w http.ResponseWriter, c accounts.Cookie) {
	maxAge := int(time.Until(c.ExpiresAt).Seconds())
	if c.Token == "" {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{Name: h.googleCookieName(), Value: c.Token, Path: "/", HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode, Expires: c.ExpiresAt, MaxAge: maxAge})
}
func (h *accountHTTP) googleStart(w http.ResponseWriter, r *http.Request) {
	var input accounts.GoogleStart
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	result, err := h.service.StartGoogle(r.Context(), raw, input)
	if err != nil {
		accountError(w, err)
		return
	}
	h.setGoogleCookie(w, result.Browser)
	writeJSON(w, 200, struct {
		URL string `json:"authorization_url"`
	}{result.AuthorizationURL})
}

// Only bounded provider callback parameters are accepted. Provider details never
// enter response bodies or redirects. No arbitrary return URL is supported.
func googleCallbackQuery(r *http.Request) (state, code string, ok bool) {
	if len(r.URL.RawQuery) > 8192 {
		return "", "", false
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", "", false
	}
	for key, values := range query {
		if len(values) != 1 || len(values[0]) > 4096 {
			return "", "", false
		}
		switch key {
		case "state", "code", "error", "error_description", "error_uri", "scope", "authuser", "prompt", "hd", "iss":
		default:
			return "", "", false
		}
	}
	// Google always returns this issuer, including on errors (RFC 9207).
	// Compare the decoded value exactly; ID-token legacy aliases do not apply.
	if query.Get("iss") != "https://accounts.google.com" {
		return "", "", false
	}
	state = query.Get("state")
	if _, err := accounts.TokenDigest(state); err != nil {
		return "", "", false
	}
	code = query.Get("code")
	if query.Get("error") != "" {
		if code != "" {
			return "", "", false
		}
		return state, "", true
	}
	return state, code, code != ""
}
func (h *accountHTTP) googleCallback(w http.ResponseWriter, r *http.Request) {
	fail := func() { http.Redirect(w, r, "/connexion?error=google_failed", http.StatusSeeOther) }
	// Clear only the flow cookie, never a valid unrelated account session on error.
	h.setGoogleCookie(w, accounts.Cookie{ExpiresAt: time.Unix(1, 0)})
	if allowed, _ := h.login.allow(requestIdentityFromContext(r.Context()).publicKey); !allowed {
		fail()
		return
	}
	state, code, ok := googleCallbackQuery(r)
	if !ok {
		fail()
		return
	}
	var browser string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == h.googleCookieName() {
			count++
			browser = cookie.Value
		}
	}
	if count != 1 {
		fail()
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		fail()
		return
	}
	result, err := h.service.GoogleCallback(r.Context(), state, browser, code, raw)
	if err != nil {
		if errors.Is(err, accounts.ErrGoogleEmailInUse) {
			http.Redirect(w, r, "/connexion?error=google_email_in_use", http.StatusSeeOther)
			return
		}
		fail()
		return
	}
	h.setCookie(w, result.Session.Cookie)
	http.Redirect(w, r, result.Destination, http.StatusSeeOther)
}
func (h *accountHTTP) reauthContinuation(w http.ResponseWriter, r *http.Request) {
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	result, err := h.service.ReauthContinuation(r.Context(), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *accountHTTP) reauthEmailRequest(w http.ResponseWriter, r *http.Request) {
	if !accountJSON(w, r, &struct{}{}) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	if err = h.service.RequestReauthEmail(r.Context(), raw); err != nil {
		accountError(w, err)
		return
	}
	w.WriteHeader(202)
}
func (h *accountHTTP) reauthEmailConfirm(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token  string          `json:"token"`
		Action accounts.Action `json:"action"`
		Target string          `json:"target"`
	}
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	result, err := h.service.ConfirmReauthEmail(r.Context(), raw, input.Token, input.Action, input.Target)
	if err != nil {
		accountError(w, err)
		return
	}
	h.setCookie(w, result.Cookie)
	writeJSON(w, 200, struct {
		Grant string `json:"grant"`
	}{result.Grant})
}
func (h *accountHTTP) googleUnlink(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Grant string `json:"grant"`
	}
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	cookie, err := h.service.UnlinkGoogle(r.Context(), raw, input.Grant)
	if err != nil {
		accountError(w, err)
		return
	}
	h.setCookie(w, cookie)
	w.WriteHeader(204)
}
func (h *accountHTTP) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Grant        string `json:"grant"`
		Confirmation string `json:"confirmation"`
	}
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	if err = h.service.Delete(r.Context(), raw, input.Grant, input.Confirmation); err != nil {
		accountError(w, err)
		return
	}
	h.clearCookie(w)
	w.WriteHeader(204)
}
