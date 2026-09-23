package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"messeances/api/internal/accounts"
)

const accountCookieName = "__Host-messeances_session"
const accountDevCookieName = "messeances_session_dev"
const accountRegistrationCookieName = "__Host-messeances_registration"
const accountDevRegistrationCookieName = "messeances_registration_dev"

type accountHTTP struct {
	service            *accounts.Service
	origin, cookieName string
	secure             bool
	login, send, step  *tokenBucketLimiter
}

func newAccountHTTP(options AccountOptions) (*accountHTTP, error) {
	u, err := url.Parse(options.Origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, accounts.ErrUnavailable
	}
	secure := u.Scheme == "https"
	if !secure && (u.Scheme != "http" || (u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback())) {
		return nil, accounts.ErrUnavailable
	}
	name := accountCookieName
	if !secure {
		name = accountDevCookieName
	}
	return &accountHTTP{service: options.Service, origin: options.Origin, cookieName: name, secure: secure,
		login: newTokenBucketLimiter(20, 20.0/900, 15*time.Minute, maxRateLimitClients, time.Now),
		send:  newTokenBucketLimiter(10, 10.0/3600, time.Hour, maxRateLimitClients, time.Now),
		step:  newTokenBucketLimiter(10, 10.0/900, 15*time.Minute, maxRateLimitClients, time.Now)}, nil
}

func accountError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusServiceUnavailable, "accounts_unavailable", "Les comptes sont indisponibles."
	var rate *accounts.RateLimitError
	switch {
	case errors.As(err, &rate):
		status, code, message = 429, "rate_limited", "Trop de requêtes. Réessayez plus tard."
		w.Header().Set("Retry-After", strconv.Itoa(rate.RetryAfter))
	case errors.Is(err, accounts.ErrInvalidInput):
		status, code, message = 400, "invalid_input", "Requête invalide."
	case errors.Is(err, accounts.ErrInvalidLink):
		status, code, message = 400, "invalid_link", "Ce lien est invalide ou expiré."
	case errors.Is(err, accounts.ErrVerificationBrowser):
		status, code, message = 403, "verification_browser_required", "Rouvrez ce lien dans le navigateur où vous avez commencé votre inscription."
	case errors.Is(err, accounts.ErrUnauthorized):
		status, code, message = 401, "authentication_required", "Connectez-vous pour continuer."
	case errors.Is(err, accounts.ErrCredentials):
		status, code, message = 401, "invalid_credentials", "Email ou mot de passe incorrect."
	case errors.Is(err, accounts.ErrPending):
		status, code, message = 403, "onboarding_required", "Terminez votre inscription."
	case errors.Is(err, accounts.ErrRecentAuth):
		status, code, message = 403, "recent_auth_required", "Confirmez votre identité."
	case errors.Is(err, accounts.ErrUsernameUnavailable):
		status, code, message = 409, "username_unavailable", "Ce nom d’utilisateur est indisponible."
	case errors.Is(err, accounts.ErrEmailUnavailable):
		status, code, message = 409, "email_unavailable", "Cette adresse email est indisponible."
	case errors.Is(err, accounts.ErrIdentityUnavailable):
		status, code, message = 409, "identity_unavailable", "Cette connexion Google est indisponible."
	case errors.Is(err, accounts.ErrLastMethod):
		status, code, message = 409, "last_login_method", "Ajoutez un mot de passe avant de retirer Google."
	}
	writeError(w, status, code, message)
}

func (h *accountHTTP) mutation(next http.HandlerFunc, limiter *tokenBucketLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != h.origin || len(r.Header.Values("X-Messeances-CSRF")) != 1 || r.Header.Get("X-Messeances-CSRF") != "1" {
			writeError(w, 403, "invalid_origin", "Requête interdite.")
			return
		}
		fetch := r.Header.Get("Sec-Fetch-Site")
		if len(r.Header.Values("Sec-Fetch-Site")) > 1 || (fetch != "" && fetch != "same-origin" && fetch != "none") {
			writeError(w, 403, "invalid_origin", "Requête interdite.")
			return
		}
		media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if len(r.Header.Values("Content-Type")) != 1 || err != nil || media != "application/json" || len(params) > 1 || (len(params) == 1 && strings.ToLower(params["charset"]) != "utf-8") || r.URL.RawQuery != "" || r.URL.ForceQuery {
			accountError(w, accounts.ErrInvalidInput)
			return
		}
		if allowed, retry := limiter.allow(requestIdentityFromContext(r.Context()).publicKey); !allowed {
			accountError(w, &accounts.RateLimitError{RetryAfter: retry})
			return
		}
		next(w, r)
	}
}

