package syncproxy

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRequestErrorMessagesNameSourceAndOperationOnly(t *testing.T) {
	failure := &Failure{Kind: FailureStatus, StatusCode: 404, Cause: errors.New("synthetic-secret-body")}
	named := NewRequestError("Grand Ecran", Operation{Name: "room", Phase: PhaseShowings}, http.MethodGet, "https://example.test/private?token=secret", failure)
	if named.Error() != "Grand Ecran room request failed (category 10, status 404)" {
		t.Fatalf("named=%q", named.Error())
	}
	unnamed := NewRequestError("Ocine", Operation{Phase: PhaseProgram}, http.MethodGet, "https://example.test/", failure)
	if unnamed.Error() != "Ocine request failed (category 10, status 404)" {
		t.Fatalf("unnamed=%q", unnamed.Error())
	}
	if strings.Contains(named.Error(), "secret") || errors.Unwrap(named) != nil {
		t.Fatal("non-cancellation cause leaked into Error or Unwrap")
	}
	if detail := named.SyncFailureDetail(); !strings.Contains(detail, "GET https://example.test/private") || !strings.Contains(detail, "HTTP 404") {
		t.Fatalf("diagnostic=%q", detail)
	}
}

func TestRequestErrorExposesOnlyCancellationCause(t *testing.T) {
	canceled := NewRequestError("Majestic", Operation{Name: "program", Phase: PhaseProgram}, http.MethodPost, "https://example.test/", &Failure{Kind: FailureCanceled, Cause: context.Canceled})
	if !errors.Is(canceled, context.Canceled) {
		t.Fatal("cancellation cause lost")
	}
	safe := SafeRequestError("mk2", Operation{Name: "films", Phase: PhaseMovies}, FailureInvalidJSON, 0, context.DeadlineExceeded)
	if !errors.Is(safe, context.DeadlineExceeded) || safe.Kind != FailureInvalidJSON || safe.Source != "mk2" {
		t.Fatalf("safe=%+v", safe)
	}
	invalid := InvalidRequest("Megarama", Operation{Name: "poster", Phase: PhaseMovies})
	if invalid.Kind != FailureInvalidURL || invalid.Operation.Phase != PhaseMovies || errors.Unwrap(invalid) != nil || invalid.SyncFailureDetail() != "" {
		t.Fatalf("invalid=%+v", invalid)
	}
}

func TestValidateSourceConfigBounds(t *testing.T) {
	proxies := []Proxy{{}}
	for _, test := range []struct {
		proxies []Proxy
		timeout time.Duration
		valid   bool
	}{
		{proxies, 5 * time.Second, true},
		{proxies, 60 * time.Second, true},
		{proxies, 5*time.Second - 1, false},
		{proxies, 60*time.Second + 1, false},
		{nil, 10 * time.Second, false},
	} {
		err := ValidateSourceConfig("cineode", test.proxies, test.timeout)
		if (err == nil) != test.valid {
			t.Fatalf("proxies=%d timeout=%s err=%v", len(test.proxies), test.timeout, err)
		}
		if err != nil && err.Error() != "cineode requires proxies and a timeout between 5s and 60s" {
			t.Fatalf("message=%q", err)
		}
	}
}

func TestStandardResponsePolicy(t *testing.T) {
	policy := StandardResponsePolicy()
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		if failure, retry := policy.BeforeRead(status); failure == nil || failure.Kind != FailureChallenge || failure.StatusCode != status || retry {
			t.Fatalf("status %d before read=%+v retry=%t", status, failure, retry)
		}
	}
	if failure, _ := policy.BeforeRead(http.StatusOK); failure != nil {
		t.Fatal("200 rejected before read")
	}
	for _, test := range []struct {
		status int
		body   string
		kind   FailureKind
		retry  bool
	}{
		{http.StatusOK, "{}", 0, false},
		{http.StatusServiceUnavailable, "", FailureServer, true},
		{http.StatusNotFound, "", FailureStatus, false},
		{http.StatusOK, "<title>Captcha</title>", FailureChallenge, false},
	} {
		failure, retry := policy.AfterRead(test.status, []byte(test.body))
		if test.kind == 0 {
			if failure != nil {
				t.Fatalf("status %d failure=%+v", test.status, failure)
			}
			continue
		}
		if failure == nil || failure.Kind != test.kind || retry != test.retry {
			t.Fatalf("status %d failure=%+v retry=%t", test.status, failure, retry)
		}
	}
}
