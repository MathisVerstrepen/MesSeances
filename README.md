# MesSeances

MesSeances helps moviegoers compare nearby screenings and find a film that fits the time they actually have. Its visual schedule brings movies, cinemas, formats, languages, and booking links into one place instead of making users search across separate cinema websites.

The application interface is in French.

## What you can do

- **Explore a visual daily timeline.** Browse screenings by cinema or by movie across the 08:00–02:00 cinema day, then adjust date, language, format, and timeline zoom.
- **Find screenings within a strict time window.** Set when a movie may start and must finish. MesSeances only returns screenings that fit completely, with an optional allowance for trailers and ads.
- **Browse the current movie catalog.** See current films from the landing page, search the full schedule catalog, and open detailed pages with available screenings, artwork, synopsis, release information, and genres when metadata is available.
- **Choose favorite cinemas.** Search cinemas by name or city and keep a local selection that drives the timeline, movie pages, and time-window search. Favorites stay in the current browser; no account is required.
- **Compare supported providers.** Movie pages combine UGC and Kinepolis showtimes when their listings have been matched as the same film.
- **Book with the cinema.** Available booking actions open the provider's official booking page in a new tab.
- **Run schedule updates from the admin area.** Authenticated administrators can start UGC and Kinepolis synchronizations together or separately and follow current status.

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

Then start PostgreSQL, the Go API, and Nuxt:

```sh
make dev
```

Open [http://localhost:3000](http://localhost:3000). The API runs at `http://localhost:8080` by default.

When admin access is enabled, configure both `ADMIN_PASSWORD` and an independently generated `ADMIN_SESSION_SECRET`. Password rotation changes login credentials without invalidating active sessions; session-secret rotation invalidates all active sessions. Leaving both blank disables admin access locally.

Backend operational logs use JSON on stderr. Prometheus metrics are available without application authentication at `GET /metrics` on the API listener. Restrict this endpoint with deployment network or reverse-proxy controls; production Compose keeps the API host binding on loopback.

To stop PostgreSQL later without deleting local data:

```sh
docker compose --project-directory . --env-file deploy/.env -f deploy/compose.yaml down
```

## Contributor checks

These checks do not run a provider synchronization or make real TMDB calls:

```sh
docker compose --project-directory . --env-file deploy/.env -f deploy/compose.yaml config
cd api && go test ./...
cd ..
npm --prefix web run typecheck
npm --prefix web run lint
```
