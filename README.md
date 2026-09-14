# MesSeances

MesSeances helps moviegoers compare nearby screenings and find a film that fits the time they actually have. Its visual schedule brings movies, cinemas, formats, languages, and booking links into one place instead of making users search across separate cinema websites.

The application interface is in French.

## What you can do

- **Explore a visual daily timeline.** Browse screenings by cinema or by movie across the 08:00–02:00 cinema day, then adjust date, language, format, and timeline zoom.
- **Find screenings within a strict time window.** Set when a movie may start and must finish. MesSeances only returns screenings that fit completely, with an optional allowance for trailers and ads.
- **Browse the current movie catalog.** See current films from the landing page, search the full schedule catalog, and open detailed pages with available screenings, artwork, synopsis, release information, and genres when metadata is available.
- **Choose favorite cinemas.** Search cinemas by name or city and keep a local selection that drives the timeline, movie pages, and time-window search. Favorites stay in the current browser; no account is required.
- **Discover cinemas by city.** Open public cinema pages for current and selected-date screenings, or browse exact-city pages for cinemas and films in the current schedule window.
- **Compare supported providers.** Movie pages combine UGC, Kinepolis, Pathé, CGR, and Megarama showtimes when their listings have been matched as the same film.
- **Book with the cinema.** Available booking actions open the provider's official booking page in a new tab.
- **Run schedule updates from the admin area.** Authenticated administrators can start UGC, Kinepolis, Pathé, CGR, and Megarama synchronizations together or separately and follow current status. Megarama is also available as a scheduled target; no schedule is enabled automatically.

## Typical flow

1. Select favorite cinemas.
2. Scan today's timeline or enter a precise free-time window.
3. Filter by language or screening format.
4. Open a movie page to compare matched showtimes.
5. Continue to the cinema's official website to book.

## Local quick start

### Requirements

- Go 1.25.13
- Node.js 22.23.1 and npm 10.9.8 (verified versions)
- Docker with Docker Compose
- PostgreSQL 18
- A valid proxy file to enable provider synchronization from the admin area

Install dependencies from the repository root:

```sh
cd api && go mod download
cd ..
npm --prefix web install
cp deploy/.env.example deploy/.env
```

MesSeances can start after migrations without a complete schedule snapshot. In this pending state, `/healthz` returns `200`, `/readyz` returns `503`, and public schedule reads return `503 schedule_unavailable`. Configure `ADMIN_PASSWORD`, an independently generated `ADMIN_SESSION_SECRET`, and `PROXY_FILE`, then trigger the first provider synchronization from the authenticated admin area. Its atomic snapshot publication becomes visible to the running API during the next five-second source poll; no restart is required.

Pathé ingestion uses only `https://www.pathe.fr/api/*` JSON endpoints. Like other provider ingestion, it requires configured proxies and the built-in Chrome-compatible TLS fingerprint transport, and always publishes a complete national Pathé snapshot.

CGR ingestion uses its public Gatsby cinema query and `https://www.cgrcinemas.fr/api/gatsby-source-boxofficeapi/*` JSON endpoints. Movie detail requests are capped at 50 IDs. It always publishes a complete national CGR snapshot. Missing CGR runtimes and unpublished room names are preserved as unknown values instead of dropping showtimes.

Megarama ingestion fetches `https://ws.ticketingcine.com/config.js?site_id=CHN0042`, extracts embedded `gl_config` JSON without evaluating JavaScript, then posts JSON-RPC `get_prog` to `https://ws.ticketingcine.com/site` once per cinema. Each request must carry that cinema's validated website as its own `Referer`; a generic chain referer is not valid. All requests, including optional film-page artwork lookup, require the existing `PROXY_FILE` transport. Two workers build one complete national snapshot before atomic publication. An explicit empty cinema program is retained; incomplete, malformed, conflicting, or all-empty ingestion preserves the previous snapshot. Diagnostics never include provider bodies, raw network errors, or proxy credentials.

