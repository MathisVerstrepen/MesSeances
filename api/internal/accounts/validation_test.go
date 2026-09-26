package accounts

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestReservedUsernameManifest(t *testing.T) {
	names, err := reservedUsernames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 869 {
		t.Fatalf("count=%d, want 869", len(names))
	}
	canonical, err := json.Marshal(names)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(canonical)); got != "76699aebd2ceca3d36faeb80049a0e58e6e97676b7223485805de6eba6430401" {
		t.Fatalf("reservation manifest changed: %s", got)
	}
	invalid := 0
	for _, name := range names {
		if !usernameSyntax.MatchString(name) {
			invalid++
			continue
		}
		if _, err := NormalizeUsername(strings.ToUpper(name)); !errors.Is(err, ErrUsernameUnavailable) {
			t.Fatalf("reserved username accepted: %q", name)
		}
	}
	if invalid != 90 {
		t.Fatalf("invalid-source terms=%d, want 90 retained", invalid)
	}
}

func TestNormalizeUsername(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
		err       error
	}{
		{"Alice_123", "alice_123", nil}, {"abc", "abc", nil}, {"a" + strings.Repeat("1", 29), "a" + strings.Repeat("1", 29), nil},
		{"ab", "", ErrInvalidInput}, {"a" + strings.Repeat("1", 30), "", ErrInvalidInput}, {"1alice", "", ErrInvalidInput},
		{" alice ", "", ErrInvalidInput}, {"alice-name", "", ErrInvalidInput}, {"élise", "", ErrInvalidInput}, {"Alİce", "", ErrInvalidInput},
		{"MESSEANCES", "", ErrUsernameUnavailable}, {"LensAdmin", "", ErrUsernameUnavailable}, {"securite", "", ErrUsernameUnavailable},
	} {
		got, err := NormalizeUsername(tc.raw)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("%q: got %q/%v want %q/%v", tc.raw, got, err, tc.want, tc.err)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	for _, raw := range []string{" Alice.Test+Tag@Example.COM ", "alice.test+tag@example.com"} {
		got, err := NormalizeEmail(raw)
		if err != nil || got != "alice.test+tag@example.com" {
			t.Fatalf("got %q/%v", got, err)
		}
	}
	for _, raw := range []string{"", "a@b", "a@@example.com", "A <a@example.com>", "a@example.com,b@example.com", "a\r\n@example.com", "\ta@example.com", "élise@example.com", "a@éxample.com", "a@-example.com", "a@example-.com", "a@example..com", ".a@example.com", "a.@example.com", "a..b@example.com", `"a"@example.com`, "a@[127.0.0.1]", strings.Repeat("a", 65) + "@example.com", "a@" + strings.Repeat("a", 64) + ".com", "a@example.com."} {
		if _, err := NormalizeEmail(raw); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid mailbox accepted: %q", raw)
		}
	}
}

func TestSessionViewContract(t *testing.T) {
	encoded, err := json.Marshal(SessionView{State: StateAnonymous})
	if err != nil || string(encoded) != `{"enabled":false,"state":"anonymous","account":null}` {
		t.Fatalf("DTO mismatch: %s/%v", encoded, err)
	}
	encoded, err = json.Marshal(AccountView{Email: "alice@example.com"})
	if err != nil || string(encoded) != `{"email":"alice@example.com","username":null,"has_password":false,"google_linked":false}` {
		t.Fatalf("account DTO mismatch: %s/%v", encoded, err)
	}
	username := "alice_123"
	googleEmail := "google@example.com"
	encoded, err = json.Marshal(AccountDetails{
		AccountView: AccountView{Email: "alice@example.com", Username: &username, HasPassword: true, GoogleLinked: true},
		GoogleEmail: &googleEmail, AllowedMethods: []LoginMethod{LoginPassword, LoginGoogle},
	})
	if err != nil || string(encoded) != `{"email":"alice@example.com","username":"alice_123","has_password":true,"google_linked":true,"avatar_url":null,"google_email":"google@example.com","pending_email":null,"allowed_methods":["password","google"]}` {
		t.Fatalf("details DTO mismatch: %s/%v", encoded, err)
	}
}
