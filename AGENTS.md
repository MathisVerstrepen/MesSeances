# MesSeances — Agent Onboarding

## Stack & Branch

- `dev` is development integration branch. `main` is protected release branch; never push feature work directly to `main`.
- Create feature worktrees from current `origin/dev`. Branch names must be lowercase `<type>/<short-kebab-case-description>` using one of `feat`, `fix`, `test`, `docs`, `refactor`, `chore`, `style`, or `perf`; descriptions use concise kebab-case without spaces or underscores.
- Feature pull requests are same-repository conventional feature branches into `dev`. Use conventional title such as `feat: describe feature` and read `.github/PULL_REQUEST_TEMPLATE/feature.md` for body. Do not use release template. Merge only after `Go CI / checks`, `Go CI / integration`, `Frontend CI / checks`, and every other registered check pass.
- Releases are same-repository `dev` into `main`. Before creating a release PR, complete release-skill preflight, merge verified latest `origin/main` into clean synchronized `dev` when not already an ancestor, and normally push the merge before preparing the changelog. Stop on conflicts, ref drift, or an existing stale release PR; do not auto-resolve or force-push. Recheck latest main ancestry immediately before PR creation. Read `.github/PULL_REQUEST_TEMPLATE/release.md`; title must be exact `Release X.Y.Z` with strict stable non-`v` version syntax, and body must contain only `## Changed`, `## Added`, `## Improved`, and `## Fixed` in that order with at least one bullet per section and `- None` as sole bullet for an empty section. Body must exactly equal tracked `docs/changelogs/X.Y.Z.md`, including one final newline. Link each non-`None` bullet to its corresponding same-repository feature pull request when commit-associated repository evidence proves one; never invent links.
- Protected `main` requires `Go CI / checks`, `Go CI / integration`, `Frontend CI / checks`, and `Release PR / Validate release metadata`, strict up-to-date branches, pull-request flow, and conversation resolution. Force pushes and branch deletion are blocked. `dev` is intentionally unprotected, but feature PRs remain required process.
- For an explicit feature PR merge request, load project skill `merge-pull-request` from `.opencode/skills/merge-pull-request/SKILL.md`. For an explicit release or publish request, load project skill `publish-new-release` from `.opencode/skills/publish-new-release/SKILL.md`. Never run either mutation procedure from an inferred request.
- Go 1.26.0 API in `api/` (`module messeances/api`), Nuxt 4 / Vue 3 / TypeScript / Tailwind frontend in `web/`.
- PostgreSQL 18 Alpine via `deploy/compose.yaml` (`movieflow_postgres_data` volume). Node 22.23.1 / npm 10.9.8 verified (see `README.md`).
- Product requirements: `.agents/PRD.md`. Do not duplicate PRD here; optimize for agent execution.
- Production url is `https://messeances.fr` and you can connect to production vps with `ssh -o BatchMode=yes -o IdentitiesOnly=yes -o ConnectTimeout=10 -i "$HOME/.ssh/github" hogwarts@65.109.65.105`.
- Use Tailwind unless you have a strong reason to override.
- Codebase Memory projet name is `movieflow`.
- Read `api/internal/database/migrations/README.md` before editing migrations and keep README up to date when making changes.
- Public frontend and Admin frontend UI use different styles, be aware of what section you are changing and what UI style you should be using

## Repository Map

- `api/cmd/api/` — API entrypoint. `api/cmd/sync-ugc/` and `api/cmd/sync-kinepolis/` — sync CLIs (proxy-required).
- `api/internal/` — `config/`, `database/` (+ `migrations/` 001–007), `enrichment/`, `httpapi/`, `kinepolis/`, `schedule/`, `synccontrol/`, `syncproxy/`, `tmdb/`, `ugc/`.
- `api/.air.toml` — Air live-reload (Go sources only, `_test.go` excluded, 200 ms delay, `stop_on_error=false`).
- `web/app/` — `pages/` (`/`, `/recherche`, `/films`, `/film/[slug]`, `/cinemas`, `/admin/sync`), `components/`, `composables/`, `assets/`, `utils/`, `types/`.
- `web/nuxt.config.ts` — `compatibilityDate 2024-11-01`, Tailwind Vite plugin, `lang: fr`, `runtimeConfig.public.apiBase=http://localhost:8080`.
- `web/oxlint.config.ts` + `web/tools/oxlint/` — frontend lint (uncommitted working-tree changes; do not modify).
- `deploy/` — development/production Compose files, Dockerfiles and active ignore policies, tracked env examples, and ignored local env files.
- `web/.nuxt/`, `web/.output/`, `web/node_modules/`, `api/bin/` — generated.
- `.codebase-memory/graph.db.zst` — persisted knowledge-graph artifact (stale vs HEAD; do not claim freshness).
- `Makefile` / `README.md` at root.

