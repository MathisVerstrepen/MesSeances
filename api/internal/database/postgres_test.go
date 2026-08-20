package database

import (
	"context"
	"strings"
	"testing"
)

func TestOpenPoolRejectsBlankAndRedactsInvalidURL(t *testing.T) {
	for _, value := range []string{"", "   "} {
		if _, err := OpenPool(context.Background(), value); err == nil || err.Error() != "database configuration is missing" {
			t.Fatalf("blank URL error=%v", err)
		}
	}
	secret := "synthetic-password"
	_, err := OpenPool(context.Background(), "postgres://user:"+secret+"@[")
	if err == nil || err.Error() != "database configuration is invalid" || strings.Contains(err.Error(), secret) {
		t.Fatalf("invalid URL error=%q", err)
	}
}

func TestEmbeddedMigrations(t *testing.T) {
	items, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 9 || items[0].version != 1 || items[0].name != "001_initial.sql" || items[1].version != 2 || items[1].name != "002_movie_enrichment.sql" || items[2].version != 3 || items[2].name != "003_admin_match_review.sql" || items[3].version != 4 || items[3].name != "004_movie_backdrop.sql" || items[4].version != 5 || items[4].name != "005_multi_provider.sql" || items[5].version != 6 || items[5].name != "006_repair_multi_provider_schema.sql" || items[6].version != 7 || items[6].name != "007_screening_formats.sql" || items[7].version != 8 || items[7].name != "008_local_movie_groups.sql" || items[8].version != 9 || items[8].name != "009_widen_runtime_minutes.sql" {
		t.Fatalf("migrations=%+v", items)
	}
}
