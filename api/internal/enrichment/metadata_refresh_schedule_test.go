package enrichment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMetadataRefreshManagerScheduledStartClaimsAndCompletes(t *testing.T) {
	store := &metadataRefreshStore{metadata: map[int64]Metadata{}}
	provider := &managedMetadataProvider{started: make(chan struct{}), release: make(chan struct{})}
	manager, err := NewMetadataRefreshManager(context.Background(), NewMetadataRefreshService(store, provider, time.Now, nil), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	claims := 0
	completion, err := manager.StartScheduled(func(context.Context) (bool, error) {
		claims++
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-completion:
		if !result.Succeeded || claims != 1 {
			t.Fatalf("result=%+v claims=%d", result, claims)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduled refresh did not complete")
	}
	status := manager.Snapshot()
	if status == nil || status.State != MetadataRefreshSucceeded {
		t.Fatalf("status=%+v", status)
	}
}

func TestMetadataRefreshManagerRejectedClaimPreservesStatusAndReleasesGate(t *testing.T) {
	store := &metadataRefreshStore{metadata: map[int64]Metadata{}}
	manager, err := NewMetadataRefreshManager(context.Background(), NewMetadataRefreshService(store, &managedMetadataProvider{started: make(chan struct{}), release: make(chan struct{})}, time.Now, nil), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	previous := waitForMetadataRefreshState(t, manager, MetadataRefreshSucceeded)
	if _, err := manager.StartScheduled(func(context.Context) (bool, error) { return false, nil }); !errors.Is(err, ErrMetadataRefreshOccurrenceClaimed) {
		t.Fatalf("duplicate claim error=%v", err)
	}
	if got := manager.Snapshot(); got == nil || got.StartedAt != previous.StartedAt || got.State != previous.State {
		t.Fatalf("status changed after duplicate claim: previous=%+v got=%+v", previous, got)
	}
	if _, err := manager.StartScheduled(func(context.Context) (bool, error) { return false, errors.New("database secret") }); !errors.Is(err, ErrMetadataRefreshUnavailable) {
		t.Fatalf("claim failure error=%v", err)
	}
	completion, err := manager.StartScheduled(func(context.Context) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("gate not released: %v", err)
	}
	if result := <-completion; !result.Succeeded {
		t.Fatalf("result=%+v", result)
	}
}
