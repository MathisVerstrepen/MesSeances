package accounts

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGoogleOIDCAdapter(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	state, _, _ := NewToken(nil)
	nonce, _, _ := NewToken(nil)
	verifier, _, _ := NewToken(nil)
	var claims map[string]any
	badSignature := false
	var mode string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/keys":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/token":
			if r.ParseForm() != nil || r.Form.Get("code_verifier") != verifier || r.Form.Get("code") != "synthetic-code" || r.Form.Get("client_id") != "test-client" || r.Form.Get("redirect_uri") != "https://messeances.fr/api/v1/auth/google/callback" {
				http.Error(w, "invalid request", 400)
				return
			}
			if mode == "oversized" {
				_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
				return
			}
			if mode == "redirect" {
				http.Redirect(w, r, "/unexpected", http.StatusFound)
				return
			}
			header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "test"})
			payload, _ := json.Marshal(claims)
			unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
			digest := sha256.Sum256([]byte(unsigned))
			sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
			if e != nil {
				http.Error(w, "sign error", 500)
				return
			}
			if badSignature {
				sig[0] ^= 0xff
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "discard-me", "token_type": "Bearer", "id_token": unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)})
		default:
			t.Error("unexpected provider endpoint")
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	p := newGoogleProvider("test-client", "test-secret", "https://messeances.fr/api/v1/auth/google/callback", server.URL, server.URL+"/auth", server.URL+"/token", server.URL+"/keys", server.Client().Transport, func() time.Time { return now })
	reset := func() {
		claims = map[string]any{"iss": server.URL, "aud": "test-client", "sub": "SubjectCaseSensitive", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": nonce, "email": "Owner@Example.com", "email_verified": true}
		badSignature = false
		mode = ""
	}
	reset()
	authorization, err := p.AuthorizationURL(state, nonce, verifier)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authorization)
	q := u.Query()
	challenge := sha256.Sum256([]byte(verifier))
	if q.Get("scope") != "openid email" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(challenge[:]) || q.Get("state") != state || q.Get("nonce") != nonce || q.Get("prompt") != "select_account" || q.Has("access_type") {
		t.Fatal("authorization proof/scope mismatch")
	}
	identity, err := p.Exchange(context.Background(), "synthetic-code", verifier, nonce)
	if err != nil || identity.Subject != "SubjectCaseSensitive" || identity.Email != "owner@example.com" || !identity.EmailVerified || identity.EmailAuthoritative {
		t.Fatalf("valid identity rejected: %v", err)
	}
	for _, test := range []struct {
		name, email   string
		verified      bool
		hd            any
		authoritative bool
	}{
		{"verified_external", "owner@example.com", true, nil, false},
		{"external_empty_hd", "owner@example.com", true, "", false},
		{"external_blank_hd", "owner@example.com", true, " ", false},
		{"unverified_external", "owner@example.com", false, nil, false},
		{"verified_gmail", "Owner@GMAIL.COM", true, nil, true},
		{"unverified_gmail", "owner@gmail.com", false, nil, false},
		{"gmail_with_hd", "owner@gmail.com", true, "example.com", true},
		{"gmail_subdomain", "owner@sub.gmail.com", true, nil, false},
		{"gmail_suffix", "owner@gmail.com.example.com", true, nil, false},
		{"gmail_lookalike", "owner@notgmail.com", true, nil, false},
		{"verified_workspace", "owner@example.com", true, "example.com", true},
		{"workspace_alias", "owner@alias.example.com", true, "example.com", true},
		{"unverified_workspace", "owner@example.com", false, "example.com", false},
		{"invalid_email_workspace", "bad address", true, "example.com", false},
		{"missing_email_workspace", "", true, "example.com", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			reset()
			claims["email"], claims["email_verified"] = test.email, test.verified
			if test.hd != nil {
				claims["hd"] = test.hd
			}
			identity, err := p.Exchange(context.Background(), "synthetic-code", verifier, nonce)
			email, _ := NormalizeEmail(test.email)
			if err != nil || identity.Subject != "SubjectCaseSensitive" || identity.Email != email || identity.EmailVerified != (test.verified && email != "") || identity.EmailAuthoritative != test.authoritative {
				t.Fatalf("email authority: identity=%+v error=%v", identity, err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		change func()
	}{
		{"signature", func() { badSignature = true }},
		{"workspace_signature", func() { claims["hd"] = "example.com"; badSignature = true }},
		{"invalid_hd_type", func() { claims["hd"] = true }},
		{"issuer", func() { claims["iss"] = "https://evil.example" }},
		{"audience", func() { claims["aud"] = "another-client" }},
		{"azp", func() { claims["azp"] = "another-client" }},
		{"multiple_audiences_without_azp", func() { claims["aud"] = []string{"test-client", "other"} }},
		{"expiry", func() { claims["exp"] = now.Add(-time.Second).Unix() }},
		{"exact_expiry", func() { claims["exp"] = now.Unix() }},
		{"future_iat", func() { claims["iat"] = now.Add(2 * time.Minute).Unix() }},
		{"old_iat", func() { claims["iat"] = now.Add(-OAuthLifetime).Unix() }},
		{"missing_iat", func() { delete(claims, "iat") }},
		{"nonce", func() { claims["nonce"] = "wrong" }},
		{"subject", func() { claims["sub"] = "" }},
		{"oversized_subject", func() { claims["sub"] = strings.Repeat("x", 256) }},
		{"oversized_response", func() { mode = "oversized" }},
		{"redirect", func() { mode = "redirect" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			reset()
			test.change()
			if _, err := p.Exchange(context.Background(), "synthetic-code", verifier, nonce); err == nil {
				t.Fatal("invalid OIDC proof accepted")
			}
		})
	}
	reset()
	if _, err = p.Exchange(context.Background(), "synthetic-code", "wrong-verifier", nonce); err == nil {
		t.Fatal("PKCE failure accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = p.Exchange(ctx, "synthetic-code", verifier, nonce); err == nil {
		t.Fatal("canceled provider accepted")
	}
	for _, email := range []any{nil, "bad address"} {
		reset()
		claims["email"] = email
		identity, err = p.Exchange(context.Background(), "synthetic-code", verifier, nonce)
		if err != nil || identity.Email != "" || identity.EmailVerified || identity.EmailAuthoritative {
			t.Fatal("unusable email handling")
		}
	}
	reset()
	claims["email_verified"] = false
	identity, err = p.Exchange(context.Background(), "synthetic-code", verifier, nonce)
	if err != nil || identity.EmailVerified {
		t.Fatal("unverified email handling")
	}
}

func TestGoogleFixedEndpointsOffline(t *testing.T) {
	provider, err := NewGoogleProvider("client", "secret", "https://messeances.fr/api/v1/auth/google/callback")
	if err != nil {
		t.Fatal(err)
	}
	p := provider.(*googleProvider)
	if p.config.Endpoint.AuthURL != googleAuthorizationEndpoint || p.config.Endpoint.TokenURL != googleTokenEndpoint || p.client.Timeout != 10*time.Second {
		t.Fatal("fixed provider configuration")
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://untrusted.example/keys", nil)
	if err != nil {
		t.Fatal("test request construction failed")
	}
	response, err := p.client.Transport.RoundTrip(request)
	if response != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Error("unexpected response close failed")
		}
	}
	if err == nil {
		t.Fatal("untrusted endpoint allowed")
	}
}
