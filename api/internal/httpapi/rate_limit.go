package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	maxRateLimitClients = 10_000

	expensiveReadBurst       = 20
	expensiveReadRefillRate  = 1.0
	expensiveReadIdleHorizon = 20 * time.Second

	shortlinkCreationBurst       = 5
	shortlinkCreationRefillRate  = 1.0 / 144.0
	shortlinkCreationIdleHorizon = 12 * time.Minute
)

type rateLimitEntry struct {
	tokens     float64
	updatedAt  time.Time
	lastAccess time.Time
}

type tokenBucketLimiter struct {
	mu          sync.Mutex
	clients     map[string]rateLimitEntry
	burst       float64
	refillRate  float64
	idleHorizon time.Duration
	maxClients  int
	nextSweep   time.Time
	now         func() time.Time
}

func newTokenBucketLimiter(burst int, refillRate float64, idleHorizon time.Duration, maxClients int, now func() time.Time) *tokenBucketLimiter {
	startedAt := now()
	return &tokenBucketLimiter{
		clients:     make(map[string]rateLimitEntry),
		burst:       float64(burst),
		refillRate:  refillRate,
		idleHorizon: idleHorizon,
		maxClients:  maxClients,
		nextSweep:   startedAt.Add(idleHorizon),
		now:         now,
	}
}

func (l *tokenBucketLimiter) allow(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if entry, exists := l.clients[key]; exists {
		if now.After(entry.updatedAt) {
			entry.tokens = math.Min(l.burst, entry.tokens+now.Sub(entry.updatedAt).Seconds()*l.refillRate)
			entry.updatedAt = now
		}
		entry.lastAccess = now
		if entry.tokens >= 1 {
			entry.tokens--
			l.clients[key] = entry
			return true, 0
		}
		l.clients[key] = entry
		return false, retryAfterSeconds((1 - entry.tokens) / l.refillRate)
	}

	if !now.Before(l.nextSweep) {
		l.sweep(now)
	}
	if len(l.clients) >= l.maxClients {
		return false, retryAfterSeconds(l.nextSweep.Sub(now).Seconds())
	}
	l.clients[key] = rateLimitEntry{tokens: l.burst - 1, updatedAt: now, lastAccess: now}
	return true, 0
}

func (l *tokenBucketLimiter) sweep(now time.Time) {
	cutoff := now.Add(-l.idleHorizon)
	for key, entry := range l.clients {
		if !entry.lastAccess.After(cutoff) {
			delete(l.clients, key)
		}
	}
	l.nextSweep = now.Add(l.idleHorizon)
}

func retryAfterSeconds(seconds float64) int {
	if seconds <= 1 {
		return 1
	}
	return int(math.Ceil(seconds))
}

func rateLimit(limiter *tokenBucketLimiter, clients clientIdentifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, retryAfter := limiter.allow(clients.key(r))
			if !allowed {
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				writeError(w, http.StatusTooManyRequests, "rate_limited", "Trop de requêtes. Réessayez plus tard.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
