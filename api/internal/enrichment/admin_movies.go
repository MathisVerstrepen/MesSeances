package enrichment

import (
	"context"
	"errors"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrAdminMovieInvalid  = errors.New("invalid admin movie request")
	ErrAdminMovieConflict = errors.New("admin movie conflict")
	ErrAdminMovieNotFound = errors.New("admin movie not found")
)

type AdminMovieField string

const (
	AdminMovieFieldTitle               AdminMovieField = "title"
	AdminMovieFieldRuntimeMinutes      AdminMovieField = "runtime_minutes"
	AdminMovieFieldReleaseDate         AdminMovieField = "release_date"
	AdminMovieFieldGenres              AdminMovieField = "genres"
	AdminMovieFieldOverview            AdminMovieField = "overview"
	AdminMovieFieldPosterURL           AdminMovieField = "poster_url"
	AdminMovieFieldBackdropURL         AdminMovieField = "backdrop_url"
	AdminMovieFieldTrailerVFYouTubeKey AdminMovieField = "trailer_vf_youtube_key"
	AdminMovieFieldTrailerVOYouTubeKey AdminMovieField = "trailer_vo_youtube_key"
)

var adminMovieFields = []AdminMovieField{
	AdminMovieFieldTitle,
	AdminMovieFieldRuntimeMinutes,
	AdminMovieFieldReleaseDate,
	AdminMovieFieldGenres,
	AdminMovieFieldOverview,
	AdminMovieFieldPosterURL,
	AdminMovieFieldBackdropURL,
	AdminMovieFieldTrailerVFYouTubeKey,
	AdminMovieFieldTrailerVOYouTubeKey,
}

type AdminMovieMetadata struct {
	Title               string   `json:"title"`
	RuntimeMinutes      int      `json:"runtime_minutes"`
	ReleaseDate         *string  `json:"release_date"`
	Genres              []string `json:"genres"`
	Overview            *string  `json:"overview"`
	PosterURL           *string  `json:"poster_url"`
	BackdropURL         *string  `json:"backdrop_url"`
	TrailerVFYouTubeKey *string  `json:"trailer_vf_youtube_key"`
	TrailerVOYouTubeKey *string  `json:"trailer_vo_youtube_key"`
}

type AdminMovieItem struct {
	ID               string             `json:"id"`
	UpdatedAt        string             `json:"updated_at"`
	Automatic        AdminMovieMetadata `json:"automatic"`
	Values           AdminMovieMetadata `json:"values"`
	OverriddenFields []AdminMovieField  `json:"overridden_fields"`
}

