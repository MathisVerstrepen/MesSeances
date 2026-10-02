package cinemaimage

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"

	"messeances/api/internal/geocoding"
)

type Identity struct{ Provider, ProviderTheaterID string }
type Record struct {
	Revision            int64
	Key                 string
	Width, Height, Size int
}
type Image struct {
	URL       string `json:"url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	SizeBytes int    `json:"size_bytes"`
}
type Result struct {
	ImageRevision int64  `json:"image_revision"`
	Image         *Image `json:"image"`
}
type Theater struct {
	Provider          string `json:"provider"`
	ProviderTheaterID string `json:"provider_theater_id"`
	TheaterID         string `json:"theater_id"`
	Slug              string `json:"slug"`
	Name              string `json:"name"`
	Address           string `json:"address"`
	PostalCode        string `json:"postal_code"`
	City              string `json:"city"`
	Result
}
type Inventory struct {
	Items          []Theater `json:"items"`
	Limit          int       `json:"limit"`
	Offset         int       `json:"offset"`
	Total          int       `json:"total"`
	ImportsEnabled bool      `json:"imports_enabled"`
}

// Repository commits metadata only. Save and Remove recheck membership and CAS
// under the media barrier and row lock; no transaction spans decoding or storage.
type Repository interface {
	List(context.Context, int, int) ([]Theater, int, error)
	Current(context.Context, Identity) (Record, error)
	Save(context.Context, Identity, int64, Record) (Record, string, error)
	Remove(context.Context, Identity, int64) (Record, string, error)
	References(context.Context, []string) (map[string]bool, error)
}
type Service struct {
	repo     Repository
	media    *Store
	importer Importer
	logger   *slog.Logger
}

func NewService(repo Repository, media *Store, importer Importer, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, nil))
	}
	return &Service{repo: repo, media: media, importer: importer, logger: logger}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func result(id Identity, r Record) Result {
	out := Result{ImageRevision: r.Revision}
	if r.Key != "" {
		out.Image = &Image{URL: "/api/v1/admin/theaters/" + url.PathEscape(id.Provider) + "/" + url.PathEscape(id.ProviderTheaterID) + "/image/" + strconv.FormatInt(r.Revision, 10), Width: r.Width, Height: r.Height, SizeBytes: r.Size}
	}
	return out
}
func validIdentity(id Identity) bool {
	return geocoding.ValidProviderTheaterID(id.Provider, id.ProviderTheaterID)
}
func (s *Service) available() bool { return s != nil && s.repo != nil && s.media != nil }
func (s *Service) List(ctx context.Context, limit, offset int) (Inventory, error) {
	if !s.available() {
		return Inventory{}, ErrStorage
	}
	items, total, err := s.repo.List(ctx, limit, offset)
	if err != nil {
		return Inventory{}, err
	}
	if items == nil {
		items = []Theater{}
	}
	return Inventory{Items: items, Limit: limit, Offset: offset, Total: total, ImportsEnabled: s.importer != nil}, nil
}
func (s *Service) check(ctx context.Context, id Identity, expected int64) error {
	if !validIdentity(id) || expected < 0 || expected > MaxRevision {
		return ErrRequest
	}
	if !s.available() {
		return ErrStorage
	}
	r, err := s.repo.Current(ctx, id)
	if err != nil {
		return err
	}
	if r.Revision != expected {
		return ErrConflict
	}
	return nil
}

// Upload admits before HTTP parsing. The callback returns strict framing plus
// revision and bytes, allowing multipart fields in either order without spooling.
func (s *Service) Upload(ctx context.Context, id Identity, read func() (int64, []byte, string, error)) (Result, error) {
	if !s.available() {
		return Result{}, ErrStorage
	}
	if !validIdentity(id) {
		return Result{}, ErrRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := s.repo.Current(ctx, id); err != nil {
		return Result{}, err
	}
	release, err := s.media.Admit(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	expected, b, media, err := read()
	if err != nil {
		return Result{}, err
	}
	if err = s.check(ctx, id, expected); err != nil {
		return Result{}, err
	}
	return s.normalizeAndSave(ctx, id, expected, b, media)
}
func (s *Service) Import(ctx context.Context, id Identity, expected int64, raw string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := s.check(ctx, id, expected); err != nil {
		return Result{}, err
	}
	if s.importer == nil {
		return Result{}, ErrImportUnavailable
	}
	if expected == MaxRevision {
		return Result{}, ErrStorage
	}
	release, err := s.media.Admit(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	b, media, err := s.importer.Fetch(ctx, raw)
	if err != nil {
		return Result{}, err
	}
	return s.normalizeAndSave(ctx, id, expected, b, media)
}
func (s *Service) normalizeAndSave(ctx context.Context, id Identity, expected int64, b []byte, media string) (Result, error) {
	if expected == MaxRevision {
		return Result{}, ErrStorage
	}
	photo, err := Normalize(ctx, b, media)
	if err != nil {
		return Result{}, err
	}
	stage, err := s.media.Stage(ctx, photo.Bytes)
	if err != nil {
		return Result{}, err
	}
	defer stage.Done()
	unlock, err := s.media.Guard(ctx)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	key, err := stage.Publish(ctx)
	if err != nil {
		return Result{}, err
	}
	r, old, err := s.repo.Save(ctx, id, expected, Record{Key: key, Width: photo.Width, Height: photo.Height, Size: len(photo.Bytes)})
	// No destructive cleanup after a rejected or ambiguous commit.
	if err != nil {
		return Result{}, err
	}
	s.removeOld(old)
	return result(id, r), nil
}
func (s *Service) Remove(ctx context.Context, id Identity, expected int64) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := s.check(ctx, id, expected); err != nil {
		return Result{}, err
	}
	unlock, err := s.media.Guard(ctx)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	r, old, err := s.repo.Remove(ctx, id, expected)
	if err != nil {
		return Result{}, err
	}
	s.removeOld(old)
	return result(id, r), nil
}
func (s *Service) removeOld(key string) {
	if key != "" {
		if s.media.Remove(key) != nil {
			s.logger.Warn("cinema_image_unlink_failed")
		}
	}
}
func (s *Service) Read(ctx context.Context, id Identity, revision int64) ([]byte, error) {
	if !s.available() {
		return nil, ErrStorage
	}
	if !validIdentity(id) || revision < 1 || revision > MaxRevision {
		return nil, ErrImageNotFound
	}
	r, err := s.repo.Current(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.Key == "" || r.Revision != revision {
		return nil, ErrImageNotFound
	}
	b, err := s.media.Read(ctx, r.Key, r.Width, r.Height, r.Size)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrImageNotFound
	}
	if err != nil {
		return nil, err
	}
	current, err := s.repo.Current(ctx, id)
	if err != nil {
		return nil, err
	}
	if current != r {
		return nil, ErrImageNotFound
	}
	return b, nil
}
func (s *Service) Cleanup(ctx context.Context) (SweepResult, error) {
	if !s.available() {
		return SweepResult{}, nil
	}
	return s.media.Sweep(ctx, time.Now(), s.repo.References)
}
