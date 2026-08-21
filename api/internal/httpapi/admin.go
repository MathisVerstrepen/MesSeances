package httpapi

import (
	"crypto/sha256"
	"strings"
	"time"

	"messeances/api/internal/enrichment"
)

type adminAPI struct {
	origin   string
	password string
	key      [32]byte
	hasKey   bool
	reviews  *enrichment.ReviewService
	locals   *enrichment.LocalMovieService
	syncs    SyncController
	now      func() time.Time
	limiter  *loginLimiter
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
	var key [32]byte
	hasKey := strings.TrimSpace(options.SessionSecret) != ""
	if hasKey {
		key = sha256.Sum256([]byte("messeances-admin-session-secret-v1\x00" + options.SessionSecret))
	}
	return &adminAPI{
		origin: origin, password: password, key: key, hasKey: hasKey,
		reviews: options.Reviews, locals: options.LocalMovies, syncs: options.Syncs, now: options.Now,
		limiter: &loginLimiter{attempts: make(map[string]loginAttempt)},
	}
}

func (a *adminAPI) configured() bool { return a.password != "" && a.hasKey && a.reviews != nil }
