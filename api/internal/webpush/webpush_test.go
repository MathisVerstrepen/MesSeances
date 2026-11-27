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
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	v, err := b64.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// RFC 8291 Appendix A.
func TestEncryptRFC8291Vector(t *testing.T) {
	asPrivate, err := ecdh.P256().NewPrivateKey(mustB64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	sub := Subscription{
		Endpoint: "https://fcm.googleapis.com/fcm/send/x",
		P256DH:   mustB64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		Auth:     mustB64(t, "BTBZMqHH6r4Tts7J_aSIgg"),
	}
	body, err := Encrypt(sub, []byte("When I grow up, I want to be a watermelon"), asPrivate, mustB64(t, "DGv6ra1nlYgDCS1FRnbzlw"))
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if got := b64.EncodeToString(body); got != want {
		t.Fatalf("body\n got %s\nwant %s", got, want)
	}
	uaPrivate, err := ecdh.P256().NewPrivateKey(mustB64(t, "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"))
	if err != nil {
		t.Fatal(err)
	}
	if plain := decrypt(t, uaPrivate, sub.Auth, body); string(plain) != "When I grow up, I want to be a watermelon" {
		t.Fatalf("decrypt %q", plain)
	}
}

// decrypt is an independent user-agent implementation of RFC 8291.
func decrypt(t *testing.T, ua *ecdh.PrivateKey, auth, body []byte) []byte {
	t.Helper()
	salt, rs, idlen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	keyID := body[21 : 21+idlen]
	if rs != 4096 || idlen != 65 {
		t.Fatalf("header rs=%d idlen=%d", rs, idlen)
	}
	server, err := ecdh.P256().NewPublicKey(keyID)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := ua.ECDH(server)
	if err != nil {
		t.Fatal(err)
	}
	info := append(append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...), keyID...)
	ikm, _ := hkdf.Key(sha256.New, shared, auth, string(info), 32)
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain[len(plain)-1] != 0x02 {
		t.Fatal("missing last-record delimiter")
	}
	return plain[:len(plain)-1]
}

