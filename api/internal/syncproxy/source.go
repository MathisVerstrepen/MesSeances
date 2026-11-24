package syncproxy

import (
	"fmt"
	"net/http"
	"time"
)

// Phase is the provider-neutral stage a source request belongs to. Sync
// orchestration classifies failures by phase, never by provider vocabulary.
type Phase string

const (
	PhaseUnknown  Phase = ""
	PhaseCinemas  Phase = "cinemas"
	PhaseProgram  Phase = "program"
	PhaseMovies   Phase = "movies"
	PhaseShowings Phase = "showings"
)

// Operation names one kind of provider request. Name is the provider's own
// label, used in error messages; Phase drives classification.
type Operation struct {
	Name  string
	Phase Phase
}

// RequestError is the shared failure of a proxied source request. It carries
// only safe facts (operation, failure kind, HTTP status) in Error; the request
// target stays in RequestDiagnostic for the authenticated admin journal.
type RequestError struct {
	RequestDiagnostic
	// Source is the provider label shown in Error, for example "Majestic".
	Source     string
	Operation  Operation
	Kind       FailureKind
	StatusCode int
	cause      error
}

func (e *RequestError) Error() string {
	if e.Operation.Name == "" {
		return fmt.Sprintf("%s request failed (category %d, status %d)", e.Source, e.Kind, e.StatusCode)
	}
	return fmt.Sprintf("%s %s request failed (category %d, status %d)", e.Source, e.Operation.Name, e.Kind, e.StatusCode)
}

// Unwrap exposes the cancellation cause only, so callers can recognise
// context cancellation without provider error bodies leaking through.
func (e *RequestError) Unwrap() error { return e.cause }

// NewRequestError converts an executor failure into a RequestError.
func NewRequestError(source string, operation Operation, method, rawURL string, failure *Failure) *RequestError {
	err := &RequestError{Source: source, Operation: operation, Kind: failure.Kind, StatusCode: failure.StatusCode, RequestDiagnostic: NewRequestDiagnostic(method, rawURL, failure)}
	if failure.Kind == FailureCanceled {
		err.cause = failure.Cause
	}
	return err
}

// SafeRequestError builds a RequestError from facts the provider derived
// itself. cause is exposed through Unwrap, so it must be a sentinel or context
// error, never a raw response or transport error.
func SafeRequestError(source string, operation Operation, kind FailureKind, statusCode int, cause error) *RequestError {
	return &RequestError{Source: source, Operation: operation, Kind: kind, StatusCode: statusCode, cause: cause}
}

// InvalidRequest rejects a request before it reaches the network.
func InvalidRequest(source string, operation Operation) *RequestError {
	return &RequestError{Source: source, Operation: operation, Kind: FailureInvalidURL}
}

// ValidateSourceConfig enforces the transport bounds shared by proxied
// sources: at least one proxy and a per-request timeout between 5s and 60s.
func ValidateSourceConfig(source string, proxies []Proxy, timeout time.Duration) error {
	if len(proxies) == 0 || timeout < 5*time.Second || timeout > 60*time.Second {
		return fmt.Errorf("%s requires proxies and a timeout between 5s and 60s", source)
	}
	return nil
}

// StandardResponsePolicy is the response contract shared by proxied sources:
// 403/429 and challenge pages abort without retry, 5xx retries on another
// proxy, and any other non-200 status fails without retry.
func StandardResponsePolicy() ResponsePolicy {
	return ResponsePolicy{
		BeforeRead: func(status int) (*Failure, bool) {
			if status == http.StatusForbidden || status == http.StatusTooManyRequests {
				return &Failure{Kind: FailureChallenge, StatusCode: status}, false
			}
			return nil, false
		},
		AfterRead: func(status int, body []byte) (*Failure, bool) {
			if IsChallenge(body) {
				return &Failure{Kind: FailureChallenge}, false
			}
			if status >= http.StatusInternalServerError {
				return &Failure{Kind: FailureServer, StatusCode: status}, true
			}
			if status != http.StatusOK {
				return &Failure{Kind: FailureStatus, StatusCode: status}, false
			}
			return nil, false
		},
	}
}
