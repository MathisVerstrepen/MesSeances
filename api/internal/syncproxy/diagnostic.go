package syncproxy

import (
	"fmt"
	"strings"
)

// RequestDiagnostic retains request context for the authenticated admin journal.
// It deliberately does not change Error or Unwrap, which are also used by
// process logs and provider cancellation/retry decisions.
type RequestDiagnostic struct {
	detail string
}

func NewRequestDiagnostic(method, requestURL string, failure *Failure) RequestDiagnostic {
	var detail strings.Builder
	fmt.Fprintf(&detail, "%s %s", method, requestURL)
	if failure.Attempt > 0 {
		fmt.Fprintf(&detail, "; attempt %d/4", failure.Attempt)
	}
	if failure.StatusCode != 0 {
		fmt.Fprintf(&detail, "; HTTP %d", failure.StatusCode)
	}
	if failure.Cause != nil {
		fmt.Fprintf(&detail, "; %v", failure.Cause)
	}
	return RequestDiagnostic{detail: detail.String()}
}

func (d RequestDiagnostic) SyncFailureDetail() string { return d.detail }
