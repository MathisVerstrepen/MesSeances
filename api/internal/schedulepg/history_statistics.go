package schedulepg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"messeances/api/internal/schedule"
)

// Every history entry point shares admission and one deadline, including pool
// acquisition. There is no waiting queue and no independently timed subqueries.
func (s *Store) historyRead(ctx context.Context, read func(context.Context, pgx.Tx) error) error {
	if s == nil || s.pool == nil {
		return schedule.ErrHistoryUnavailable
	}
	for {
		active := s.historyCalls.Load()
		if active >= 2 {
			return schedule.ErrHistoryBusy
		}
		if s.historyCalls.CompareAndSwap(active, active+1) {
			break
		}
	}
	defer s.historyCalls.Add(-1)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		if ctx.Err() != nil {
			return historyReadError(ctx.Err())
		}
		return schedule.ErrHistoryUnavailable
	}
	defer rollbackScheduleTx(tx)
	// Filter selectivity varies widely. A cached generic plan can turn these
	// bounded reads into nested-loop scans after repeated prepared executions.
	// Replan within this transaction only; do not alter pooled session defaults.
	if _, err = tx.Exec(ctx, `SET LOCAL statement_timeout='2s'; SET LOCAL timezone='UTC'; SET LOCAL plan_cache_mode='force_custom_plan'`); err == nil {
		err = read(ctx, tx)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if ctx.Err() != nil {
		return historyReadError(ctx.Err())
	}
	return historyReadError(err)
}

func historyReadError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return schedule.ErrHistoryQueryTimeout
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "57014" {
			return schedule.ErrHistoryQueryTimeout
		}
		if len(pgErr.Code) >= 2 && pgErr.Code[:2] == "08" || pgErr.Code == "57P01" {
			return schedule.ErrHistoryUnavailable
		}
	}
	return err
}

// SQL JSON contains bounded aggregate rows only. Decode all SQL numeric values
// as int64 and reject unsafe JSON integers before decoding shared Go row types.
func decodeHistoryJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := checkHistoryNumbers(value); err != nil {
		return err
	}
	return json.Unmarshal(data, destination)
}

