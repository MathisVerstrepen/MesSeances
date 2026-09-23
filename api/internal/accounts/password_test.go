package accounts

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestPasswordPolicy(t *testing.T) {
	for _, password := range []string{"a long unique passphrase", "unique-ten", strings.Repeat("😀", 10), strings.Repeat("😀", 128), "  leading and trailing  ", "no-composition-needed"} {
		if err := ValidatePassword(password); err != nil {
			t.Errorf("valid password rejected: %v", err)
		}
	}
	for _, password := range []string{"short", "ninechars", strings.Repeat("😀", 9), strings.Repeat("a", 129), strings.Repeat("😀", 129), "invalid utf8 string\xff", "passwordpassword", "123456789012345"} {
		if err := ValidatePassword(password); !errors.Is(err, ErrInvalidInput) {
			t.Error("invalid password accepted")
		}
	}
}

func TestArgonHasher(t *testing.T) {
	h, err := NewArgonHasher(nil)
	if err != nil {
		t.Fatal(err)
	}
	const password = "a unique long passphrase "
	encoded, err := h.Hash(context.Background(), password)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatal("unexpected parameters")
	}
	for _, tc := range []struct {
		password string
		want     bool
	}{{password, true}, {strings.TrimSpace(password), false}, {"wrong", false}} {
		match, rehash, err := h.Verify(context.Background(), tc.password, encoded)
		if err != nil || match != tc.want || rehash {
			t.Fatalf("match=%t rehash=%t err=%v", match, rehash, err)
		}
	}
	second, err := h.Hash(context.Background(), password)
	if err != nil || second == encoded {
		t.Fatal("salt not independently generated")
	}
	if err := h.Dummy(context.Background(), "wrong"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Hash(ctx, password); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	h.slots <- struct{}{}
	h.slots <- struct{}{}
	if _, err := h.Hash(context.Background(), password); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("admission not bounded: %v", err)
	}
	<-h.slots
	<-h.slots
}

func TestArgonLegacyRehashAndBoundedParser(t *testing.T) {
	salt := bytes.Repeat([]byte{1}, 16)
	key := argon2.IDKey([]byte("a long unique phrase"), salt, 1, 8192, 1, 32)
	encoded := fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
	h := &ArgonHasher{slots: make(chan struct{}, 2)}
	match, rehash, err := h.Verify(context.Background(), "a long unique phrase", encoded)
	if err != nil || !match || !rehash {
		t.Fatalf("legacy verify: %t/%t/%v", match, rehash, err)
	}
	for _, invalid := range []string{"", encoded + "$", encoded + "\n", strings.Replace(encoded, "v=19", "v=16", 1), strings.Replace(encoded, "m=8192", "m=4294967295", 1), strings.Replace(encoded, "t=1", "t=999", 1), strings.Replace(encoded, "p=1", "p=255", 1), strings.Replace(encoded, "m=8192", "m=08192", 1), strings.Replace(encoded, "p=1$", "p=1garbage$", 1), encoded[:len(encoded)-1], strings.Repeat("x", 10000)} {
		if _, _, _, err := parsePasswordHash(invalid); !errors.Is(err, ErrInvalidHash) {
			t.Error("malformed/unbounded hash accepted")
		}
	}
	if _, err := NewArgonHasher(bytes.NewReader(nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatal("randomness failure not closed")
	}
}

func BenchmarkArgonHash(b *testing.B) {
	h, err := NewArgonHasher(nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := h.Hash(context.Background(), "benchmark unique passphrase"); err != nil {
			b.Fatal(err)
		}
	}
}
