package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"movieflow/api/internal/enrichment"
)

type adminLocalMovieStore struct {
	groups     []enrichment.LocalMovieGroup
	groupsErr  error
	mergeErr   error
	unmergeErr error
	limit      int
	offset     int
	merged     []enrichment.LocalMovieSource
	primary    enrichment.LocalMovieSource
	unmergedID int64
}

func (s *adminLocalMovieStore) LocalMovieGroups(_ context.Context, limit, offset int) ([]enrichment.LocalMovieGroup, error) {
	s.limit, s.offset = limit, offset
	return s.groups, s.groupsErr
}

func (s *adminLocalMovieStore) MergeLocalMovies(_ context.Context, members []enrichment.LocalMovieSource, primary enrichment.LocalMovieSource) (enrichment.LocalMovieGroup, error) {
	s.merged, s.primary = members, primary
	if s.mergeErr != nil {
		return enrichment.LocalMovieGroup{}, s.mergeErr
	}
	return enrichment.LocalMovieGroup{ID: 7, Primary: primary, Members: []enrichment.LocalMovieMember{{LocalMovieSource: members[0], Available: true}, {LocalMovieSource: members[1], Available: true}}}, nil
}

func (s *adminLocalMovieStore) UnmergeLocalMovie(_ context.Context, id int64) error {
	s.unmergedID = id
	return s.unmergeErr
}

func localMovieAdminHandler(t *testing.T, store *adminLocalMovieStore) http.Handler {
	t.Helper()
	reviews := enrichment.NewReviewService(adminReviewStore{}, nil, nil)
	return testHandlerWithAdmin(t, AdminOptions{
		Password:    "password",
		Reviews:     reviews,
		LocalMovies: enrichment.NewLocalMovieService(store),
	})
}