func checkHistoryNumbers(value any) error {
	switch value := value.(type) {
	case json.Number:
		n, err := value.Int64()
		if err != nil || n < 0 || n > 9007199254740991 {
			return fmt.Errorf("history count exceeds safe integer range")
		}
	case []any:
		for _, item := range value {
			if err := checkHistoryNumbers(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range value {
			if err := checkHistoryNumbers(item); err != nil {
				return err
			}
		}
	}
	return nil
}

// Same Unicode White_Space boundary set as strings.TrimSpace; lower() uses
// pg_c_utf8's Unicode simple case mapping matches strings.ToLower independently
// of database locale and UNION-derived C collations. Sorting remains bytewise.
const historyWhitespace = `U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'`

// Genre expansion and aliases mirror schedule.statisticsGenreParents and
// schedule.statisticsGenre before per-film deduplication. This read-only
// projection also supplies history options/filters.
const historyCanonicalCTE = `WITH retained_sources AS MATERIALIZED (
 SELECT DISTINCT provider,movie_provider_id FROM screening_history_showtimes
), source_movies AS MATERIALIZED (
 SELECT s.source_provider provider,s.source_movie_id movie_provider_id,c.id movie_id
 FROM retained_sources h JOIN public_movie_sources s ON (s.source_provider,s.source_movie_id)=(h.provider,h.movie_provider_id)
 JOIN public_movies p ON p.id=s.public_movie_id JOIN public_movies c ON c.id=coalesce(p.redirect_to_id,p.id)
), movies AS MATERIALIZED (
 SELECT DISTINCT c.id,CASE WHEN o.title_overridden THEN o.title ELSE c.title END title,
 CASE WHEN o.runtime_minutes_overridden THEN o.runtime_minutes ELSE c.runtime_minutes END runtime,
 CASE WHEN o.genres_overridden THEN o.genres ELSE c.genres END genres
 FROM source_movies s JOIN public_movies c ON c.id=s.movie_id LEFT JOIN public_movie_metadata_overrides o ON o.public_movie_id=c.id
), movie_genres AS MATERIALIZED (
 SELECT m.id,coalesce(lower(g.label COLLATE pg_catalog.pg_c_utf8),'unknown') value,coalesce(min(g.label COLLATE "C"),'Non renseigné') label
  FROM movies m LEFT JOIN LATERAL (
   SELECT unnest(CASE
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8)='comédie dramatique' THEN ARRAY['Comédie','Drame']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8)='comédie romantique' THEN ARRAY['Comédie','Romance']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8)='comédie d''action' THEN ARRAY['Comédie','Action']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8) IN ('familial','famille','famille/enfants') THEN ARRAY['Famille']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8) IN ('opéra','opera') THEN ARRAY['Opéra']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8) IN ('science-fiction','science fiction') THEN ARRAY['Science-fiction']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8) IN ('histoire','historique') THEN ARRAY['Histoire']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8) IN ('horreur','horreur / épouvante') THEN ARRAY['Horreur']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8) IN ('romance','amour') THEN ARRAY['Romance']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8) IN ('animation','dessin animé') THEN ARRAY['Animation']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8)='comédie' THEN ARRAY['Comédie']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8)='drame' THEN ARRAY['Drame']
    WHEN lower(label COLLATE pg_catalog.pg_c_utf8)='action' THEN ARRAY['Action']
    ELSE ARRAY[label] END) label
   FROM (SELECT btrim(genre,` + historyWhitespace + `) label FROM unnest(m.genres) genre) raw WHERE label<>''
  ) g ON true
 GROUP BY m.id,lower(g.label COLLATE pg_catalog.pg_c_utf8)
), theaters AS MATERIALIZED (
 SELECT t.* FROM screening_history_theaters t WHERE EXISTS (SELECT 1 FROM screening_history_showtimes h WHERE h.theater_id=t.id)
)`

// Validate each retained association once rather than joining every screening.
// Source and movie keys are unique, so absence of a valid one-hop canonical
// target is equivalent to any broken association in the original screening set.
const historyIntegritySQL = `SELECT EXISTS (
 SELECT 1 FROM (SELECT DISTINCT provider,theater_id FROM screening_history_showtimes) h
 WHERE NOT EXISTS (SELECT 1 FROM screening_history_theaters t WHERE (t.provider,t.id)=(h.provider,h.theater_id))
) OR EXISTS (
 SELECT 1 FROM (SELECT DISTINCT provider,movie_provider_id FROM screening_history_showtimes) h
 WHERE NOT EXISTS (
  SELECT 1 FROM public_movie_sources s
  JOIN public_movies p ON p.id=s.public_movie_id
  JOIN public_movies c ON c.id=coalesce(p.redirect_to_id,p.id)
  WHERE (s.source_provider,s.source_movie_id)=(h.provider,h.movie_provider_id) AND c.redirect_to_id IS NULL
 )
)`

func checkHistoryIntegrity(ctx context.Context, tx pgx.Tx) error {
	var broken bool
	if err := tx.QueryRow(ctx, historyIntegritySQL).Scan(&broken); err != nil {
		return err
	}
	if broken {
		return fmt.Errorf("history canonical association is invalid")
	}
	return nil
}

func (s *Store) HistoryStatistics(ctx context.Context, query schedule.StatisticsQuery) (schedule.HistoryStatistics, error) {
	query, err := schedule.NormalizeHistoryQuery(query)
	if err != nil {
		return schedule.HistoryStatistics{}, err
	}
	result := schedule.HistoryStatistics{Mode: "history", Timezone: schedule.Timezone}
	err = s.historyRead(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := checkHistoryIntegrity(ctx, tx); err != nil {
			return err
		}
		var data []byte
		if err := tx.QueryRow(ctx, historyCoverageSQL).Scan(&result.GeneratedAt, &data); err != nil {
			return err
		}
		if err := decodeHistoryJSON(data, &result.Coverage); err != nil {
			return err
		}
		result.Range = result.Coverage.RecordedWindow
		if query.Date != "" {
			result.Range = &schedule.Window{From: query.Date, Through: query.DateTo}
		}
		filmID, err := historyFilmID(ctx, tx, query.Film)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, historyStatisticsSQL, query.Date, query.DateTo, query.City, query.Theater, query.Chain, query.Language, query.Format, query.Genre, query.Pass, filmID).Scan(&data); err != nil {
			return err
		}
		// Unmarshal only aggregate fields, preserving coverage and request range.
		if err := decodeHistoryJSON(data, &result); err != nil {
			return err
		}
		return historyStatisticsOptions(ctx, tx, query, &result)
	})
	if err != nil {
		return schedule.HistoryStatistics{}, err
	}
	return result, nil
}

