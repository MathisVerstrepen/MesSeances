// Package webpush sends RFC 8030 push messages encrypted with RFC 8291
// (aes128gcm) and authenticated with RFC 8292 VAPID, using only the standard
// library. Endpoints are restricted to known browser push services.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidSubscription = errors.New("invalid push subscription")
	ErrInvalidKey          = errors.New("invalid VAPID key")
	// ErrGone means the subscription is permanently invalid and must be removed.
	ErrGone = errors.New("push subscription gone")
	// ErrRejected is a permanent failure for this message only.
	ErrRejected = errors.New("push message rejected")
	// ErrRetry is a transient failure (throttling, 5xx or transport).
	ErrRetry = errors.New("push delivery temporarily failed")
)

const (
	recordSize      = 4096
	maxPayload      = 3000
	maxEndpointSize = 2048
)

// Push services accepted as endpoints. A suffix match requires a dot boundary,
// so only these operators can ever receive server-originated requests.
var allowedHosts = []string{
	"fcm.googleapis.com",
	"push.services.mozilla.com",
	"push.apple.com",
	"notify.windows.com",
}

var b64 = base64.RawURLEncoding

// Subscription is the browser PushSubscription with decoded keys.
type Subscription struct {
	Endpoint string
	P256DH   []byte
	Auth     []byte
}

// ParseSubscription validates a browser subscription. Keys are base64url,
// with or without padding, as browsers produce either form.
func ParseSubscription(endpoint, p256dh, auth string) (Subscription, error) {
	if err := ValidateEndpoint(endpoint); err != nil {
		return Subscription{}, err
	}
	key, err := decodeKey(p256dh)
	if err != nil || len(key) != 65 {
		return Subscription{}, ErrInvalidSubscription
	}
	if _, err := ecdh.P256().NewPublicKey(key); err != nil {
		return Subscription{}, ErrInvalidSubscription
	}
	secret, err := decodeKey(auth)
	if err != nil || len(secret) != 16 {
		return Subscription{}, ErrInvalidSubscription
	}
	return Subscription{Endpoint: endpoint, P256DH: key, Auth: secret}, nil
}

func decodeKey(raw string) ([]byte, error) {
	if len(raw) > 200 {
		return nil, ErrInvalidSubscription
	}
	return b64.DecodeString(strings.TrimRight(raw, "="))
}

// ValidateEndpoint accepts only https URLs on an allowed push service, without
// credentials, explicit ports or fragments.
func ValidateEndpoint(endpoint string) error {
	if len(endpoint) == 0 || len(endpoint) > maxEndpointSize || strings.ContainsAny(endpoint, " \t\r\n\\") {
		return ErrInvalidSubscription
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" || u.Opaque != "" || u.Path == "" {
		return ErrInvalidSubscription
	}
	host := strings.ToLower(u.Hostname())
	for _, allowed := range allowedHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return nil
		}
	}
	return ErrInvalidSubscription
}

// VAPID holds the application server identity.
type VAPID struct {
	private   *ecdsa.PrivateKey
	publicB64 string
	subject   string
}

// NewVAPID parses a base64url P-256 private scalar and its uncompressed public
// key, and checks that they form a pair. Subject is a mailto: or https: URI.
func NewVAPID(publicKey, privateKey, subject string) (*VAPID, error) {
	pub, err := b64.DecodeString(publicKey)
	if err != nil || len(pub) != 65 {
		return nil, ErrInvalidKey
	}
	scalar, err := b64.DecodeString(privateKey)
	if err != nil || len(scalar) != 32 {
		return nil, ErrInvalidKey
	}
	priv, err := ecdh.P256().NewPrivateKey(scalar)
	if err != nil || !bytes.Equal(priv.PublicKey().Bytes(), pub) {
		return nil, ErrInvalidKey
	}
	if !validSubject(subject) {
		return nil, ErrInvalidKey
	}
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
	if err != nil {
		return nil, ErrInvalidKey
	}
	return &VAPID{private: key, publicB64: publicKey, subject: subject}, nil
}

