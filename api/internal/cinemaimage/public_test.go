package cinemaimage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/image/webp"
)

type publicRepository struct {
	Repository
	calls   int
	current func(context.Context, Identity) (Record, error)
}

func (r *publicRepository) Current(ctx context.Context, id Identity) (Record, error) {
	r.calls++
	if r.current != nil {
		return r.current(ctx, id)
	}
	return r.Repository.Current(ctx, id)
}

func TestPublicImageCurrentMetadata(t *testing.T) {
	s, repo, id := serviceFixture(t)
	r := &publicRepository{Repository: repo}
	s.repo = r
	if image, err := s.PublicImage(t.Context(), id); err != nil || image != nil || len(repo.records) != 0 || r.calls != 1 {
		t.Fatal("empty lookup created metadata", image, err, r.calls)
	}
	if _, err := upload(t, s, id, 0); err != nil {
		t.Fatal(err)
	}
	r.calls = 0
	image, err := s.PublicImage(t.Context(), id)
	want := &PublicImage{URL: "/api/v1/theaters/ugc/25/image/1", Width: 20, Height: 10}
	if err != nil || !reflect.DeepEqual(image, want) || r.calls != 1 {
		t.Fatal(image, err, r.calls)
	}
	b, err := json.Marshal(image)
	if err != nil || string(b) != `{"url":"/api/v1/theaters/ugc/25/image/1","width":20,"height":10}` {
		t.Fatal("unexpected public fields", string(b), err)
	}
	// Metadata remains file-free, even when the current file is missing.
	if err := s.media.Remove(repo.records[id].Key); err != nil {
		t.Fatal(err)
	}
	if image, err = s.PublicImage(t.Context(), id); err != nil || !reflect.DeepEqual(image, want) {
		t.Fatal(image, err)
	}
	if _, err := upload(t, s, id, 1); err != nil {
		t.Fatal(err)
	}
	if image, err = s.PublicImage(t.Context(), id); err != nil || image == nil || image.URL != "/api/v1/theaters/ugc/25/image/2" {
		t.Fatal("replacement not current", image, err)
	}
	if _, err := s.Remove(t.Context(), id, 2); err != nil {
		t.Fatal(err)
	}
	if image, err = s.PublicImage(t.Context(), id); err != nil || image != nil {
		t.Fatal("tombstone exposed", image, err)
	}
	repo.present[id] = false
	if image, err = s.PublicImage(t.Context(), id); err != nil || image != nil {
		t.Fatal("nonmember exposed", image, err)
	}
}

func TestPublicImageDisabledAndValidation(t *testing.T) {
	r := &publicRepository{}
	for _, s := range []*Service{nil, {}, NewService(r, nil, nil, nil)} {
		if image, err := s.PublicImage(t.Context(), Identity{"ugc", "25"}); err != nil || image != nil || r.calls != 0 {
			t.Fatal("disabled lookup touched repository", image, err, r.calls)
		}
	}
	s, _, _ := serviceFixture(t)
	s.repo = r
	r.current = func(context.Context, Identity) (Record, error) { return Record{}, ErrStorage }
	if _, err := s.PublicImage(t.Context(), Identity{"ugc", "../25"}); !errors.Is(err, ErrRequest) || r.calls != 0 {
		t.Fatal("invalid identity lookup", err, r.calls)
	}
	if _, err := s.PublicImage(t.Context(), Identity{"ugc", "25"}); !errors.Is(err, ErrStorage) {
		t.Fatal("lookup error hidden", err)
	}
	base := Record{Revision: 1, Key: strings.Repeat("a", 32) + ".webp", Width: 1600, Height: 1, Size: 1}
	for name, change := range map[string]func(*Record){
		"zero revision":     func(r *Record) { r.Revision = 0 },
		"negative revision": func(r *Record) { r.Revision = -1 },
		"unsafe revision":   func(r *Record) { r.Revision = MaxRevision + 1 },
		"key":               func(r *Record) { r.Key = "../image.webp" },
		"zero width":        func(r *Record) { r.Width = 0 },
		"large height":      func(r *Record) { r.Height = MaxEdge + 1 },
		"zero size":         func(r *Record) { r.Size = 0 },
		"large size":        func(r *Record) { r.Size = MaxOutput + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			record := base
			change(&record)
			r.current = func(context.Context, Identity) (Record, error) { return record, nil }
			if image, err := s.PublicImage(t.Context(), Identity{"ugc", "25"}); image != nil || !errors.Is(err, ErrStorage) {
				t.Fatal(image, err)
			}
		})
	}
	base.Revision = MaxRevision
	r.current = func(context.Context, Identity) (Record, error) { return base, nil }
	for _, id := range []Identity{{"kinepolis", "001-opaque-ID"}, {"pathe", "001-opaque-ID"}, {"cineville", "1"}} {
		image, err := s.PublicImage(t.Context(), id)
		if err != nil || image == nil || image.URL != "/api/v1/theaters/"+id.Provider+"/"+id.ProviderTheaterID+"/image/9007199254740991" {
			t.Fatal("identity/revision changed", image, err)
		}
	}
}

func TestPublicImageNormalizedReadAndRevocation(t *testing.T) {
	s, repo, id := serviceFixture(t)
	if _, err := upload(t, s, id, 0); err != nil {
		t.Fatal(err)
	}
	b, err := s.Read(t.Context(), id, 1)
	if err != nil || !bytes.HasPrefix(b, []byte("RIFF")) || string(b[8:12]) != "WEBP" {
		t.Fatal("not normalized WebP", err)
	}
	config, err := webp.DecodeConfig(bytes.NewReader(b))
	if err != nil || config.Width != 20 || config.Height != 10 {
		t.Fatal(config, err)
	}
	if _, err := upload(t, s, id, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(t.Context(), id, 1); !errors.Is(err, ErrImageNotFound) {
		t.Fatal("obsolete revision read", err)
	}
	repo.present[id] = false
	if _, err := s.Read(t.Context(), id, 2); !errors.Is(err, ErrNotFound) {
		t.Fatal("nonmember read", err)
	}
	repo.present[id] = true
	r := &publicRepository{Repository: repo}
	r.current = func(ctx context.Context, id Identity) (Record, error) {
		if r.calls == 2 {
			repo.present[id] = false
		}
		return repo.Current(ctx, id)
	}
	s.repo = r
	if _, err := s.Read(t.Context(), id, 2); !errors.Is(err, ErrNotFound) || r.calls != 2 {
		t.Fatal("membership not rechecked", err, r.calls)
	}
	s.repo = repo
	repo.present[id] = true
	if err := s.media.Remove(repo.records[id].Key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(t.Context(), id, 2); !errors.Is(err, ErrImageNotFound) {
		t.Fatal("missing file read", err)
	}
	if _, err := upload(t, s, id, 2); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(s.media.root.Name()+"/"+repo.records[id].Key, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(t.Context(), id, 3); !errors.Is(err, ErrStorage) {
		t.Fatal("unsafe file read", err)
	}
}
