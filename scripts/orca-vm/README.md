# Orca Docker workspaces

Recipe `local-docker` creates an SSH container and a private PostgreSQL 18 container per workspace. SSH, Nuxt and API ports bind to random localhost ports. No host Docker socket or host agent-state directory is mounted. Workspaces use ordinary Orca linked worktrees, schema version 1.

## Setup

Requirements: Docker Engine, Node, SSH, `gh` authenticated as the intended GitHub account, and local `opencode`, `bun`, `rtk`, `codebase-memory-mcp` executables. The image uses Go 1.25.13, Node 22.23.1, npm 10.9.8 and copies the installed OpenCode binary. It copies active files from `~/.config/opencode` without rewriting them, including agents, plugins, skills and their dependencies. Credentials, caches, sessions and host runtime data are excluded. This configuration uses `/home/mathis` inside the image.

Python 3, make and g++ are included for Orca's native `node-pty` terminal installation on Linux. Orca installs its relay inside each running environment; the base and authenticated images contain no initialized Orca runtime.

Base image excludes config `.env`. Auth preparation copies that exact file, when present, with owner-only permissions into the authenticated layer, so credential-dependent config plugins also work. OpenCode provider credentials still come from the interactive container login. Host session databases and runtime state are never copied.

```sh
./scripts/orca-vm/docker-base-snapshot.sh
./scripts/orca-vm/docker-base-auth.sh start
docker exec -it --user mathis messeances-orca-auth opencode auth login
# Log in to both openai and commandcode, used by the existing configuration.
./scripts/orca-vm/docker-base-auth.sh finish
orca-ide vm recipe doctor local-docker --repo-path "$PWD" --json
orca-ide vm recipe doctor local-docker --repo-path "$PWD" --provision --json
```

Use a device-code/headless login method when authenticating OAuth providers. A browser loopback callback cannot reach the container. Enter API keys in the interactive prompt only. `finish` verifies both credential records, stops and commits the auth container with the SSH entrypoint, stores its immutable image ID, then removes the auth container. `cancel` removes an unfinished auth container. Authenticated images contain account credentials: keep them local and rebuild/re-authenticate when credentials expire. Never push them to an image registry.

Runtime state, temporary build context and SSH keys live in ignored `.local/`. `docker-state.json` contains non-secret initial settings; scripts resolve `ORCA_DOCKER_*` overrides before runtime state. `ORCA_OPENCODE_CONFIG_SOURCE` overrides the host configuration directory. `GH_TOKEN` or `GITHUB_TOKEN` overrides `gh auth token`. BuildKit passes Git auth as a secret; create passes it over stdin only for the fetch. No Git credential is retained. Public repo reads work afterwards; private repo work requires separate Git authentication inside the environment.

The default working container has 4 CPU and 8 GiB RAM; PostgreSQL has 1 CPU and 1 GiB RAM. No automatic timeout is configured. Sleep stops both containers, wake starts both and re-emits the SSH result, and delete removes both containers, the PostgreSQL anonymous volume, private network and matching known-hosts entry. Host keys are generated independently per container and trusted through local Docker, with strict SSH checking.

Each workspace network takes a random `/28` subnet from `10.240.0.0/16`, avoiding Docker's small default address pools. Docker rejects overlapping allocations and the recipe retries up to eight times. Set `ORCA_DOCKER_NETWORK_PREFIX` or state `networkPrefix` to another two-octet prefix if this range conflicts with your LAN/VPN. Existing networks and daemon settings are not changed.

## Use

The workspace composer reads `environmentRecipes` from the primary checkout. Keep `orca.yaml` there to expose **Docker · MesSeances** in **Run on**. A recipe only on a feature branch can be checked by doctor but does not appear in the primary checkout's picker.

Inside the actual workspace checkout, run `orca-dev` to install checkout dependencies and launch Air/API and Nuxt against its isolated PostgreSQL. This image-installed helper works with ordinary `dev` checkouts; repository `make dev` remains the host Docker Compose launcher. The base clone warms dependency caches, but linked worktrees install their own frontend dependencies. Application secrets and provider proxies must be configured separately in ignored `deploy/.env`. `make check` and integration tests use the container toolchain and database.

The recipe result includes `userData.webUrl` and `userData.apiUrl`. Use the web URL after `orca-dev` starts. Database is internal to the workspace network and is never published to the host. Browser requests use the Nuxt same-origin development proxy; SSR reaches localhost:8080 inside the working container.

## Catalogue copy

Every new environment copies the current local catalogue before returning its SSH connection. Start the repository's local Compose PostgreSQL and keep its catalogue synchronized. Source discovery requires exactly one running `postgres` service in Compose project `movieflow`. Override with `ORCA_DOCKER_SOURCE_COMPOSE_PROJECT` (state `sourceComposeProject`) or `ORCA_DOCKER_SOURCE_POSTGRES_CONTAINER` (state `sourcePostgresContainer`). Container selection uses Docker labels rather than fixed container names. Source tools connect using that container's `POSTGRES_USER` and `POSTGRES_DB` over its local socket; source is read-only.

Native `pg_dump` exports public schema definitions separately from one consistent data snapshot. `catalogue-tables.txt` is the explicit data allowlist: schedule generations, public films and sources, enrichment, cinema locations, screening history, TMDB catalogue evidence, migration ledger and catalogue identity sequences. Account records, passwords, sessions, tokens, watchlists, mail, rate limits, short links, sync jobs and scheduled-sync configuration are never exported. Newly added tables are excluded until explicitly reviewed and added to the allowlist. Schema definitions remain available for all application tables, including empty account tables.

`pg_restore` loads pre-data schema, allowlisted data, then indexes and foreign keys into the fresh private sidecar. Temporary archives live in ignored owner-only `.local/` and are removed on success or failure. The recipe requires future showtimes in the active publication, analyzes the restored database, briefly starts the API to apply checkout migrations and validate snapshot/public catalogue responses, then stops it. Missing source, catalogue without future showtimes, dump, restore or startup failure aborts creation and removes its containers, database volume and network. No provider sync runs during creation.

Publication timestamps and revisions are copied exactly. The application's `/readyz` requires a publication less than 24 hours old and coverage of today's date. An older publication with future showtimes is copied with a stderr warning when the public catalogue still serves successfully; `/readyz` may remain 503 until an explicit sync refreshes it. The recipe never rewrites dates to manufacture freshness.

Sleep/wake preserves the workspace database without re-copying it. Workspace edits never change the source or another workspace. Delete/recreate takes a fresh local snapshot. Refresh the host catalogue explicitly when its showtimes become outdated; the recipe does not store catalogue data in the authenticated image. No image rebuild or new OpenCode login is needed for changes to the host-side copy scripts.

Run the opt-in Docker integration test against the configured local source and authenticated image:

```sh
ORCA_DOCKER_INTEGRATION=1 node --test scripts/orca-vm/catalogue-copy.test.mjs
```

It creates disposable environments, injects synthetic private records only into a test sidecar, checks exclusion, catalogue revisions/sequences, independent databases, sleep/wake and populated linked-worktree HTTP responses. Missing-source, dump, restore and no-future-showtimes failures must leave no resources or archives. The local source database is not modified.

Image rebuilds reset authenticated state, requiring a new auth snapshot. Existing workspace containers retain their old image. Re-run base snapshot after changing the host OpenCode config to copy the new exact files. Restart OpenCode to load any updated config.
