package enrichment

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrLocalMovieInvalid  = errors.New("invalid local movie request")
	ErrLocalMovieConflict = errors.New("local movie conflict")
	ErrLocalMovieNotFound = errors.New("local movie not found")
)

type LocalMovieSource struct {
	SourceProvider string `json:"source_provider"`
	SourceMovieID  string `json:"source_movie_id"`
}

type LocalMovieMember struct {
	LocalMovieSource
	Available            bool    `json:"available"`
	SourceTitle          *string `json:"source_title"`
	SourceRuntimeMinutes *int    `json:"source_runtime_minutes"`
	SourcePosterURL      *string `json:"source_poster_url"`
}

type LocalMovieGroup struct {
	LocalMovieID   string             `json:"local_movie_id"`
	Primary        LocalMovieSource   `json:"primary"`
	MetadataSource *LocalMovieSource  `json:"metadata_source"`
	Members        []LocalMovieMember `json:"members"`
	ID             int64              `json:"-"`
}

type LocalMovieStore interface {
	LocalMovieGroups(context.Context, int, int) ([]LocalMovieGroup, error)
	MergeLocalMovies(context.Context, []LocalMovieSource, LocalMovieSource) (LocalMovieGroup, error)
	AddLocalMovieMembers(context.Context, int64, []LocalMovieSource) error
	UnmergeLocalMovie(context.Context, int64) error
	MergeLocalMovieGroups(context.Context, int64, []int64) error
}

// MaxMergedLocalMovieGroups bounds one group-into-group merge request.
const MaxMergedLocalMovieGroups = 50

type LocalMovieService struct{ store LocalMovieStore }

func NewLocalMovieService(store LocalMovieStore) *LocalMovieService {
	return &LocalMovieService{store: store}
}

func (s *LocalMovieService) Groups(ctx context.Context, limit, offset int) ([]LocalMovieGroup, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("local movie service unavailable")
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrLocalMovieInvalid
	}
	groups, err := s.store.LocalMovieGroups(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	for groupIndex := range groups {
		prepareLocalMovieGroup(&groups[groupIndex])
	}
	return groups, nil
}

func (s *LocalMovieService) Merge(ctx context.Context, members []LocalMovieSource, primary LocalMovieSource) (LocalMovieGroup, error) {
	if s == nil || s.store == nil {
		return LocalMovieGroup{}, fmt.Errorf("local movie service unavailable")
	}
	if err := validateLocalMovieMerge(members, primary); err != nil {
		return LocalMovieGroup{}, err
	}
	group, err := s.store.MergeLocalMovies(ctx, append([]LocalMovieSource(nil), members...), primary)
	if err != nil {
		return LocalMovieGroup{}, err
	}
	prepareLocalMovieGroup(&group)
	return group, nil
}

func (s *LocalMovieService) AddMembers(ctx context.Context, localMovieID string, members []LocalMovieSource) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("local movie service unavailable")
	}
	id, err := ParseLocalMovieID(localMovieID)
	if err != nil {
		return err
	}
	if err := validateLocalMovieMembers(members); err != nil {
		return err
	}
	return s.store.AddLocalMovieMembers(ctx, id, append([]LocalMovieSource(nil), members...))
}

func (s *LocalMovieService) Unmerge(ctx context.Context, localMovieID string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("local movie service unavailable")
	}
	id, err := ParseLocalMovieID(localMovieID)
	if err != nil {
		return err
	}
	return s.store.UnmergeLocalMovie(ctx, id)
}

// MergeGroups moves every member of the source groups into the target group,
// which keeps its local movie ID and primary source, then deletes the sources.
func (s *LocalMovieService) MergeGroups(ctx context.Context, targetLocalMovieID string, sourceLocalMovieIDs []string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("local movie service unavailable")
	}
	targetID, err := ParseLocalMovieID(targetLocalMovieID)
	if err != nil {
		return err
	}
	sourceIDs, err := parseMergedLocalMovieGroupIDs(targetID, sourceLocalMovieIDs)
	if err != nil {
		return err
	}
	return s.store.MergeLocalMovieGroups(ctx, targetID, sourceIDs)
}

func parseMergedLocalMovieGroupIDs(targetID int64, values []string) ([]int64, error) {
	if len(values) == 0 || len(values) > MaxMergedLocalMovieGroups {
		return nil, ErrLocalMovieInvalid
	}
	ids := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		id, err := ParseLocalMovieID(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[id]; exists || id == targetID {
			return nil, ErrLocalMovieInvalid
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func LocalMovieID(id int64) string {
	if id <= 0 {
		return ""
	}
	return "local-film-" + strconv.FormatInt(id, 10)
}

func ParseLocalMovieID(value string) (int64, error) {
	const prefix = "local-film-"
	if !strings.HasPrefix(value, prefix) {
		return 0, ErrLocalMovieInvalid
	}
	raw := strings.TrimPrefix(value, prefix)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		return 0, ErrLocalMovieInvalid
	}
	return id, nil
}

func validateLocalMovieMerge(members []LocalMovieSource, primary LocalMovieSource) error {
	if len(members) < 2 || !validSourceIdentity(primary.SourceProvider, primary.SourceMovieID) || validateLocalMovieMembers(members) != nil {
		return ErrLocalMovieInvalid
	}
	primaryFound := false
	for _, member := range members {
		primaryFound = primaryFound || member == primary
	}
	if !primaryFound {
		return ErrLocalMovieInvalid
	}
	return nil
}

func validateLocalMovieMembers(members []LocalMovieSource) error {
	if len(members) == 0 {
		return ErrLocalMovieInvalid
	}
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		if !validSourceIdentity(member.SourceProvider, member.SourceMovieID) {
			return ErrLocalMovieInvalid
		}
		key := localMovieSourceKey(member)
		if _, exists := seen[key]; exists {
			return ErrLocalMovieInvalid
		}
		seen[key] = struct{}{}
	}
	return nil
}

func localMovieSourceKey(source LocalMovieSource) string {
	return source.SourceProvider + "\x00" + source.SourceMovieID
}

func prepareLocalMovieGroup(group *LocalMovieGroup) {
	group.LocalMovieID = LocalMovieID(group.ID)
	group.MetadataSource = nil
	for index := range group.Members {
		member := &group.Members[index]
		if member.SourcePosterURL != nil && !validSourcePosterURL(member.SourceProvider, *member.SourcePosterURL) {
			member.SourcePosterURL = nil
		}
		if !member.Available {
			continue
		}
		if member.LocalMovieSource == group.Primary {
			source := member.LocalMovieSource
			group.MetadataSource = &source
			return
		}
		if group.MetadataSource == nil {
			source := member.LocalMovieSource
			group.MetadataSource = &source
		}
	}
}
