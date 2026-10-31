# Repository validation

## Setup

Run `make install` online once: application `go mod download`, existing npm installation/`nuxt prepare`, then pinned tools. `make install-tools` installs only gotestsum v1.13.0 and golangci-lint v2.13.1 into absolute repository `api/bin/`. `python3 scripts/validate.py install-tools --tool gotestsum` installs only the reporter, as Go CI does. Pins, module identity, embedded build Go version, and readable binary versions are verified; unverifiable, development, or wrong binaries block execution. No runtime `go run ...@version` fallback exists.

The application requires Go 1.26.0 or newer; tooling compilation may acquire Go 1.26+ during setup because golangci-lint v2.13.1 requires it. Application `api/go.mod`/`go.sum` are not tooling manifests. Validation requires Python 3.12+, Git, and a writable private artifact directory. Frontend checks require Node 22.23.1 and npm 10.9.8, matching verified CI versions. Install selected system prerequisites independently; the runner never uses sudo or repairs an environment.

## Commands

```sh
make install
make preflight
make check
make test-go
make test-race
make lint
python3 scripts/validate.py run --check web-unit --check web-typecheck
python3 scripts/validate.py run --check go-unit --go-package ./internal/config --go-run '^Test'
PYTHONDONTWRITEBYTECODE=1 python3 scripts/validate.py run --check tooling-unit
```

`run` defaults to `format`, `go-unit`, `web-unit`, `go-lint`, `web-typecheck`, `web-lint`, then `build`. `tooling-unit`, `go-race`, and `go-integration` are opt-in. `--check` is repeatable and executes in canonical order, not option order. Unknown options/checks return usage exit 2; arbitrary shell commands and Go flags are unsupported. Relative `--go-package` and `--go-run` selectors are allowed only with one selected `go-unit` or `go-race` check. Canonical integration selection cannot be overridden. `make test` still runs Go and the current npm unit script; use `make check` for structured evidence for both.

Independent cheap checks continue after another fails or blocks. Build is blocked unless every selected default cheap check passed; explicit `--check build` checks only build prerequisites. Execution is sequential, especially Nuxt typecheck/build. Ensure no dev/watch/typecheck/build process concurrently mutates this worktree's Nuxt generated files; the runner is not a cross-process scheduler or lock manager.

`preflight` accepts the same bounded selection but launches no validation commands. A passed preflight means prerequisites ready, not tests passed. `preflight --browser-accounts` checks only browser prerequisites unless explicit `--check` options add other checks. Script resolves repository root independently of caller cwd.

## Playwright browser checks

`make install-browser` explicitly prepares pinned Chromium after npm setup.
`make test-browser` runs desktop/mobile frontend scenarios against an owned
loopback Nuxt fixture and synthetic API. It is separate from `make check` and
the structured runner; do not select an unsupported `web-browser` check.
See [browser testing](browser-testing.md) for CLI/skill setup, hydration
readiness, fixture lifecycle, report locations and coverage limits.

## Selected prerequisites

| Selection | Required prerequisites |
|---|---|
| All | Python 3.12+, Git repository identity, complete readable source snapshot, private writable evidence |
| Go unit/race/integration | Go minimum from `api/go.mod`, installed pinned gotestsum, offline selected package graph/cache |
| Go lint | Go, installed pinned golangci-lint, offline package graph/cache; not gotestsum |
| Go race | Additionally enabled CGO and an available Go-configured C compiler |
| Format | Make, gofmt, Node/npm and installed pinned Biome; no Go package graph |
| Web unit | Node/npm; existing native Node test script |
| Web typecheck | Node/npm, installed pinned Nuxt and vue-tsc |
| Web lint | Node/npm, installed pinned Oxlint |
| Build | Make, Go graph/cache, Node/npm and installed pinned Nuxt; not test/lint tools |
| Tooling unit | Existing Python unittest suite, Bash, jq |
| Integration via Make | Reachable local Docker daemon, `postgres:18-alpine` image (pulled only if absent), adequate tmpfs memory; no host psql |
| Direct manual integration | Explicit disposable loopback PostgreSQL 18 URI, installed psql, reachable DB and database CREATE privilege |
| Browser preflight | Node/global WebSocket, executable Chrome `--version`, existing default fixture/web services |

