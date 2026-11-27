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

type pushDeliverFunc func(context.Context) (accounts.PushDeliveryResult, error)

func (f pushDeliverFunc) DeliverPush(ctx context.Context) (accounts.PushDeliveryResult, error) {
	return f(ctx)
}

func TestAccountPushRuntimeSanitizedSerialCycles(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	calls := 0
	probe := pushDeliverFunc(func(context.Context) (accounts.PushDeliveryResult, error) {
		calls++
		switch calls {
		case 1:
			return accounts.PushDeliveryResult{}, errors.New("https://fcm.googleapis.com/fcm/send/secret")
		case 2:
			return accounts.PushDeliveryResult{Sent: 2, Removed: 1}, nil
		}
		cancel()
		return accounts.PushDeliveryResult{}, nil
	})
	runAccountPushCycles(ctx, probe, slog.New(slog.NewTextHandler(&output, nil)), time.Millisecond)
	text := output.String()
	if calls != 3 || !strings.Contains(text, "account_push_failed") || !strings.Contains(text, "sent=2") || strings.Contains(text, "fcm") {
		t.Fatalf("calls=%d log=%q", calls, text)
	}
}
