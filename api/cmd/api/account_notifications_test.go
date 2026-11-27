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

type notificationSweepFunc func(context.Context) (accounts.NotificationSweepResult, error)

func (f notificationSweepFunc) SweepNotifications(ctx context.Context) (accounts.NotificationSweepResult, error) {
	return f(ctx)
}

func TestAccountNotificationRuntimeImmediateAndDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, done := make(chan struct{}), make(chan struct{})
	probe := notificationSweepFunc(func(ctx context.Context) (accounts.NotificationSweepResult, error) {
		close(started)
		<-ctx.Done()
		return accounts.NotificationSweepResult{}, ctx.Err()
	})
	go func() { defer close(done); runAccountNotifications(ctx, probe, slog.New(slog.DiscardHandler)) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("no immediate background cycle")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker not drained")
	}
}

func TestAccountNotificationRuntimeErrorSanitizationAndSerialCycles(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	calls := 0
	probe := notificationSweepFunc(func(context.Context) (accounts.NotificationSweepResult, error) {
		calls++
		switch calls {
		case 1:
			return accounts.NotificationSweepResult{}, errors.New("private account and title")
		case 2:
			return accounts.NotificationSweepResult{Events: 3, Notifications: 2}, nil
		}
		cancel()
		return accounts.NotificationSweepResult{}, nil
	})
	runAccountNotificationCycles(ctx, probe, logger, time.Millisecond)
	text := output.String()
	if calls != 3 || !strings.Contains(text, "account_notifications_failed") || !strings.Contains(text, "events=3") || strings.Contains(text, "private") {
		t.Fatalf("calls=%d log=%q", calls, text)
	}
}