Prerequisites are deduplicated within an invocation, never reused from prior reports. Go probes use `go list -mod=readonly -tags=nodynamic -deps -test` for the selected graph, not compilation or test execution. Frontend probes inspect relevant installed package metadata/executables without running install or `nuxt prepare`. A missing prerequisite blocks affected checks only.

Go execution fixes `GOENV=off`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, empty `GOPRIVATE`, `GONOPROXY=none`, and `GOFLAGS=-mod=readonly`; `nodynamic` stays explicit argv. npm offline configuration and disabled Nuxt telemetry prevent dependency acquisition. This is not an OS network sandbox: local HTTP fixtures remain usable. Structured runs remove gotestsum environment overrides, account-browser/provider live opt-ins, and `TEST_DATABASE_URL` except during explicitly selected integration. Ordinary Go test caching remains enabled; cached event replay is counted without reusing a validation report.

## Integration and browser boundaries

### Umami retention schema contract

Run `make test-umami-retention` for the independent analytics retention suite.
It requires Python 3.12+ and reachable local Docker, prepares `postgres:15-alpine`
only if absent, and creates a uniquely named and labelled container with tmpfs
data, no published ports and no external network. Generated credentials stay in
the child environment. The suite never uses `DATABASE_URL`, `TEST_DATABASE_URL`,
Compose resources, persistent volumes or production data. Cleanup verifies test
ownership before removal and verifies absence afterwards, including on test
failure and SIGINT/SIGTERM. SIGKILL or Docker/host failure can leave a resource;
inspect its `messeances.umami-retention-test` ownership label before removal.

The suite executes the real `deploy/umami-retention.sql` against an independent
minimal fixture. It covers compatible column/nullability changes, optional
preserved tables, unknown-table/missing-column/type diagnostics, cleanup and
preservation across all nine analytics tables, inherited expiration, idempotence,
and transaction rollback on a later deletion failure. It is separate from
`make check` and runs in the `Umami retention / schema contract` CI job. Before an
Umami upgrade, also test the candidate's actual migrated schema in a disposable
database; the fixture verifies contract behavior rather than upstream migrations.

### Application PostgreSQL integration

Run canonical integration with an automatically owned disposable database:

```sh
make test-integration
# Equivalent explicit lifecycle; never enabled by ordinary preflight/check:
python3 scripts/validate.py run --check go-integration --disposable-database
```

The finite `--disposable-database` option is valid only for `run --check go-integration`. Other Go prerequisites must pass before creation. Docker server availability is checked, then the image is inspected; only an absent image triggers explicit `docker pull postgres:18-alpine` dependency setup (up to 300 seconds). Initial image download requires network access. Existing images are not refreshed or deleted. Go/npm execution remains offline. The daemon must publish loopback ports reachable from the runner; remote Docker hosts are not automatically forwarded or repaired.

Each invocation reserves a unique name and run label, creates a stopped container with `--pull=never`, maps `127.0.0.1::5432`, mounts `/var/lib/postgresql` as tmpfs (the PostgreSQL 18 data root), then captures and verifies its full container ID, exact name, label, and image before starting. Concurrent runs share neither container nor host port. No Compose project, persistent volume, existing database, or Docker prune is used. Generated password is supplied to Docker via environment-variable names without values in argv; the owned URL is injected only into the integration test child's private environment, ignoring inherited `TEST_DATABASE_URL` and libpq overrides. Docker administrators can inspect container credentials while it exists; the Docker daemon must be trusted.

Readiness uses the container's `psql` through `docker exec`, password via private environment, read-only server version/CREATE privilege query, and bounded connection/statement/process timeouts. No host psql is required. Create is bounded to 120 seconds, start to 60, readiness to 60 plus a final probe of at most 10, and other Docker operations to 30 each. Readiness retries only connection startup, never tests. Existing fixtures remain authoritative for DDL/migration coverage and schema cleanup. Integration tests need enough memory for their tmpfs database; no filesystem-backed fallback is inferred.