Megarama cinema identities retain `EMS` codes and global film identities retain their five-character codes. Local `emsx...HC...` film identities are qualified with the cinema's config ID; session IDs are preserved. Published future sessions define the horizon, including distant events rather than a fixed seven-day window. Session wall times are interpreted in `Europe/Paris`; nonexistent or ambiguous DST times without source disambiguation are rejected. Sessions before 03:00 belong to the previous cinema day.

Megarama canonical end times use source runtime plus the persisted session `first_part_duration`. If source runtime is missing, public reads use confirmed cached TMDB runtime plus that same first-part duration, without another provider crawl or TMDB request. Missing first-part duration adds zero. This value is not the search ads buffer, and manually overridden display runtime does not change this canonical end. A usable canonical end always takes precedence over the generic estimate below, without a second advertising allowance. Source runtime stays unchanged in storage.

When canonical `end_time` equals `start_time` and the response movie has a positive, safe runtime, the API separately estimates the end as advertised start + advertising duration + movie runtime. Every showing response includes nullable `estimated_end_time` (UTC timestamp) and `estimated_end_ads_minutes` fields, populated together only for this fallback; source and canonical ends remain unchanged. Movie, cinema and timeline responses use 15 minutes of advertising; slot search uses its `buffer_ads` value (0..120, default 15). `include_ads=false` shifts attendance start by that buffer but does not add it twice to the estimated end. Estimates drive timeline durations, inclusive finish-before searches and selected-showing compatibility. UI estimates have a dotted underline and a hover/focus explanation using the returned advertising duration and runtime; structured data omits their `endDate`. Without a usable canonical end or runtime-based estimate, the end stays hidden and the showing remains ineligible for finish-before and compatibility checks.

Megarama session booking links use verified cinema-specific HTTPS hosts and `#showsession?id=<session-id>`. Missing links fall back to the validated official cinema website and are labeled as website links rather than reservations. Poster URLs are limited to verified `images.monnaie-services.com` paths: global `/movie_poster/120/` images are resized to `/600/`, while local `/ems_spectacle/120/` images retain their source size. Missing global posters can use `og:image` from `https://www.ticketingcine.com/film/<CODE>.html`; absent artwork remains absent.

### Upcoming French theatrical releases

The configurable scheduler target `tmdb_upcoming_movies` imports first French theatrical releases for `(today in Europe/Paris, the same calendar date next year]`, clamping February 29 to February 28. Eligibility uses the earliest FR type-2/3 release across TMDB's full release history, not nationality or the worldwide primary date. Historical French releases exclude re-releases. Preview screenings remain available without removing an upcoming film.

Configure `TMDB_API_READ_ACCESS_TOKEN` and admin access to make this target available through the existing authenticated schedule CRUD. It supports daily, weekly, and cron schedules without `PROXY_FILE`. No schedule or initial import is created automatically; choose a cadence explicitly. Import shares the in-process TMDB gate with metadata jobs and holds a dedicated PostgreSQL session lease before claiming an occurrence. Contention, two scheduled retries, and shutdown use the existing scheduler lifecycle. This is not a provider synchronization and creates no `sync_runs` rows.

Administrators can also manually launch this same import with an empty-body `POST /api/v1/admin/tmdb-upcoming-movies/sync` and poll `GET` on the same path. Both require the admin session; POST also requires the configured web origin. All responses use `Cache-Control: no-store`. Acceptance returns `202 {"job":{"state":"running","started_at":"<UTC timestamp>","finished_at":null}}`; GET returns `200 {"job":null}` before any accepted run, otherwise the latest local manual or scheduled job. Terminal states are `succeeded` or `failed`, with `finished_at` populated and only failures adding `error_code: "sync_failed"`. Status is bounded to one in-memory job, resets on API restart, and is not shared between replicas. Gate or lease contention returns `409 tmdb_upcoming_sync_in_progress`; missing configuration or a closed manager returns `503 tmdb_upcoming_sync_unavailable` on admission; unexpected admission failures return `502 tmdb_upcoming_sync_failed`. GET returns 503 when the manager is not configured. Manual runs claim no occurrence, create or enable no schedule, and do not retry automatically. Accepted work survives request cancellation and uses the same publication, lease cleanup, and shutdown cancellation as scheduled work.

