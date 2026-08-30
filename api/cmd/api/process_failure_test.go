package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"messeances/api/internal/observability"
)

func TestLogProcessFailureAllowlist(t *testing.T) {
	tests := []struct {
		reason string
		stage  string
	}{
		{reason: "configuration error", stage: "configuration"},
		{reason: "sync configuration is invalid", stage: "configuration"},
		{reason: "database startup failed", stage: "database"},
		{reason: "database migration failed", stage: "migration"},
		{reason: "shortlink retention startup failed", stage: "retention"},
		{reason: "sync run retention startup failed", stage: "retention"},
		{reason: "schedule snapshot startup failed", stage: "schedule"},
		{reason: "schedule service startup failed", stage: "schedule"},
		{reason: "TMDB configuration is invalid", stage: "configuration"},
		{reason: "TMDB metadata refresh configuration is invalid", stage: "configuration"},
		{reason: "geocoding configuration is invalid", stage: "configuration"},
		{reason: "sync schedule configuration is invalid", stage: "configuration"},
		{reason: "API server failed", stage: "server"},
		{reason: "API server shutdown failed", stage: "server"},
	}

	for _, test := range tests {
		t.Run(test.reason, func(t *testing.T) {
			event, _ := captureProcessFailure(t, errors.New(test.reason))
			assertLogFields(t, event, map[string]string{
				"msg":            "process_stopped",
				"component":      "api",
				"error_code":     "process_failure",
				"failure_stage":  test.stage,
				"failure_reason": test.reason,
			})
			if _, exists := event["error"]; exists {
				t.Fatalf("raw error field logged: %v", event)
			}
		})
	}
}

func TestLogProcessFailureFallsBackWithoutLeakingUnknownOrWrappedErrors(t *testing.T) {
	const secret = "sentinel-user:sentinel-password@database/internal-path"
	tests := []error{
		errors.New(secret),
		errors.New("database migration history is incompatible"),
		fmt.Errorf("database migration failed: %w", errors.New(secret)),
		fmt.Errorf("wrapped safe detail: %w", &processFailureError{detail: processFailureDetail{stage: "migration", reason: secret}}),
		nil,
	}

	for _, err := range tests {
		event, output := captureProcessFailure(t, err)
		assertLogFields(t, event, map[string]string{
			"msg":            "process_stopped",
			"component":      "api",
			"error_code":     "process_failure",
			"failure_stage":  "unknown",
			"failure_reason": "process failure",
		})
		if strings.Contains(output, secret) || strings.Contains(output, "database migration failed:") {
			t.Fatalf("sensitive error logged: %s", output)
		}
	}
}

func TestLogProcessFailureAcceptsDirectIncompatibleHistoryDetail(t *testing.T) {
	event, _ := captureProcessFailure(t, migrationHistoryIncompatibleProcessFailure)
	assertLogFields(t, event, map[string]string{
		"msg":            "process_stopped",
		"component":      "api",
		"error_code":     "process_failure",
		"failure_stage":  "migration",
		"failure_reason": "database migration history is incompatible",
	})
}

func TestLogDotEnvFailureUsesFixedSafeDetail(t *testing.T) {
	var output bytes.Buffer
	logDotEnvFailure(observability.NewLogger(&output))
	event := parseLogEvent(t, output.Bytes())
	assertLogFields(t, event, map[string]string{
		"msg":            "process_start_failed",
		"component":      "api",
		"error_code":     "configuration_error",
		"failure_stage":  "configuration",
		"failure_reason": "dotenv load failed",
	})
}

func captureProcessFailure(t *testing.T, err error) (map[string]any, string) {
	t.Helper()
	var output bytes.Buffer
	logProcessFailure(observability.NewLogger(&output), err)
	return parseLogEvent(t, output.Bytes()), output.String()
}

func parseLogEvent(t *testing.T, output []byte) map[string]any {
	t.Helper()
	var event map[string]any
	if err := json.Unmarshal(output, &event); err != nil {
		t.Fatalf("parse log event %q: %v", output, err)
	}
	return event
}

func assertLogFields(t *testing.T, event map[string]any, want map[string]string) {
	t.Helper()
	for name, value := range want {
		if event[name] != value {
			t.Errorf("%s=%v want %q", name, event[name], value)
		}
	}
}
