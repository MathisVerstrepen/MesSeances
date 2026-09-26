package accounts

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory      uint32 = 64 * 1024
	passwordIterations  uint32 = 3
	passwordParallelism uint8  = 1
)

var ErrInvalidHash = errors.New("invalid password hash")
var ErrCommonPassword = errors.New("common password")

//go:embed common_passwords_v1.txt
var commonPasswords string

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) || len(password) > 512 {
		return ErrInvalidInput
	}
	count := utf8.RuneCountInString(password)
	if count < 10 || count > 128 {
		return ErrInvalidInput
	}
	for _, common := range strings.Split(strings.TrimSuffix(commonPasswords, "\n"), "\n") {
		if password == common {
			return ErrCommonPassword
		}
	}
	return nil
}

// ArgonHasher admits at most two hashes and rejects excess work immediately.
// Create once per enabled runtime, not per request. Cancellation cannot interrupt
// Argon2 itself; its admission slot remains held until the bounded work finishes.
type ArgonHasher struct {
	slots    chan struct{}
	random   io.Reader
	randomMu sync.Mutex
	dummy    string
}

func NewArgonHasher(source io.Reader) (*ArgonHasher, error) {
	if source == nil {
		source = rand.Reader
	}
	h := &ArgonHasher{slots: make(chan struct{}, 2), random: source}
	// Dummy is random and never accepted as an account credential.
	token, _, err := NewToken(source)
	if err != nil {
		return nil, err
	}
	h.dummy, err = h.Hash(context.Background(), token)
	if err != nil {
		return nil, err
	}
	return h, nil
}

func (h *ArgonHasher) admit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case h.slots <- struct{}{}:
		return nil
	default:
		return ErrUnavailable
	}
}

func (h *ArgonHasher) Hash(ctx context.Context, password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	if err := h.admit(ctx); err != nil {
		return "", err
	}
	defer func() { <-h.slots }()
	var salt [16]byte
	h.randomMu.Lock()
	_, err := io.ReadFull(h.random, salt[:])
	h.randomMu.Unlock()
	if err != nil {
		return "", ErrUnavailable
	}
	key := argon2.IDKey([]byte(password), salt[:], passwordIterations, passwordMemory, passwordParallelism, 32)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", passwordMemory, passwordIterations, passwordParallelism, base64.RawStdEncoding.EncodeToString(salt[:]), base64.RawStdEncoding.EncodeToString(key)), nil
}

func (h *ArgonHasher) Verify(ctx context.Context, password, encoded string) (bool, bool, error) {
	if !utf8.ValidString(password) || len(password) > 512 {
		return false, false, ErrInvalidInput
	}
	params, salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, false, err
	}
	if err := h.admit(ctx); err != nil {
		return false, false, err
	}
	defer func() { <-h.slots }()
	actual := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, 32)
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	matches := subtle.ConstantTimeCompare(actual, expected) == 1
	return matches, matches && (params.memory != passwordMemory || params.iterations != passwordIterations || params.parallelism != passwordParallelism), nil
}

func (h *ArgonHasher) Dummy(ctx context.Context, password string) error {
	_, _, err := h.Verify(ctx, password, h.dummy)
	return err
}

type passwordParams struct {
	memory, iterations uint32
	parallelism        uint8
}

func parsePasswordHash(encoded string) (passwordParams, []byte, []byte, error) {
	var p passwordParams
	if len(encoded) > 128 {
		return p, nil, nil, ErrInvalidHash
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return p, nil, nil, ErrInvalidHash
	}
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil || n != 3 || parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", p.memory, p.iterations, p.parallelism) {
		return p, nil, nil, ErrInvalidHash
	}
	// Bounded legacy parameters allow rehash-on-success, never attacker-sized allocations.
	if p.memory < 8*1024 || p.memory > passwordMemory || p.iterations < 1 || p.iterations > passwordIterations || p.parallelism != 1 {
		return p, nil, nil, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) != 16 || base64.RawStdEncoding.EncodeToString(salt) != parts[4] {
		return p, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) != 32 || base64.RawStdEncoding.EncodeToString(key) != parts[5] {
		return p, nil, nil, ErrInvalidHash
	}
	return p, salt, key, nil
}
