package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"messeances/api/internal/observability"
)

const requestIDHeader = "X-Request-ID"

type requestIdentity struct {
	publicKey                 string
	internalService           bool
	internalServiceConfigured bool
}

type requestIdentityContextKey struct{}

func requestMetadata(clients clientIdentifier, authenticator internalServiceAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := incomingRequestID(r.Header.Values(requestIDHeader))
			if requestID == "" {
				var err error
				requestID, err = generateRequestID()
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal_error", "Une erreur interne est survenue.")
					return
				}
			}
			client := clients.resolve(r)
			internalService := authenticator.authenticate(r)
			keyClass := client.keyClass
			if internalService {
				keyClass = observability.RateLimitKeyInternalService
			}
			identity := requestIdentity{
				publicKey:                 client.key,
				internalService:           internalService,
				internalServiceConfigured: authenticator.configured,
			}
			ctx := context.WithValue(r.Context(), requestIdentityContextKey{}, identity)
			ctx = observability.WithHTTPRequestMetadata(ctx, requestID, keyClass)
			w.Header().Set(requestIDHeader, requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requestIdentityFromContext(ctx context.Context) requestIdentity {
	identity, ok := ctx.Value(requestIdentityContextKey{}).(requestIdentity)
	if !ok || identity.publicKey == "" {
		return requestIdentity{publicKey: unknownClientKey}
	}
	return identity
}

func incomingRequestID(values []string) string {
	if len(values) != 1 || !lowerHexString(values[0], 32) {
		return ""
	}
	return values[0]
}

func generateRequestID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func lowerHexString(value string, length int) bool {
	return len(value) == length && strings.Trim(value, "0123456789abcdef") == ""
}