func validSubject(subject string) bool {
	if len(subject) > 256 || strings.ContainsAny(subject, " \t\r\n") {
		return false
	}
	u, err := url.Parse(subject)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "mailto":
		return strings.Contains(u.Opaque, "@")
	case "https":
		return u.Host != ""
	}
	return false
}

// PublicKey is the applicationServerKey given to browsers.
func (v *VAPID) PublicKey() string { return v.publicB64 }

func (v *VAPID) authorization(endpoint string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", ErrInvalidSubscription
	}
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}{u.Scheme + "://" + u.Host, now.Add(12 * time.Hour).Unix(), v.subject})
	if err != nil {
		return "", err
	}
	unsigned := header + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, v.private, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return "vapid t=" + unsigned + "." + b64.EncodeToString(signature) + ", k=" + v.publicB64, nil
}

// Encrypt produces an RFC 8291 aes128gcm body with one record. The ephemeral
// key and salt are injectable for deterministic tests.
func Encrypt(sub Subscription, plaintext []byte, ephemeral *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(plaintext) > maxPayload || len(salt) != 16 {
		return nil, ErrRejected
	}
	userKey, err := ecdh.P256().NewPublicKey(sub.P256DH)
	if err != nil || len(sub.Auth) != 16 {
		return nil, ErrInvalidSubscription
	}
	shared, err := ephemeral.ECDH(userKey)
	if err != nil {
		return nil, ErrInvalidSubscription
	}
	serverPublic := ephemeral.PublicKey().Bytes()
	keyInfo := append(append([]byte("WebPush: info\x00"), sub.P256DH...), serverPublic...)
	ikm, err := hkdf.Key(sha256.New, shared, sub.Auth, string(keyInfo), 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	record := append(append([]byte{}, plaintext...), 0x02) // last-record delimiter
	header := make([]byte, 0, 16+4+1+len(serverPublic))
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, recordSize)
	header = append(header, byte(len(serverPublic)))
	header = append(header, serverPublic...)
	return gcm.Seal(header, nonce, record, nil), nil
}

// Message is one push request. Topic lets the push service replace an
// undelivered earlier message with the same topic.
type Message struct {
	Payload []byte
	TTL     time.Duration
	Topic   string
	Urgency string
}

type Sender struct {
	VAPID  *VAPID
	Client *http.Client
	Now    func() time.Time
}

// NewSender returns a sender whose client never follows redirects.
func NewSender(v *VAPID) *Sender {
	return &Sender{VAPID: v, Now: time.Now, Client: &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (s *Sender) Send(ctx context.Context, sub Subscription, msg Message) error {
	if s == nil || s.VAPID == nil || s.Client == nil {
		return ErrRetry
	}
	if err := ValidateEndpoint(sub.Endpoint); err != nil {
		return ErrGone
	}
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return ErrRetry
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return ErrRetry
	}
	body, err := Encrypt(sub, msg.Payload, ephemeral, salt)
	if errors.Is(err, ErrInvalidSubscription) {
		return ErrGone
	}
	if err != nil {
		return ErrRejected
	}
	auth, err := s.VAPID.authorization(sub.Endpoint, s.Now())
	if err != nil {
		return ErrRetry
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return ErrGone
	}
	ttl := max(0, int64(msg.TTL/time.Second))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", strconv.FormatInt(ttl, 10))
	req.Header.Set("Authorization", auth)
	if msg.Urgency != "" {
		req.Header.Set("Urgency", msg.Urgency)
	}
	if msg.Topic != "" {
		req.Header.Set("Topic", msg.Topic)
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return ErrRetry
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	return classify(resp.StatusCode)
}

func classify(status int) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusNotFound || status == http.StatusGone:
		return ErrGone
	case status == http.StatusTooManyRequests || status >= 500:
		return ErrRetry
	default:
		return ErrRejected
	}
}

// GenerateKeys returns a new base64url (public, private) VAPID pair.
func GenerateKeys() (string, string, error) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return b64.EncodeToString(key.PublicKey().Bytes()), b64.EncodeToString(key.Bytes()), nil
}
