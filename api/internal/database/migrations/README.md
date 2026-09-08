# Database schema

This document describes the PostgreSQL schema after applying migrations `001_initial.sql` through [030_public_movie_metadata_overrides.sql](030_public_movie_metadata_overrides.sql). The SQL files are the source of truth. Update this document when adding a migration; this is the resulting schema, not a migration-by-migration changelog or a report of a deployed database.

## Migration execution

[RunMigrations](../postgres.go) embeds only `migrations/*.sql`, orders files by their numeric prefix, and applies pending migrations in one transaction protected by a transaction-scoped advisory lock. Recorded versions and filenames must match a prefix of the embedded history. History validation does not compare SQL checksums. This README is not embedded or executed. The application connection pool uses the `public` search path.

`movieflow_schema_migrations` stores the version, filename, and application timestamp of each applied migration.

It is created by the Go runner, not by a numbered migration:

| Column | Type | Definition |
| --- | --- | --- |
| `version` | `bigint` | Primary key |
| `name` | `text` | Not null; migration filename |
| `applied_at` | `timestamptz` | Not null; default `now()` |

## Conventions and relationships

- In the column tables below, columns are **not null with no default** unless marked nullable or given a default. Primary keys also imply not null. `identity` means `GENERATED ALWAYS AS IDENTITY`.
- Providers are `ugc`, `kinepolis`, `pathe`, and `cgr`, unless a table explicitly allows another value. Provider fields are strings with checks, not PostgreSQL enums.
- Runtime fields are `integer >= 0`; `0` represents an unknown runtime. The old `smallint`, 600-minute limit, and positive-only checks no longer apply.
- `timestamptz` stores instants; `date` stores calendar/service dates. Schedule metadata fixes the timezone to `Europe/Paris`.
- Primary keys and unique constraints create implicit indexes. Additional indexes are listed separately below. Foreign keys use default `NO ACTION` deletion behavior unless `CASCADE` is specified.
- PostgreSQL `CHECK` accepts both true and SQL null. A check on a nullable column does not itself make that column required. Important cases are noted below.

The generation-scoped schedule consists of `provider_snapshots`, `theaters`, `theater_dates`, `theater_passes`, `movies`, and `showtimes`. Their `generation_id` is a required positive `bigint`. `schedule_snapshot.version` selects the active generation by application convention; there is no generation parent table or foreign key to the snapshot. Multiple generations can coexist, so schedule joins must include `generation_id`.

```text
schedule_snapshot.version .. active generation (logical link)
theaters (generation_id, id)
  <- theater_dates (generation_id, theater_id)
       <- showtimes (generation_id, theater_id, service_date)
  <- theater_passes (generation_id, theater_id) -> passes
movies (generation_id, provider, provider_id)
  <- showtimes (generation_id, provider, movie_provider_id)

public_movies
  <- public_movie_sources
  <- movie_slug_aliases
  <- public_movie_metadata_overrides
  <- public_movies.redirect_to_id
local_movie_groups <-> local_movie_group_members
sync_schedules <- sync_schedule_occurrence_claims
```

Movie matches, metadata cache entries, local group source identities, public catalog source identities, and theater locations persist independently of schedule generations. There are no foreign keys from these source identities to generation-scoped `movies` or `theaters`. `sync_runs.schedule_id` is also not a foreign key.

### Provider identity checks

These rules apply to provider theater IDs, provider movie IDs (including source IDs and identity anchors), and showing IDs. `theater_locations.provider_theater_id` is an exception: it only has a nonblank check.

| Provider | Theater ID | Movie/source movie ID | Showing ID |
| --- | --- | --- | --- |
| `ugc` | `^[1-9][0-9]*$` | `^[1-9][0-9]*$` | `^[1-9][0-9]*$` |
| `kinepolis` | `^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$` | Same as theater ID | Same as theater ID |
| `pathe` | `^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$` | Same as theater ID | `^V[1-9][0-9]*S[1-9][0-9]*$` |
| `cgr` | `^[A-Z][0-9]{4}$` | `^[1-9][0-9]{0,127}$` | `^[A-Z][0-9]{4}-[a-f0-9]{64}$` |

Identity columns use `varchar(128)` unless documented otherwise. Derived IDs and slugs must also fit their own 128-character columns. Pathé showing identities include both venue and session tokens, not just an `S...` token.

## Schedule tables

