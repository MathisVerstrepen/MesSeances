package schedulepg

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestHistoryGenreAliasesIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	historyPublish(t, store, testDataset())
	var movieID int64
	if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='200'`).Scan(&movieID); err != nil {
		t.Fatal(err)
	}
	// Exercise both current metadata overrides and ordinary stored movie genres.
	historyExec(t, pool, `INSERT INTO public_movie_metadata_overrides(public_movie_id,genres,genres_overridden) VALUES($1,ARRAY[]::text[],true)`, movieID)
	unrelated := []string{"Comédie musicale", "Spectacle", "Musique", "Musical", "Crime", "Policier", "Fantastique", "Biopic"}
	historyExec(t, pool, `UPDATE public_movies SET genres=$1 WHERE id=(SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='202')`, unrelated)
	for _, tc := range []struct {
		label   string
		aliases []string
	}{
		{"Famille", []string{"Familial", "Famille", "Famille/Enfants"}},
		{"Opéra", []string{"Opéra", "Opera"}},
		{"Science-fiction", []string{"Science-Fiction", "Science fiction"}},
		{"Histoire", []string{"Histoire", "Historique"}},
		{"Horreur", []string{"Horreur", "Horreur / Épouvante"}},
		{"Romance", []string{"Romance", "Amour"}},
		{"Animation", []string{"Animation", "Dessin animé"}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			aliases := append(slices.Clone(tc.aliases), "\u2003"+strings.ToUpper(tc.aliases[0])+"\u00a0")
			singleAlias := []string{tc.aliases[len(tc.aliases)-1]}
			historyExec(t, pool, `UPDATE public_movie_metadata_overrides SET genres=$2 WHERE public_movie_id=$1`, movieID, aliases)
			historyExec(t, pool, `UPDATE public_movies SET genres=$1 WHERE id=(SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='201')`, singleAlias)
			source, err := NewPostgresSource(t.Context(), store)
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewService(source, ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 15, 7, 0, 0, 0, time.UTC) }})
			if err != nil {
				t.Fatal(err)
			}
			base := historyGet(t, store, schedule.StatisticsQuery{})
			assertHistorySums(t, base)
			want := schedule.StatisticsCountBucket{Value: strings.ToLower(tc.label), Label: tc.label, Count: 2}
			if len(base.Genres) != len(unrelated)+2 || !slices.Contains(base.Genres, want) || !slices.Contains(base.Genres, schedule.StatisticsCountBucket{Value: "unknown", Label: "Non renseigné", Count: 1}) {
				t.Fatalf("genres=%+v want canonical=%+v and unchanged unknowns", base.Genres, want)
			}
			for _, label := range unrelated {
				if !slices.Contains(base.Genres, schedule.StatisticsCountBucket{Value: strings.ToLower(label), Label: label, Count: 1}) {
					t.Fatalf("unrelated genre changed: %s", label)
				}
			}
			for _, alias := range append(slices.Clone(aliases), "", tc.label, want.Value) {
				query := schedule.StatisticsQuery{Date: "2026-08-15", Genre: alias}
				history := historyGet(t, store, query)
				upcoming, err := service.Statistics(t.Context(), query)
				if err != nil {
					t.Fatal(err)
				}
				if history.Totals != upcoming.Totals || !reflect.DeepEqual(history.Genres, upcoming.Genres) || !reflect.DeepEqual(history.Options.Genres, upcoming.Options.Genres) {
					t.Fatalf("SQL/Go parity for %q: history=%+v upcoming=%+v", alias, history, upcoming)
				}
				if !reflect.DeepEqual(history.Options.Genres, base.Options.Genres) {
					t.Fatal("genre filter changed options")
				}
				if alias != "" && (history.Totals.Movies != 2 || history.Totals.Showtimes != 3 || !reflect.DeepEqual(history.Genres, []schedule.StatisticsCountBucket{want})) {
					t.Fatalf("filter %q: totals=%+v genres=%+v", alias, history.Totals, history.Genres)
				}
			}
			options, err := store.HistoryOptions(t.Context(), schedule.HistoryOptionsQuery{Kind: "genre", Selected: []string{want.Value}})
			canonical := schedule.HistoryOption{Value: want.Value, Label: want.Label}
			if err != nil || len(options.Items) != len(unrelated)+2 || !slices.Contains(options.Items, canonical) || !reflect.DeepEqual(options.Selected, []schedule.HistoryOption{canonical}) {
				t.Fatalf("canonical options=%+v error=%v", options, err)
			}
			search, err := store.HistoryOptions(t.Context(), schedule.HistoryOptionsQuery{Kind: "genre", Q: tc.label})
			if err != nil || !reflect.DeepEqual(search.Items, []schedule.HistoryOption{canonical}) {
				t.Fatalf("canonical search=%+v error=%v", search, err)
			}
			var stored []string
			if err := pool.QueryRow(t.Context(), `SELECT genres FROM public_movie_metadata_overrides WHERE public_movie_id=$1`, movieID).Scan(&stored); err != nil || !slices.Equal(stored, aliases) {
				t.Fatalf("stored override changed: %v %v", stored, err)
			}
			if err := pool.QueryRow(t.Context(), `SELECT genres FROM public_movies WHERE id=(SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='201')`).Scan(&stored); err != nil || !slices.Equal(stored, singleAlias) {
				t.Fatalf("stored movie genres changed: %v %v", stored, err)
			}
		})
	}
}

func TestHistoryCompoundGenresIntegration(t *testing.T) {
	pool := newHistoryPool(t)
	store := NewStore(pool)
	historyPublish(t, store, testDataset())
	var movieID int64
	if err := pool.QueryRow(t.Context(), `SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='200'`).Scan(&movieID); err != nil {
		t.Fatal(err)
	}
	historyExec(t, pool, `INSERT INTO public_movie_metadata_overrides(public_movie_id,genres,genres_overridden) VALUES($1,ARRAY[]::text[],true)`, movieID)
	overlaps := []string{"Comédie dramatique", "Comédie romantique", "Comédie d'action", "comédie", "Drame", "romance", "Action", "Amour", "amour"}
	historyExec(t, pool, `UPDATE public_movies SET genres=$1 WHERE id=(SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='201')`, overlaps)
	historyExec(t, pool, `UPDATE public_movies SET genres=$1 WHERE id=(SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='202')`, []string{"COMÉDIE", "DRAME", "ROMANCE", "ACTION"})
	historyExec(t, pool, `UPDATE public_movies SET genres=ARRAY['Comédie musicale'] WHERE id=(SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='203')`)
	for _, tc := range []struct{ compound, parent string }{
		{"Comédie dramatique", "Drame"},
		{"Comédie romantique", "Romance"},
		{"Comédie d'action", "Action"},
	} {
		t.Run(tc.compound, func(t *testing.T) {
			genres := []string{"\u2003" + strings.ToUpper(tc.compound) + "\u00a0"}
			historyExec(t, pool, `UPDATE public_movie_metadata_overrides SET genres=$2 WHERE public_movie_id=$1`, movieID, genres)
			source, err := NewPostgresSource(t.Context(), store)
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewService(source, ServiceOptions{Now: func() time.Time { return time.Date(2026, 8, 15, 7, 0, 0, 0, time.UTC) }})
			if err != nil {
				t.Fatal(err)
			}
			base := historyGet(t, store, schedule.StatisticsQuery{})
			assertHistorySums(t, base)
			if base.Totals.Movies != 4 || base.Totals.Showtimes != 5 || len(base.Genres) != 5 || len(base.Options.Genres) != 5 || !slices.Contains(base.Genres, schedule.StatisticsCountBucket{Value: "comédie musicale", Label: "Comédie musicale", Count: 1}) {
				t.Fatalf("totals=%+v genres=%+v options=%+v", base.Totals, base.Genres, base.Options.Genres)
			}
			for _, parent := range []string{"Comédie", "Drame", "Romance", "Action"} {
				movies, showtimes := 2, 2
				if parent == "Comédie" || parent == tc.parent {
					movies, showtimes = 3, 4
				}
				want := schedule.StatisticsCountBucket{Value: strings.ToLower(parent), Label: parent, Count: movies}
				if !slices.Contains(base.Genres, want) || !slices.Contains(base.Options.Genres, schedule.StatisticsGenreOption{Value: want.Value, Label: parent}) {
					t.Fatalf("missing canonical parent %+v in %+v", want, base.Genres)
				}
				filtered := historyGet(t, store, schedule.StatisticsQuery{Genre: parent})
				if filtered.Totals.Movies != movies || filtered.Totals.Showtimes != showtimes || !slices.Contains(filtered.Genres, want) {
					t.Fatalf("filter %q: totals=%+v genres=%+v", parent, filtered.Totals, filtered.Genres)
				}
			}
			for _, filter := range []string{"", "comédie", "drame", "romance", "action", "Amour", "\u2003COMÉDIE\u00a0", tc.compound} {
				query := schedule.StatisticsQuery{Date: "2026-08-15", Genre: filter}
				history := historyGet(t, store, query)
				upcoming, err := service.Statistics(t.Context(), query)
				if err != nil {
					t.Fatal(err)
				}
				if history.Totals != upcoming.Totals || !reflect.DeepEqual(history.Genres, upcoming.Genres) || !reflect.DeepEqual(history.Options.Genres, upcoming.Options.Genres) || !reflect.DeepEqual(history.Options.Genres, base.Options.Genres) {
					t.Fatalf("SQL/Go parity for %q: history=%+v upcoming=%+v", filter, history, upcoming)
				}
				if filter == tc.compound && history.Totals.Movies != 0 {
					t.Fatalf("compound filter unexpectedly matched: %+v", history.Totals)
				}
			}
			options, err := store.HistoryOptions(t.Context(), schedule.HistoryOptionsQuery{Kind: "genre", Selected: []string{"comédie", "drame", "romance", "action"}})
			if err != nil || len(options.Items) != 5 || len(options.Selected) != 4 {
				t.Fatalf("parent options=%+v error=%v", options, err)
			}
			for _, parent := range []string{"Comédie", "Drame", "Romance", "Action"} {
				want := schedule.HistoryOption{Value: strings.ToLower(parent), Label: parent}
				if !slices.Contains(options.Items, want) || !slices.Contains(options.Selected, want) {
					t.Fatalf("missing canonical parent option %+v in %+v", want, options)
				}
			}
			search, err := store.HistoryOptions(t.Context(), schedule.HistoryOptionsQuery{Kind: "genre", Q: tc.compound})
			if err != nil || len(search.Items) != 0 {
				t.Fatalf("compound option remains: %+v %v", search, err)
			}
			var stored []string
			if err := pool.QueryRow(t.Context(), `SELECT genres FROM public_movie_metadata_overrides WHERE public_movie_id=$1`, movieID).Scan(&stored); err != nil || !slices.Equal(stored, genres) {
				t.Fatalf("stored override changed: %v %v", stored, err)
			}
			if err := pool.QueryRow(t.Context(), `SELECT genres FROM public_movies WHERE id=(SELECT public_movie_id FROM public_movie_sources WHERE source_provider='ugc' AND source_movie_id='201')`).Scan(&stored); err != nil || !slices.Equal(stored, overlaps) {
				t.Fatalf("stored movie genres changed: %v %v", stored, err)
			}
		})
	}
}