## Common Commands (exact — do not invent)

```sh
# Online setup: Go/frontend dependencies and pinned test/lint tools
make install
make install-tools                    # Tools only, installed into api/bin/
make preflight                        # Read-only default prerequisite checks

# Development database setup (not required for make test-integration)
cp deploy/.env.example deploy/.env
docker compose --project-directory . --env-file deploy/.env -f deploy/compose.yaml up -d --wait postgres
docker compose --project-directory . --env-file deploy/.env -f deploy/compose.yaml down  # keep data; never `down -v` unless explicitly requested
export DATABASE_URL='postgres://movieflow:movieflow@localhost:5432/movieflow?sslmode=disable'

# Full sync
make sync PROXY_FILE=/home/mathis/Documents/Dev/movieflow/tmp/proxies.txt
# Or directly:
cd api && go run ./cmd/sync-ugc -proxy-file /home/mathis/Documents/Dev/movieflow/tmp/proxies.txt

# Dev (from repo root: starts Postgres, Air API, Nuxt together)
make dev

# Normal aggregate verification (no integration tests, provider sync, or real TMDB calls)
make check
# Focused verification
make fmt-check                       # Go and frontend formatting
make test                            # Go tests + frontend unit tests
make test-go                         # Go tests with JSONL/JUnit evidence
make test-race                       # Go race tests with JSONL/JUnit evidence
make lint                            # Preinstalled pinned Go linter
make build                           # Go + frontend production builds
make test-integration                # Owns disposable PostgreSQL 18 setup and cleanup
python3 scripts/validate.py run --check go-unit --go-package ./internal/config --go-run '^Test'
python3 scripts/validate.py run --check web-unit --check web-typecheck
PYTHONDONTWRITEBYTECODE=1 python3 scripts/validate.py run --check tooling-unit
docker compose --project-directory . --env-file deploy/.env -f deploy/compose.yaml config

# Capture full-page screenshots (API and Nuxt must already be running)
make screenshot
make screenshot URL=http://localhost:3000/films OUTPUT=/tmp/opencode/films.png WIDTH=1440 HEIGHT=900 WAIT_MS=1000
make screenshot URL=http://localhost:3000/admin/sync OUTPUT=/tmp/opencode/admin-sync.png API_URL=http://localhost:8080
```

- `make screenshot` never starts, stops, or manages API or Nuxt. Defaults: `URL=http://localhost:3000/`, `OUTPUT=/tmp/opencode/messeances-screenshot.png`, `WIDTH=1440`, `HEIGHT=900`, `WAIT_MS=1000`, `API_URL=http://localhost:8080`, and `CHROME_BIN=google-chrome`.
- For `/admin` and `/admin/...` URLs, screenshot authentication loads `ADMIN_PASSWORD` from environment first, otherwise repository-root `deploy/.env`, without logging or persisting it.
- You have the right to fetch any urls, but if you fetch movie theater chains sources, use proxies from /home/mathis/Documents/Dev/movieflow/tmp/proxies.txt.

## Configuration & Security Cautions

- Go binaries auto-load the first `deploy/.env` found under `cwd` then its parent; env vars take precedence; files are never merged. For a fresh clone, run `cp deploy/.env.example deploy/.env`. Unreadable/malformed `deploy/.env` blocks startup with a generic error.
- `DATABASE_URL` required for API and full sync; unused in diagnostic mode. `TMDB_API_READ_ACCESS_TOKEN` optional bearer, never in URL/args/versioned file/logs. `ADMIN_PASSWORD` required for admin review APIs; never logged. `PROXY_FILE` optional for admin-triggered syncs; misconfigured value blocks API startup generically. `PORT` default 8080, `WEB_ORIGIN` default `http://localhost:3000`, `NUXT_PUBLIC_API_BASE` default `http://localhost:8080`.
- UGC requests must go through proxies (`-proxy-file`); sync phases use two workers, and each request can make up to four attempts across distinct proxies on transport/5xx failures; 403/429/block page/challenge aborts immediately. Never publish proxy file, credentials, or raw output — only counters and public info.
- Do not log or persist TMDB token, provider error bodies, or raw responses. Do not store password/cookie in `localStorage`/`sessionStorage`/URL/logs.