### `schedule_snapshot`

Stores the active schedule generation version and its provider, time window, and generation metadata in at most one row.

| Column | Type | Definition |
| --- | --- | --- |
| `singleton` | `boolean` | Primary key; default `true`; must be true |
| `version` | `bigint` | Positive active generation version |
| `schema_version` | `integer` | Must equal `1` |
| `provider` | `varchar(32)` | Provider or `combined` |
| `scope` | `varchar(32)` | Must equal `all_cinemas` |
| `generated_at` | `timestamptz` | Generation timestamp |
| `timezone` | `varchar(64)` | Must equal `Europe/Paris` |
| `window_from`, `window_through` | `date` | Inclusive schedule window |

Check: `window_through >= window_from`. There is no longer a 14-day maximum window.

### `provider_snapshots`

Stores each provider's snapshot timestamp, coverage window, and schema metadata within a schedule generation.

Primary key: `(generation_id, provider)`.

| Column | Type | Definition |
| --- | --- | --- |
| `generation_id` | `bigint` | Positive generation ID |
| `provider` | `varchar(32)` | Provider; not `combined` |
| `schema_version` | `integer` | Must equal `1` |
| `scope` | `varchar(32)` | Must equal `all_cinemas` |
| `generated_at` | `timestamptz` | Provider snapshot timestamp |
| `timezone` | `varchar(64)` | Must equal `Europe/Paris` |
| `window_from`, `window_through` | `date` | Check: `window_through >= window_from`; no maximum span |

### `theaters`

Stores cinema identities, names, and postal addresses for each schedule generation.

Primary key: `(generation_id, id)`. Unique: `(generation_id, slug)` and `(generation_id, provider, provider_id)`.

| Column | Type | Definition |
| --- | --- | --- |
| `generation_id` | `bigint` | Positive generation ID |
| `id`, `provider_id`, `slug` | `varchar(128)` | `id = provider || '-' || provider_id`; `slug = id` |
| `provider` | `varchar(32)` | Provider; default `ugc` |
| `name` | `varchar(1024)` | Nonblank after trimming |
| `address` | `varchar(2048)` | Nonblank except for Kinepolis |
| `city` | `varchar(256)` | Nonblank after trimming |
| `postal_code` | `varchar(256)` | Nonblank except for Kinepolis |

### `theater_dates`, `passes`, and `theater_passes`

- `theater_dates` stores the service dates included for each cinema in a schedule generation.
- `passes` stores the supported cinema subscription pass codes.
- `theater_passes` stores which subscription passes each cinema accepts within a schedule generation.

| Table | Columns | Keys and relationships |
| --- | --- | --- |
| `theater_dates` | `generation_id bigint`, `theater_id varchar(128)`, `service_date date` | PK `(generation_id, theater_id, service_date)`; FK `(generation_id, theater_id)` to `theaters(generation_id, id)` with `ON DELETE CASCADE` |
| `passes` | `code varchar(256)` | PK `code`; only allowed value is `UGC_ILLIMITE` |
| `theater_passes` | `generation_id bigint`, `theater_id varchar(128)`, `pass_code varchar(256)` | PK `(generation_id, theater_id, pass_code)`; FK to `theaters(generation_id, id)` with `ON DELETE CASCADE`; FK `pass_code` to `passes(code)` with `ON DELETE CASCADE` |

### `movies`

Stores provider-supplied movie identities, titles, runtimes, and source metadata within each schedule generation, separately from the durable public catalog.

Primary key: `(generation_id, provider, provider_id)`. Unique: `(generation_id, slug)`.

| Column | Type | Definition |
| --- | --- | --- |
| `generation_id` | `bigint` | Positive generation ID |
| `provider` | `varchar(32)` | Provider; default `ugc` |
| `provider_id`, `slug` | `varchar(128)` | `slug = provider || '-film-' || provider_id` |
| `title` | `varchar(1024)` | Nonblank after trimming |
| `runtime_minutes` | `integer` | Nonnegative |
| `poster_url` | `varchar(4096)` | Nullable; no URL check here |
| `source_overview` | `varchar(10000)` | Nullable; length at most 10,000 |
| `source_release_date` | `date` | Nullable |
| `source_genres` | `text[]` | Default `'{}'`; at most 32 elements |

### `showtimes`

Stores individual screenings with their cinema, movie, service date, start and end times, language, format, room, and booking URL.

