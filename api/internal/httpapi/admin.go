package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"movieflow/api/internal/enrichment"
	"movieflow/api/internal/synccontrol"
)

const (
	adminCookieName = "movieflow_admin_session"
	adminSessionTTL = 12 * time.Hour
	maxAdminBody    = 4096
	loginWindow     = 15 * time.Minute
	maxLoginFails   = 5
)

type adminAPI struct {
	origin   string
	password string
	key      [32]byte
	reviews  *enrichment.ReviewService
	locals   *enrichment.LocalMovieService
	syncs    SyncController
	now      func() time.Time
	limiter  *loginLimiter
}

type loginAttempt struct {
	failures int
	resetAt  time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

type sessionResponse struct {
	Authenticated bool `json:"authenticated"`
}

func newAdminAPI(origin string, options AdminOptions) *adminAPI {
	if options.Now == nil {
		options.Now = time.Now
	}
	password := options.Password
	if strings.TrimSpace(password) == "" {
		password = ""
	}
	return &adminAPI{
		origin: origin, password: password,
		key:     sha256.Sum256([]byte("movieflow-admin-session-v1\x00" + password)),
		reviews: options.Reviews, locals: options.LocalMovies, syncs: options.Syncs, now: options.Now,
		limiter: &loginLimiter{attempts: make(map[string]loginAttempt)},
	}
}

type syncResponse struct {
	Job *synccontrol.Status `json:"job"`
}

func (a *adminAPI) syncStatus(w http.ResponseWriter, _ *http.Request) {
	if a.syncs == nil {
		writeError(w, http.StatusServiceUnavailable, "sync_unavailable", "Service de synchronisation indisponible.")
		return
	}
	status := a.syncs.Status()
	if status.ID == "" {
		writeJSON(w, http.StatusOK, syncResponse{})
		return
	}
	writeJSON(w, http.StatusOK, syncResponse{Job: &status})
}

func (a *adminAPI) startSync(w http.ResponseWriter, r *http.Request) {
	if a.syncs == nil {
		writeError(w, http.StatusServiceUnavailable, "sync_unavailable", "Service de synchronisation indisponible.")
		return
	}
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	status, err := a.syncs.Start(synccontrol.Target(chi.URLParam(r, "target")))
	switch {
	case errors.Is(err, synccontrol.ErrInvalidTarget):
		writeError(w, http.StatusBadRequest, "invalid_sync_target", "Cible de synchronisation invalide.")
	case errors.Is(err, synccontrol.ErrInProgress):
		writeError(w, http.StatusConflict, "sync_in_progress", "Une synchronisation est déjà en cours.")
	case err != nil:
		writeError(w, http.StatusBadGateway, "sync_failed", "La synchronisation n'a pas pu démarrer.")
	default:
		writeJSON(w, http.StatusAccepted, syncResponse{Job: &status})
	}
}

func (a *adminAPI) configured() bool { return a.password != "" && a.reviews != nil }

func (a *adminAPI) login(w http.ResponseWriter, r *http.Request) {
	if !a.configured() {
		writeError(w, http.StatusServiceUnavailable, "admin_unavailable", "Service administrateur indisponible.")
		return
	}
	ip := clientAddress(r.RemoteAddr)
	now := a.now().UTC()
	if !a.limiter.allowed(ip, now) {
		writeError(w, http.StatusTooManyRequests, "authentication_failed", "Authentification impossible.")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeAdminJSON(w, r, &input); err != nil {
		a.limiter.failed(ip, now)
		writeError(w, http.StatusUnauthorized, "authentication_failed", "Authentification impossible.")
		return
	}
	want, got := sha256.Sum256([]byte(a.password)), sha256.Sum256([]byte(input.Password))
	if subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
		a.limiter.failed(ip, now)
		writeError(w, http.StatusUnauthorized, "authentication_failed", "Authentification impossible.")
		return
	}
	a.limiter.succeeded(ip)
	expires := now.Add(adminSessionTTL)
	http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: a.sign(expires), Path: "/api/v1/admin", Expires: expires, MaxAge: int(adminSessionTTL.Seconds()), HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true})
}

func (a *adminAPI) session(w http.ResponseWriter, r *http.Request) {
	if !a.configured() {
		writeError(w, http.StatusServiceUnavailable, "admin_unavailable", "Service administrateur indisponible.")
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: a.authenticated(r)})
}

func (a *adminAPI) logout(w http.ResponseWriter, r *http.Request) {
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: "", Path: "/api/v1/admin", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: false})
}

func (a *adminAPI) pendingMatches(w http.ResponseWriter, r *http.Request) {
	limit, offset := 50, 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err == nil {
		if raw := r.URL.Query().Get("offset"); raw != "" {
			offset, err = strconv.Atoi(raw)
		}
	}
	if err != nil || limit < 1 || limit > 100 || offset < 0 {
		writeError(w, http.StatusBadRequest, "invalid_query", "Pagination invalide.")
		return
	}
	items, err := a.reviews.Pending(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Une erreur interne est survenue.")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Items  []enrichment.PendingMatch `json:"items"`
		Limit  int                       `json:"limit"`
		Offset int                       `json:"offset"`
	}{Items: items, Limit: limit, Offset: offset})
}

type localMovieGroupsResponse struct {
	Items  []enrichment.LocalMovieGroup `json:"items"`
	Limit  int                          `json:"limit"`
	Offset int                          `json:"offset"`
}

type mergeLocalMoviesRequest struct {
	Members []enrichment.LocalMovieSource `json:"members"`
	Primary enrichment.LocalMovieSource   `json:"primary"`
}