// The account contract is flat, string-only JSON. Validate exact names, duplicate
// keys (including escaped aliases), null, invalid UTF-8 and trailing values before
// decoding. encoding/json alone accepts duplicates and case-insensitive names.
func accountJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if err != nil || !utf8.Valid(body) {
		accountError(w, accounts.ErrInvalidInput)
		return false
	}
	fields := map[string]bool{}
	t := reflect.TypeOf(dst).Elem()
	for i := 0; i < t.NumField(); i++ {
		fields[t.Field(i).Tag.Get("json")] = true
	}
	d := json.NewDecoder(bytes.NewReader(body))
	opening, err := d.Token()
	if err != nil || opening != json.Delim('{') {
		accountError(w, accounts.ErrInvalidInput)
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		name, ok := key.(string)
		if e != nil || !ok || seen[name] || !fields[name] {
			accountError(w, accounts.ErrInvalidInput)
			return false
		}
		seen[name] = true
		var value json.RawMessage
		if err = d.Decode(&value); err != nil || len(value) == 0 || value[0] != '"' || !accountJSONString(value) {
			accountError(w, accounts.ErrInvalidInput)
			return false
		}
	}
	closing, err := d.Token()
	var extra any
	if err != nil || closing != json.Delim('}') || !errors.Is(d.Decode(&extra), io.EOF) || json.Unmarshal(body, dst) != nil {
		accountError(w, accounts.ErrInvalidInput)
		return false
	}
	return true
}

// encoding/json replaces unpaired UTF-16 surrogates with U+FFFD. Reject them
// rather than silently changing a password; properly paired escapes stay valid.
func accountJSONString(raw []byte) bool {
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if code >= 0xDC00 && code <= 0xDFFF {
			return false
		}
		if code < 0xD800 || code > 0xDBFF {
			continue
		}
		if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if err != nil || low < 0xDC00 || low > 0xDFFF {
			return false
		}
		i += 6
	}
	return true
}

func (h *accountHTTP) cookie(r *http.Request) (string, error) {
	return accountNamedCookie(r, h.cookieName)
}

func (h *accountHTTP) registrationCookieName() string {
	if h.secure {
		return accountRegistrationCookieName
	}
	return accountDevRegistrationCookieName
}

func (h *accountHTTP) registrationCookie(r *http.Request) string {
	raw, err := accountNamedCookie(r, h.registrationCookieName())
	if err != nil {
		return ""
	}
	return raw
}

func accountNamedCookie(r *http.Request, name string) (string, error) {
	var result string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			count++
			result = cookie.Value
		}
	}
	if count > 1 {
		return "", accounts.ErrUnauthorized
	}
	if result != "" {
		if _, err := accounts.TokenDigest(result); err != nil {
			return "", accounts.ErrUnauthorized
		}
	}
	return result, nil
}

func (h *accountHTTP) setCookie(w http.ResponseWriter, c accounts.Cookie) {
	h.setNamedCookie(w, h.cookieName, c)
}

func (h *accountHTTP) setNamedCookie(w http.ResponseWriter, name string, c accounts.Cookie) {
	maxAge := int(time.Until(c.ExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: c.Token, Path: "/", Expires: c.ExpiresAt, MaxAge: maxAge, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
}

func (h *accountHTTP) clearCookie(w http.ResponseWriter) {
	h.clearNamedCookie(w, h.cookieName)
}

func (h *accountHTTP) clearNamedCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", Expires: time.Unix(1, 0), MaxAge: -1, HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
}