Primary key: `(generation_id, id)`. Unique: `(generation_id, provider, provider_showing_id)`.

| Column | Type | Definition |
| --- | --- | --- |
| `generation_id` | `bigint` | Positive generation ID |
| `id`, `provider_showing_id` | `varchar(128)` | `id = provider || '-showing-' || provider_showing_id` |
| `provider` | `varchar(32)` | Provider; default `ugc` |
| `service_date` | `date` | Service date |
| `theater_id`, `movie_provider_id` | `varchar(128)` | Referenced schedule identities |
| `start_time`, `end_time` | `timestamptz` | End must follow start, except CGR permits equality |
| `language` | `varchar(16)` | Matches `^[A-Z][A-Z0-9_]{0,15}$`; cannot be `ALL` |
| `provider_version` | `varchar(256)` | Nonblank after trimming |
| `format` | `varchar(16)` | `2D`, `3D`, `IMAX`, `DOLBY`, `SCREENX`, `LASER_ULTRA`, `4DX`, or `ICE` |
| `room` | `varchar(256)` | Empty string permitted |
| `booking_url` | `varchar(4096)` | Nonblank after trimming; no URL pattern check |

Foreign keys: `(generation_id, provider, movie_provider_id)` references `movies(generation_id, provider, provider_id)`; `(generation_id, theater_id, service_date)` references `theater_dates(generation_id, theater_id, service_date)`. Neither cascades on deletion. The database does not check that the showtime provider matches the referenced theater provider.

## Movie enrichment and grouping

### `movie_matches`

Stores each provider movie's TMDB matching decision, confidence score, candidate matches, and evaluation and retry timestamps.

Primary key: `(source_provider, source_movie_id, metadata_provider)`.

| Column | Type | Definition |
| --- | --- | --- |
| `source_provider` | `varchar(32)` | Provider |
| `source_movie_id` | `varchar(128)` | Provider movie identity |
| `metadata_provider` | `varchar(32)` | Must equal `tmdb` |
| `status` | `varchar(32)` | `matched`, `review_required`, `unmatched`, or `rejected` |
| `metadata_movie_id` | `bigint` | Nullable; matched ID is checked as positive |
| `score` | `double precision` | Nullable; between 0 and 1 when present |
| `normalized_source_title` | `varchar(1024)` | Nonblank after trimming |
| `source_runtime_minutes` | `integer` | Nonnegative |
| `candidates` | `jsonb` | Default `'[]'::jsonb`; array of at most five entries |
| `evaluated_at`, `retry_after`, `updated_at` | `timestamptz` | Evaluation, retry, and update timestamps |

For `matched`, the check requires a nonnull score and tests `metadata_movie_id > 0`; because that ID is nullable, SQL null can still pass. For every other status, ID and score must both be null. There is no foreign key to `movie_metadata_cache`.

### `movie_metadata_cache`

Stores cached French-language TMDB movie metadata, artwork, trailer keys, IMDb IDs, and cache refresh timestamps.

Primary key: `(provider, provider_movie_id, locale)`.

| Column | Type | Definition |
| --- | --- | --- |
| `provider` | `varchar(32)` | Must equal `tmdb` |
| `provider_movie_id` | `bigint` | Positive |
| `locale` | `varchar(16)` | Must equal `fr-FR` |
| `provider_title`, `localized_title` | `varchar(1024)` | Nonblank after trimming |
| `overview` | `varchar(10000)` | Nullable; length at most 10,000 |
| `release_date` | `date` | Nullable |
| `poster_url` | `varchar(4096)` | Nullable; must start with `https://` |
| `backdrop_url` | `varchar(4096)` | Nullable; restricted TMDB URL, described below |
| `runtime_minutes` | `integer` | Nonnegative |
| `genres` | `text[]` | Default `'{}'`; at most 32 elements |
| `fetched_at`, `refresh_after` | `timestamptz` | Cache timestamps |
| `trailer_vf_youtube_key`, `trailer_vo_youtube_key` | `varchar(11)` | Nullable; each matches `^[A-Za-z0-9_-]{11}$`; must differ if both present |
| `imdb_id` | `varchar(32)` | Nullable; matches `^tt[0-9]{7,30}$` |