// NULL means no filter; zero means an unknown identity, never an unrestricted read.
// Registered aliases target only nonredirecting rows and take precedence, just
// like SnapshotView.movieAlias. Canonical IDs follow at most one redirect.
func historyFilmID(ctx context.Context, tx pgx.Tx, slug string) (*int64, error) {
	if slug == "" {
		return nil, nil
	}
	var lookupID int64
	if id, err := strconv.ParseInt(strings.TrimPrefix(slug, "film-"), 10, 64); err == nil && id > 0 && slug == "film-"+strconv.FormatInt(id, 10) {
		lookupID = id
	}
	var id int64
	err := tx.QueryRow(ctx, `SELECT coalesce(
 (SELECT p.id FROM movie_slug_aliases a JOIN public_movies p ON p.id=a.public_movie_id WHERE a.slug=$1 AND p.redirect_to_id IS NULL),
 (SELECT c.id FROM public_movies p JOIN public_movies c ON c.id=coalesce(p.redirect_to_id,p.id) WHERE p.id=$2 AND c.redirect_to_id IS NULL),
 0)::bigint`, slug, lookupID).Scan(&id)
	return &id, err
}

const historyCoverageSQL = `SELECT transaction_timestamp(),jsonb_build_object(
 'collection_started_at',(SELECT min(collection_started_at) FROM screening_history_providers),
 'last_publication_at',(SELECT max(last_publication_at) FROM screening_history_providers),
 'recorded_window',(SELECT CASE WHEN min(service_date) IS NULL THEN NULL ELSE jsonb_build_object('from',min(service_date)::text,'through',max(service_date)::text) END FROM screening_history_showtimes),
 'completeness','unknown','bootstrap','none','providers',coalesce((SELECT jsonb_agg(to_jsonb(p) ORDER BY provider) FROM (SELECT provider,collection_started_at,last_publication_at,source_generated_at FROM screening_history_providers ORDER BY provider LIMIT 10) p),'[]'))`

