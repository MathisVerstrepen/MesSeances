package cinemaimage

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type inventoryRepository struct {
	memoryRepository
	query ListQuery
	items []Theater
	total int
	err   error
	calls int
}

func (r *inventoryRepository) List(_ context.Context, q ListQuery) ([]Theater, int, error) {
	r.query = q
	r.calls++
	return r.items, r.total, r.err
}

func TestServiceListQueryAndInventory(t *testing.T) {
	for _, q := range []ListQuery{
		{Limit: 20}, {Limit: 1, Offset: 20, Search: "LiLLe"},
		{Limit: 2, Provider: "ugc"}, {Limit: 1, Offset: 1, Search: `100%_\`, Provider: "kinepolis"},
	} {
		t.Run(q.Search+q.Provider, func(t *testing.T) {
			r := &inventoryRepository{items: []Theater{{Name: "Matched Cinema"}}, total: 42}
			s := NewService(r, testStore(t), &syntheticImporter{}, nil)
			out, err := s.List(t.Context(), q)
			if err != nil || r.query != q || r.calls != 1 || !reflect.DeepEqual(out.Items, r.items) || out.Total != 42 || out.Limit != q.Limit || out.Offset != q.Offset || !out.ImportsEnabled {
				t.Fatalf("inventory=%+v query=%+v err=%v", out, r.query, err)
			}
			r.items, r.total = nil, 0
			s.importer = nil
			out, err = s.List(t.Context(), q)
			if err != nil || out.Items == nil || len(out.Items) != 0 || out.Total != 0 || out.ImportsEnabled {
				t.Fatal("empty inventory", out, err)
			}
			r.err = ErrStorage
			if _, err = s.List(t.Context(), q); !errors.Is(err, ErrStorage) {
				t.Fatal("repository error lost", err)
			}
		})
	}
}

func TestListQueryValidationBeforeRepositoryIO(t *testing.T) {
	for _, q := range []ListQuery{
		{Limit: 0}, {Limit: 101}, {Limit: 20, Offset: -1},
		{Limit: 20, Provider: "other"}, {Limit: 20, Provider: "UGC"},
		{Limit: 20, Search: " space "}, {Limit: 20, Search: "\xff"},
		{Limit: 20, Search: strings.Repeat("é", 1025)}, {Limit: 20, Search: "a\x00"},
		{Limit: 20, Search: "a\n"}, {Limit: 20, Search: "a\u0085"},
	} {
		r := &inventoryRepository{}
		s := NewService(r, testStore(t), nil, nil)
		if _, err := s.List(t.Context(), q); !errors.Is(err, ErrRequest) || r.calls != 0 {
			t.Fatalf("query=%+v calls=%d err=%v", q, r.calls, err)
		}
		// Invalid queries also fail before a PostgreSQL connection is attempted.
		if _, _, err := NewPostgresRepository(nil).List(t.Context(), q); !errors.Is(err, ErrRequest) {
			t.Fatal(err)
		}
	}
}

func TestInventorySearchPattern(t *testing.T) {
	for _, tc := range []struct{ search, want string }{
		{"", ""}, {"LiLLe", "%LiLLe%"}, {"Cinéma", "%Cinéma%"},
		{"%", `%\%%`}, {"_", `%\_%`}, {`\`, `%\\%`},
		{`100%_\`, `%100\%\_\\%`}, {"O'Neil", "%O'Neil%"},
		{`\%_`, `%\\\%\_%`},
	} {
		if got := inventorySearchPattern(tc.search); got != tc.want {
			t.Errorf("pattern(%q)=%q want %q", tc.search, got, tc.want)
		}
	}
}