All discovery pages and every retained TMDB ID, including inactive and excluded records, are verified before a single atomic publication. Invalid pagination, malformed release evidence, provider failures, cancellation, and commit failures preserve the previous publication. Explicit empty evidence or movie HTTP 404 withdraws membership without deleting identities or URLs. Valid non-theatrical-only evidence remains stored without a theatrical date. Eligible movies reuse metadata for 30 days; inactive verification requests no details and creates no new public identity. The existing metadata-refresh target also includes retained TMDB-only movies and leaves review assessment untouched. Provider matching later attaches showtimes to the durable public identity. Manual metadata overrides and general `release_date` remain distinct from verified `french_release_date`.

`GET /api/v1/movies/upcoming` accepts only optional positive `page` (default 1). Each page contains four complete nonempty Wednesday-Tuesday release weeks, skipping gaps without splitting a week or capping its film count; the final page may contain fewer weeks. Public display starts at the next strictly future Wednesday in Europe/Paris, hiding the entire current Wednesday-Tuesday week even when some releases are still future, and retains the import window's inclusive calendar-anniversary upper bound. Eligibility and explicit admin exclusions apply before week counting; no dates beyond that horizon are added to complete its final week. Results sort by French date, normalized title, and numeric public ID. Response contains last successful `generated_at`, `catalog_revision`, `timezone`, `window`, flat `items`, `page`, global eligible film `total`, distinct eligible `total_weeks`, and `total_pages` (ceiling of weeks divided by four). Out-of-range positive pages return `items: []` with actual totals. Removed `month`, `genres`, and `page_size` parameters, as well as unknown, duplicate, or malformed parameters, return `400 invalid_query`; no facets or `page_size` are returned. An existing catalog without a successful import returns `503 upcoming_unavailable` with `Cache-Control: no-store`; a successful empty import returns `200`, `items: []`, and all three totals zero. Display eligibility advances at Paris Wednesday midnight without another import; stored membership, exact release dates, detail status and real showtimes remain unchanged.

Movie catalog responses add required nullable `french_release_date`; detail adds `release_status` (`upcoming`, `showing`, `ended`, or `unavailable`). A future verified French date takes precedence even beyond the list horizon; actual current screenings come next, then withdrawn evidence, then the legacy ended state. The existing required detail `date` parameter remains required. A successful TMDB-only publication enables list and detail reads before the first provider snapshot, including empty-session details with `city=Paris`. Default `/movies` remains empty without screenings; `include_ended=true` includes the durable inventory. Timeline routes and `/readyz` remain unavailable until a real schedule exists. Catalog and later provider publications become visible through the existing five-second poll, without restart.

#### Upcoming review API

Review signals never hide a movie. Only an explicit `excluded` decision removes it from the upcoming list and film/week/page totals; detail URLs, release status, generic inventory, sitemap inventory and real showtimes remain available. `approved` preserves normal eligibility, including after changed signals; `unreviewed` resets a decision without clearing evidence. Decisions survive withdrawal, reappearance, restart and public-owner reconciliation because they belong to the TMDB ID. Visibility changes use the existing enrichment revision and snapshot poll; `generated_at` remains the last successful sync time.

Migration 033 leaves existing movies eligible and pending assessment until the next successful import. Each assessment stores only normalized FR `{type,date,note}` rows: types 1..6, written calendar date, plain-text whitespace-normalized note, sorted and deduplicated. Type 1 is a premiere, not television. Malformed FR arrays/types/dates/notes, invalid UTF-8, NUL, more than 64 input rows or more than 1,024 note code points reject the entire publication; bounds are checked before deduplication/normalization. Non-FR rows are ignored. No provider bodies, HTML envelopes or credentials are retained.

