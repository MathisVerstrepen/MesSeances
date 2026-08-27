package publicmoviepg

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const scheduleWriterLockID int64 = 6211428337968315

type sourceKey struct {
	provider string
	id       string
}

type source struct {
	key           sourceKey
	publicID      int64
	slug          string
	title         string
	runtime       int
	poster        *string
	overview      *string
	releaseDate   *time.Time
	genres        []string
	localGroupID  int64
	localPrimary  bool
	confirmedTMDB int64
}

type publicMovie struct {
	id            int64
	redirectTo    int64
	anchor        sourceKey
	confirmedTMDB int64
}

type metadata struct {
	title       string
	runtime     int
	poster      *string
	backdrop    *string
	overview    *string
	releaseDate *time.Time
	genres      []string
	tmdbID      int64
}

type tmdbMetadata struct {
	title       string
	runtime     int
	poster      *string
	backdrop    *string
	overview    *string
	releaseDate *time.Time
	genres      []string
}

type component struct {
	members      []*source
	localGroupID int64
	primary      sourceKey
	tmdbID       int64
	anchor       sourceKey
	metadata     metadata
	publicID     int64
}

// Reconcile updates durable public identities using only strict persisted evidence.
// Caller transaction remains responsible for advancing its schedule or enrichment revision.
func Reconcile(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", scheduleWriterLockID); err != nil {
		return fmt.Errorf("lock public movie reconciliation failed")
	}
	if err := retainActiveSources(ctx, tx); err != nil {
		return err
	}
	sources, err := loadSources(ctx, tx)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return validateTargets(ctx, tx)
	}
	movies, err := loadPublicMovies(ctx, tx)
	if err != nil {
		return err
	}
	groups, primaries, err := loadLocalEvidence(ctx, tx)
	if err != nil {
		return err
	}
	matches, err := loadTMDBEvidence(ctx, tx)
	if err != nil {
		return err
	}
	for key, source := range sources {
		if groupID := groups[key]; groupID > 0 {
			source.localGroupID = groupID
			source.localPrimary = primaries[groupID] == key
		}
		if tmdbID := matches[key]; tmdbID > 0 {
			if source.localGroupID > 0 {
				return fmt.Errorf("public movie identity evidence overlaps")
			}
			source.confirmedTMDB = tmdbID
		}
	}
	components := buildComponents(sources)
	tmdb, err := loadTMDBMetadata(ctx, tx, components)
	if err != nil {
		return err
	}
	for _, item := range components {
		item.anchor = chooseAnchor(item)
		item.metadata = chooseMetadata(item, tmdb[item.tmdbID])
		if item.metadata.title == "" || item.metadata.runtime < 0 {
			return fmt.Errorf("public movie canonical metadata is invalid")
		}
	}
	retainedOrphans, err := assignPublicIDs(ctx, tx, components, movies)
	if err != nil {
		return err
	}
	if err := persistAssignments(ctx, tx, components, movies, retainedOrphans); err != nil {
		return err
	}
	if err := persistAliases(ctx, tx, components); err != nil {
		return err
	}
	return validateTargets(ctx, tx)
}

func retainActiveSources(ctx context.Context, tx pgx.Tx) error {
	return retainNewActiveSources(ctx, tx)
}

