package accounts

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestTokensAndAddressKeys(t *testing.T) {
	token, digest, err := NewToken(nil)
	if err != nil || len(token) != 43 {
		t.Fatal("token generation failed")
	}
	got, err := TokenDigest(token)
	if err != nil || got != digest {
		t.Fatal("digest unstable")
	}
	other, _, err := NewToken(nil)
	if err != nil || token == other {
		t.Fatal("token randomness failed")
	}
	for _, raw := range []string{"", token + "=", strings.Repeat("!", 43), strings.Repeat("A", 42) + "B"} {
		if _, err := TokenDigest(raw); !errors.Is(err, ErrInvalidInput) {
			t.Error("invalid token accepted")
		}
	}
	if _, _, err := NewToken(bytes.NewReader(nil)); !errors.Is(err, ErrUnavailable) {
		t.Fatal("random failure not closed")
	}
	key := bytes.Repeat([]byte{1}, 32)
	a, err := AddressKey(key, "suppression", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := AddressKey(key, "login", "alice@example.com")
	if err != nil || a == b {
		t.Fatal("HMAC purpose separation failed")
	}
	if _, err := AddressKey(key, "login", "Alice@Example.com"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unnormalized address key accepted")
	}
}