Cache backdrops must start with `https://image.tmdb.org/t/p/w780/` and have a nonempty suffix that does not start with `/`. The URL cannot contain `%`, `?`, `#`, a backslash, or `..`. This is not the same check as the public catalog backdrop check. The former single `trailer_youtube_key` column was removed in migration 026.

### `movie_enrichment_state` and `theater_location_state`

- `movie_enrichment_state` stores the current version counter for movie enrichment data.
- `theater_location_state` stores the current version counter for theater location data.

Each is an independent singleton version counter with the same definition: `singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton)` and `version bigint NOT NULL CHECK (version >= 0)`. Their migrations seed `(true, 0)`. The constraint permits at most one row; it does not prevent deleting that row.

### `local_movie_groups` and `local_movie_group_members`

- `local_movie_groups` stores local movie group identities and the primary provider movie selected for each group.
- `local_movie_group_members` stores the provider movie identities belonging to each local movie group.

| Table | Columns | Keys and relationships |
| --- | --- | --- |
| `local_movie_groups` | `id bigint identity` (positive), `primary_source_provider varchar(32)`, `primary_source_movie_id varchar(128)` | PK `id`; provider movie identity checks apply to the primary source |
| `local_movie_group_members` | `local_movie_id bigint`, `source_provider varchar(32)`, `source_movie_id varchar(128)` | PK `(local_movie_id, source_provider, source_movie_id)`; unique `(source_provider, source_movie_id)`; FK `local_movie_id` to `local_movie_groups(id)` with `ON DELETE CASCADE` |

The group also has a reverse FK `(id, primary_source_provider, primary_source_movie_id)` to the member primary key. It is `DEFERRABLE INITIALLY DEFERRED`, so the primary source must be a member by transaction commit. Each source movie can belong to only one local group.

## Durable public movie catalog

### `public_movies`

Stores durable public movie identities and their catalog metadata, confirmed TMDB association, and optional redirect to another public movie.

Primary key: `id`. Public identities survive schedule generations and can redirect to another public movie.

| Column | Type | Definition |
| --- | --- | --- |
| `id` | `bigint identity` | Positive |
| `redirect_to_id` | `bigint` | Nullable; FK to `public_movies(id)`; cannot equal `id` |
| `identity_anchor_provider` | `varchar(32)` | Provider |
| `identity_anchor_source_movie_id` | `varchar(128)` | Provider movie identity |
| `title` | `varchar(1024)` | Nonblank after trimming |
| `runtime_minutes` | `integer` | Nonnegative |
| `poster_url` | `varchar(4096)` | Nullable; must start with `https://` |
| `backdrop_url` | `varchar(4096)` | Nullable; TMDB `https://image.tmdb.org/t/p/w780/` prefix followed by a nonempty suffix containing no `/`, `?`, `#`, or backslash |
| `overview` | `varchar(10000)` | Nullable |
| `release_date` | `date` | Nullable |
| `genres` | `text[]` | Default `'{}'`; validated by `public_movie_genres_valid` |
| `confirmed_tmdb_id` | `bigint` | Nullable; positive when present; no FK |
| `created_at`, `updated_at`, `last_seen_at` | `timestamptz` | Each defaults to `CURRENT_TIMESTAMP` |
| `trailer_vf_youtube_key`, `trailer_vo_youtube_key` | `varchar(11)` | Nullable; YouTube key pattern; must differ if both present; each requires nonnull `confirmed_tmdb_id` |
| `imdb_id` | `varchar(32)` | Nullable; `^tt[0-9]{7,30}$`; requires nonnull `confirmed_tmdb_id` |

Partial unique indexes enforce unique confirmed TMDB IDs and unique source anchors among nonredirected rows only. The self-FK prevents dangling redirects and the check prevents direct self-redirection, but neither prevents longer cycles. Timestamp defaults do not automatically update existing rows.

### `public_movie_sources`

Stores provider movie identities and source metadata linked to durable public movies, including first-seen and last-seen timestamps.

Primary key: `(source_provider, source_movie_id)`. Each provider movie maps to one public movie.

