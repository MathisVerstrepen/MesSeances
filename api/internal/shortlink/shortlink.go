package shortlink

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

const (
	codeBytes         = 16
	maxCreateAttempts = 5
)

var (
	ErrInvalidTarget = errors.New("invalid shortlink target")
	ErrInvalidCode   = errors.New("invalid shortlink code")
	ErrCollision     = errors.New("shortlink code collision")
	ErrNotFound      = errors.New("shortlink not found")
	ErrUnavailable   = errors.New("shortlink unavailable")
)

type Link struct {
	Code   string `json:"code"`
	Target string `json:"target"`
}

type Store interface {
	Create(context.Context, Link) error
	Resolve(context.Context, string) (Link, error)
}

type Service struct {
	store  Store
	random io.Reader
}

type ServiceOptions struct {
	Random io.Reader
}

func NewService(store Store, options ServiceOptions) *Service {
	if options.Random == nil {
		options.Random = rand.Reader
	}
	return &Service{store: store, random: options.Random}
}

func (s *Service) Create(ctx context.Context, target string) (Link, error) {
	if !ValidTarget(target) {
		return Link{}, ErrInvalidTarget
	}
	for range maxCreateAttempts {
		code, err := s.newCode()
		if err != nil {
			return Link{}, ErrUnavailable
		}
		link := Link{Code: code, Target: target}
		err = s.store.Create(ctx, link)
		if err == nil {
			return link, nil
		}
		if !errors.Is(err, ErrCollision) {
			return Link{}, ErrUnavailable
		}
	}
	return Link{}, ErrUnavailable
}

func (s *Service) Resolve(ctx context.Context, code string) (Link, error) {
	if !ValidCode(code) {
		return Link{}, ErrNotFound
	}
	link, err := s.store.Resolve(ctx, code)
	if errors.Is(err, ErrNotFound) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, ErrUnavailable
	}
	if link.Code != code || !ValidTarget(link.Target) {
		return Link{}, ErrUnavailable
	}
	return link, nil
}

func (s *Service) newCode() (string, error) {
	buffer := make([]byte, codeBytes)
	if _, err := io.ReadFull(s.random, buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