func TestAdminLocalMovieGroupsListContractWithoutTMDBProvider(t *testing.T) {
	primary := enrichment.LocalMovieSource{SourceProvider: enrichment.SourceUGC, SourceMovieID: "200"}
	fallback := enrichment.LocalMovieSource{SourceProvider: enrichment.SourceKinepolis, SourceMovieID: "HO0001"}
	title, runtime := "Film A", 100
	store := &adminLocalMovieStore{groups: []enrichment.LocalMovieGroup{{
		ID:      7,
		Primary: primary,
		Members: []enrichment.LocalMovieMember{
			{LocalMovieSource: primary},
			{LocalMovieSource: fallback, Available: true, SourceTitle: &title, SourceRuntimeMinutes: &runtime},
		},
	}}}
	handler := localMovieAdminHandler(t, store)
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/local-movie-groups?limit=20&offset=40", "", "", cookie)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || store.limit != 20 || store.offset != 40 {
		t.Fatalf("status=%d headers=%v pagination=%d/%d body=%s", response.Code, response.Header(), store.limit, store.offset, response.Body.String())
	}
	want := `{"items":[{"local_movie_id":"local-film-7","primary":{"source_provider":"ugc","source_movie_id":"200"},"metadata_source":{"source_provider":"kinepolis","source_movie_id":"HO0001"},"members":[{"source_provider":"ugc","source_movie_id":"200","available":false,"source_title":null,"source_runtime_minutes":null,"source_poster_url":null},{"source_provider":"kinepolis","source_movie_id":"HO0001","available":true,"source_title":"Film A","source_runtime_minutes":100,"source_poster_url":null}]}],"limit":20,"offset":40}`
	if strings.TrimSpace(response.Body.String()) != want {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestAdminLocalMovieMergeAndUnmergeContracts(t *testing.T) {
	store := &adminLocalMovieStore{}
	handler := localMovieAdminHandler(t, store)
	cookie := loginAdmin(t, handler, "password")
	body := `{"members":[{"source_provider":"ugc","source_movie_id":"200"},{"source_provider":"kinepolis","source_movie_id":"HO0001"}],"primary":{"source_provider":"ugc","source_movie_id":"200"}}`
	merge := adminRequest(handler, http.MethodPost, "/api/v1/admin/local-movie-groups", body, "http://localhost:3000", cookie)
	if merge.Code != http.StatusCreated || merge.Header().Get("Cache-Control") != "no-store" || !strings.Contains(merge.Body.String(), `"local_movie_id":"local-film-7"`) || len(store.merged) != 2 || store.primary.SourceMovieID != "200" {
		t.Fatalf("merge status=%d merged=%+v primary=%+v body=%s", merge.Code, store.merged, store.primary, merge.Body.String())
	}
	unmerge := adminRequest(handler, http.MethodPost, "/api/v1/admin/local-movie-groups/local-film-7/unmerge", "", "http://localhost:3000", cookie)
	if unmerge.Code != http.StatusOK || store.unmergedID != 7 || strings.TrimSpace(unmerge.Body.String()) != `{"status":"unmerged","local_movie_id":"local-film-7"}` {
		t.Fatalf("unmerge status=%d id=%d body=%s", unmerge.Code, store.unmergedID, unmerge.Body.String())
	}
}

func TestAdminLocalMovieSecurityAndStrictInputs(t *testing.T) {
	handler := localMovieAdminHandler(t, &adminLocalMovieStore{})
	unauthorized := adminRequest(handler, http.MethodGet, "/api/v1/admin/local-movie-groups", "", "", nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized", "Authentification requise.")
	cookie := loginAdmin(t, handler, "password")
	wrongOrigin := adminRequest(handler, http.MethodPost, "/api/v1/admin/local-movie-groups", `{}`, "https://evil.example", cookie)
	assertAPIError(t, wrongOrigin, http.StatusForbidden, "origin_forbidden", "Origine non autorisée.")

	for _, target := range []string{
		"/api/v1/admin/local-movie-groups?limit=0",
		"/api/v1/admin/local-movie-groups?limit=101",
		"/api/v1/admin/local-movie-groups?limit=",
		"/api/v1/admin/local-movie-groups?limit=20&limit=30",
		"/api/v1/admin/local-movie-groups?offset=-1",
		"/api/v1/admin/local-movie-groups?unknown=1",
	} {
		response := adminRequest(handler, http.MethodGet, target, "", "", cookie)
		assertAPIError(t, response, http.StatusBadRequest, "invalid_query", "Pagination invalide.")
	}

	valid := `{"members":[{"source_provider":"ugc","source_movie_id":"200"},{"source_provider":"kinepolis","source_movie_id":"HO0001"}],"primary":{"source_provider":"ugc","source_movie_id":"200"}}`
	for _, body := range []string{
		`{}`,
		strings.TrimSuffix(valid, "}"),
		strings.TrimSuffix(valid, "}") + `,"unknown":true}`,
		valid + valid,
		valid + strings.Repeat(" ", maxAdminBody),
	} {
		response := adminRequest(handler, http.MethodPost, "/api/v1/admin/local-movie-groups", body, "http://localhost:3000", cookie)
		assertAPIError(t, response, http.StatusBadRequest, "invalid_request", "Requête invalide.")
	}
	badUnmerge := adminRequest(handler, http.MethodPost, "/api/v1/admin/local-movie-groups/local-film-7/unmerge", `{}`, "http://localhost:3000", cookie)
	assertAPIError(t, badUnmerge, http.StatusBadRequest, "invalid_request", "Requête invalide.")
}

func TestAdminLocalMovieUnavailableFailsClosed(t *testing.T) {
	reviews := enrichment.NewReviewService(adminReviewStore{}, nil, nil)
	handler := testHandlerWithAdmin(t, AdminOptions{Password: "password", Reviews: reviews})
	cookie := loginAdmin(t, handler, "password")
	response := adminRequest(handler, http.MethodGet, "/api/v1/admin/local-movie-groups", "", "", cookie)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control=%q", response.Header().Get("Cache-Control"))
	}
	assertAPIError(t, response, http.StatusBadGateway, "local_movie_failed", "Le regroupement de films n'a pas pu être modifié.")
}

func TestAdminLocalMovieErrorMappings(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid", err: enrichment.ErrLocalMovieInvalid, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "missing", err: enrichment.ErrLocalMovieNotFound, wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "conflict", err: enrichment.ErrLocalMovieConflict, wantStatus: http.StatusConflict, wantCode: "local_movie_conflict"},
		{name: "store failure", err: errors.New("database detail must not escape"), wantStatus: http.StatusBadGateway, wantCode: "local_movie_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &adminLocalMovieStore{unmergeErr: test.err}
			handler := localMovieAdminHandler(t, store)
			cookie := loginAdmin(t, handler, "password")
			response := adminRequest(handler, http.MethodPost, "/api/v1/admin/local-movie-groups/local-film-7/unmerge", "", "http://localhost:3000", cookie)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) || strings.Contains(response.Body.String(), "database detail") {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
