package accounts

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const googleIssuer = "https://accounts.google.com"
const googleAuthorizationEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"
const googleTokenEndpoint = "https://oauth2.googleapis.com/token"
const googleKeysEndpoint = "https://www.googleapis.com/oauth2/v3/certs"

type googleProvider struct {
	config   oauth2.Config
	client   *http.Client
	verifier *oidc.IDTokenVerifier
	now      func() time.Time
}

// NewGoogleProvider uses fixed documented endpoints, not discovery-controlled or
// caller-supplied hosts. Construction is offline; provider outages affect callbacks only.
func NewGoogleProvider(clientID, secret, callback string) (GoogleProvider, error) {
	if clientID == "" || secret == "" || callback == "" {
		return nil, ErrUnavailable
	}
	return newGoogleProvider(clientID, secret, callback, googleIssuer, googleAuthorizationEndpoint, googleTokenEndpoint, googleKeysEndpoint, http.DefaultTransport, time.Now), nil
}

// Endpoint injection is private and used only by local issuer/JWKS tests.
func newGoogleProvider(clientID, secret, callback, issuer, auth, token, keys string, transport http.RoundTripper, now func() time.Time) *googleProvider {
	client := &http.Client{Timeout: 10 * time.Second, Transport: boundedGoogleTransport{base: transport, token: token, keys: keys}, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable }}
	ctx := oidc.ClientContext(context.Background(), client)
	return &googleProvider{
		config: oauth2.Config{ClientID: clientID, ClientSecret: secret, RedirectURL: callback, Scopes: []string{oidc.ScopeOpenID, "email"}, Endpoint: oauth2.Endpoint{AuthURL: auth, TokenURL: token, AuthStyle: oauth2.AuthStyleInParams}},
		client: client, now: now,
		verifier: oidc.NewVerifier(issuer, oidc.NewRemoteKeySet(ctx, keys), &oidc.Config{ClientID: clientID, SupportedSigningAlgs: []string{oidc.RS256}, Now: now}),
	}
}

type boundedGoogleTransport struct {
	base        http.RoundTripper
	token, keys string
}

func (t boundedGoogleTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != t.token && r.URL.String() != t.keys {
		return nil, ErrUnavailable
	}
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, ErrUnavailable
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(body) > 1<<20 {
		return nil, ErrUnavailable
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

func (p *googleProvider) AuthorizationURL(state, nonce, verifier string) (string, error) {
	for _, value := range []string{state, nonce, verifier} {
		if _, err := TokenDigest(value); err != nil {
			return "", ErrInvalidInput
		}
	}
	return p.config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("nonce", nonce), oauth2.SetAuthURLParam("prompt", "select_account")), nil
}

func (p *googleProvider) Exchange(ctx context.Context, code, verifier, nonce string) (GoogleIdentity, error) {
	if code == "" || len(code) > 4096 {
		return GoogleIdentity{}, ErrInvalidLink
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.client)
	token, err := p.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return GoogleIdentity{}, ErrInvalidLink
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 16384 {
		return GoogleIdentity{}, ErrInvalidLink
	}
	id, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return GoogleIdentity{}, ErrInvalidLink
	}
	var claims struct {
		Email           string `json:"email"`
		Verified        bool   `json:"email_verified"`
		AuthorizedParty string `json:"azp"`
	}
	if err = id.Claims(&claims); err != nil {
		return GoogleIdentity{}, ErrInvalidLink
	}
	now := p.now()
	if id.Nonce != nonce || nonce == "" || id.Subject == "" || len(id.Subject) > 255 || !now.Before(id.Expiry) || id.IssuedAt.IsZero() || id.IssuedAt.After(now.Add(time.Minute)) || !now.Before(id.IssuedAt.Add(OAuthLifetime)) || (claims.AuthorizedParty != "" && claims.AuthorizedParty != p.config.ClientID) || (len(id.Audience) > 1 && claims.AuthorizedParty != p.config.ClientID) {
		return GoogleIdentity{}, ErrInvalidLink
	}
	email, _ := NormalizeEmail(claims.Email) // Missing/unusable email cannot create a new account.
	return GoogleIdentity{Subject: id.Subject, Email: email, EmailVerified: claims.Verified && email != ""}, nil
}
