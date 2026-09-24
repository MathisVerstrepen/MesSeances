package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/accounts"
)

type accountCredentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type accountEmail struct {
	Email string `json:"email"`
}
type accountConfirmation struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}
type accountVerification struct {
	Token string `json:"token"`
}
type accountUsername struct {
	Username string `json:"username"`
}
type accountReauth struct {
	Password string          `json:"password"`
	Action   accounts.Action `json:"action"`
	Target   string          `json:"target"`
}
type accountPassword struct {
	Password string `json:"password"`
	Grant    string `json:"grant"`
}
type accountEmailRequest struct {
	Email string `json:"email"`
	Grant string `json:"grant"`
}
type accountEmailConfirm struct {
	Token string `json:"token"`
	Grant string `json:"grant"`
}

func registerAccountLifecycle(router chi.Router, options AccountOptions, unavailable http.HandlerFunc) {
	h, err := newAccountHTTP(options)
	if err != nil {
		options.Service = nil
		registerAccountRoutes(router, options)
		return
	}
	get := func(path string, handler http.HandlerFunc) {
		router.Get("/api/v1"+path, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" || r.URL.ForceQuery {
				accountError(w, accounts.ErrInvalidInput)
				return
			}
			handler(w, r)
		})
	}
	post := func(path string, handler http.HandlerFunc, limiter *tokenBucketLimiter) {
		router.Post("/api/v1"+path, h.mutation(handler, limiter))
	}
	get("/auth/session", h.session)
	get("/account", h.details)
	get("/account/theaters", h.theaterPreferences)
	post("/account/theaters", h.saveTheaterPreferences, h.theaters)
	post("/auth/register", h.register, h.send)
	post("/auth/login", h.loginHandler, h.login)
	post("/auth/verification/request", h.requestVerification, h.send)
	post("/auth/verification/confirm", h.confirmVerification, h.login)
	post("/auth/password/reset/request", h.requestReset, h.send)
	post("/auth/password/reset/confirm", h.confirmReset, h.login)
	post("/auth/logout", h.logout(false), h.login)
	post("/auth/logout-all", h.logout(true), h.login)
	post("/account/username", h.username, h.login)
	post("/account/reauth/password", h.reauthPassword, h.step)
	post("/account/password", h.password, h.step)
	post("/account/email/request", h.emailRequest, h.send)
	post("/account/email/confirm", h.emailConfirm, h.step)
	post("/account/email/cancel", h.emailCancel, h.step)
	post("/auth/google/start", h.googleStart, h.login)
	post("/account/reauth/email/request", h.reauthEmailRequest, h.step)
	post("/account/reauth/email/confirm", h.reauthEmailConfirm, h.step)
	post("/account/google/unlink", h.googleUnlink, h.step)
	get("/account/reauth/continuation", h.reauthContinuation)
	router.Get("/api/v1/auth/google/callback", h.googleCallback)
	router.Delete("/api/v1/account", h.mutation(h.deleteAccount, h.step))
	router.Post("/api/v1/account/avatar", h.mutationBoundary(h.uploadAvatar, h.step))
	router.Delete("/api/v1/account/avatar", h.mutation(h.removeAvatar, h.step))
	get("/account/avatar/{revision}", h.avatar)
}

