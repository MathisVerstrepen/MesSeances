package veo

import (
	"errors"
	"strings"

	"messeances/api/internal/schedule"
)

// CheckoutRejection is deliberately closed: provider text is never a reason.
type CheckoutRejection string

type formSubreason string

const (
	formBody             formSubreason = "body_encoding"
	formHTML             formSubreason = "html_parse"
	formCanonicalMissing formSubreason = "canonical_missing"
	formCanonicalInvalid formSubreason = "canonical_invalid"
	formRawCount         formSubreason = "raw_form_count"
	formDOMCount         formSubreason = "dom_form_count"
	formInert            formSubreason = "template_inert"
	formActionCount      formSubreason = "action_count"
	formActionMismatch   formSubreason = "action_mismatch"
	formMethodCount      formSubreason = "method_count"
	formMethodNotPOST    formSubreason = "method_non_post"
	formLogin            formSubreason = "login_password"
	formFronton          formSubreason = "fronton_scope"
)

const (
	RejectRoom          CheckoutRejection = "room"
	RejectBookingForm   CheckoutRejection = "booking_form"
	RejectClock         CheckoutRejection = "clock"
	RejectEndResolution CheckoutRejection = "end_resolution"
)

// CheckoutParseError carries only public, validated identities. Error never
// renders its cause; Unwrap retains cancellation and parser sentinel semantics.
type CheckoutParseError struct {
	Reason     CheckoutRejection
	CinemaID   string
	ShowingID  string
	Cause      error
	formReason formSubreason
	actionDiff actionDifference
	pool       clientPoolPosition
	actionOnly bool
	path       *CheckoutActionPathFacts
}

// FormSubreason exposes only diagnostic classification, never provider values.
func (e *CheckoutParseError) FormSubreason() string { return string(e.formReason) }

// ActionDifferences and ClientPoolPosition expose value-free backend diagnostics.
// The position refers to the HTTP client pool, not a proxy-file line number.
func (e *CheckoutParseError) ActionDifferences() uint32      { return uint32(e.actionDiff) }
func (e *CheckoutParseError) ClientPoolPosition() (int, int) { return e.pool.index, e.pool.count }

// ActionPathFacts returns an isolated copy of the rejected response's closed
// structure, not data from a later re-fetch or another request.
func (e *CheckoutParseError) ActionPathFacts() (CheckoutActionPathFacts, bool) {
	if e.path == nil {
		return CheckoutActionPathFacts{}, false
	}
	copy := *e.path
	copy.Classes = append([]string(nil), copy.Classes...)
	return copy, true
}

func formRejection(reason formSubreason) error {
	return &CheckoutParseError{Reason: RejectBookingForm, formReason: reason, Cause: errShape}
}

func formDiagnosticRejection(reason formSubreason, difference actionDifference) *CheckoutParseError {
	if reason != formActionMismatch {
		difference = 0
	}
	return &CheckoutParseError{Reason: RejectBookingForm, formReason: reason, actionDiff: difference, Cause: errShape}
}

func (*CheckoutParseError) Error() string   { return "véo checkout payload rejected" }
func (e *CheckoutParseError) Unwrap() error { return e.Cause }

func checkoutRejection(reason CheckoutRejection, cause error) error {
	return &CheckoutParseError{Reason: reason, Cause: cause}
}

func identifyCheckoutError(err error, row schedule.ShowtimeRecord, results ...CheckoutResult) error {
	var rejected *CheckoutParseError
	if !errors.As(err, &rejected) {
		return err
	}
	copy := *rejected
	copy.CinemaID, copy.ShowingID = "", ""
	copy.pool = clientPoolPosition{}
	if len(results) == 1 {
		copy.pool = results[0].pool
	}
	cinema, ok := strings.CutPrefix(row.TheaterID, "veo-")
	if ok && schedule.ValidVeoIdentity("theater", cinema) && schedule.ValidVeoIdentity("showing", row.ProviderShowingID) && strings.HasPrefix(row.ProviderShowingID, cinema+"-") {
		copy.CinemaID, copy.ShowingID = cinema, row.ProviderShowingID
	}
	return &copy
}
