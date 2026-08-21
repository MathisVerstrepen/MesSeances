package synccontrol

import (
	"fmt"
	"time"
)

type FailureCode string

const (
	FailureNone            FailureCode = "none"
	FailureClientCreation  FailureCode = "client_creation_failed"
	FailureProviderSync    FailureCode = "provider_sync_failed"
	FailureDatasetRejected FailureCode = "dataset_rejected"
	FailureReplacement     FailureCode = "replacement_failed"
	FailureCanceled        FailureCode = "canceled"
	FailureInternal        FailureCode = "internal_failure"
)

type SyncOutcome struct {
	Version     int64     `json:"version"`
	Cinemas     int       `json:"cinemas"`
	Dates       int       `json:"dates,omitempty"`
	Requests    int       `json:"requests,omitempty"`
	Showtimes   int       `json:"showtimes"`
	Skipped     int       `json:"skipped,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
}

type EnrichmentCounts struct {
	Reused         int `json:"reused"`
	Matched        int `json:"matched"`
	ReviewRequired int `json:"review_required"`
	Unmatched      int `json:"unmatched"`
	Failed         int `json:"failed"`
}

type EnrichmentOutcome struct {
	Status string            `json:"status"`
	Counts *EnrichmentCounts `json:"counts,omitempty"`
}

type ProviderOutcome struct {
	Sync       SyncOutcome       `json:"sync"`
	Enrichment EnrichmentOutcome `json:"enrichment"`
}

type RunError struct {
	Code  FailureCode
	cause error
}

func NewRunError(code FailureCode, cause error) *RunError {
	return &RunError{Code: code, cause: cause}
}

func (e *RunError) Error() string { return fmt.Sprintf("sync run failed: %s", e.Code) }
func (e *RunError) Unwrap() error { return e.cause }
