package accountmail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"regexp"
	"sync"
)

var ErrPayload = errors.New("invalid encrypted account payload")

const MaxPayloadBytes = 16 * 1024

type Envelope struct {
	KeyID      string
	Nonce      []byte
	Ciphertext []byte
}

// Cipher also protects short-lived OAuth PKCE verifiers. Bind each envelope to
// its table, logical event ID and purpose; a ciphertext cannot move between rows.
// New instances encrypt with one key; Open rejects other IDs rather than guessing.
type Cipher struct {
	keyID  string
	aead   cipher.AEAD
	random io.Reader
	mu     sync.Mutex
}

func NewCipher(keyID string, key []byte, source io.Reader) (*Cipher, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(keyID) || len(key) != 32 {
		return nil, ErrPayload
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrPayload
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrPayload
	}
	if source == nil {
		source = rand.Reader
	}
	return &Cipher{keyID: keyID, aead: aead, random: source}, nil
}

func (c *Cipher) Seal(binding string, plaintext []byte) (Envelope, error) {
	if binding == "" || len(binding) > 1024 || len(plaintext) == 0 || len(plaintext) > MaxPayloadBytes {
		return Envelope{}, ErrPayload
	}
	nonce := make([]byte, c.aead.NonceSize())
	c.mu.Lock()
	_, err := io.ReadFull(c.random, nonce)
	c.mu.Unlock()
	if err != nil {
		return Envelope{}, ErrPayload
	}
	ciphertext := c.aead.Seal(nil, nonce, plaintext, c.aad(binding))
	return Envelope{KeyID: c.keyID, Nonce: nonce, Ciphertext: ciphertext}, nil
}

func (c *Cipher) Open(binding string, envelope Envelope) ([]byte, error) {
	if binding == "" || len(binding) > 1024 || envelope.KeyID != c.keyID || len(envelope.Nonce) != c.aead.NonceSize() || len(envelope.Ciphertext) <= c.aead.Overhead() || len(envelope.Ciphertext) > MaxPayloadBytes+c.aead.Overhead() {
		return nil, ErrPayload
	}
	plaintext, err := c.aead.Open(nil, envelope.Nonce, envelope.Ciphertext, c.aad(binding))
	if err != nil {
		return nil, ErrPayload
	}
	return plaintext, nil
}

func (c *Cipher) aad(binding string) []byte {
	return []byte("messeances-payload-v1\x00" + c.keyID + "\x00" + binding)
}