func TestEndpointAllowlist(t *testing.T) {
	for _, ok := range []string{
		"https://fcm.googleapis.com/fcm/send/abc:def",
		"https://updates.push.services.mozilla.com/wpush/v2/gAAA",
		"https://web.push.apple.com/QGuQ",
		"https://wns2-par02p.notify.windows.com/w/?token=x",
	} {
		if ValidateEndpoint(ok) != nil {
			t.Fatalf("rejected %s", ok)
		}
	}
	for _, bad := range []string{
		"", "http://fcm.googleapis.com/fcm/send/x", "https://evilfcm.googleapis.com.example/x",
		"https://fcm.googleapis.com.evil.example/x", "https://localhost/x", "https://127.0.0.1/x",
		"https://user@fcm.googleapis.com/x", "https://fcm.googleapis.com:8443/x", "https://fcm.googleapis.com/x#f",
		"https://fcm.googleapis.com", "https://xfcm.googleapis.com/x", "https://fcm.googleapis.com/" + strings.Repeat("a", 2048),
	} {
		if ValidateEndpoint(bad) == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestParseSubscription(t *testing.T) {
	ua, _ := ecdh.P256().GenerateKey(nil)
	key := b64.EncodeToString(ua.PublicKey().Bytes())
	auth := b64.EncodeToString(bytes.Repeat([]byte{7}, 16))
	if _, err := ParseSubscription("https://fcm.googleapis.com/fcm/send/x", key+"=", auth+"=="); err != nil {
		t.Fatal("padded keys rejected", err)
	}
	for name, args := range map[string][3]string{
		"short auth":  {"https://fcm.googleapis.com/fcm/send/x", key, b64.EncodeToString([]byte("short"))},
		"bad point":   {"https://fcm.googleapis.com/fcm/send/x", b64.EncodeToString(bytes.Repeat([]byte{4}, 65)), auth},
		"bad base64":  {"https://fcm.googleapis.com/fcm/send/x", "*", auth},
		"bad service": {"https://example.com/push", key, auth},
	} {
		if _, err := ParseSubscription(args[0], args[1], args[2]); !errors.Is(err, ErrInvalidSubscription) {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestVAPIDKeysAndAuthorization(t *testing.T) {
	public, private, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVAPID(public, private, "mailto:ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	otherPublic, _, _ := GenerateKeys()
	for name, args := range map[string][3]string{
		"mismatch": {otherPublic, private, "mailto:ops@example.com"},
		"subject":  {public, private, "ops@example.com"},
		"http":     {public, private, "http://example.com"},
		"short":    {public, private[:10], "https://example.com"},
	} {
		if _, err := NewVAPID(args[0], args[1], args[2]); !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("%s accepted", name)
		}
	}
	now := time.Unix(1_800_000_000, 0)
	header, err := v.authorization("https://fcm.googleapis.com/fcm/send/abc", now)
	if err != nil {
		t.Fatal(err)
	}
	token, key, ok := strings.Cut(strings.TrimPrefix(header, "vapid t="), ", k=")
	if !ok || key != public {
		t.Fatal(header)
	}
	parts := strings.Split(token, ".")
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	if len(parts) != 3 || json.Unmarshal(mustB64(t, parts[1]), &claims) != nil || claims.Aud != "https://fcm.googleapis.com" || claims.Exp != now.Add(12*time.Hour).Unix() || claims.Sub != "mailto:ops@example.com" {
		t.Fatalf("claims %+v", claims)
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), mustB64(t, public))
	if err != nil {
		t.Fatal(err)
	}
	sig := mustB64(t, parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("signature")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSendHeadersAndClassification(t *testing.T) {
	public, private, _ := GenerateKeys()
	v, _ := NewVAPID(public, private, "https://messeances.fr")
	ua, _ := ecdh.P256().GenerateKey(nil)
	auth := bytes.Repeat([]byte{1}, 16)
	sub := Subscription{Endpoint: "https://fcm.googleapis.com/fcm/send/abc", P256DH: ua.PublicKey().Bytes(), Auth: auth}
	for _, tc := range []struct {
		status int
		want   error
	}{{201, nil}, {404, ErrGone}, {410, ErrGone}, {429, ErrRetry}, {503, ErrRetry}, {400, ErrRejected}, {413, ErrRejected}, {301, ErrRejected}} {
		s := NewSender(v)
		var seen *http.Request
		var body []byte
		s.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			seen = r
			body, _ = io.ReadAll(r.Body)
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		})
		err := s.Send(context.Background(), sub, Message{Payload: []byte(`{"title":"x"}`), TTL: time.Hour, Topic: "rule-7", Urgency: "normal"})
		if !errors.Is(err, tc.want) {
			t.Fatalf("status %d: %v", tc.status, err)
		}
		if seen.Header.Get("TTL") != "3600" || seen.Header.Get("Topic") != "rule-7" || seen.Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(seen.Header.Get("Authorization"), "vapid t=") {
			t.Fatalf("headers %v", seen.Header)
		}
		if string(decrypt(t, ua, auth, body)) != `{"title":"x"}` {
			t.Fatal("payload")
		}
	}
	s := NewSender(v)
	s.Client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("dial") })
	if err := s.Send(context.Background(), sub, Message{Payload: []byte("x")}); !errors.Is(err, ErrRetry) {
		t.Fatal("transport", err)
	}
	if err := s.Send(context.Background(), Subscription{Endpoint: "https://example.com/x", P256DH: sub.P256DH, Auth: auth}, Message{}); !errors.Is(err, ErrGone) {
		t.Fatal("disallowed endpoint", err)
	}
	if err := s.Send(context.Background(), sub, Message{Payload: bytes.Repeat([]byte("a"), 3001)}); !errors.Is(err, ErrRejected) {
		t.Fatal("oversized", err)
	}
}