type AdminMovieList struct {
	Items  []AdminMovieItem `json:"items"`
	Total  int64            `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

type AdminMovieQuery struct {
	Limit           int
	Offset          int
	Search          string
	RuntimeMin      *int
	RuntimeMax      *int
	ReleaseDateFrom *string
	ReleaseDateTo   *string
	Genre           string
	OverrideStatus  string
	OverrideField   AdminMovieField
	Sort            string
	Direction       string
}

type AdminMovieOverrideValue[T any] struct {
	Present bool
	Value   *T
}

type AdminMovieOverrides struct {
	Title               AdminMovieOverrideValue[string]
	RuntimeMinutes      AdminMovieOverrideValue[int]
	ReleaseDate         AdminMovieOverrideValue[string]
	Genres              AdminMovieOverrideValue[[]string]
	Overview            AdminMovieOverrideValue[string]
	PosterURL           AdminMovieOverrideValue[string]
	BackdropURL         AdminMovieOverrideValue[string]
	TrailerVFYouTubeKey AdminMovieOverrideValue[string]
	TrailerVOYouTubeKey AdminMovieOverrideValue[string]
}

type AdminMoviePatch struct {
	ExpectedUpdatedAt time.Time
	Overrides         AdminMovieOverrides
	Restore           []AdminMovieField
}

type AdminMovieStore interface {
	AdminMovies(context.Context, AdminMovieQuery) (AdminMovieList, error)
	UpdateAdminMovie(context.Context, int64, AdminMoviePatch) (AdminMovieItem, error)
}

type AdminMovieService struct{ store AdminMovieStore }

func NewAdminMovieService(store AdminMovieStore) *AdminMovieService {
	return &AdminMovieService{store: store}
}

func (s *AdminMovieService) List(ctx context.Context, query AdminMovieQuery) (AdminMovieList, error) {
	if s == nil || s.store == nil {
		return AdminMovieList{}, errors.New("admin movie service unavailable")
	}
	query.Search = strings.TrimSpace(query.Search)
	query.Genre = strings.TrimSpace(query.Genre)
	if !validAdminMovieQuery(query) {
		return AdminMovieList{}, ErrAdminMovieInvalid
	}
	return s.store.AdminMovies(ctx, query)
}

func (s *AdminMovieService) Update(ctx context.Context, id int64, patch AdminMoviePatch) (AdminMovieItem, error) {
	if s == nil || s.store == nil {
		return AdminMovieItem{}, errors.New("admin movie service unavailable")
	}
	if id <= 0 || !normalizeAdminMoviePatch(&patch) {
		return AdminMovieItem{}, ErrAdminMovieInvalid
	}
	return s.store.UpdateAdminMovie(ctx, id, patch)
}

func validAdminMovieQuery(query AdminMovieQuery) bool {
	if query.Limit < 1 || query.Limit > 100 || query.Offset < 0 || runeLength(query.Search) > 1024 || runeLength(query.Genre) > 256 {
		return false
	}
	if query.RuntimeMin != nil && (*query.RuntimeMin < 0 || int64(*query.RuntimeMin) > math.MaxInt32) || query.RuntimeMax != nil && (*query.RuntimeMax < 0 || int64(*query.RuntimeMax) > math.MaxInt32) {
		return false
	}
	if query.RuntimeMin != nil && query.RuntimeMax != nil && *query.RuntimeMin > *query.RuntimeMax {
		return false
	}
	if query.ReleaseDateFrom != nil && !validAdminMovieDate(*query.ReleaseDateFrom) || query.ReleaseDateTo != nil && !validAdminMovieDate(*query.ReleaseDateTo) {
		return false
	}
	if query.ReleaseDateFrom != nil && query.ReleaseDateTo != nil && *query.ReleaseDateFrom > *query.ReleaseDateTo {
		return false
	}
	if query.OverrideStatus != "all" && query.OverrideStatus != "overridden" && query.OverrideStatus != "automatic" {
		return false
	}
	if query.OverrideField != "" && !ValidAdminMovieField(query.OverrideField) || query.OverrideStatus == "automatic" && query.OverrideField != "" {
		return false
	}
	if query.Sort != "title" && query.Sort != "runtime_minutes" && query.Sort != "release_date" && query.Sort != "updated_at" && query.Sort != "id" {
		return false
	}
	return query.Direction == "asc" || query.Direction == "desc"
}

func normalizeAdminMoviePatch(patch *AdminMoviePatch) bool {
	if patch == nil || patch.ExpectedUpdatedAt.IsZero() {
		return false
	}
	restored := make(map[AdminMovieField]bool, len(patch.Restore))
	for _, field := range patch.Restore {
		if !ValidAdminMovieField(field) || restored[field] || overridePresent(patch.Overrides, field) {
			return false
		}
		restored[field] = true
	}
	if len(restored) == 0 && !anyOverridePresent(patch.Overrides) {
		return false
	}
	if patch.Overrides.Title.Present {
		if patch.Overrides.Title.Value == nil {
			return false
		}
		value := strings.TrimSpace(*patch.Overrides.Title.Value)
		if value == "" || runeLength(value) > 1024 {
			return false
		}
		patch.Overrides.Title.Value = &value
	}
	if patch.Overrides.RuntimeMinutes.Present && (patch.Overrides.RuntimeMinutes.Value == nil || *patch.Overrides.RuntimeMinutes.Value < 0 || int64(*patch.Overrides.RuntimeMinutes.Value) > math.MaxInt32) {
		return false
	}
	if patch.Overrides.ReleaseDate.Present && patch.Overrides.ReleaseDate.Value != nil && !validAdminMovieDate(*patch.Overrides.ReleaseDate.Value) {
		return false
	}
	if patch.Overrides.Genres.Present {
		if patch.Overrides.Genres.Value == nil || len(*patch.Overrides.Genres.Value) > 32 {
			return false
		}
		genres := append(make([]string, 0, len(*patch.Overrides.Genres.Value)), (*patch.Overrides.Genres.Value)...)
		for index := range genres {
			genres[index] = strings.TrimSpace(genres[index])
			if genres[index] == "" || runeLength(genres[index]) > 256 {
				return false
			}
		}
		patch.Overrides.Genres.Value = &genres
	}
	if !validOptionalAdminMovieString(patch.Overrides.Overview, 10000, nil) ||
		!validOptionalAdminMovieString(patch.Overrides.PosterURL, 4096, validAdminMovieURL) ||
		!validOptionalAdminMovieString(patch.Overrides.BackdropURL, 4096, validAdminMovieURL) ||
		!validOptionalAdminMovieString(patch.Overrides.TrailerVFYouTubeKey, 11, validAdminMovieTrailerKey) ||
		!validOptionalAdminMovieString(patch.Overrides.TrailerVOYouTubeKey, 11, validAdminMovieTrailerKey) {
		return false
	}
	return true
}

func validOptionalAdminMovieString(value AdminMovieOverrideValue[string], limit int, validate func(string) bool) bool {
	if !value.Present || value.Value == nil {
		return true
	}
	if runeLength(*value.Value) > limit {
		return false
	}
	return validate == nil || validate(*value.Value)
}

func ValidAdminMovieField(field AdminMovieField) bool {
	for _, candidate := range adminMovieFields {
		if field == candidate {
			return true
		}
	}
	return false
}

func AdminMovieFields() []AdminMovieField {
	return append([]AdminMovieField(nil), adminMovieFields...)
}

func overridePresent(overrides AdminMovieOverrides, field AdminMovieField) bool {
	switch field {
	case AdminMovieFieldTitle:
		return overrides.Title.Present
	case AdminMovieFieldRuntimeMinutes:
		return overrides.RuntimeMinutes.Present
	case AdminMovieFieldReleaseDate:
		return overrides.ReleaseDate.Present
	case AdminMovieFieldGenres:
		return overrides.Genres.Present
	case AdminMovieFieldOverview:
		return overrides.Overview.Present
	case AdminMovieFieldPosterURL:
		return overrides.PosterURL.Present
	case AdminMovieFieldBackdropURL:
		return overrides.BackdropURL.Present
	case AdminMovieFieldTrailerVFYouTubeKey:
		return overrides.TrailerVFYouTubeKey.Present
	case AdminMovieFieldTrailerVOYouTubeKey:
		return overrides.TrailerVOYouTubeKey.Present
	default:
		return false
	}
}

func anyOverridePresent(overrides AdminMovieOverrides) bool {
	for _, field := range adminMovieFields {
		if overridePresent(overrides, field) {
			return true
		}
	}
	return false
}

func validAdminMovieDate(value string) bool {
	parsed, err := time.Parse(time.DateOnly, value)
	return err == nil && parsed.Format(time.DateOnly) == value
}

func validAdminMovieURL(value string) bool {
	if value == "" || strings.ContainsFunc(value, unicode.IsSpace) {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs() && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil
}

var adminMovieTrailerKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func validAdminMovieTrailerKey(value string) bool {
	return adminMovieTrailerKeyPattern.MatchString(value)
}

func runeLength(value string) int {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return math.MaxInt
	}
	return utf8.RuneCountInString(value)
}
