package synccontrol

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestProviderLogCanonicalFormat(t *testing.T) {
	timestamp := time.Date(2026, 8, 26, 7, 57, 6, 123456789, time.UTC)
	line := failureLog(timestamp, TargetUGC, StageProviderFetch, logFailure{
		Operation: operationShowings, Category: categoryHTTPStatus, HTTPStatus: 403,
		Attempt: 1, AttemptLimit: 4, Progress: logProgress{Requests: intPointer(26)},
	})
	want := `ts=2026-08-26T07:57:06.123456789Z level=error provider=ugc event=provider_failed stage=provider_fetch operation=showings category=http_status http_status=403 attempt=1/4 requests=26 message="Le fournisseur a renvoyé un statut HTTP inattendu."`
	if line != want {
		t.Fatalf("line=%q\nwant=%q", line, want)
	}
	finished := timestamp.Add(time.Minute)
	if normalized := normalizeProviderLog(TargetUGC, []string{line}, timestamp.Add(-time.Minute), &finished); len(normalized) != 1 || normalized[0] != want {
		t.Fatalf("normalized=%q", normalized)
	}
}

func TestProviderLogNormalizationOrdersBoundsAndRedacts(t *testing.T) {
	started := time.Date(2026, 8, 26, 7, 0, 0, 0, time.UTC)
	finished := started.Add(time.Hour)
	lines := make([]string, 0, 22)
	for i := 0; i < 21; i++ {
		lines = append(lines, lifecycleLog(started.Add(time.Duration(i)*time.Second), TargetUGC, eventFetchStarted))
	}
	secret := "https://user:proxy-password@proxy.example/path?token=secret cookie=session-secret body=provider-body-secret"
	lines = append(lines, secret+"\nsecond-line")
	normalized := normalizeProviderLog(TargetUGC, lines, started, &finished)
	if len(normalized) != maxProviderLogLines {
		t.Fatalf("lines=%d values=%q", len(normalized), normalized)
	}
	if !strings.Contains(normalized[0], "ts=2026-08-26T07:00:02Z") || !strings.Contains(normalized[len(normalized)-1], "event=log_truncated") {
		t.Fatalf("normalized=%q", normalized)
	}
	joined := strings.Join(normalized, "\n")
	if len(joined) > maxProviderLogBytes || strings.Contains(joined, secret) || strings.Contains(joined, "proxy.example") {
		t.Fatalf("unsafe normalized log=%q", joined)
	}
}

func TestProviderLogNormalizationEnforcesJoinedByteBudget(t *testing.T) {
	started := time.Date(2026, 8, 26, 7, 0, 0, 123456789, time.UTC)
	finished := started.Add(time.Hour)
	maximum := math.MaxInt
	var progress logProgress
	for maximum > 0 {
		progress = logProgress{Requests: &maximum, Cinemas: &maximum, Movies: &maximum, Dates: &maximum, Showtimes: &maximum, Skipped: &maximum}
		if failureLog(started, TargetKinepolis, StageProviderFetch, logFailure{Operation: operationDatasetValidation, Category: categoryHTTPStatus, HTTPStatus: 599, Attempt: 10, AttemptLimit: 10, Progress: progress}) != "" {
			break
		}
		maximum /= 10
	}
	lines := make([]string, maxProviderLogLines)
	for i := range lines {
		lines[i] = failureLog(started.Add(time.Duration(i)*time.Second), TargetKinepolis, StageProviderFetch, logFailure{
			Operation: operationDatasetValidation, Category: categoryHTTPStatus, HTTPStatus: 599,
			Attempt: 10, AttemptLimit: 10, Progress: progress,
		})
		if len(lines[i]) == 0 || len(lines[i]) > maxProviderLogLine {
			t.Fatalf("fixture line outside per-line bound: %d", len(lines[i]))
		}
	}
	if len(strings.Join(lines, "\n")) <= maxProviderLogBytes {
		t.Fatalf("fixture does not exceed joined budget: %d", len(strings.Join(lines, "\n")))
	}
	normalized := normalizeProviderLog(TargetKinepolis, lines, started, &finished)
	joined := strings.Join(normalized, "\n")
	if len(normalized) > maxProviderLogLines || len(joined) > maxProviderLogBytes || normalized[0] == lines[0] || !strings.Contains(normalized[len(normalized)-1], "event=log_truncated") {
		t.Fatalf("lines=%d bytes=%d values=%q", len(normalized), len(joined), normalized)
	}
}

func TestProviderLogNormalizationRejectsNonCanonicalInputs(t *testing.T) {
	started := time.Date(2026, 8, 26, 7, 0, 0, 0, time.UTC)
	finished := started.Add(time.Hour)
	valid := lifecycleLog(started.Add(time.Minute), TargetCGR, eventProviderStarted)
	tests := []string{
		strings.Replace(valid, "provider=cgr", "provider=ugc", 1),
		strings.Replace(valid, "event=provider_started", "event=attacker", 1),
		strings.Replace(valid, "level=info", "level=error", 1),
		strings.Replace(valid, `message="Synchronisation fournisseur démarrée."`, `message="token=secret"`, 1),
		valid + "\nraw-body",
		strings.TrimSuffix(valid, `"`),
		strings.Repeat("x", maxProviderLogLine+1),
		`ts=2026-08-26T07:01:00Z level=error provider=cgr event=provider_failed stage=provider_fetch operation=movies category=http_status http_status=999 attempt=11/4 message="Le fournisseur a renvoyé un statut HTTP inattendu."`,
	}
	for _, input := range tests {
		normalized := normalizeProviderLog(TargetCGR, []string{input}, started, &finished)
		if len(normalized) != 1 || !strings.Contains(normalized[0], "event=log_truncated") || strings.Contains(strings.Join(normalized, "\n"), "secret") {
			t.Fatalf("input=%q normalized=%q", input, normalized)
		}
	}
}

func TestSanitizeStatusKeepsLogsOnlyForFailedProviders(t *testing.T) {
	started := time.Date(2026, 8, 26, 7, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	failedLine := failureLog(finished, TargetUGC, StageProviderFetch, logFailure{Operation: operationUnknown, Category: categoryUnknown})
	safe := SanitizeStatus(Status{StartedAt: started, FinishedAt: &finished, Providers: map[string]ProviderStatus{
		"ugc":       {State: ProviderFailed, ErrorCode: FailureProviderSync, Log: []string{failedLine}},
		"kinepolis": {State: ProviderSucceeded, Log: []string{"secret"}},
	}})
	if len(safe.Providers["ugc"].Log) != 1 || safe.Providers["kinepolis"].Log != nil {
		t.Fatalf("safe=%+v", safe.Providers)
	}
	input := Status{StartedAt: started, FinishedAt: &finished, Providers: map[string]ProviderStatus{"ugc": {State: ProviderFailed, Log: []string{failedLine}}}}
	copy := SanitizeStatus(input)
	copy.Providers["ugc"].Log[0] = "mutated"
	if input.Providers["ugc"].Log[0] != failedLine {
		t.Fatal("sanitized log aliases caller storage")
	}
}
