# Repository validation

## Setup

Run `make install` online once: application `go mod download`, existing npm installation/`nuxt prepare`, then pinned tools. `make install-tools` installs only gotestsum v1.13.0 and golangci-lint v2.13.1 into absolute repository `api/bin/`. `python3 scripts/validate.py install-tools --tool gotestsum` installs only the reporter, as Go CI does. Pins, module identity, embedded build Go version, and readable binary versions are verified; unverifiable, development, or wrong binaries block execution. No runtime `go run ...@version` fallback exists.

The application requires Go 1.25.13 or newer; tooling compilation may acquire Go 1.26+ during setup because golangci-lint v2.13.1 requires it. Application `api/go.mod`/`go.sum` are not tooling manifests. Validation requires Python 3.12+, Git, and a writable private artifact directory. Frontend checks require Node 22.23.1 and npm 10.9.8, matching verified CI versions. Install selected system prerequisites independently; the runner never uses sudo or repairs an environment.

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
| Integration | Explicit disposable loopback PostgreSQL 18 URI, installed psql, reachable DB and database CREATE privilege |
| Browser preflight | Node/global WebSocket, executable Chrome `--version`, existing default fixture/web services |

Prerequisites are deduplicated within an invocation, never reused from prior reports. Go probes use `go list -mod=readonly -tags=nodynamic -deps -test` for the selected graph, not compilation or test execution. Frontend probes inspect relevant installed package metadata/executables without running install or `nuxt prepare`. A missing prerequisite blocks affected checks only.

Go execution fixes `GOENV=off`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, empty `GOPRIVATE`, `GONOPROXY=none`, and `GOFLAGS=-mod=readonly`; `nodynamic` stays explicit argv. npm offline configuration and disabled Nuxt telemetry prevent dependency acquisition. This is not an OS network sandbox: local HTTP fixtures remain usable. Structured runs remove gotestsum environment overrides, account-browser/provider live opt-ins, and `TEST_DATABASE_URL` except during explicitly selected integration. Ordinary Go test caching remains enabled; cached event replay is counted without reusing a validation report.

## Integration and browser boundaries

Configure `TEST_DATABASE_URL` in your environment for an approved disposable loopback PostgreSQL 18 database, then:

```sh
python3 scripts/validate.py preflight --check go-integration
make test-integration
```

Supported URI schemes are `postgres`/`postgresql`, with username, nonempty database, loopback host (`localhost` or loopback IP), optional port, and optional `sslmode` only. Unsupported/malformed options block rather than guessing. Credentials are passed through private child libpq environment, never argv or metadata. Probe disables implicit password/service files and conflicting ambient libpq settings; `psql -X -w` reads server version and CREATE privilege with connection/statement/process timeouts and read-only transactions. Preflight never creates schemas, applies migrations, resets fixtures, or starts PostgreSQL. Test fixtures remain authoritative for DDL/migration coverage and cleanup.

Canonical packages, in existing CI order:

```text
./internal/database ./internal/publicmoviepg ./internal/schedulepg ./internal/enrichment ./internal/synccontrol ./internal/syncschedule ./internal/shortlink ./internal/accounts ./internal/accountmail ./cmd/api
```

Command retains `-tags=nodynamic -run Integration$ -count=1`. This intentionally matches existing CI, not every integration package in the repository. Existing account-specific commands remain separate. Go CI prepares application dependencies and gotestsum online, then checks runner-provided `psql` with `command -v psql`; missing client fails visibly, never triggers silent package installation. If runner image changes and lacks psql/Python, environment owner must explicitly prepare those prerequisites before checks.

```sh
python3 scripts/validate.py preflight --browser-accounts
```

This optional readiness check starts nothing and runs no scenarios. It checks Chrome version, Node WebSocket, API `/healthz`, synthetic mailbox GET with `X-Browser-Harness: 1`, and anonymous web `/api/v1/auth/session` on fixed `127.0.0.1:18089`/`127.0.0.1:13009`. Requests have bounded bodies/timeouts, no proxies, redirects, or cookie persistence. Response bodies are inspected only in memory. Missing services block readiness. No claim covers custom ports, self-mocking SPA/watchlist modes, screenshot authentication, browser acceptance, or migration correctness. Follow the [accounts validation runbook](accounts.md#validation) for separate service preparation and scenario commands.

## Evidence and exits

Every invocation creates unique `tmp/validation/<UTC timestamp>-<UUID>/`. Run directories are mode 0700; report/log/JSONL/JUnit files are mode 0600 with umask 077. Evidence is ignored and local. `report.json` is atomically updated after commands and finalized with schema version 1, kind, selection, exact argv/cwd, timestamps/durations, prerequisite references, versions, real subprocess exits, normalized signal exits, counts, artifacts, and source/index fingerprints. No shared latest file, report overwrite, retry-to-green, automatic evidence reuse, or CI artifact upload exists. Report does not hash itself.

Metadata includes only safe allowlisted runtime/environment facts. It excludes database URI/identity, credentials, credential hashes, arbitrary environment dumps, captured output, raw exception messages, dynamic subtest names/reasons, and HTTP bodies/cookies. Complete child output stays in private stdout/stderr logs; treat raw evidence as potentially sensitive even when metadata is safe. Do not upload raw logs or traces without explicit inspection and authorization. Console gives status and exact report path, not child output.

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
