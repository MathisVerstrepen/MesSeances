# MesSeances

MesSeances helps moviegoers compare nearby screenings and find a film that fits the time they actually have. Its visual schedule brings movies, cinemas, formats, languages, and booking links into one place instead of making users search across separate cinema websites.

The application interface is in French.

## What you can do

- **Explore a visual daily timeline.** Browse screenings by cinema or by movie across the 08:00–02:00 cinema day, then adjust date, language, format, and timeline zoom.
- **Find screenings within a strict time window.** Set when a movie may start and must finish. MesSeances only returns screenings that fit completely, with an optional allowance for trailers and ads.
- **Browse the current movie catalog.** See current films from the landing page, search the full schedule catalog, and open detailed pages with available screenings, artwork, synopsis, release information, and genres when metadata is available.
- **Choose favorite cinemas.** Search cinemas by name or city and keep a local selection that drives the timeline, movie pages, and time-window search. Favorites stay in the current browser; no account is required.
- **Discover cinemas by city.** Open public cinema pages for current and selected-date screenings, or browse exact-city pages for cinemas and films in the current schedule window.
- **Compare supported providers.** Movie pages combine UGC, Kinepolis, and Pathé showtimes when their listings have been matched as the same film.
- **Book with the cinema.** Available booking actions open the provider's official booking page in a new tab.
- **Run schedule updates from the admin area.** Authenticated administrators can start UGC, Kinepolis, and Pathé synchronizations together or separately and follow current status.

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

`make sync` runs UGC, Kinepolis, then Pathé with the same required proxy file. Provider-specific full synchronizations are also available:

```sh
cd api
go run ./cmd/sync-ugc -proxy-file /path/to/proxies.txt
go run ./cmd/sync-kinepolis -proxy-file /path/to/proxies.txt
go run ./cmd/sync-pathe -proxy-file /path/to/proxies.txt
```

Pathé ingestion uses only `https://www.pathe.fr/api/*` JSON endpoints. Like other provider ingestion, it requires configured proxies and the built-in Chrome-compatible TLS fingerprint transport. `sync-pathe` supports optional `-from` and `-timeout` flags and always publishes a complete national Pathé snapshot.

Then start PostgreSQL, the Go API, and Nuxt:

```sh
make dev
```

Open [http://localhost:3000](http://localhost:3000). The API runs at `http://localhost:8080` by default.

When admin access is enabled, configure both `ADMIN_PASSWORD` and an independently generated `ADMIN_SESSION_SECRET`. Password rotation changes login credentials without invalidating active sessions; session-secret rotation invalidates all active sessions. Leaving both blank disables admin access locally.

Sync timing defaults are `SYNC_REQUEST_TIMEOUT=20s`, `SYNC_KINEPOLIS_REQUEST_INTERVAL=2s`, and `SYNC_OPERATION_TIMEOUT=2m`. Request timeout applies to UGC, Kinepolis, and Pathé and must be between 5s and 60s. Kinepolis interval must be at least 1s, and operation timeout must be positive. Explicit `-timeout` flags override request timeout; Kinepolis also supports `-request-interval`.

`PORT` must be a decimal port from 1 through 65535. `WEB_ORIGIN` must be an exact `http` or `https` origin without credentials, path, query, or fragment.

Nuxt uses three distinct origins. `NUXT_API_BASE` is private to server-side rendering and defaults to `http://localhost:8080`; production Compose fixes it to the internal `http://api:8080` service address. `NUXT_PUBLIC_API_BASE` is the API origin reachable by visitors' browsers and defaults to `http://localhost:8080`. `NUXT_PUBLIC_SITE_URL` is the canonical public site origin used for absolute canonical and social metadata URLs and defaults to `http://localhost:3000`; production Compose derives it from `WEB_ORIGIN`. Configure public values as exact `http` or `https` origins without a trailing slash or path. Never expose the internal `api:8080` address as a public browser URL.

Backend operational logs use JSON on stderr. Prometheus metrics are available without application authentication at `GET /metrics` on the API listener. Restrict this endpoint with deployment network or reverse-proxy controls; production Compose keeps the API host binding on loopback.

## Production analytics

Production Compose includes self-hosted Umami 3.3.1 and a dedicated PostgreSQL 15 service. Analytics storage is isolated in the `umami_postgres_data` volume, and its database has no published host port. Umami failure does not block the API or web services. The dashboard is bound to host loopback at `127.0.0.1:3001` by default.

Bootstrap Umami in two stages:

1. Copy `deploy/.env.production.example` to ignored `deploy/.env.production`. Generate independent values for `UMAMI_POSTGRES_PASSWORD`, `UMAMI_APP_SECRET`, and `UMAMI_TWO_FACTOR_ENCRYPTION_KEY`; `openssl rand -hex 32` produces a URL-safe 64-character value suitable for each. Keep both `NUXT_PUBLIC_UMAMI_*` values empty, run `make prod`, and reach the loopback dashboard through operator-managed access such as an SSH tunnel to port 3001. Sign in with Umami's initial `admin` / `umami` credentials, immediately replace the password, and create the MesSeances website.
2. Configure operator-managed public routing, DNS, and TLS so visitors can reach the tracker script without publishing the Compose port directly. Keep dashboard access restricted. This repository does not provision a reverse proxy, DNS, certificates, or public dashboard access. Set `NUXT_PUBLIC_UMAMI_SCRIPT_URL` to the browser-reachable absolute script URL and `NUXT_PUBLIC_UMAMI_WEBSITE_ID` to the website UUID, then rerun `make prod`. Both values are intentionally public and are not credentials; leaving either empty disables tracker injection.

Keep Umami secrets only in ignored deployment environment files. Back up `umami_postgres_data` under the same retention policy as other production data. Normal Compose recreation preserves named volumes. Never run `docker compose down -v`: `-v` deletes both application and analytics database volumes.

To stop PostgreSQL later without deleting local data:

```sh
docker compose --project-directory . --env-file deploy/.env -f deploy/compose.yaml down
```

## Contributor checks

These offline checks do not run UGC, Kinepolis, or Pathé synchronization and do not make real TMDB calls:

```sh
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