Four ordered hints are computed only when a first French type-2/3 date exists: `limited_only` (type 2 without type 3), `non_theatrical_before_or_same_day` (type 4/5/6 on or before that date), `broadcaster_theatrical_note` (bounded broadcaster names in theatrical notes), and `single_screening_note` (bounded French single/special-screening phrases in theatrical notes). They are not classifications or exclusion proof. No genre/runtime/title/ID exceptions or absence-of-press predicate exists. Portable fixture `api/internal/enrichment/testdata/upcoming_review_2026-09-13.json` preserves 312 public parsed records from the 2026-09-13 evaluation: hint counts 21/3/1/1, union 23, including two earlier-or-equal television rows. Tests assert first-date agreement and legitimate counterexamples without live requests.

`GET /api/v1/admin/tmdb-upcoming-movies` is DB-only and available with configured admin access even without TMDB credentials or a successful import. Query keys are only `filter`, `search`, `limit` (default 50, 1..100), and `offset` (default 0, nonnegative int32). Filter defaults to `needs_review` (assessed, flagged, unreviewed); alternatives are `pending_assessment` (regardless of decision), `approved`, `excluded`, and `all`. Filters do not implicitly remove inactive/out-of-window records. Search is trimmed, nonempty when supplied, valid UTF-8 up to 1,024 code points, literal case-insensitive title substring or exact canonical decimal TMDB ID; `%`, `_` and backslash are literal. Unknown, duplicate, blank, malformed or overflowing query values fail. Results sort by the non-theatrical timing hint first, then French date/null last, case-folded title and numeric TMDB ID. Count/page share a consistent DB view and captured Paris clock.

List shape is `{ "items": [], "total": 0, "limit": 50, "offset": 0 }`. Each item has `tmdb_id` (number), `public_movie_id` (decimal string), canonical `slug`, `title`, nullable `french_release_date`, `active`, `in_window`, `publicly_visible`, `assessment_status` (`pending`/`assessed`), nullable UTC RFC3339 `assessed_at`, nonnull `french_releases` and `reason_codes` arrays, `decision` (`unreviewed`/`approved`/`excluded`), and positive integer `revision`. `in_window` uses the same upcoming display window as the public list; `publicly_visible` means active, in that window and not excluded. No review fields enter public catalog DTOs.

`PATCH /api/v1/admin/tmdb-upcoming-movies/{tmdbID}/decision` accepts exactly `{"decision":"excluded","expected_revision":1}` with `Content-Type: application/json`, a 4,096-byte body limit and no query parameters. TMDB ID is canonical positive decimal; ID and revision must not exceed 9,007,199,254,740,991. Missing/null/wrong-type/unknown/duplicate fields or trailing JSON fail. Success returns the updated item. Mutation compares revision before no-op detection: a current no-op changes neither item nor publication revision; a stale no-op conflicts. Evidence/date/membership or pending-to-assessed changes increment item revision; assessment timestamp-only refresh does not. Sync never writes decisions.

Both review routes require the existing admin cookie and return `Cache-Control: no-store`; PATCH also requires the configured Origin. Errors use the existing JSON envelope: 401 `unauthorized`, 403 `origin_forbidden`, 503 `admin_unavailable`, 400 `invalid_upcoming_review_query` / `invalid_upcoming_review_id` / `invalid_upcoming_review_update`, 404 `upcoming_review_not_found`, 409 `upcoming_review_conflict`, and safe 500 `upcoming_review_list_failed` / `upcoming_review_update_failed`. On conflict reload before choosing again; never automatically replay a decision. Review requests never call TMDB or trigger sync. Existing manual sync routes and lifecycle remain unchanged.

### Theater geocoding

