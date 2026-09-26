package accounts

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"strings"
)

type Digest [sha256.Size]byte

// NewToken returns a 256-bit bearer token and the only representation stored in
// session/token tables. A nil source selects crypto/rand.Reader.
func NewToken(source io.Reader) (string, Digest, error) {
	if source == nil {
		source = rand.Reader
	}
	var raw [32]byte
	if _, err := io.ReadFull(source, raw[:]); err != nil {
		return "", Digest{}, ErrUnavailable
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	digest, err := TokenDigest(token)
	return token, digest, err
}

func TokenDigest(token string) (Digest, error) {
	if len(token) != 43 {
		return Digest{}, ErrInvalidInput
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != 32 {
		return Digest{}, ErrInvalidInput
	}
	return sha256.Sum256([]byte(token)), nil
}

// AddressKey uses purpose separation so suppression and quota records cannot be
// correlated by comparing digests. The key must be independent of encryption.
func AddressKey(key []byte, purpose, normalizedEmail string) (Digest, error) {
	if len(key) != 32 || purpose == "" || strings.ContainsRune(purpose, 0) {
		return Digest{}, ErrInvalidInput
	}
	email, err := NormalizeEmail(normalizedEmail)
	if err != nil || email != normalizedEmail {
		return Digest{}, ErrInvalidInput
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte("messeances-address-v1\x00" + purpose + "\x00" + email))
	var digest Digest
	copy(digest[:], h.Sum(nil))
	return digest, nil
}