func retainNewActiveSources(ctx context.Context, tx pgx.Tx) error {
	rows, err := tx.Query(ctx, `SELECT movie.provider, movie.provider_id, movie.slug, movie.title, movie.runtime_minutes,
       movie.poster_url, movie.source_overview, movie.source_release_date, movie.source_genres
FROM movies movie
JOIN schedule_snapshot snapshot ON snapshot.singleton=true AND snapshot.version=movie.generation_id
LEFT JOIN public_movie_sources source ON source.source_provider=movie.provider AND source.source_movie_id=movie.provider_id
WHERE source.source_movie_id IS NULL
ORDER BY movie.provider, movie.provider_id`)
	if err != nil {
		return fmt.Errorf("read new public movie sources failed")
	}
	defer rows.Close()
	type pending struct {
		provider, id, slug, title string
		runtime                   int
		poster, overview          *string
		release                   *time.Time
		genres                    []string
	}
	var items []pending
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.provider, &item.id, &item.slug, &item.title, &item.runtime, &item.poster, &item.overview, &item.release, &item.genres); err != nil {
			return fmt.Errorf("read new public movie sources failed")
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return fmt.Errorf("read new public movie sources failed")
	}
	rows.Close()
	for _, item := range items {
		var publicID int64
		err := tx.QueryRow(ctx, `SELECT id FROM public_movies
WHERE redirect_to_id IS NULL AND identity_anchor_provider=$1 AND identity_anchor_source_movie_id=$2
FOR UPDATE`, item.provider, item.id).Scan(&publicID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lock reappeared public movie identity failed")
		}
		if errors.Is(err, pgx.ErrNoRows) {
			if err = tx.QueryRow(ctx, `INSERT INTO public_movies (
    identity_anchor_provider, identity_anchor_source_movie_id, title, runtime_minutes,
    poster_url, overview, release_date, genres
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, item.provider, item.id, item.title, item.runtime, item.poster, item.overview, item.release, item.genres).Scan(&publicID); err != nil {
				return fmt.Errorf("allocate public movie identity failed")
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO public_movie_sources (
    source_provider, source_movie_id, public_movie_id, source_slug, title, runtime_minutes,
    poster_url, overview, release_date, genres
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, item.provider, item.id, publicID, item.slug, item.title, item.runtime, item.poster, item.overview, item.release, item.genres); err != nil {
			return fmt.Errorf("retain new public movie source failed")
		}
	}
	// Apply omission-preserving updates to both old and newly inserted sources.
	_, err = tx.Exec(ctx, `UPDATE public_movie_sources source SET
    source_slug=movie.slug, title=movie.title, runtime_minutes=movie.runtime_minutes,
    poster_url=COALESCE(movie.poster_url, source.poster_url),
    overview=COALESCE(NULLIF(btrim(movie.source_overview), ''), source.overview),
    release_date=COALESCE(movie.source_release_date, source.release_date),
    genres=CASE WHEN cardinality(movie.source_genres)>0 THEN movie.source_genres ELSE source.genres END,
    last_seen_at=CURRENT_TIMESTAMP
FROM movies movie, schedule_snapshot snapshot
WHERE snapshot.singleton=true AND movie.generation_id=snapshot.version
  AND source.source_provider=movie.provider AND source.source_movie_id=movie.provider_id`)
	if err != nil {
		return fmt.Errorf("refresh active public movie sources failed")
	}
	return nil
}

func loadSources(ctx context.Context, tx pgx.Tx) (map[sourceKey]*source, error) {
	rows, err := tx.Query(ctx, `SELECT source_provider, source_movie_id, public_movie_id, source_slug, title,
       runtime_minutes, poster_url, overview, release_date, genres
FROM public_movie_sources ORDER BY source_provider, source_movie_id`)
	if err != nil {
		return nil, fmt.Errorf("read public movie sources failed")
	}
	defer rows.Close()
	result := make(map[sourceKey]*source)
	for rows.Next() {
		item := &source{}
		if err := rows.Scan(&item.key.provider, &item.key.id, &item.publicID, &item.slug, &item.title, &item.runtime, &item.poster, &item.overview, &item.releaseDate, &item.genres); err != nil {
			return nil, fmt.Errorf("read public movie sources failed")
		}
		result[item.key] = item
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read public movie sources failed")
	}
	return result, nil
}

func loadPublicMovies(ctx context.Context, tx pgx.Tx) (map[int64]publicMovie, error) {
	rows, err := tx.Query(ctx, `SELECT id, COALESCE(redirect_to_id,0), identity_anchor_provider,
       identity_anchor_source_movie_id, COALESCE(confirmed_tmdb_id,0)
FROM public_movies ORDER BY id FOR UPDATE`)
	if err != nil {
		return nil, fmt.Errorf("lock public movies failed")
	}
	defer rows.Close()
	result := make(map[int64]publicMovie)
	for rows.Next() {
		var item publicMovie
		if err := rows.Scan(&item.id, &item.redirectTo, &item.anchor.provider, &item.anchor.id, &item.confirmedTMDB); err != nil {
			return nil, fmt.Errorf("lock public movies failed")
		}
		result[item.id] = item
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("lock public movies failed")
	}
	return result, nil
}

func loadLocalEvidence(ctx context.Context, tx pgx.Tx) (map[sourceKey]int64, map[int64]sourceKey, error) {
	rows, err := tx.Query(ctx, `SELECT member.local_movie_id, member.source_provider, member.source_movie_id,
       grouping.primary_source_provider, grouping.primary_source_movie_id
FROM local_movie_group_members member
JOIN local_movie_groups grouping ON grouping.id=member.local_movie_id
ORDER BY member.local_movie_id, member.source_provider, member.source_movie_id`)
	if err != nil {
		return nil, nil, fmt.Errorf("read local movie evidence failed")
	}
	defer rows.Close()
	groups := make(map[sourceKey]int64)
	primaries := make(map[int64]sourceKey)
	for rows.Next() {
		var groupID int64
		var member, primary sourceKey
		if err := rows.Scan(&groupID, &member.provider, &member.id, &primary.provider, &primary.id); err != nil {
			return nil, nil, fmt.Errorf("read local movie evidence failed")
		}
		groups[member] = groupID
		primaries[groupID] = primary
	}
	if rows.Err() != nil {
		return nil, nil, fmt.Errorf("read local movie evidence failed")
	}
	return groups, primaries, nil
}

func loadTMDBEvidence(ctx context.Context, tx pgx.Tx) (map[sourceKey]int64, error) {
	rows, err := tx.Query(ctx, `SELECT source_provider, source_movie_id, metadata_movie_id
FROM movie_matches WHERE metadata_provider='tmdb' AND status='matched'
ORDER BY source_provider, source_movie_id`)
	if err != nil {
		return nil, fmt.Errorf("read confirmed TMDB evidence failed")
	}
	defer rows.Close()
	result := make(map[sourceKey]int64)
	for rows.Next() {
		var key sourceKey
		var id int64
		if err := rows.Scan(&key.provider, &key.id, &id); err != nil || id <= 0 {
			return nil, fmt.Errorf("read confirmed TMDB evidence failed")
		}
		result[key] = id
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read confirmed TMDB evidence failed")
	}
	return result, nil
}

func buildComponents(sources map[sourceKey]*source) []*component {
	byKey := make(map[string]*component)
	keys := make([]sourceKey, 0, len(sources))
	for key := range sources {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return lessSourceKey(keys[i], keys[j]) })
	for _, key := range keys {
		item := sources[key]
		componentKey := "source:" + key.provider + ":" + key.id
		if item.localGroupID > 0 {
			componentKey = fmt.Sprintf("local:%020d", item.localGroupID)
		} else if item.confirmedTMDB > 0 {
			componentKey = fmt.Sprintf("tmdb:%020d", item.confirmedTMDB)
		}
		group := byKey[componentKey]
		if group == nil {
			group = &component{localGroupID: item.localGroupID, tmdbID: item.confirmedTMDB}
			byKey[componentKey] = group
		}
		if item.localPrimary {
			group.primary = item.key
		}
		group.members = append(group.members, item)
	}
	componentKeys := make([]string, 0, len(byKey))
	for key := range byKey {
		componentKeys = append(componentKeys, key)
	}
	sort.Strings(componentKeys)
	result := make([]*component, 0, len(componentKeys))
	for _, key := range componentKeys {
		result = append(result, byKey[key])
	}
	return result
}

func loadTMDBMetadata(ctx context.Context, tx pgx.Tx, components []*component) (map[int64]tmdbMetadata, error) {
	ids := make([]int64, 0)
	seen := make(map[int64]bool)
	for _, component := range components {
		if component.tmdbID > 0 && !seen[component.tmdbID] {
			seen[component.tmdbID] = true
			ids = append(ids, component.tmdbID)
		}
	}
	if len(ids) == 0 {
		return map[int64]tmdbMetadata{}, nil
	}
	rows, err := tx.Query(ctx, `SELECT provider_movie_id, localized_title, runtime_minutes, poster_url,
       backdrop_url, overview, release_date, genres
FROM movie_metadata_cache WHERE provider='tmdb' AND locale='fr-FR' AND provider_movie_id=ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("read canonical TMDB metadata failed")
	}
	defer rows.Close()
	result := make(map[int64]tmdbMetadata)
	for rows.Next() {
		var id int64
		var item tmdbMetadata
		if err := rows.Scan(&id, &item.title, &item.runtime, &item.poster, &item.backdrop, &item.overview, &item.releaseDate, &item.genres); err != nil {
			return nil, fmt.Errorf("read canonical TMDB metadata failed")
		}
		result[id] = item
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("read canonical TMDB metadata failed")
	}
	return result, nil
}

func chooseAnchor(component *component) sourceKey {
	if component.primary.provider != "" {
		return component.primary
	}
	members := append([]*source(nil), component.members...)
	sort.Slice(members, func(i, j int) bool { return lessSourceKey(members[i].key, members[j].key) })
	return members[0].key
}

func chooseMetadata(component *component, tmdb tmdbMetadata) metadata {
	ordered := append([]*source(nil), component.members...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].localPrimary != ordered[j].localPrimary {
			return ordered[i].localPrimary
		}
		return lessSourceKey(ordered[i].key, ordered[j].key)
	})
	result := metadata{genres: []string{}, tmdbID: component.tmdbID}
	if strings.TrimSpace(tmdb.title) != "" {
		result.title = tmdb.title
	}
	if tmdb.runtime > 0 {
		result.runtime = tmdb.runtime
	}
	result.poster, result.backdrop, result.overview, result.releaseDate = tmdb.poster, tmdb.backdrop, nonblank(tmdb.overview), tmdb.releaseDate
	if len(tmdb.genres) > 0 {
		result.genres = append([]string{}, tmdb.genres...)
	}
	for _, item := range ordered {
		if result.title == "" && strings.TrimSpace(item.title) != "" {
			result.title = item.title
		}
		if result.runtime == 0 && item.runtime > 0 {
			result.runtime = item.runtime
		}
		if result.poster == nil {
			result.poster = item.poster
		}
		if result.overview == nil {
			result.overview = nonblank(item.overview)
		}
		if result.releaseDate == nil {
			result.releaseDate = item.releaseDate
		}
		if len(result.genres) == 0 && len(item.genres) > 0 {
			result.genres = append([]string{}, item.genres...)
		}
	}
	return result
}