Disposal stops the active test process group first, re-verifies exact ownership, removes only that full container ID with `docker rm --force`, then separately verifies absence. A failed inspect is not proof of absence: a successful exact-name container listing must confirm none. Cleanup runs on success, failed tests, create/start/readiness errors, and SIGINT/SIGTERM. Signals during create are deferred until its bounded completion/ID acquisition to avoid killing the client while Docker creates the container; missing ID is recovered only through verified exact name/label/image. Repeated signals are ignored during bounded cleanup/finalization. Cleanup can take up to five 30-second Docker operations. Cleanup failure makes an otherwise successful run nonzero, while original failed test/signal exit is preserved. Lifecycle argv, timestamps, real exits, private logs, owned identity, and cleanup disposition appear in `database_lifecycle` in the existing report; passwords/URLs and full Docker environment inspection never do.

SIGKILL, host failure, Docker daemon loss, or a create request still unresolved after its timeout cannot guarantee disposal. Inspect `database_lifecycle.name`, `label`, and `container_id` and verify exact ownership before manual removal; never prune resources or delete unrelated containers/volumes. Failed or unverifiable cleanup is reported, not silently treated as success.

Direct manual selection remains separate: configure `TEST_DATABASE_URL` for an approved disposable loopback PostgreSQL 18 database and host psql, then use:

```sh
python3 scripts/validate.py preflight --check go-integration
python3 scripts/validate.py run --check go-integration
```

Supported URI schemes are `postgres`/`postgresql`, with username, nonempty database, loopback host (`localhost` or loopback IP), optional port, and optional `sslmode` only. Unsupported/malformed options block rather than guessing. Credentials are passed through private child libpq environment, never argv or metadata. Probe disables implicit password/service files and conflicting ambient libpq settings; `psql -X -w` reads server version and CREATE privilege with bounded read-only queries. This mode never creates/disposes a container or drops the operator's database. Ordinary preflight never creates schemas, applies migrations, resets fixtures, or starts PostgreSQL.

Canonical packages, in existing CI order:

```text
./internal/database ./internal/publicmoviepg ./internal/schedulepg ./internal/enrichment ./internal/synccontrol ./internal/syncschedule ./internal/shortlink ./internal/accounts ./internal/accountmail ./cmd/api
```

Command retains `-tags=nodynamic -run Integration$ -count=1`. This intentionally matches existing CI, not every integration package in the repository. Existing account-specific commands remain separate. Go CI prepares application dependencies and gotestsum online, then runs the same self-managed Make target without a fixed-port service, inherited database URL, or host-psql installation. Missing Docker/Python prerequisites fail visibly, never trigger sudo or silent system-package installation.

```sh
python3 scripts/validate.py preflight --browser-accounts
```