| Column | Type | Definition |
| --- | --- | --- |
| `source_provider` | `varchar(32)` | Provider |
| `source_movie_id` | `varchar(128)` | Provider movie identity |
| `public_movie_id` | `bigint` | FK to `public_movies(id)` |
| `source_slug` | `varchar(128)` | Nonblank after trimming; not unique |
| `title` | `varchar(1024)` | Nonblank after trimming |
| `runtime_minutes` | `integer` | Nonnegative |
| `poster_url` | `varchar(4096)` | Nullable; must start with `https://` |
| `overview` | `varchar(10000)` | Nullable |
| `release_date` | `date` | Nullable |
| `genres` | `text[]` | Default `'{}'`; validated by `public_movie_genres_valid` |
| `first_seen_at`, `last_seen_at` | `timestamptz` | Each defaults to `CURRENT_TIMESTAMP` |

### `movie_slug_aliases`

Stores source, local, and TMDB slug aliases that resolve to durable public movie identities.

| Column | Type | Definition |
| --- | --- | --- |
| `slug` | `varchar(128)` | Primary key; nonblank; cannot match canonical slug pattern `^film-[1-9][0-9]*$` |
| `public_movie_id` | `bigint` | FK to `public_movies(id)` |
| `alias_kind` | `varchar(16)` | `source`, `local`, or `tmdb` |
| `source_provider` | `varchar(32)` | Nullable; provider for a source alias |
| `source_movie_id` | `varchar(128)` | Nullable; provider movie identity |
| `first_seen_at` | `timestamptz` | Default `CURRENT_TIMESTAMP` |
| `retargeted_at` | `timestamptz` | Nullable |

For `local` and `tmdb`, both source fields must be null. For `source`, the check requires a nonnull source movie ID and tests provider membership and identity syntax. The nullable `source_provider` can still pass through SQL null semantics. No FK links these fields to `public_movie_sources`.

### `public_movie_metadata_overrides`

Stores manually overridden public movie metadata values and per-field flags distinguishing an override from an inherited value.

One optional override row per public movie. `public_movie_id bigint` is the primary key and references `public_movies(id)` with `ON DELETE CASCADE`.

Each value below is nullable and has a companion `<column>_overridden boolean NOT NULL DEFAULT false` column:

| Value column | Type | Validation when overridden |
| --- | --- | --- |
| `title` | `varchar(1024)` | Must be nonnull and nonblank |
| `runtime_minutes` | `integer` | Must be nonnull and nonnegative |
| `release_date` | `date` | Null permitted to explicitly clear the value |
| `genres` | `text[]` | Must be nonnull; validated by `public_movie_metadata_override_genres_valid` |
| `overview` | `varchar(10000)` | Null permitted |
| `poster_url`, `backdrop_url` | `varchar(4096)` | Null permitted; otherwise match `^https://[^[:space:]/?#]+([/?#][^[:space:]]*)?$` |
| `trailer_vf_youtube_key`, `trailer_vo_youtube_key` | `varchar(11)` | Null permitted; otherwise match `^[A-Za-z0-9_-]{11}$` |

At least one override flag must be true. Every inactive value must be null. Both trailer values must differ when present. Unlike base public movie trailer fields, override trailers have no confirmed-TMDB requirement; override backdrop URLs are not restricted to TMDB. There is no IMDb override column or timestamp column.

### Genre validation functions

`public_movie_genres_valid(text[])` and `public_movie_metadata_override_genres_valid(text[])` are separate SQL functions, both `IMMUTABLE` and `STRICT`. They allow at most 32 elements, each nonnull, nonblank after trimming, and at most 256 characters long. Empty arrays are valid. The override migration defines its own helper independently. These stronger per-element checks do not apply to `movies.source_genres` or `movie_metadata_cache.genres`.

## Theater locations and geocoding

### `theater_locations`

Stores resolved cinema coordinates, geocoding status and match evidence, manual locations, and optional ambiguous-match candidates.

Primary key: `(provider, provider_theater_id)`. No FK to generation-scoped theaters.

| Column | Type | Definition |
| --- | --- | --- |
| `provider` | `varchar(32)` | Provider |
| `provider_theater_id` | `varchar(128)` | Nonblank after trimming |
| `latitude`, `longitude` | `double precision` | Nullable coordinate pair; latitude in `[-90, 90]`, longitude in `[-180, 180]` |
| `source` | `varchar(32)` | `ign` or `manual`, coupled to status |
| `matched_label` | `text` | Nullable; nonblank when required |
| `match_score` | `double precision` | Nullable; between 0 and 1 when present |
| `address_hash` | `char(64)` | Nullable; IGN check uses `^[a-f0-9]{64}$`; manual rows require null |
| `status` | `varchar(32)` | `matched`, `ambiguous`, `not_found`, or `manual` |
| `updated_at` | `timestamptz` | Update timestamp |
| `candidate_latitude`, `candidate_longitude` | `double precision` | Nullable pair; same coordinate bounds |
| `candidate_postal_code`, `candidate_city`, `candidate_type` | `text` | Nullable; nonblank if present |