func assignPublicIDs(ctx context.Context, tx pgx.Tx, components []*component, movies map[int64]publicMovie) (map[int64]bool, error) {
	idComponents := make(map[int64]map[*component]bool)
	anchorComponents := make(map[int64]*component)
	for _, item := range components {
		for _, member := range item.members {
			movie, ok := movies[member.publicID]
			if !ok || movie.redirectTo != 0 {
				return nil, fmt.Errorf("public movie source points to inactive identity")
			}
			if idComponents[member.publicID] == nil {
				idComponents[member.publicID] = make(map[*component]bool)
			}
			idComponents[member.publicID][item] = true
			if containsSource(item, movie.anchor) {
				anchorComponents[member.publicID] = item
			}
		}
	}
	retainedOrphans := make(map[int64]bool)
	for id := range idComponents {
		if anchorComponents[id] == nil {
			retainedOrphans[id] = true
		}
	}
	claimed := make(map[int64]bool)
	for _, component := range components {
		candidates := make([]int64, 0)
		if component.primary.provider != "" {
			if primary := findSource(component, component.primary); primary != nil {
				candidates = append(candidates, primary.publicID)
			}
		}
		ids := componentPublicIDs(component)
		if component.tmdbID > 0 {
			candidates = append(candidates, ids...)
		}
		for _, id := range ids {
			if anchorComponents[id] == component {
				candidates = append(candidates, id)
			}
		}
		for _, id := range ids {
			if len(idComponents[id]) == 1 {
				candidates = append(candidates, id)
			}
		}
		for _, id := range candidates {
			reservedFor := anchorComponents[id]
			if id > 0 && !claimed[id] && !retainedOrphans[id] && (reservedFor == nil || reservedFor == component) {
				component.publicID = id
				claimed[id] = true
				break
			}
		}
		if component.publicID != 0 {
			continue
		}
		if err := tx.QueryRow(ctx, `INSERT INTO public_movies (
    identity_anchor_provider, identity_anchor_source_movie_id, title, runtime_minutes,
    poster_url, backdrop_url, overview, release_date, genres, confirmed_tmdb_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULL) RETURNING id`, component.anchor.provider, component.anchor.id,
			component.metadata.title, component.metadata.runtime, component.metadata.poster, component.metadata.backdrop,
			component.metadata.overview, component.metadata.releaseDate, component.metadata.genres).Scan(&component.publicID); err != nil {
			return nil, fmt.Errorf("allocate reconciled public movie failed")
		}
		claimed[component.publicID] = true
	}
	return retainedOrphans, nil
}

