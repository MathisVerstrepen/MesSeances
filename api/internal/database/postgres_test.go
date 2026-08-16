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
	if len(items) != 1 || items[0].version != 1 || items[0].name != "001_initial.sql" {
		t.Fatalf("migrations=%+v", items)
	}
}