func (h *accountHTTP) session(w http.ResponseWriter, r *http.Request) {
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.Session(r.Context(), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 200, view)
}
func (h *accountHTTP) details(w http.ResponseWriter, r *http.Request) {
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.Details(r.Context(), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, 200, view)
}
func (h *accountHTTP) register(w http.ResponseWriter, r *http.Request) {
	var input accountCredentials
	if !accountJSON(w, r, &input) {
		return
	}
	cookie, err := h.service.Register(r.Context(), input.Email, input.Password)
	if err != nil {
		accountError(w, err)
		return
	}
	h.setNamedCookie(w, h.registrationCookieName(), cookie)
	w.WriteHeader(http.StatusAccepted)
}
func (h *accountHTTP) loginHandler(w http.ResponseWriter, r *http.Request) {
	var input accountCredentials
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	result, err := h.service.Login(r.Context(), input.Email, input.Password, raw)
	if err != nil {
		accountError(w, err)
		return
	}
	h.setCookie(w, result.Cookie)
	writeJSON(w, 200, result.View)
}
func (h *accountHTTP) requestVerification(w http.ResponseWriter, r *http.Request) {
	var input accountEmail
	if !accountJSON(w, r, &input) {
		return
	}
	if err := h.service.RequestVerification(r.Context(), input.Email, h.registrationCookie(r)); err != nil {
		accountError(w, err)
		return
	}
	w.WriteHeader(202)
}
func (h *accountHTTP) requestReset(w http.ResponseWriter, r *http.Request) {
	var input accountEmail
	if !accountJSON(w, r, &input) {
		return
	}
	if err := h.service.RequestReset(r.Context(), input.Email); err != nil {
		accountError(w, err)
		return
	}
	w.WriteHeader(202)
}
func (h *accountHTTP) confirmVerification(w http.ResponseWriter, r *http.Request) {
	var input accountVerification
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		// Method selection belongs to the service. Email registration needs only
		// its independent attempt cookie; invalid session proof cannot help Google.
		raw = ""
	}
	result, err := h.service.ConfirmVerification(r.Context(), input.Token, h.registrationCookie(r), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	h.setCookie(w, result.Cookie)
	h.clearNamedCookie(w, h.registrationCookieName())
	writeJSON(w, 200, result.View)
}
func (h *accountHTTP) confirmReset(w http.ResponseWriter, r *http.Request) {
	var input accountConfirmation
	if !accountJSON(w, r, &input) {
		return
	}
	if err := h.service.ConfirmReset(r.Context(), input.Token, input.Password); err != nil {
		accountError(w, err)
		return
	}
	h.clearCookie(w)
	w.WriteHeader(204)
}
func (h *accountHTTP) logout(all bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !accountJSON(w, r, &struct{}{}) {
			return
		}
		raw, err := h.cookie(r)
		if err != nil {
			if !all {
				h.clearCookie(w)
				w.WriteHeader(204)
				return
			}
			accountError(w, err)
			return
		}
		if err = h.service.Logout(r.Context(), raw, all); err != nil {
			accountError(w, err)
			return
		}
		h.clearCookie(w)
		w.WriteHeader(204)
	}
}
func (h *accountHTTP) username(w http.ResponseWriter, r *http.Request) {
	var input accountUsername
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	result, err := h.service.Username(r.Context(), raw, input.Username)
	if err != nil {
		accountError(w, err)
		return
	}
	h.setCookie(w, result.Cookie)
	writeJSON(w, 200, result.View)
}
func (h *accountHTTP) reauthPassword(w http.ResponseWriter, r *http.Request) {
	var input accountReauth
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	result, err := h.service.ReauthPassword(r.Context(), raw, input.Password, input.Action, input.Target)
	if err != nil {
		accountError(w, err)
		return
	}
	h.setCookie(w, result.Cookie)
	writeJSON(w, 200, struct {
		Grant string `json:"grant"`
	}{result.Grant})
}
func (h *accountHTTP) password(w http.ResponseWriter, r *http.Request) {
	var input accountPassword
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	cookie, err := h.service.ChangePassword(r.Context(), raw, input.Password, input.Grant)
	if err != nil {
		accountError(w, err)
		return
	}
	h.setCookie(w, cookie)
	w.WriteHeader(204)
}
func (h *accountHTTP) emailRequest(w http.ResponseWriter, r *http.Request) {
	var input accountEmailRequest
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	if err = h.service.RequestEmailChange(r.Context(), raw, input.Email, input.Grant); err != nil {
		accountError(w, err)
		return
	}
	w.WriteHeader(202)
}
func (h *accountHTTP) emailConfirm(w http.ResponseWriter, r *http.Request) {
	var input accountEmailConfirm
	if !accountJSON(w, r, &input) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	if err = h.service.ConfirmEmailChange(r.Context(), raw, input.Token, input.Grant); err != nil {
		accountError(w, err)
		return
	}
	h.clearCookie(w)
	w.WriteHeader(204)
}
func (h *accountHTTP) emailCancel(w http.ResponseWriter, r *http.Request) {
	if !accountJSON(w, r, &struct{}{}) {
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	if err = h.service.CancelEmailChange(r.Context(), raw); err != nil {
		accountError(w, err)
		return
	}
	w.WriteHeader(204)
}
