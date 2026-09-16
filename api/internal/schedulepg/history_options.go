package schedulepg

import (
	"context"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/schedule"
)

const historyOptionsCTE = historyCanonicalCTE + `, inventory AS MATERIALIZED (
 SELECT 'city' kind,city_slug value,min(city_name COLLATE "C") label,min(city_name COLLATE "C") sort_label,
 jsonb_build_object('slug',city_slug,'name',min(city_name COLLATE "C")) item FROM theaters GROUP BY city_slug
 UNION ALL SELECT 'theater',id,name||' ('||city_name||')',name,
 jsonb_build_object('id',id,'slug',slug,'name',name,'city',city_name,'city_slug',city_slug,'chain',provider,'passes',passes) FROM theaters
 UNION ALL SELECT 'genre',value,CASE WHEN value='unknown' THEN 'Non renseigné' ELSE min(label COLLATE "C") END,
 CASE WHEN value='unknown' THEN 'Non renseigné' ELSE min(label COLLATE "C") END,
 jsonb_build_object('value',value,'label',CASE WHEN value='unknown' THEN 'Non renseigné' ELSE min(label COLLATE "C") END) FROM movie_genres GROUP BY value
 UNION ALL SELECT DISTINCT 'pass',p,p,p,to_jsonb(p) FROM theaters CROSS JOIN LATERAL unnest(passes) p
)`

const historyOptionsSQL = historyOptionsCTE + `, matches AS MATERIALIZED (
 SELECT value,label FROM inventory WHERE kind=$1 AND ($2='' OR strpos(lower(label COLLATE pg_catalog.pg_c_utf8),$2)>0 OR strpos(lower(value COLLATE pg_catalog.pg_c_utf8),$2)>0)
 ORDER BY lower(btrim(sort_label,` + historyWhitespace + `) COLLATE pg_catalog.pg_c_utf8) COLLATE "C",value COLLATE "C" LIMIT 101
), selected AS (
 SELECT value,label FROM inventory WHERE kind=$1 AND value=ANY($3::text[]) ORDER BY array_position($3,value) LIMIT 50
)
SELECT jsonb_build_object('items',coalesce((SELECT jsonb_agg(m) FROM (SELECT * FROM matches LIMIT 100) m),'[]'),
 'selected',coalesce((SELECT jsonb_agg(s) FROM selected s),'[]'),'has_more',(SELECT count(*)>100 FROM matches))`

func (s *Store) HistoryOptions(ctx context.Context, query schedule.HistoryOptionsQuery) (schedule.HistoryOptions, error) {
	query, err := schedule.NormalizeHistoryOptionsQuery(query)
	if err != nil {
		return schedule.HistoryOptions{}, err
	}
	var result schedule.HistoryOptions
	err = s.historyRead(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := checkHistoryIntegrity(ctx, tx); err != nil {
			return err
		}
		var data []byte
		if err := tx.QueryRow(ctx, historyOptionsSQL, query.Kind, query.Q, query.Selected).Scan(&data); err != nil {
			return err
		}
		return decodeHistoryJSON(data, &result)
	})
	if err != nil {
		return schedule.HistoryOptions{}, err
	}
	return result, nil
}

func historyStatisticsOptions(ctx context.Context, tx pgx.Tx, query schedule.StatisticsQuery, result *schedule.HistoryStatistics) error {
	var data []byte
	if err := tx.QueryRow(ctx, historyStatisticsOptionsSQL, query.City, query.Theater, query.Genre, query.Pass).Scan(&data); err != nil {
		return err
	}
	return decodeHistoryJSON(data, result)
}

const historyStatisticsOptionsSQL = historyOptionsCTE + `, ranked AS MATERIALIZED (
 SELECT *,row_number() OVER (PARTITION BY kind ORDER BY lower(btrim(sort_label,` + historyWhitespace + `) COLLATE pg_catalog.pg_c_utf8) COLLATE "C",value COLLATE "C") position FROM inventory
), chosen AS MATERIALIZED (
 SELECT * FROM ranked WHERE position<=100 OR (kind='city' AND value=ANY($1::text[])) OR (kind='theater' AND value=ANY($2::text[])) OR (kind='genre' AND value=$3) OR (kind='pass' AND value=$4)
), observed AS MATERIALIZED (
 SELECT DISTINCT CASE WHEN language IN ('VF','VOSTFR','VO','VF_SME','VFSTF') THEN language ELSE 'unknown' END language,
 CASE WHEN format IN ('2D','3D','IMAX','DOLBY','SCREENX','LASER_ULTRA','4DX','ICE') THEN format ELSE 'unknown' END format FROM screening_history_showtimes
)
SELECT jsonb_build_object('options',jsonb_build_object(
 'cities',coalesce((SELECT jsonb_agg(item ORDER BY position) FROM chosen WHERE kind='city'),'[]'),
 'theaters',coalesce((SELECT jsonb_agg(item ORDER BY position) FROM chosen WHERE kind='theater'),'[]'),
 'genres',coalesce((SELECT jsonb_agg(item ORDER BY position) FROM chosen WHERE kind='genre'),'[]'),
 'passes',coalesce((SELECT jsonb_agg(item ORDER BY position) FROM chosen WHERE kind='pass'),'[]'),
 'chains',coalesce((SELECT jsonb_agg(provider ORDER BY provider) FROM (SELECT DISTINCT provider FROM theaters) c),'[]'),
 'languages',coalesce((SELECT jsonb_agg(language ORDER BY language) FROM (SELECT DISTINCT language FROM observed) l),'[]'),
 'formats',coalesce((SELECT jsonb_agg(format ORDER BY format) FROM (SELECT DISTINCT format FROM observed) f),'[]')),
 'limits',jsonb_build_object('options',jsonb_build_object(
 'cities',EXISTS(SELECT 1 FROM ranked WHERE kind='city' AND position=101),
 'theaters',EXISTS(SELECT 1 FROM ranked WHERE kind='theater' AND position=101),
 'genres',EXISTS(SELECT 1 FROM ranked WHERE kind='genre' AND position=101),
 'passes',EXISTS(SELECT 1 FROM ranked WHERE kind='pass' AND position=101))))`
