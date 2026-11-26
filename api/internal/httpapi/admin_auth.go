package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	adminCookieName = "messeances_admin_session"
	adminSessionTTL = 12 * time.Hour
)

type adminCredential int

const (
	adminCredentialNone adminCredential = iota
	adminCredentialInvalid
	adminCredentialSession
	adminCredentialToken
)

type adminTokenContextKey struct{}

// credential classifies the request's admin proof. An Authorization header is
// authoritative: when present it must carry the configured bearer token, and a
// session cookie never rescues an invalid header.
func (a *adminAPI) credential(r *http.Request) adminCredential {
	if values, present := r.Header["Authorization"]; present {
		if a.validToken(values) {
			return adminCredentialToken
		}
		return adminCredentialInvalid
	}
	if a.authenticated(r) {
		return adminCredentialSession
	}
	return adminCredentialNone
}

func (a *adminAPI) validToken(values []string) bool {
	if !a.hasToken || len(values) != 1 {
		return false
	}
	scheme, token, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return false
	}
	got := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(a.tokenHash[:], got[:]) == 1
}

func adminTokenAuthenticated(ctx context.Context) bool {
	authenticated, _ := ctx.Value(adminTokenContextKey{}).(bool)
	return authenticated
}

func (a *adminAPI) login(w http.ResponseWriter, r *http.Request) {
	if !a.configured() {
		writeError(w, http.StatusServiceUnavailable, "admin_unavailable", "Service administrateur indisponible.")
		return
	}
	now := a.now().UTC()
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeAdminJSON(w, r, &input); err != nil {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "Authentification impossible.")
		return
	}
	want, got := sha256.Sum256([]byte(a.password)), sha256.Sum256([]byte(input.Password))
	if subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
		writeError(w, http.StatusUnauthorized, "authentication_failed", "Authentification impossible.")
		return
	}
	expires := now.Add(adminSessionTTL)
	http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: a.sign(expires), Path: "/api/v1/admin", Expires: expires, MaxAge: int(adminSessionTTL.Seconds()), HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true})
}

func (a *adminAPI) session(w http.ResponseWriter, r *http.Request) {
	if !a.configured() {
		writeError(w, http.StatusServiceUnavailable, "admin_unavailable", "Service administrateur indisponible.")
		return
	}
	credential := a.credential(r)
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: credential == adminCredentialSession || credential == adminCredentialToken})
}

func (a *adminAPI) logout(w http.ResponseWriter, r *http.Request) {
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: "", Path: "/api/v1/admin", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: false})
}

func (a *adminAPI) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.configured() {
			writeError(w, http.StatusServiceUnavailable, "admin_unavailable", "Service administrateur indisponible.")
			return
		}
		switch a.credential(r) {
		case adminCredentialSession:
			next.ServeHTTP(w, r)
		case adminCredentialToken:
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				a.logger.InfoContext(r.Context(), "admin_token_mutation", "component", "admin", "method", r.Method, "path", r.URL.Path)
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminTokenContextKey{}, true)))
		default:
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
		}
	})
}

func (a *adminAPI) noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// requireOrigin is the CSRF guard for cookie sessions. Bearer-token requests
// are exempt: browsers never attach that header implicitly, and CORS does not
// allow cross-origin pages to send it.
func (a *adminAPI) requireOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if adminTokenAuthenticated(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Origin") != a.origin {
			writeError(w, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *adminAPI) sign(expires time.Time) string {
	payload := "v1." + strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, a.key[:])
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *adminAPI) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(adminCookieName)
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return false
	}
	expiresUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || !a.now().UTC().Before(time.Unix(expiresUnix, 0)) {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	payload := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, a.key[:])
	_, _ = mac.Write([]byte(payload))
	return hmac.Equal(signature, mac.Sum(nil))
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}
