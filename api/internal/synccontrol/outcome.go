package synccontrol

import (
	"fmt"
	"time"
)

type FailureCode string
type FailureStage string
type EnrichmentState string

const (
	EnrichmentSkipped  EnrichmentState = "skipped"
	EnrichmentComplete EnrichmentState = "complete"
	EnrichmentDegraded EnrichmentState = "degraded"
)

const (
	FailureNone            FailureCode = "none"
	FailureClientCreation  FailureCode = "client_creation_failed"
	FailureProviderSync    FailureCode = "provider_sync_failed"
	FailureDatasetRejected FailureCode = "dataset_rejected"
	FailureReplacement     FailureCode = "replacement_failed"
	FailureCanceled        FailureCode = "canceled"
	FailureInternal        FailureCode = "internal_failure"
)

const (
	StageNone              FailureStage = "none"
	StageClientCreation    FailureStage = "client_creation"
	StageProviderFetch     FailureStage = "provider_fetch"
	StageDatasetValidation FailureStage = "dataset_validation"
	StagePublication       FailureStage = "publication"
	StageEnrichment        FailureStage = "enrichment"
	StageOrchestration     FailureStage = "orchestration"
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
	Status EnrichmentState   `json:"status"`
	Counts *EnrichmentCounts `json:"counts,omitempty"`
}

type ProviderOutcome struct {
	Sync       SyncOutcome       `json:"sync"`
	Enrichment EnrichmentOutcome `json:"enrichment"`
}

type RunError struct {
	Code     FailureCode
	Provider Target
	Stage    FailureStage
	cause    error
}

func NewRunError(code FailureCode, cause error) *RunError {
	return &RunError{Code: code, Stage: StageOrchestration, cause: cause}
}

func newProviderRunError(provider Target, stage FailureStage, code FailureCode, cause error) *RunError {
	return &RunError{Code: code, Provider: provider, Stage: stage, cause: cause}
}

func (e *RunError) Error() string { return fmt.Sprintf("sync run failed: %s", e.Code) }
func (e *RunError) Unwrap() error { return e.cause }