type unmergeLocalMovieResponse struct {
	Status       string `json:"status"`
	LocalMovieID string `json:"local_movie_id"`
}

func (a *adminAPI) localMovieGroups(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parseLocalMoviePagination(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query", "Pagination invalide.")
		return
	}
	if a.locals == nil {
		a.writeLocalMovieError(w, errors.New("local movie service unavailable"))
		return
	}
	items, err := a.locals.Groups(r.Context(), limit, offset)
	if err != nil {
		a.writeLocalMovieError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, localMovieGroupsResponse{Items: items, Limit: limit, Offset: offset})
}

func (a *adminAPI) mergeLocalMovies(w http.ResponseWriter, r *http.Request) {
	var input mergeLocalMoviesRequest
	if err := decodeAdminJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	if a.locals == nil {
		a.writeLocalMovieError(w, errors.New("local movie service unavailable"))
		return
	}
	group, err := a.locals.Merge(r.Context(), input.Members, input.Primary)
	if err != nil {
		a.writeLocalMovieError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, group)
}

func (a *adminAPI) unmergeLocalMovie(w http.ResponseWriter, r *http.Request) {
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	localMovieID := chi.URLParam(r, "localMovieID")
	if a.locals == nil {
		a.writeLocalMovieError(w, errors.New("local movie service unavailable"))
		return
	}
	if err := a.locals.Unmerge(r.Context(), localMovieID); err != nil {
		a.writeLocalMovieError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, unmergeLocalMovieResponse{Status: "unmerged", LocalMovieID: localMovieID})
}

func parseLocalMoviePagination(r *http.Request) (int, int, bool) {
	query := r.URL.Query()
	for key, values := range query {
		if (key != "limit" && key != "offset") || len(values) != 1 || values[0] == "" {
			return 0, 0, false
		}
	}
	limit, offset := 50, 0
	var err error
	if raw, exists := query["limit"]; exists {
		limit, err = strconv.Atoi(raw[0])
	}
	if err == nil {
		if raw, exists := query["offset"]; exists {
			offset, err = strconv.Atoi(raw[0])
		}
	}
	return limit, offset, err == nil && limit >= 1 && limit <= 100 && offset >= 0
}

func (a *adminAPI) writeLocalMovieError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, enrichment.ErrLocalMovieInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	case errors.Is(err, enrichment.ErrLocalMovieNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Groupe de films introuvable.")
	case errors.Is(err, enrichment.ErrLocalMovieConflict):
		writeError(w, http.StatusConflict, "local_movie_conflict", "Ce regroupement ne peut plus être modifié.")
	default:
		writeError(w, http.StatusBadGateway, "local_movie_failed", "Le regroupement de films n'a pas pu être modifié.")
	}
}

func (a *adminAPI) approveMatch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		TMDBID int64 `json:"tmdb_id"`
	}
	if err := decodeAdminJSON(w, r, &input); err != nil || input.TMDBID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	err := a.reviews.Approve(r.Context(), chi.URLParam(r, "sourceProvider"), chi.URLParam(r, "sourceMovieID"), input.TMDBID)
	if err != nil {
		a.writeReviewError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": enrichment.StatusMatched})
}

func (a *adminAPI) rejectMatch(w http.ResponseWriter, r *http.Request) {
	if !emptyAdminBody(w, r) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Requête invalide.")
		return
	}
	err := a.reviews.Reject(r.Context(), chi.URLParam(r, "sourceProvider"), chi.URLParam(r, "sourceMovieID"))
	if err != nil {
		a.writeReviewError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": enrichment.StatusRejected})
}

func (a *adminAPI) writeReviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, enrichment.ErrReviewNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Correspondance introuvable.")
	case errors.Is(err, enrichment.ErrReviewConflict):
		writeError(w, http.StatusConflict, "review_conflict", "Cette correspondance ne peut plus être modifiée.")
	case errors.Is(err, enrichment.ErrReviewUnavailable):
		writeError(w, http.StatusServiceUnavailable, "review_unavailable", "Service de validation indisponible.")
	default:
		writeError(w, http.StatusBadGateway, "review_failed", "La correspondance n'a pas pu être modifiée.")
	}
}

func (a *adminAPI) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.configured() {
			writeError(w, http.StatusServiceUnavailable, "admin_unavailable", "Service administrateur indisponible.")
			return
		}
		if !a.authenticated(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *adminAPI) noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (a *adminAPI) requireOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func decodeAdminJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return fmt.Errorf("invalid content type")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func emptyAdminBody(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBody)
	body, err := io.ReadAll(r.Body)
	return err == nil && len(body) == 0
}

func clientAddress(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func (l *loginLimiter) allowed(address string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.attempts[address]
	if ok && !now.Before(entry.resetAt) {
		delete(l.attempts, address)
		ok = false
	}
	if !ok && len(l.attempts) >= 10000 {
		for key, candidate := range l.attempts {
			if !now.Before(candidate.resetAt) {
				delete(l.attempts, key)
			}
		}
		if len(l.attempts) >= 10000 {
			return false
		}
	}
	if !ok {
		return true
	}
	return entry.failures < maxLoginFails
}

func (l *loginLimiter) failed(address string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.attempts[address]
	if !ok || !now.Before(entry.resetAt) {
		entry = loginAttempt{resetAt: now.Add(loginWindow)}
	}
	entry.failures++
	if len(l.attempts) < 10000 || ok {
		l.attempts[address] = entry
	}
}

func (l *loginLimiter) succeeded(address string) {
	l.mu.Lock()
	delete(l.attempts, address)
	l.mu.Unlock()
}