- IGN rows use `matched`, `ambiguous`, or `not_found`; manual rows use `manual`.
- Resolved coordinates are present exactly for `matched` or `manual`, and both coordinates are null otherwise.
- `matched` and `ambiguous` require a nonblank `matched_label` and nonnull score; `not_found` and `manual` require both to be null.
- Candidate fields may be populated only for `ambiguous`; none is mandatory even then.
- The IGN hash pattern check does not explicitly require a nonnull hash, so SQL null is accepted.

### `theater_geocoding_runs`

Stores geocoding execution history with run states, start and finish timestamps, result summaries, and bounded error codes.

| Column | Type | Definition |
| --- | --- | --- |
| `id` | `bigint identity` | Primary key |
| `state` | `text` | `running`, `succeeded`, or `failed` |
| `started_at` | `timestamptz` | Start timestamp |
| `finished_at` | `timestamptz` | Nullable |
| `summary` | `jsonb` | Nullable; object when present in a terminal state |
| `error_code` | `text` | Nullable; `run_failed`, `canceled`, or `internal_failure` |

Running rows require null finish, summary, and error fields. Succeeded rows require a finish timestamp, no error, and test `jsonb_typeof(summary) = 'object'` (SQL null also passes). Failed rows require a finish timestamp and error code, with an optional object summary. A partial unique index permits at most one running geocoding run.

## Synchronization scheduling and history

### `sync_schedules`

Stores configurable daily, weekly, or cron schedules for provider synchronization and TMDB metadata refresh tasks.

Multiple schedules may share a target; `target` is not unique. The former `provider` primary key was replaced in migration 029.

| Column | Type | Definition |
| --- | --- | --- |
| `id` | `bigint identity` | Primary key |
| `target` | `text` | Provider or `tmdb_metadata_refresh`; not `all` |
| `revision` | `bigint` | Default `1`; positive |
| `enabled` | `boolean` | Whether the schedule is enabled |
| `schedule_kind` | `text` | `daily`, `weekly`, or `cron` |
| `local_time` | `text` | Nullable; daily/weekly require 24-hour `HH:MM` |
| `weekdays` | `text[]` | Nullable; weekly requires a nonempty array contained in `{mon,tue,wed,thu,fri,sat,sun}` |
| `cron_expression` | `varchar(255)` | Nullable; cron requires a nonblank expression |
| `updated_at` | `timestamptz` | Default `now()` |

Daily schedules require null weekdays and cron expression. Weekly schedules require null cron expression. Cron schedules require null local time and weekdays. SQL does not parse cron syntax or require distinct weekdays. There is no stored timezone column.

### `sync_schedule_occurrence_claims`

Stores one claimed execution occurrence per schedule, identified by schedule revision and scheduled timestamp.

| Column | Type | Definition |
| --- | --- | --- |
| `schedule_id` | `bigint` | Primary key; FK to `sync_schedules(id)` with `ON DELETE CASCADE` |
| `schedule_revision` | `bigint` | Positive |
| `scheduled_for` | `timestamptz` | Claimed occurrence |
| `updated_at` | `timestamptz` | Default `now()` |

Stores at most one claim row per schedule, not an unbounded occurrence history. The revision is not constrained to equal the current schedule revision.

### `sync_runs`

Stores provider synchronization execution history, including run state, coverage window, provider results, and manual or scheduled trigger details.

| Column | Type | Definition |
| --- | --- | --- |
| `id` | `bigint identity` | Primary key |
| `target` | `text` | Provider or `all`; not `tmdb_metadata_refresh` |
| `state` | `text` | `running`, `succeeded`, or `failed` |
| `started_at` | `timestamptz` | Start timestamp |
| `finished_at` | `timestamptz` | Nullable; null exactly when running |
| `window_from`, `window_through` | `date` | Check: `window_through >= window_from` |
| `providers` | `jsonb` | Must be an object; SQL does not validate its internal fields |
| `trigger_source` | `text` | Default `manual`; `manual` or `scheduled` |
| `schedule_id`, `schedule_revision` | `bigint` | Nullable; no FK |
| `scheduled_for` | `timestamptz` | Nullable |
| `schedule_attempt` | `smallint` | Nullable |