// Select theater keys once and semi-join them. Joining the materialized theater
// inventory here can rescan it for every screening when combined filters are
// underestimated; membership preserves the same unique-theater semantics.
// Count narrow canonical movie/theater pairs before joining display metadata.
// Reuse their weights for rankings without multiplying screenings by genres,
// passes or the number of source identities mapped to one public movie.
const historyStatisticsSQL = historyCanonicalCTE + `, matched_theaters AS MATERIALIZED (
 SELECT t.id FROM theaters t
 WHERE (coalesce(cardinality($3::text[]),0)=0 OR t.city_slug=ANY($3)) AND (coalesce(cardinality($4::text[]),0)=0 OR t.id=ANY($4))
 AND ($5='' OR t.provider=$5) AND ($9='' OR $9=ANY(t.passes))
), matched AS MATERIALIZED (
 SELECT h.service_date,h.start_time,h.theater_id,s.movie_id,
 CASE WHEN h.language IN ('VF','VOSTFR','VO','VF_SME','VFSTF') THEN h.language ELSE 'unknown' END language,
 CASE WHEN h.format IN ('2D','3D','IMAX','DOLBY','SCREENX','LASER_ULTRA','4DX','ICE') THEN h.format ELSE 'unknown' END format
 FROM screening_history_showtimes h JOIN source_movies s USING(provider,movie_provider_id)
 WHERE ($1::text='' OR h.service_date>=nullif($1,'')::date) AND ($2::text='' OR h.service_date<=nullif($2,'')::date)
 AND h.theater_id IN (SELECT id FROM matched_theaters)
 AND ($10::bigint IS NULL OR s.movie_id=$10)
 AND ($6='' OR CASE WHEN h.language IN ('VF','VOSTFR','VO','VF_SME','VFSTF') THEN h.language ELSE 'unknown' END=$6)
 AND ($7='' OR CASE WHEN h.format IN ('2D','3D','IMAX','DOLBY','SCREENX','LASER_ULTRA','4DX','ICE') THEN h.format ELSE 'unknown' END=$7)
 AND ($8='' OR EXISTS (SELECT 1 FROM movie_genres g WHERE g.id=s.movie_id AND g.value=$8))
), movie_theaters AS MATERIALIZED (
 SELECT movie_id,theater_id,count(*) showtime_count FROM matched GROUP BY movie_id,theater_id
), movie_counts AS MATERIALIZED (
 SELECT 'film-'||m.id slug,m.id,m.title,m.runtime,c.showtime_count,c.theater_count
 FROM (SELECT movie_id,sum(showtime_count) showtime_count,count(*) theater_count FROM movie_theaters GROUP BY movie_id) c
 JOIN movies m ON m.id=c.movie_id
), top_showtimes AS (
 SELECT slug,title,showtime_count,theater_count FROM movie_counts ORDER BY showtime_count DESC,theater_count DESC,lower(btrim(title,` + historyWhitespace + `) COLLATE pg_catalog.pg_c_utf8) COLLATE "C",slug COLLATE "C" LIMIT 10
), top_theaters AS (
 SELECT slug,title,showtime_count,theater_count FROM movie_counts ORDER BY theater_count DESC,showtime_count DESC,lower(btrim(title,` + historyWhitespace + `) COLLATE pg_catalog.pg_c_utf8) COLLATE "C",slug COLLATE "C" LIMIT 10
), city_counts AS MATERIALIZED (
 SELECT t.city_slug slug,min(t.city_name COLLATE "C") name,sum(h.showtime_count) showtime_count,count(DISTINCT h.movie_id) movie_count,count(DISTINCT t.id) theater_count
 FROM movie_theaters h JOIN theaters t ON t.id=h.theater_id GROUP BY t.city_slug
), city_ranks AS (
 SELECT * FROM city_counts ORDER BY showtime_count DESC,movie_count DESC,lower(btrim(name,` + historyWhitespace + `) COLLATE pg_catalog.pg_c_utf8) COLLATE "C",slug COLLATE "C" LIMIT 100
), theater_counts AS MATERIALIZED (
 SELECT t.id,t.slug,t.name,t.city_name city,t.city_slug,t.provider chain,c.showtime_count,c.movie_count
 FROM (SELECT theater_id,sum(showtime_count) showtime_count,count(*) movie_count FROM movie_theaters GROUP BY theater_id) c
 JOIN theaters t ON t.id=c.theater_id
), theater_ranks AS (
 SELECT * FROM theater_counts ORDER BY showtime_count DESC,movie_count DESC,lower(btrim(name,` + historyWhitespace + `) COLLATE pg_catalog.pg_c_utf8) COLLATE "C",id COLLATE "C" LIMIT 100
), genre_counts AS MATERIALIZED (
 SELECT g.value,CASE WHEN g.value='unknown' THEN 'Non renseigné' ELSE (SELECT min(all_genres.label COLLATE "C") FROM movie_genres all_genres WHERE all_genres.value=g.value) END label,count(DISTINCT m.id) count
 FROM movie_counts m JOIN movie_genres g ON g.id=m.id GROUP BY g.value
), genre_ranks AS (
 SELECT * FROM genre_counts ORDER BY count DESC,lower(btrim(label,` + historyWhitespace + `) COLLATE pg_catalog.pg_c_utf8) COLLATE "C",value COLLATE "C" LIMIT 100
), heat AS (
 SELECT extract(isodow FROM service_date)::int weekday,extract(hour FROM start_time AT TIME ZONE 'Europe/Paris')::int AS hour,count(*) showtime_count FROM matched GROUP BY 1,2
), versions AS (
 SELECT language value,CASE WHEN language='unknown' THEN 'Non renseigné' ELSE language END label,count(*) count FROM matched GROUP BY language ORDER BY count DESC,lower(CASE WHEN language='unknown' THEN 'Non renseigné' ELSE language END) COLLATE "C",language COLLATE "C"
), formats AS (
 SELECT format value,CASE WHEN format='unknown' THEN 'Non renseigné' ELSE format END label,count(*) count FROM matched GROUP BY format ORDER BY count DESC,lower(CASE WHEN format='unknown' THEN 'Non renseigné' ELSE format END) COLLATE "C",format COLLATE "C"
), runtime_counts AS (
 SELECT CASE WHEN runtime<=0 OR runtime>153722867 THEN 'unknown' WHEN runtime<90 THEN 'short' WHEN runtime<=120 THEN 'medium' ELSE 'long' END value,count(*) count FROM movie_counts GROUP BY 1
)
SELECT jsonb_build_object(
 'totals',jsonb_build_object('showtimes',(SELECT count(*) FROM matched),'movies',(SELECT count(*) FROM movie_counts),'cities',(SELECT count(*) FROM city_counts),'theaters',(SELECT count(*) FROM theater_counts)),
 'top_movies',jsonb_build_object('by_showtimes',coalesce((SELECT jsonb_agg(t) FROM top_showtimes t),'[]'),'by_theaters',coalesce((SELECT jsonb_agg(t) FROM top_theaters t),'[]')),
 'heatmap',(SELECT jsonb_agg(jsonb_build_object('weekday',d,'hour',h,'showtime_count',coalesce(x.showtime_count,0)) ORDER BY d,h) FROM generate_series(1,7) d CROSS JOIN generate_series(0,23) h LEFT JOIN heat x ON x.weekday=d AND x.hour=h),
 'versions',coalesce((SELECT jsonb_agg(v) FROM versions v),'[]'),'formats',coalesce((SELECT jsonb_agg(f) FROM formats f),'[]'),
 'genres',coalesce((SELECT jsonb_agg(g) FROM genre_ranks g),'[]'),
 'runtimes',(SELECT jsonb_agg(jsonb_build_object('value',r.value,'label',r.label,'count',coalesce(c.count,0)) ORDER BY r.position) FROM (VALUES (1,'short','Moins de 1h30'),(2,'medium','De 1h30 à 2h'),(3,'long','Plus de 2h'),(4,'unknown','Non renseignée')) r(position,value,label) LEFT JOIN runtime_counts c USING(value)),
 'local',jsonb_build_object('cities',coalesce((SELECT jsonb_agg(c) FROM city_ranks c),'[]'),'theaters',coalesce((SELECT jsonb_agg(t) FROM theater_ranks t),'[]')),
 'concentration',jsonb_build_object('top_movie_count',(SELECT count(*) FROM top_showtimes),'top_showtime_count',coalesce((SELECT sum(showtime_count) FROM top_showtimes),0),'other_showtime_count',(SELECT count(*) FROM matched)-coalesce((SELECT sum(showtime_count) FROM top_showtimes),0)),
 'limits',jsonb_build_object('genres',(SELECT count(*)>100 FROM genre_counts),'local',jsonb_build_object('cities',(SELECT count(*)>100 FROM city_counts),'theaters',(SELECT count(*)>100 FROM theater_counts)))
)`