Theater coordinates live in stable rows outside schedule generations. After a complete snapshot exists, authenticated administrators can launch geocoding from the theater-locations page. This in-process job uses IGN Géoplateforme with a fixed 20-second timeout and processes new theaters, every ambiguous row, and changed not-found rows. It preserves every matched or manual row and unchanged not-found row. Requests run sequentially at no more than five starts per second and use bounded retries. Launch returns immediately, status and terminal counters are durable, and only one admin-launched geocoding job can run across API replicas.

Results are stored as `matched`, `ambiguous`, or `not_found`. Failed requests leave prior rows untouched. Schedule loading accepts manual coordinates and only matched IGN coordinates whose stored address hash still matches current address inputs.

`GET /api/v1/theaters` returns `latitude` and `longitude` as numbers when accepted and explicit JSON `null` otherwise. Other theater-bearing API responses remain unchanged.

Then start PostgreSQL, the Go API, and Nuxt:

```sh
make dev
```

Open [http://localhost:3000](http://localhost:3000). The API runs at `http://localhost:8080` by default.

When admin access is enabled, configure both `ADMIN_PASSWORD` and an independently generated `ADMIN_SESSION_SECRET`. Password rotation changes login credentials without invalidating active sessions; session-secret rotation invalidates all active sessions. Leaving both blank disables admin access locally.

`INTERNAL_API_SHARED_SECRET` is optional for local development. Leaving it blank disables internal service identity and keeps Nuxt on public API routes and quotas. A configured value must be exactly 64 lowercase hexadecimal characters, and server-side Nuxt must receive the same value as private `NUXT_INTERNAL_API_SHARED_SECRET`.

Sync timing defaults are `SYNC_REQUEST_TIMEOUT=20s`, `SYNC_KINEPOLIS_REQUEST_INTERVAL=2s`, `SYNC_CINEVILLE_REQUEST_INTERVAL=2s`, and `SYNC_OPERATION_TIMEOUT=2m`. Request timeout applies to UGC, Kinepolis, Pathé, CGR, Megarama, and Cinéville and must be between 5s and 60s. Kinepolis and Cinéville intervals must be at least 1s, and operation timeout must be positive.

Cinéville supports manual, all-provider, and scheduled sync through the existing admin controls. Proxy-only acquisition discovers the current Next.js build and actual cinema routes, excludes the test headquarters, and imports real sessions from both program arrays across the entire advertised future range. One stale-build refresh restarts acquisition without mixing builds. Cinema-scoped showing IDs and signed int64 film visas are preserved. Missing source metadata stays missing; all Cinéville source and canonical ends remain unknown (`end_time == start_time`), even after TMDB enrichment. Runtime from the resolved movie, including cached enrichment, can provide the separate display/planning estimate described above without changing stored data. Malformed or incomplete acquisition never replaces the previous snapshot.

`PORT` must be a decimal port from 1 through 65535. `WEB_ORIGIN` must be an exact `http` or `https` origin without credentials, path, query, or fragment.

`TRUSTED_PROXY_CIDRS` is an optional comma-separated list of exact CIDR ranges for reverse proxies that connect directly to the API. When the socket peer is trusted, public rate limits resolve `X-Forwarded-For` from right to left across trusted hops; malformed chains fall back to the socket peer. Forwarding headers from every other peer are ignored. Leave this setting empty for direct client connections. Operators must enumerate deployed ingress peer ranges and must not use broad public network ranges.

Nuxt uses three distinct origins. `NUXT_API_BASE` is private to server-side rendering and defaults to `http://localhost:8080`; production Compose fixes it to the internal `http://api:8080` service address. `NUXT_PUBLIC_API_BASE` is the API origin reachable by visitors' browsers and defaults to `http://localhost:8080`. `NUXT_PUBLIC_SITE_URL` is the canonical public site origin used for absolute canonical and social metadata URLs and defaults to `http://localhost:3000`; production Compose derives it from `WEB_ORIGIN`. Configure public values as exact `http` or `https` origins without a trailing slash or path. Never expose the internal `api:8080` address as a public browser URL.

## Production internal API identity

Production Compose requires one operator-owned `INTERNAL_API_SHARED_SECRET`. It passes that value to API as `INTERNAL_API_SHARED_SECRET` and to web as private `NUXT_INTERNAL_API_SHARED_SECRET`; it is never public Nuxt runtime config. Copy `deploy/.env.production.example` to ignored `deploy/.env.production`, replace the syntactically valid all-zero fake value with output from `openssl rand -hex 32`, and restrict file access. Generate this value independently from every password and session secret. Keep it only in ignored operator configuration. Never put it in tracked files, browser-visible variables, URLs, command arguments, tickets, chat, logs, screenshots, or shell transcripts.

Deploy API and web images together with the same ignored environment file. For rotation, replace the single `INTERNAL_API_SHARED_SECRET` value and recreate API and web together in one Compose rollout; never rotate one service independently. Missing or empty production interpolation stops Compose. A runtime mismatch fails closed: invalid service credentials receive no internal capacity, regular reads use public quota, and internal film-bundle requests fail authentication, which can surface as film SSR `502` or a cold sitemap `503`.

After both services are healthy, request `/sitemaps/films.xml`, `/sitemaps/cinemas.xml`, and `/sitemaps/cities.xml` once through the public origin to seed successful process-local cache entries. Restarting or recreating web clears those entries, so seed them again after each rollout before relying on stale-on-refresh-failure behavior.

Production example sets `TRUSTED_PROXY_CIDRS=127.0.0.1/32` only for operator-managed Nginx connecting directly to API through its IPv4 loopback binding. This is safe only when Nginx is the immediate socket peer and every API proxy location overwrites `X-Forwarded-For` from verified client data, or constructs a chain using only explicitly trusted upstream proxies. Otherwise leave the variable empty. Never trust `127.0.0.0/8`, Docker or private network ranges, or `::1/128` without evidence for the exact immediate peer. Repository Compose binds services to loopback but does not install, inspect, test, reload, or manage external Nginx; those host changes remain a separate operator action. See [Nginx admin login rate limiting](docs/nginx-admin-login-rate-limit.md) for the matching forwarding contract.

Backend operational logs use JSON on stderr. Prometheus metrics are available without application authentication at `GET /metrics` on the API listener. Restrict this endpoint with deployment network or reverse-proxy controls; production Compose keeps the API host binding on loopback.

## Production analytics

Production Compose includes self-hosted Umami 3.3.1 and a dedicated PostgreSQL 15 service. Analytics storage is isolated in the `umami_postgres_data` volume, and its database has no published host port. Umami failure does not block the API or web services. The dashboard is bound to host loopback at `127.0.0.1:3001` by default.

The `umami-retention` service runs `deploy/umami-retention.sql` immediately after the analytics database becomes healthy and then every 24 hours. Each run uses one PostgreSQL transaction and deletes audience sessions, session links, website events, event data, session data, revenue events, session replay chunks and saved replay references, and heatmap events whose retention timestamp is null or at least 25 months old. Account, team, website, report, segment, link, pixel, board, share, two-factor, and application-setting records are preserved. The SQL takes a transaction-scoped advisory lock and validates the complete public table set plus required analytics columns before deleting anything. A schema mismatch aborts and rolls back the run, then the container restart policy retries; purge output never includes database credentials. Service health remains unavailable until the first successful purge and becomes unavailable if no successful purge has completed within 25 hours, so production `--wait` startup detects this retention failure.

This retention SQL is intentionally coupled to the official Umami v3.3.1 Prisma schema and the pinned `UMAMI_IMAGE`. Do not upgrade Umami independently. Before changing the image version, compare the new official `prisma/schema.prisma`, classify every new or changed analytics table, update the schema guard and deletion order, test against a disposable database, and deploy both changes together. Until the retention service completes its first successful run, pre-existing rows older than 25 months remain.

Bootstrap Umami in two stages:

1. Copy `deploy/.env.production.example` to ignored `deploy/.env.production`. Generate independent values for `UMAMI_POSTGRES_PASSWORD`, `UMAMI_APP_SECRET`, and `UMAMI_TWO_FACTOR_ENCRYPTION_KEY`; `openssl rand -hex 32` produces a URL-safe 64-character value suitable for each. Keep both `NUXT_PUBLIC_UMAMI_*` values empty, run `make prod`, and reach the loopback dashboard through operator-managed access such as an SSH tunnel to port 3001. Sign in with Umami's initial `admin` / `umami` credentials, immediately replace the password, and create the MesSeances website.
2. Configure operator-managed public routing, DNS, and TLS so visitors can reach the tracker script without publishing the Compose port directly. Keep dashboard access restricted. This repository does not provision a reverse proxy, DNS, certificates, or public dashboard access. Set `NUXT_PUBLIC_UMAMI_SCRIPT_URL` to the browser-reachable absolute script URL and `NUXT_PUBLIC_UMAMI_WEBSITE_ID` to the website UUID, then rerun `make prod`. Both values are intentionally public and are not credentials; leaving either empty disables tracker injection.

Keep Umami secrets only in ignored deployment environment files. Back up `umami_postgres_data` under the same retention policy as other production data. Normal Compose recreation preserves named volumes. Never run `docker compose down -v`: `-v` deletes both application and analytics database volumes.

Persisted synchronization diagnostics in `sync_runs` have a 30-day maximum. Migration 024 removes already-expired terminal rows and adds a partial retention index. API startup performs the same cutoff purge and refuses to start if it fails; while the API remains running it repeats the purge every 24 hours. Only `succeeded` and `failed` rows with a non-null `finished_at` at or before the cutoff are deleted. Rows with `state='running'` are never selected, regardless of `started_at`. Existing expired rows remain until migration or API startup first succeeds.

Short links become eligible for deletion once strictly older than 90 days. Migration 027 removes eligible links and adds an index on `created_at`. API startup repeats the strict cutoff purge and refuses to start if it fails; while running it retries every 24 hours, so healthy cleanup can retain an eligible link until the next daily run and failures can extend retention further. Periodic failures are logged without link targets or database details. Short-link resolution responses use `Cache-Control: no-store` so newly resolved targets cannot outlive database retention in browser or intermediary caches.

## Production log retention

Production Compose routes stdout and stderr from every container to Docker's `journald` logging driver. This includes API JSON logs on stderr. The repository also provides `deploy/journald/90-messeances-retention.conf`, which sets global `MaxRetentionSec=30day` and daily journal-file rotation with `MaxFileSec=1day`. This global policy is intended only for the confirmed dedicated VPS.

Host configuration cannot be installed or verified from this repository worktree. Production's 30-day technical-log maximum is therefore blocked until an operator runs these commands on the VPS from the deployed repository checkout:

```sh
sudo install -D -m 0644 deploy/journald/90-messeances-retention.conf /etc/systemd/journald.conf.d/90-messeances-retention.conf
sudo systemctl restart systemd-journald
sudo journalctl --rotate
sudo journalctl --vacuum-time=30d
```

`journalctl --vacuum-time=30d` permanently removes archived host journal files older than 30 days. Verify effective host settings and container drivers after `make prod`:

```sh
sudo systemd-analyze cat-config systemd/journald.conf | grep -E '^(MaxRetentionSec=30day|MaxFileSec=1day)$'
sudo journalctl --disk-usage
for service in postgres umami-postgres umami umami-retention api web; do
  container_id="$(docker compose --env-file deploy/.env.production -f deploy/compose.production.yaml ps -q "$service")"
  test -n "$container_id"
  test "$(docker inspect --format '{{.HostConfig.LogConfig.Type}}' "$container_id")" = journald
done
sudo journalctl CONTAINER_NAME=messeances-production-api-1 --since '10 minutes ago' --no-pager
```

The final command should show recent API JSON records after the API has handled traffic. MesSeances does not retain reverse-proxy or separate host request logs; if operators add either later, they must keep those outputs under the same maximum or update the privacy statement.

To stop PostgreSQL later without deleting local data:

```sh
docker compose --project-directory . --env-file deploy/.env -f deploy/compose.yaml down
```

## Development and releases

Development integrates through feature pull requests into `dev`. Release pull requests use selectable release template from `dev` to protected `main`; valid merged releases create strict stable tags, stable GitHub Releases, versioned API/web GHCR images, and `latest` aliases. Production deployment remains manual.

See [development and release operation](docs/releasing.md) for exact worktree commands, pull request title/body contract, GitHub and `gh` flows, branch-protection check names, publication behavior, and failure recovery.

## Contributor checks

These offline checks do not run UGC, Kinepolis, Pathé, CGR, or Megarama synchronization and do not make real TMDB or IGN calls:

```sh
python -m unittest discover -s scripts/tests
docker compose --project-directory . --env-file deploy/.env -f deploy/compose.yaml config
docker compose --project-directory . --env-file deploy/.env.production.example -f deploy/compose.production.yaml config
cd api && go test ./...
cd ..
npm --prefix web run test:unit
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web run build
```

For a deliberate proxy-only Megarama full-chain contract smoke, run from `api/` with an operator-supplied proxy file. This opt-in test builds and validates a dataset in memory, logs counts only, and does not publish to a database or call TMDB/IGN. Ordinary tests skip it when the variable is unset:

```sh
MEGARAMA_LIVE_PROXY_FILE=/absolute/path/to/proxies.txt go test ./internal/megarama -run '^TestProxyFullSyncContractIntegration$' -count=1 -v
```

With the API and Nuxt already running from the current build, verify exact entity titles, breadcrumb and catalog `ItemList` structured data, current-only `/films` discovery, all-canonical sitemap inventory without `lastmod`, contextual entity links, historical redirects, and crawler error behavior:

```sh
npm --prefix web run verify:crawlability
EXPECT_UPSTREAM_FAILURE=1 npm --prefix web run verify:crawlability
```

Run the failure mode with Nuxt configured against an intentionally unavailable API origin. Neither command starts services or triggers provider/TMDB requests.

### Core Web Vitals benchmark

Use one unchanged production-like API/Nuxt runtime with populated current data. Command measures one URL per invocation and does not start or stop services. Before measuring, open running instance's `/sitemap.xml` and select one successful current film, cinema, and city URL that renders expected poster content. Do not use crawlability fixture slugs or empty/error routes.

Run five independent three-run mobile measurements with same absolute Chrome executable, preserving generated report filenames after each invocation:

```sh
make web-vitals URL="http://localhost:3000/" RUNS=3 CHROME_BIN="/absolute/path/to/chrome"
make web-vitals URL="http://localhost:3000/films" RUNS=3 CHROME_BIN="/absolute/path/to/chrome"
make web-vitals URL="http://localhost:3000/film/<current-film-slug>" RUNS=3 CHROME_BIN="/absolute/path/to/chrome"
make web-vitals URL="http://localhost:3000/cinema/<current-cinema-slug>" RUNS=3 CHROME_BIN="/absolute/path/to/chrome"
make web-vitals URL="http://localhost:3000/ville/<current-city-slug>/cinemas" RUNS=3 CHROME_BIN="/absolute/path/to/chrome"
```

Local HTML and JSON reports are written under `web/.lighthouseci/reports/` and ignored by Git. Record exact URL, report filenames, and median LCP, CLS, and TBT for each template separately. Do not average templates together, and report threshold failures unchanged.

Each invocation fails unless median LCP is at most 2500 ms, median CLS is strictly below 0.1, and median TBT is at most 200 ms. LCP and CLS are direct page-load lab metrics. TBT is only an INP proxy because page-load Lighthouse does not measure representative INP; report INP as unconfirmed unless independent field evidence establishes it.