Manual runs require all four schedule fields to be null. Scheduled runs require an individual provider target, nonnull `scheduled_for`, and checks for positive schedule ID/revision and attempt between 0 and 2. The latter three columns lack explicit nonnull checks, so SQL null can pass. Scheduled occurrence attempts are unique by `(schedule_id, schedule_revision, scheduled_for, schedule_attempt)` for scheduled rows; nulls retain PostgreSQL's default distinct behavior. There is no one-running-run unique index on this table.

## Short links

### `short_links`

Stores short shareable codes mapped to relative application URLs, together with their creation timestamps.

| Column | Type | Definition |
| --- | --- | --- |
| `code` | `text` | Primary key; matches `^[A-Za-z0-9_-]{22}$` |
| `target` | `text` | Nonempty; at most 2,048 bytes; starts with `/` but not `//`; contains no CR or LF |
| `created_at` | `timestamptz` | Default `now()` |

There is no expiration column, target uniqueness constraint, or database TTL. Migration 027 deletes links older than 90 days at migration time and adds a retention index. Similarly, migration 024 deletes terminal sync runs finished at least 30 days earlier and adds an index. These SQL statements do not install recurring cleanup jobs or triggers.

## Additional indexes

All indexes below use PostgreSQL's default B-tree method. Primary-key and unique-constraint indexes described with the tables are additional to this list.

| Index | Table | Columns / expression | Predicate / uniqueness |
| --- | --- | --- | --- |
| `theaters_city_lower_idx` | `theaters` | `generation_id, lower(city)` | Nonunique |
| `theater_dates_service_date_idx` | `theater_dates` | `generation_id, service_date, theater_id` | Nonunique |
| `theater_passes_pass_code_idx` | `theater_passes` | `generation_id, pass_code, theater_id` | Nonunique |
| `showtimes_service_theater_start_idx` | `showtimes` | `generation_id, service_date, theater_id, start_time, id` | Nonunique |
| `showtimes_service_window_idx` | `showtimes` | `generation_id, service_date, start_time, end_time` | Nonunique |
| `showtimes_movie_service_start_idx` | `showtimes` | `generation_id, movie_provider_id, provider, service_date, start_time` | Nonunique |
| `movie_matches_retry_idx` | `movie_matches` | `status, retry_after` | Nonunique |
| `public_movies_active_tmdb_id_key` | `public_movies` | `confirmed_tmdb_id` | Unique where `redirect_to_id IS NULL AND confirmed_tmdb_id IS NOT NULL` |
| `public_movies_anchor_key` | `public_movies` | `identity_anchor_provider, identity_anchor_source_movie_id` | Unique where `redirect_to_id IS NULL` |
| `public_movie_sources_public_movie_id_idx` | `public_movie_sources` | `public_movie_id` | Nonunique |
| `movie_slug_aliases_public_movie_id_idx` | `movie_slug_aliases` | `public_movie_id` | Nonunique |
| `sync_runs_latest_idx` | `sync_runs` | `started_at DESC, id DESC` | Nonunique |
| `sync_runs_scheduled_occurrence_attempt_idx` | `sync_runs` | `schedule_id, schedule_revision, scheduled_for, schedule_attempt` | Unique where `trigger_source = 'scheduled'` |
| `sync_runs_terminal_retention_idx` | `sync_runs` | `finished_at` | Nonunique where `state IN ('succeeded', 'failed') AND finished_at IS NOT NULL` |
| `theater_geocoding_runs_latest_idx` | `theater_geocoding_runs` | `started_at DESC, id DESC` | Nonunique |
| `theater_geocoding_runs_one_running_idx` | `theater_geocoding_runs` | `(state)` | Unique where `state = 'running'` |
| `short_links_retention_idx` | `short_links` | `created_at` | Nonunique |

The migrations define no views, materialized views, triggers, or custom enum types. Identity sequences support `local_movie_groups.id`, `public_movies.id`, `sync_runs.id`, `theater_geocoding_runs.id`, and `sync_schedules.id`. Temporary `public_movie_backfill_sources` and `public_movie_backfill_map` tables in migration 014 are dropped on commit and are not part of the durable schema.