## Testing Expectations

- Read `docs/testing.md` for the canonical setup, selected prerequisites, evidence schema, exit semantics and database lifecycle. Prepare dependencies online with `make install`; `make install-tools` installs only gotestsum v1.13.0 and golangci-lint v2.13.1 into ignored `api/bin/`. Building the linter may acquire Go 1.26+ during setup; application Go requirement remains 1.26.0. Validation requires Python 3.12+ and Git; frontend checks require Node 22.23.1/npm 10.9.8. Checks do not install tools or fetch Go/npm dependencies; missing or mismatched prerequisites block rather than silently repair.
- Prefer `make check` as the normal aggregate gate. Its structured runner executes formatting, Go unit tests, frontend unit tests, Go lint, frontend typecheck, frontend lint, then Go/frontend builds. Independent cheap checks continue after a failure; build is withheld unless selected cheap checks passed. Integration, race and tooling tests are opt-in. A passed `make preflight` means prerequisites ready, not test coverage passed.
- Use focused checks for implementation feedback. `--check` is repeatable and follows canonical order; `--go-package`/`--go-run` are allowed only for a single Go unit or race selection. Direct API test/build commands need `-tags=nodynamic`; Make and the runner supply it. `make test` retains direct frontend unit execution; use the runner or `make check` for structured evidence for both layers.
- Use `make test-integration` for canonical CI integration coverage. It creates a uniquely owned `postgres:18-alpine` container with tmpfs data, generated credentials and a random loopback port, waits for readiness, runs the fixed CI package selection, then verifies disposal. Requires reachable local Docker and adequate memory, but no host psql, externally configured `TEST_DATABASE_URL`, development database or Compose volume. Missing image is pulled during explicit lifecycle setup; Go/npm remain offline. Tests, failures and SIGINT/SIGTERM trigger cleanup; SIGKILL/host/daemon failure may leave a resource. Inspect reported ownership before manual removal; never prune Docker or delete unrelated databases/volumes. Direct external-database selection is separate and documented in `docs/testing.md`.
- Keep Nuxt typecheck/build generation sequential. Do not run another dev/watch/typecheck/build process that mutates the same worktree's generated state while validating. Browser readiness is separate: `python3 scripts/validate.py preflight --browser-accounts` starts nothing and runs no browser scenarios; use `docs/accounts.md` for fixture preparation and acceptance commands.
- Consume `tmp/validation/<run-id>/report.json` and its linked raw stdout/stderr, Go JSONL and JUnit artifacts instead of reconstructing terminal history or parsing RTK summaries. Check selected commands, actual child exits, counts, tool/environment facts, artifact completeness/hashes and source/index fingerprints, including uncommitted and non-ignored untracked content. Matching source boundaries do not prove immutability or authorize automatic reuse. Reuse only relevant verified evidence manually; do not rerun a complete gate solely because ownership changed.
- Report `passed`, `failed`, `skipped`, `blocked` and `zero-selected` distinctly. Go exit zero with no selected tests is not success; all-skipped coverage is not a pass. Count Go top-level tests, subtests and package results separately, never as a combined leaf-test total. Make can return wrapper exit 2 when the runner fails; preserve the report's underlying child exit. Do not hide failures with trailing logging commands or mix previous terminal runs with current evidence.
- Evidence is ignored/private (directories 0700, files 0600). Metadata excludes credentials/URLs and arbitrary environment dumps, but raw child logs may contain sensitive output. Do not commit or upload reports, logs, proxy files, tokens or cookies without explicit inspection and authorization. The runner captures raw evidence before concise console output; do not replace it with unsupported `rtk lint`/formatter adapters for Oxlint/Biome.
