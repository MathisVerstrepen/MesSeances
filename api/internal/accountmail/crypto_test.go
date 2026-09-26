package accountmail

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncryptedPayload(t *testing.T) {
	c, err := NewCipher("key-1", bytes.Repeat([]byte{1}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	binding := "account_mail_outbox:synthetic-event:verification"
	plaintext := []byte(`{"recipient":"alice@example.com","token":"synthetic-secret"}`)
	envelope, err := c.Seal(binding, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(envelope.Ciphertext, plaintext) || len(envelope.Nonce) != 12 {
		t.Fatal("invalid encryption")
	}
	got, err := c.Open(binding, envelope)
	if err != nil || !bytes.Equal(got, plaintext) {
		t.Fatal("round trip failed")
	}
	second, err := c.Seal(binding, plaintext)
	if err != nil || bytes.Equal(envelope.Nonce, second.Nonce) {
		t.Fatal("nonce reused")
	}
	if _, err := c.Open("account_oauth_flows:synthetic-event", envelope); !errors.Is(err, ErrPayload) {
		t.Fatal("cross-context replay accepted")
	}
	envelope.Ciphertext[0] ^= 1
	if _, err := c.Open(binding, envelope); !errors.Is(err, ErrPayload) {
		t.Fatal("tampering accepted")
	}
	for _, e := range []Envelope{{KeyID: "other", Nonce: second.Nonce, Ciphertext: second.Ciphertext}, {KeyID: "key-1", Nonce: []byte{1}, Ciphertext: second.Ciphertext}, {KeyID: "key-1", Nonce: second.Nonce, Ciphertext: []byte{1}}} {
		if _, err := c.Open(binding, e); !errors.Is(err, ErrPayload) {
			t.Fatal("invalid envelope accepted")
		}
	}
	if _, err := c.Seal(binding, bytes.Repeat([]byte{1}, MaxPayloadBytes+1)); !errors.Is(err, ErrPayload) {
		t.Fatal("payload bound missing")
	}
	if _, err := NewCipher("key-1", []byte{1}, nil); !errors.Is(err, ErrPayload) {
		t.Fatal("short key accepted")
	}
	c, err = NewCipher("key-1", bytes.Repeat([]byte{1}, 32), bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Seal(binding, plaintext); !errors.Is(err, ErrPayload) {
		t.Fatal("random failure accepted")
	}
}
