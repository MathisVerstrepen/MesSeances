# MesSeances

MesSeances helps moviegoers compare nearby screenings and find a film that fits the time they actually have. Its visual schedule brings movies, cinemas, formats, languages, and booking links into one place instead of making users search across separate cinema websites.

The application interface is in French.

## What you can do

- **Explore a visual daily timeline.** Browse screenings by cinema or by movie across the 08:00–02:00 cinema day, then adjust date, language, format, and timeline zoom.
- **Find screenings within a strict time window.** Set when a movie may start and must finish. MesSeances only returns screenings that fit completely, with an optional allowance for trailers and ads.
- **Browse the current movie catalog.** See current films from the landing page, search the full schedule catalog, and open detailed pages with available screenings, artwork, synopsis, release information, and genres when metadata is available.
- **Choose favorite cinemas.** Search cinemas by name or city and keep a local selection that drives the timeline, movie pages, and time-window search. Favorites stay in the current browser; no account is required.
- **Discover cinemas by city.** Open public cinema pages for current and selected-date screenings, or browse exact-city pages for cinemas and films in the current schedule window.
- **Compare supported providers.** Movie pages combine UGC, Kinepolis, Pathé, and CGR showtimes when their listings have been matched as the same film.
- **Book with the cinema.** Available booking actions open the provider's official booking page in a new tab.
- **Run schedule updates from the admin area.** Authenticated administrators can start UGC, Kinepolis, Pathé, and CGR synchronizations together or separately and follow current status.

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
- A valid proxy file for the initial provider synchronization

Install dependencies from the repository root:

```sh
cd api && go mod download
cd ..
npm --prefix web install
cp deploy/.env.example deploy/.env
```

MesSeances does not start with an empty database. Populate PostgreSQL with a complete schedule snapshot first:

```sh
make sync PROXY_FILE=/path/to/proxies.txt
```

`make sync` runs UGC, Kinepolis, Pathé, then CGR with the same required proxy file. Provider-specific full synchronizations are also available:

```sh
cd api
go run ./cmd/sync-ugc -proxy-file /path/to/proxies.txt
go run ./cmd/sync-kinepolis -proxy-file /path/to/proxies.txt
go run ./cmd/sync-pathe -proxy-file /path/to/proxies.txt
go run ./cmd/sync-cgr
```

Pathé ingestion uses only `https://www.pathe.fr/api/*` JSON endpoints. Like other provider ingestion, it requires configured proxies and the built-in Chrome-compatible TLS fingerprint transport. `sync-pathe` supports optional `-from` and `-timeout` flags and always publishes a complete national Pathé snapshot.

CGR ingestion uses its public Gatsby cinema query and `https://www.cgrcinemas.fr/api/gatsby-source-boxofficeapi/*` JSON endpoints. Movie detail requests are capped at 50 IDs. `sync-cgr` supports optional `-from`, `-timeout`, and `-proxy-file` flags, works with a direct bounded HTTP client when no proxy file is supplied, and always publishes a complete national CGR snapshot. Missing CGR runtimes and unpublished room names are preserved as unknown values instead of dropping showtimes.

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

Sync timing defaults are `SYNC_REQUEST_TIMEOUT=20s`, `SYNC_KINEPOLIS_REQUEST_INTERVAL=2s`, and `SYNC_OPERATION_TIMEOUT=2m`. Request timeout applies to UGC, Kinepolis, Pathé, and CGR and must be between 5s and 60s. Kinepolis interval must be at least 1s, and operation timeout must be positive. Explicit `-timeout` flags override request timeout; Kinepolis also supports `-request-interval`.

`PORT` must be a decimal port from 1 through 65535. `WEB_ORIGIN` must be an exact `http` or `https` origin without credentials, path, query, or fragment.

`TRUSTED_PROXY_CIDRS` is an optional comma-separated list of exact CIDR ranges for reverse proxies that connect directly to the API. When the socket peer is trusted, public rate limits resolve `X-Forwarded-For` from right to left across trusted hops; malformed chains fall back to the socket peer. Forwarding headers from every other peer are ignored. Leave this setting empty for direct client connections. Operators must enumerate deployed ingress peer ranges and must not use broad public network ranges.

Nuxt uses three distinct origins. `NUXT_API_BASE` is private to server-side rendering and defaults to `http://localhost:8080`; production Compose fixes it to the internal `http://api:8080` service address. `NUXT_PUBLIC_API_BASE` is the API origin reachable by visitors' browsers and defaults to `http://localhost:8080`. `NUXT_PUBLIC_SITE_URL` is the canonical public site origin used for absolute canonical and social metadata URLs and defaults to `http://localhost:3000`; production Compose derives it from `WEB_ORIGIN`. Configure public values as exact `http` or `https` origins without a trailing slash or path. Never expose the internal `api:8080` address as a public browser URL.

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

These offline checks do not run UGC, Kinepolis, Pathé, or CGR synchronization and do not make real TMDB or IGN calls:

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

With the API and Nuxt already running from the current build, verify exact entity titles, breadcrumb and catalog `ItemList` structured data, current-only `/films` discovery, all-canonical sitemap inventory without `lastmod`, contextual entity links, historical redirects, and crawler error behavior:

```sh
npm --prefix web run verify:crawlability
EXPECT_UPSTREAM_FAILURE=1 npm --prefix web run verify:crawlability
```

Run the failure mode with Nuxt configured against an intentionally unavailable API origin. Neither command starts services or triggers provider/TMDB requests.
