package schedulepg

import (
	"context"
	"testing"
)

func TestReplaceRejectsDuplicateAndOversizedProviderBatchesBeforeDatabaseAccess(t *testing.T) {
	store := &Store{}
	pathe := patheTestDataset()
	if _, err := store.Replace(context.Background(), []Dataset{pathe, pathe}); err == nil || err.Error() != "invalid schedule replacement providers" {
		t.Fatalf("duplicate provider error=%v", err)
	}
	if _, err := store.Replace(context.Background(), []Dataset{testDataset(), kinepolisTestDataset(), pathe, testDataset()}); err == nil || err.Error() != "invalid schedule replacement batch" {
		t.Fatalf("oversized batch error=%v", err)
	}
}
