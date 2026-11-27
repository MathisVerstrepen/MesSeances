package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/accounts"
)

type digestSweepFunc func(context.Context) (accounts.DigestResult, error)

func (f digestSweepFunc) SweepDigests(ctx context.Context) (accounts.DigestResult, error) {
	return f(ctx)
}

func TestAccountDigestRuntimeSanitizedSerialCycles(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	calls := 0
	probe := digestSweepFunc(func(context.Context) (accounts.DigestResult, error) {
		calls++
		switch calls {
		case 1:
			return accounts.DigestResult{}, errors.New("user@example.com")
		case 2:
			return accounts.DigestResult{Sent: 3, Empty: 1}, nil
		}
		cancel()
		return accounts.DigestResult{}, nil
	})
	runAccountDigestCycles(ctx, probe, slog.New(slog.NewTextHandler(&output, nil)), time.Millisecond)
	text := output.String()
	if calls != 3 || !strings.Contains(text, "account_digests_failed") || !strings.Contains(text, "sent=3") || strings.Contains(text, "example.com") {
		t.Fatalf("calls=%d log=%q", calls, text)
	}
}
