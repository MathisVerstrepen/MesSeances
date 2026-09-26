package accounts

import (
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
)

var (
	ErrInvalidInput        = errors.New("invalid account input")
	ErrUnavailable         = errors.New("account service unavailable")
	ErrUsernameUnavailable = errors.New("username unavailable")
	usernameSyntax         = regexp.MustCompile(`^[a-z][a-z0-9_]{2,29}$`)
	emailLocalSyntax       = regexp.MustCompile("^[a-z0-9!#$%&'*+/=?^_`{|}~.-]+$")
	emailLabelSyntax       = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

//go:embed reserved_usernames.json
var reservedUsernamesJSON []byte

var reservedUsernames = sync.OnceValues(func() ([]string, error) {
	var names []string
	if err := json.Unmarshal(reservedUsernamesJSON, &names); err != nil || len(names) == 0 {
		return nil, ErrUnavailable
	}
	for i, name := range names {
		if name == "" || name != strings.ToLower(strings.TrimSpace(name)) || (i > 0 && names[i-1] >= name) {
			return nil, ErrUnavailable
		}
	}
	return names, nil
})

// NormalizeUsername folds ASCII case only. Spaces are not valid handle characters.
// Database uniqueness, including deleted-name claims, must still be checked atomically.
func NormalizeUsername(raw string) (string, error) {
	if len(raw) < 3 || len(raw) > 30 {
		return "", ErrInvalidInput
	}
	for _, c := range []byte(raw) {
		if c >= 128 {
			return "", ErrInvalidInput
		}
	}
	name := strings.ToLower(raw)
	if !usernameSyntax.MatchString(name) {
		return "", ErrInvalidInput
	}
	names, err := reservedUsernames()
	if err != nil {
		return "", err
	}
	if _, found := slices.BinarySearch(names, name); found {
		return "", ErrUsernameUnavailable
	}
	return name, nil
}

// NormalizeEmail accepts an ASCII dot-atom mailbox and a dotted DNS domain.
// No display names, IP literals, quoted local parts or provider-specific rewriting.
func NormalizeEmail(raw string) (string, error) {
	// Controls are invalid even at the edges; ordinary surrounding spaces are allowed.
	for _, c := range []byte(raw) {
		if c < 32 || c >= 127 {
			return "", ErrInvalidInput
		}
	}
	email := strings.ToLower(strings.TrimSpace(raw))
	if len(email) > 254 || strings.Count(email, "@") != 1 {
		return "", ErrInvalidInput
	}
	local, domain, _ := strings.Cut(email, "@")
	if len(local) == 0 || len(local) > 64 || !emailLocalSyntax.MatchString(local) || strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return "", ErrInvalidInput
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 || len(domain) > 253 {
		return "", ErrInvalidInput
	}
	for _, label := range labels {
		if !emailLabelSyntax.MatchString(label) {
			return "", ErrInvalidInput
		}
	}
	return email, nil
}