func persistAssignments(ctx context.Context, tx pgx.Tx, components []*component, movies map[int64]publicMovie, retainedOrphans map[int64]bool) error {
	assigned := make(map[int64]bool)
	for id := range retainedOrphans {
		assigned[id] = true
	}
	oldTargets := make(map[int64]map[int64]bool)
	desiredTMDB := make(map[int64]int64)
	for _, component := range components {
		desiredTMDB[component.publicID] = component.metadata.tmdbID
	}
	for id, movie := range movies {
		if movie.redirectTo == 0 && movie.confirmedTMDB > 0 && movie.confirmedTMDB != desiredTMDB[id] {
			if _, err := tx.Exec(ctx, "UPDATE public_movies SET confirmed_tmdb_id=NULL WHERE id=$1", id); err != nil {
				return fmt.Errorf("clear corrected public movie TMDB identity failed")
			}
		}
	}
	for _, component := range components {
		assigned[component.publicID] = true
		for _, member := range component.members {
			if oldTargets[member.publicID] == nil {
				oldTargets[member.publicID] = make(map[int64]bool)
			}
			oldTargets[member.publicID][component.publicID] = true
			if _, err := tx.Exec(ctx, `UPDATE public_movie_sources SET public_movie_id=$3
WHERE source_provider=$1 AND source_movie_id=$2`, member.key.provider, member.key.id, component.publicID); err != nil {
				return fmt.Errorf("assign public movie source failed")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE public_movies SET
    title=$2, runtime_minutes=$3, poster_url=$4, backdrop_url=$5, overview=$6,
    release_date=$7, genres=$8, confirmed_tmdb_id=$9,
    updated_at=CASE WHEN title IS DISTINCT FROM $2::varchar
        OR runtime_minutes IS DISTINCT FROM $3::integer
        OR poster_url IS DISTINCT FROM $4::varchar
        OR backdrop_url IS DISTINCT FROM $5::varchar
        OR overview IS DISTINCT FROM $6::varchar
        OR release_date IS DISTINCT FROM $7::date
        OR genres IS DISTINCT FROM $8::text[]
        OR confirmed_tmdb_id IS DISTINCT FROM $9::bigint
        THEN CURRENT_TIMESTAMP ELSE updated_at END,
    last_seen_at=GREATEST(last_seen_at, (SELECT max(last_seen_at) FROM public_movie_sources WHERE public_movie_id=$1))
WHERE id=$1 AND redirect_to_id IS NULL`, component.publicID, component.metadata.title, component.metadata.runtime,
			component.metadata.poster, component.metadata.backdrop, component.metadata.overview, component.metadata.releaseDate,
			component.metadata.genres, nullableID(component.metadata.tmdbID)); err != nil {
			return fmt.Errorf("update canonical public movie failed")
		}
	}
	for oldID, targets := range oldTargets {
		if assigned[oldID] {
			continue
		}
		if len(targets) != 1 {
			return fmt.Errorf("public movie split retention is ambiguous")
		}
		var target int64
		for target = range targets {
		}
		if _, err := tx.Exec(ctx, `UPDATE public_movies SET redirect_to_id=$2, confirmed_tmdb_id=NULL,
    updated_at=CASE WHEN redirect_to_id IS DISTINCT FROM $2 THEN CURRENT_TIMESTAMP ELSE updated_at END
WHERE id=$1 AND redirect_to_id IS NULL`, oldID, target); err != nil {
			return fmt.Errorf("write public movie redirect tombstone failed")
		}
	}
	// Flatten prior tombstones and aliases if their direct target became a loser.
	for id, movie := range movies {
		if movie.redirectTo == 0 {
			continue
		}
		target := movie.redirectTo
		for !assigned[target] {
			next := oldTargets[target]
			if len(next) != 1 {
				return fmt.Errorf("public movie redirect target is invalid")
			}
			for target = range next {
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE public_movies SET redirect_to_id=$2 WHERE id=$1", id, target); err != nil {
			return fmt.Errorf("flatten public movie redirect failed")
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE movie_slug_aliases alias SET public_movie_id=movie.redirect_to_id,
    retargeted_at=CASE WHEN alias.public_movie_id<>movie.redirect_to_id THEN CURRENT_TIMESTAMP ELSE alias.retargeted_at END
FROM public_movies movie WHERE alias.public_movie_id=movie.id AND movie.redirect_to_id IS NOT NULL`); err != nil {
		return fmt.Errorf("flatten public movie aliases failed")
	}
	return nil
}

func persistAliases(ctx context.Context, tx pgx.Tx, components []*component) error {
	for _, component := range components {
		for _, member := range component.members {
			command, err := tx.Exec(ctx, `INSERT INTO movie_slug_aliases (
    slug, public_movie_id, alias_kind, source_provider, source_movie_id
) VALUES ($1,$2,'source',$3,$4)
ON CONFLICT (slug) DO UPDATE SET
    public_movie_id=EXCLUDED.public_movie_id,
    retargeted_at=CASE WHEN movie_slug_aliases.public_movie_id<>EXCLUDED.public_movie_id THEN CURRENT_TIMESTAMP ELSE movie_slug_aliases.retargeted_at END
WHERE movie_slug_aliases.alias_kind='source'
  AND movie_slug_aliases.source_provider=EXCLUDED.source_provider
  AND movie_slug_aliases.source_movie_id=EXCLUDED.source_movie_id`, member.slug, component.publicID, member.key.provider, member.key.id)
			if err != nil || command.RowsAffected() != 1 {
				return fmt.Errorf("write source movie alias failed")
			}
			if _, err := tx.Exec(ctx, `UPDATE movie_slug_aliases SET public_movie_id=$3,
    retargeted_at=CASE WHEN public_movie_id<>$3 THEN CURRENT_TIMESTAMP ELSE retargeted_at END
WHERE alias_kind='source' AND source_provider=$1 AND source_movie_id=$2`, member.key.provider, member.key.id, component.publicID); err != nil {
				return fmt.Errorf("retarget source movie aliases failed")
			}
		}
		if component.localGroupID > 0 {
			slug := fmt.Sprintf("local-film-%d", component.localGroupID)
			if err := upsertEvidenceAlias(ctx, tx, slug, "local", component.publicID); err != nil {
				return err
			}
		}
		if component.tmdbID > 0 {
			slug := fmt.Sprintf("tmdb-film-%d", component.tmdbID)
			if err := upsertEvidenceAlias(ctx, tx, slug, "tmdb", component.publicID); err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertEvidenceAlias(ctx context.Context, tx pgx.Tx, slug, kind string, publicID int64) error {
	command, err := tx.Exec(ctx, `INSERT INTO movie_slug_aliases (slug,public_movie_id,alias_kind)
VALUES ($1,$2,$3)
ON CONFLICT (slug) DO UPDATE SET public_movie_id=EXCLUDED.public_movie_id,
    retargeted_at=CASE WHEN movie_slug_aliases.public_movie_id<>EXCLUDED.public_movie_id THEN CURRENT_TIMESTAMP ELSE movie_slug_aliases.retargeted_at END
WHERE movie_slug_aliases.alias_kind=EXCLUDED.alias_kind`, slug, publicID, kind)
	if err != nil || command.RowsAffected() != 1 {
		return fmt.Errorf("write movie evidence alias failed")
	}
	return nil
}

func validateTargets(ctx context.Context, tx pgx.Tx) error {
	var invalid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
    SELECT 1 FROM public_movies movie
    JOIN public_movies target ON target.id=movie.redirect_to_id
    WHERE movie.redirect_to_id IS NOT NULL AND target.redirect_to_id IS NOT NULL
) OR EXISTS (
    SELECT 1 FROM public_movie_sources source
    JOIN public_movies movie ON movie.id=source.public_movie_id
    WHERE movie.redirect_to_id IS NOT NULL
) OR EXISTS (
    SELECT 1 FROM movie_slug_aliases alias
    JOIN public_movies movie ON movie.id=alias.public_movie_id
    WHERE movie.redirect_to_id IS NOT NULL
)`).Scan(&invalid); err != nil || invalid {
		return fmt.Errorf("public movie reconciliation targets are invalid")
	}
	return nil
}

func componentPublicIDs(component *component) []int64 {
	set := make(map[int64]bool)
	for _, member := range component.members {
		set[member.publicID] = true
	}
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func containsSource(component *component, key sourceKey) bool {
	return findSource(component, key) != nil
}

func findSource(component *component, key sourceKey) *source {
	for _, item := range component.members {
		if item.key == key {
			return item
		}
	}
	return nil
}

func lessSourceKey(a, b sourceKey) bool {
	if a.provider != b.provider {
		if a.provider == "ugc" {
			return true
		}
		if b.provider == "ugc" {
			return false
		}
		return a.provider < b.provider
	}
	return a.id < b.id
}

func nonblank(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return value
}

func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}
