package observability

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type RateLimitKeyClass string

const (
	RateLimitKeyInternalService     RateLimitKeyClass = "internal_service"
	RateLimitKeyForwardedClient     RateLimitKeyClass = "forwarded_client"
	RateLimitKeyDirectClient        RateLimitKeyClass = "direct_client"
	RateLimitKeyTrustedPeerFallback RateLimitKeyClass = "trusted_peer_fallback"
	RateLimitKeyUnknownPeer         RateLimitKeyClass = "unknown_peer"
)

type httpRequestMetadata struct {
	requestID         string
	rateLimitKeyClass RateLimitKeyClass
}

type httpRequestMetadataContextKey struct{}

func WithHTTPRequestMetadata(ctx context.Context, requestID string, keyClass RateLimitKeyClass) context.Context {
	return context.WithValue(ctx, httpRequestMetadataContextKey{}, httpRequestMetadata{
		requestID:         boundedRequestID(requestID),
		rateLimitKeyClass: boundedRateLimitKeyClass(keyClass),
	})
}

func HTTPMiddleware(logger *slog.Logger, metrics *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(wrapped, r)
			status := wrapped.Status()
			if status == 0 {
				status = http.StatusOK
			}
			method := normalizeMethod(r.Method)
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}
			duration := time.Since(started)
			metadata := requestMetadata(r.Context())
			metrics.ObserveHTTP(method, route, status, duration)
			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			}
			logger.Log(r.Context(), level, "http_request_completed",
				"component", "http", "method", method, "route", route,
				"status", status, "duration", duration.Seconds(),
				"request_id", metadata.requestID,
				"rate_limit_key_class", metadata.rateLimitKeyClass)
		})
	}
}

func requestMetadata(ctx context.Context) httpRequestMetadata {
	metadata, ok := ctx.Value(httpRequestMetadataContextKey{}).(httpRequestMetadata)
	if !ok {
		return httpRequestMetadata{requestID: "unknown", rateLimitKeyClass: RateLimitKeyUnknownPeer}
	}
	metadata.requestID = boundedRequestID(metadata.requestID)
	metadata.rateLimitKeyClass = boundedRateLimitKeyClass(metadata.rateLimitKeyClass)
	return metadata
}

func boundedRequestID(value string) string {
	if len(value) != 32 || strings.Trim(value, "0123456789abcdef") != "" {
		return "unknown"
	}
	return value
}

func boundedRateLimitKeyClass(value RateLimitKeyClass) RateLimitKeyClass {
	switch value {
	case RateLimitKeyInternalService, RateLimitKeyForwardedClient, RateLimitKeyDirectClient, RateLimitKeyTrustedPeerFallback, RateLimitKeyUnknownPeer:
		return value
	default:
		return RateLimitKeyUnknownPeer
	}
}

func normalizeMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}
