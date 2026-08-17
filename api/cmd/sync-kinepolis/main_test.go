package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) }
func runTest(t *testing.T, args []string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runWithIO(context.Background(), args, fixedNow, &stdout, &stderr)
	return code, stderr.String()
}

func TestRunRejectsMissingAndInvalidProxyBeforeDatabaseOrNetwork(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if code, stderr := runTest(t, nil); code != 2 || !strings.Contains(stderr, "proxy-file is required") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	missing := filepath.Join(t.TempDir(), "missing.txt")
	if code, stderr := runTest(t, []string{"-proxy-file", missing}); code != 2 || !strings.Contains(stderr, "proxy file unavailable") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	secret := "synthetic-password"
	invalid := filepath.Join(t.TempDir(), "invalid.txt")
	if err := os.WriteFile(invalid, []byte("http://user:"+secret+"@missing-port\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, stderr := runTest(t, []string{"-proxy-file", invalid}); code != 2 || !strings.Contains(stderr, "proxy file is invalid") || strings.Contains(stderr, secret) {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunAcceptsValidProxyThenRequiresDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	proxyFile := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(proxyFile, []byte("127.0.0.1:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code, stderr := runTest(t, []string{"-proxy-file", proxyFile})
	if code != 2 || !strings.Contains(stderr, "DATABASE_URL is required") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunRejectsOtherInvalidConfiguration(t *testing.T) {
	for _, args := range [][]string{{"-from", "invalid"}, {"-from", "2026-08-15", "-through", "2026-09-15"}, {"unexpected"}} {
		if code, _ := runTest(t, args); code != 2 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
}

func TestMakeSyncOrchestratesBothProvidersWithSameProxyFile(t *testing.T) {
	body, err := os.ReadFile("../../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "sync-kinepolis:") {
		t.Fatal("public sync-kinepolis target still present")
	}
	ugc := "go run ./cmd/sync-ugc -proxy-file \"$$PROXY_FILE\""
	kinepolis := "go run ./cmd/sync-kinepolis -proxy-file \"$$PROXY_FILE\""
	ugcIndex, kinepolisIndex := strings.Index(text, ugc), strings.Index(text, kinepolis)
	if ugcIndex < 0 || kinepolisIndex < 0 || ugcIndex >= kinepolisIndex {
		t.Fatalf("sync orchestration incorrect: ugc=%d kinepolis=%d", ugcIndex, kinepolisIndex)
	}
	if strings.Count(text, "Usage: make sync PROXY_FILE=/path/to/proxies.txt") != 1 {
		t.Fatal("PROXY_FILE validation missing or duplicated")
	}
}