This optional readiness check starts nothing and runs no scenarios. It checks Chrome version, Node WebSocket, API `/healthz`, synthetic mailbox GET with `X-Browser-Harness: 1`, and anonymous web `/api/v1/auth/session` on fixed `127.0.0.1:18089`/`127.0.0.1:13009`. Requests have bounded bodies/timeouts, no proxies, redirects, or cookie persistence. Response bodies are inspected only in memory. Missing services block readiness. No claim covers custom ports, self-mocking SPA/watchlist modes, screenshot authentication, browser acceptance, or migration correctness. Follow the [accounts validation runbook](accounts.md#validation) for separate service preparation and scenario commands.

## Evidence and exits

Every invocation creates unique `tmp/validation/<UTC timestamp>-<UUID>/`. Run directories are mode 0700; report/log/JSONL/JUnit files are mode 0600 with umask 077. Evidence is ignored and local. `report.json` is atomically updated after commands and finalized with schema version 1, kind, selection, exact argv/cwd, timestamps/durations, prerequisite references, versions, real subprocess exits, normalized signal exits, counts, artifacts, and source/index fingerprints. No shared latest file, report overwrite, retry-to-green, automatic evidence reuse, or CI artifact upload exists. Report does not hash itself.

Metadata includes only safe allowlisted runtime/environment facts. It excludes database URI/identity, credentials, credential hashes, arbitrary environment dumps, captured output, raw exception messages, dynamic subtest names/reasons, and HTTP bodies/cookies. Complete child output stays in private stdout/stderr logs; treat raw evidence as potentially sensitive even when metadata is safe. Do not upload raw logs or traces without explicit inspection and authorization. Console gives status and exact report path, not child output.

Failed, launched `go-integration` checks additionally print sanitized failing package and top-level test identifiers from existing JSONL. Packages must exactly match the canonical integration selection; console uses repository-owned relative paths. Test names must be ASCII Go `Test` identifiers no longer than 128 characters; subtest names and raw output are never printed. Identifiers are deduplicated and sorted, with at most 20 tests and a fixed cap marker. Scanning is limited to 1 MiB per line and 64 MiB total. Partial JSONL can identify failures without complete JUnit evidence; malformed, missing, unreadable or oversized JSONL produces a fixed safe fallback. Diagnostics do not change check status, actual exit codes, report schema, private evidence, or database cleanup.

| Status | Meaning |
|---|---|
| `passed` | Command exit zero, required complete evidence, meaningful selected test coverage (or successful non-test check) |
| `failed` | Nonzero child exit/signal, or invalid/incomplete evidence even if child exited zero |
| `skipped` | Not selected, preflight-only command, or all selected tests skipped/todo; reason distinguishes them |
| `blocked` | Missing prerequisites/snapshot, dependent build withheld, source boundary mismatch, or interruption |
| `zero-selected` | Successful complete test evidence but no selected top-level tests; never a pass |

Only all selected checks passed can produce a passed validation. Aggregate precedence is failed, blocked, zero-selected, then skipped/noncoverage. Runner returns first observed nonzero check exit in canonical order; blocked/zero-selected/all-skipped/source-change without child failure return 1. Usage errors return 2. Interrupts stop active child process group, finalize available evidence, block remaining checks, and return 130/143. Make may return wrapper exit 2 after runner failure; `checks[].returncode` preserves underlying child exit, including zero when evidence or zero-selection gates reject it.

Go counts come from complete JSONL, not console summaries or JUnit totals. Unique top-level functions, subtests, and package results have separate pass/fail/skip totals. Their sum is not distinct leaf-test coverage. Packages without tests still need terminal events; globally zero tests is not success. Successful Go checks require terminal events and parseable JUnit recognized root. Failed commands retain partial/missing artifacts and actual exit. Web counts are native Node TAP counters (tests/suites/pass/fail/cancelled/skipped/todo); tooling counts use unittest's terminal count/disposition. Lint, format, typecheck, and build have null test counts.

## Manual evidence verification

Before consuming an existing report, inspect its final status, actual exits, selected check IDs/argv/cwd, prerequisite dispositions, expected pins/runtime versions, count categories, and every required artifact's existence/completeness/size/SHA-256. Compare relevant environment facts and both source/index fingerprints against current worktree. A different selection or environment is not equivalent evidence.

Snapshot covers union of HEAD-tracked, index-tracked, and non-ignored untracked paths using NUL-safe Git enumeration. It hashes length-delimited relative path, file type/executable mode, streamed content digest, deletion markers, and symlink text without following external targets; index-stage metadata is hashed separately. Tracked ignored files remain included. Ignored untracked binaries, node_modules, generated Nuxt files, dotenv, reports, and agent state are excluded. Git failures/unreadable content block, never degrade to HEAD-only evidence. Start/end mismatch blocks apparent pass unless an executed check already failed. Matching boundaries do not prove immutability during the run or authorize automatic reuse. Choose missing coverage manually; do not rerun a complete gate merely because ownership changed.

## Self hosted CI on the VPS

The optional `messeances-vps` scale set uses gha-outrunner v1.2.0 and QEMU/libvirt
on `Hogwarts-3`. Deployment sources are in `deploy/ci/outrunner/`; the deployed
copy, binaries, image, state and private credential live beneath
`/home/hogwarts/config/gha-outrunner`. Host packages, systemd units and root-owned
helper scripts use their conventional system locations. Fireactions is inactive.

Each job gets a fresh Ubuntu 24.04 VM with 8 vCPUs, 12 GiB RAM and a 40 GiB virtual
disk. The controller permits one VM at a time. Docker runs inside the guest;
the production Docker socket, host paths and long-lived GitHub token are never
mounted into the guest. Public Go/npm/browser dependencies are warmed into the
base image; job writes go to disposable overlays and cannot update that cache.
The compilation warmup selects no tests and is not validation evidence.

The dedicated `virbr-mfci` network uses `10.231.64.0/24`. nftables rules block
access to the host, private networks, metadata endpoints, IPv6 and other guests.
Guest egress permits public HTTP/HTTPS and DNS to the configured public resolvers.
The firewall changes only its own two tables and this bridge. No public management
endpoint or DNS name is needed. Upstream hardcodes the libvirt network name
`default`, so installation refuses an existing unrelated network with that name.
The stock libvirt default subnet overlaps existing Docker networks and must not
be started. Installation suppresses package service starts until our network is
configured.

The controller runs as the dedicated `outrunner` user. systemd provides the
repository-scoped fine-grained token through `LoadCredential`; guests receive
only their single-use JIT configuration. The private token file must be a regular
file with mode 0600 at `secrets/github-token`. Never paste it into commands, GitHub
logs, tracked files or chat. Token rotation requires restarting the controller.
The libvirt management API is a local Unix socket restricted to root and the
`libvirt` group; it does not require interactive Polkit authentication. Membership
in that group is administrative host access and is granted only to the controller.

All external contributors require GitHub workflow approval. Audit the exact
revision before approving: PR-editable workflow conditions and runner labels
are routing, not security boundaries. Fork PRs normally use GitHub-hosted runners;
VM isolation and the host firewall remain required even after approval.

`Go CI / integration` uses the VPS for same-repository PRs only when repository
variable `CI_SELF_HOSTED_ENABLED` is `true`; otherwise it uses `ubuntu-latest`.
Other normal CI jobs remain on GitHub-hosted runners to preserve their parallelism.
`VPS CI validation` runs guest isolation checks and canonical PostgreSQL integration
when deployment files change, and can be dispatched manually after its workflow
has reached the default branch. Deployment safeguards are tested on a hosted job.
No raw test reports or logs are uploaded as artifacts.

From the VPS, the existing Docker administrator account can operate the service
without altering sudo policy or Docker daemon configuration:

```sh
cd /home/hogwarts/config/gha-outrunner
bash deployment/admin.sh status
bash deployment/admin.sh stop
bash deployment/admin.sh activate
# Persistent shutdown, including across a host reboot:
bash deployment/admin.sh disable
```

For a fresh installation, first copy `deploy/ci/outrunner/` into that directory's
`deployment/` subdirectory, then run `admin.sh download` and `admin.sh prepare`.
The download command fetches pinned upstream outrunner assets and the current
Ubuntu image; it overwrites those local download files, so do not run it during
an image build. Provision the scoped token file, build the image, then activate.

`activate` verifies the private credential, image presence and GitHub's external
contributor approval policy before starting the controller and watchdog. Images
must be built with `bash deployment/admin.sh build-image` while the controller is
stopped. The builder refuses to overwrite an existing image or unfinished build;
archive an old image only after confirming that no CI VM uses it. It verifies the
Ubuntu, Go, Node and official runner downloads, boots a credential-free build VM,
checks the tool versions and network isolation, resets guest identity and seals
the cleanly shut-down image. No host directory is shared with that VM. Keep the
pinned official runner current: upstream disables runner auto-update, and GitHub
can stop accepting an outdated runner. Review the image and package pins at least
every two weeks, including the Go minimum and Node/npm/browser versions used by
this repository. Image manifests record the source checksum and tool versions.

GitHub jobs time out after 20 minutes. A host watchdog checks every minute and
force-stops only a verified CI domain 45 minutes after first observation and
restarts the controller to clear a lost job assignment. It also removes only
inactive CI domains' leftover UEFI variable stores. The watchdog runs as root for
those two operations, with writable paths limited to its leases and libvirt's
UEFI directory. Normal job
completion destroys the VM. Startup/shutdown cleanup verifies the exact reserved
name pattern, confirms domain absence before deleting its overlay, rejects
symlinks and fails visibly on disposal errors. After an abrupt host/controller
failure, inspect only `messeances-vps-<eight hex digits>` domains and matching
files in `state/overlays`; never prune Docker or remove unrelated VM storage.

For immediate CI rollback, set `CI_SELF_HOSTED_ENABLED=false` before submitting
or rerunning jobs. Existing queued jobs retain their original runner selection;
cancel and rerun them after the variable change. Disabling the controller alone
leaves self-hosted jobs queued. Repository-local verification for deployment work:

```sh
for script in deploy/ci/outrunner/*.sh; do bash -n "$script"; done
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s deploy/ci/outrunner/tests -v
actionlint .github/workflows/go.yml .github/workflows/ci-vps.yml
git diff --check
```
