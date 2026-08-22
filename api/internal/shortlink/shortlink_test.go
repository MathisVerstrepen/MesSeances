package shortlink

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

type stubStore struct {
	createErrors []error
	created      []Link
	resolved     Link
	resolveError error
}

func (s *stubStore) Create(_ context.Context, link Link) error {
	s.created = append(s.created, link)
	if len(s.createErrors) == 0 {
		return nil
	}
	err := s.createErrors[0]
	s.createErrors = s.createErrors[1:]
	return err
}

func (s *stubStore) Resolve(_ context.Context, _ string) (Link, error) {
	return s.resolved, s.resolveError
}

func TestServiceCreateGeneratesSecureShapeAndPreservesTarget(t *testing.T) {
	store := &stubStore{}
	service := NewService(store, ServiceOptions{Random: bytes.NewReader(make([]byte, 16))})
	link, err := service.Create(context.Background(), "/films?sort=title&q=Am%C3%A9lie")
	if err != nil || link.Code != "AAAAAAAAAAAAAAAAAAAAAA" || link.Target != "/films?sort=title&q=Am%C3%A9lie" || len(store.created) != 1 || store.created[0] != link {
		t.Fatalf("link=%+v created=%+v err=%v", link, store.created, err)
	}
}

func TestServiceCreateRetriesCollisions(t *testing.T) {
	random := append(make([]byte, 16), bytes.Repeat([]byte{1}, 16)...)
	store := &stubStore{createErrors: []error{ErrCollision, nil}}
	link, err := NewService(store, ServiceOptions{Random: bytes.NewReader(random)}).Create(context.Background(), "/")
	if err != nil || link.Code != "AQEBAQEBAQEBAQEBAQEBAQ" || len(store.created) != 2 || store.created[0].Code == store.created[1].Code {
		t.Fatalf("link=%+v created=%+v err=%v", link, store.created, err)
	}
}

func TestServiceCreateErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		target string
		store  *stubStore
		random []byte
	}{
		{name: "invalid target", target: "https://evil.example", store: &stubStore{}, random: make([]byte, 16)},
		{name: "random failure", target: "/", store: &stubStore{}, random: nil},
		{name: "store unavailable", target: "/", store: &stubStore{createErrors: []error{errors.New("database secret")}}, random: make([]byte, 16)},
		{name: "collisions exhausted", target: "/", store: &stubStore{createErrors: []error{ErrCollision, ErrCollision, ErrCollision, ErrCollision, ErrCollision}}, random: make([]byte, 80)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewService(test.store, ServiceOptions{Random: bytes.NewReader(test.random)}).Create(context.Background(), test.target)
			want := ErrUnavailable
			if test.name == "invalid target" {
				want = ErrInvalidTarget
			}
			if !errors.Is(err, want) {
				t.Fatalf("err=%v want=%v", err, want)
			}
		})
	}
}

func TestServiceRejectsCinemasCreate(t *testing.T) {
	store := &stubStore{}
	_, err := NewService(store, ServiceOptions{Random: bytes.NewReader(make([]byte, 16))}).Create(context.Background(), "/cinemas?q=Lille&shared_theaters=ugc-25")
	if !errors.Is(err, ErrInvalidTarget) || len(store.created) != 0 {
		t.Fatalf("created=%+v err=%v", store.created, err)
	}
}

func TestServiceResolve(t *testing.T) {
	code := "AAAAAAAAAAAAAAAAAAAAAA"
	for _, test := range []struct {
		name  string
		code  string
		store *stubStore
		want  error
	}{
		{name: "success", code: code, store: &stubStore{resolved: Link{Code: code, Target: "/planning?date=2026-08-22"}}},
		{name: "invalid code", code: "bad", store: &stubStore{}, want: ErrNotFound},
		{name: "not found", code: code, store: &stubStore{resolveError: ErrNotFound}, want: ErrNotFound},
		{name: "store unavailable", code: code, store: &stubStore{resolveError: errors.New("database secret")}, want: ErrUnavailable},
		{name: "corrupt code", code: code, store: &stubStore{resolved: Link{Code: "BBBBBBBBBBBBBBBBBBBBBB", Target: "/"}}, want: ErrUnavailable},
		{name: "corrupt target", code: code, store: &stubStore{resolved: Link{Code: code, Target: "https://evil.example"}}, want: ErrUnavailable},
		{name: "legacy cinemas target", code: code, store: &stubStore{resolved: Link{Code: code, Target: "/cinemas?q=Lille"}}, want: ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			link, err := NewService(test.store, ServiceOptions{}).Resolve(context.Background(), test.code)
			if !errors.Is(err, test.want) || test.want == nil && (link.Code != code || link.Target == "") {
				t.Fatalf("link=%+v err=%v want=%v", link, err, test.want)
			}
		})
	}
}
