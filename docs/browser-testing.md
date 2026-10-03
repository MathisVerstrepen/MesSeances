# Playwright browser workflow

Movieflow uses pinned `@playwright/test@1.63.0` for repeatable regression tests
and [Microsoft Playwright CLI](https://github.com/microsoft/playwright-cli)
with its official `playwright-cli` skill for agent exploration. Existing tests
are the first source of browser coverage; extend them when a new scenario
should remain repeatable. Do not recreate temporary CDP clients or use fixed
sleeps to establish readiness.

## Setup and regression tests

From the repository root, prepare dependencies explicitly:

```sh
npm --prefix web ci
make install-browser
make test-browser
# Focused feedback; Chromium desktop only.
npm --prefix web run test:browser -- --project=desktop e2e/cinema.spec.ts
```

Linux CI installs browser system libraries explicitly with
`cd web && npx --no-install playwright install --with-deps chromium`. Local
checks do not install dependencies automatically. The ordinary `make check`
gate remains separate; `Frontend CI / browser` runs this opt-in suite on PRs
to `dev` and `main`. No branch protection settings are changed here.

The suite starts and stops its own loopback Nuxt fixture on port 13400, with
a synthetic API on a random loopback port. `PLAYWRIGHT_PORT=13401 make
test-browser` selects another port; an occupied port fails rather than
reusing or stopping someone else's server. Generation is isolated under
`web/node_modules/.cache/playwright-<port>/`; it does not rewrite the
developer's `.nuxt` directory. The fixture disables the dev type checker and
Oxc tsconfig discovery so it does not depend on `.nuxt/tsconfig.json`. Run
normal typecheck separately for application types. A fixture-only Nuxt
`app:mounted` marker lets interactions wait for hydration without sleeps.

Coverage includes SSR cinema identity and listings, the films tab and search,
back/forward navigation, a missing cinema's SSR 404, and rejection of synthetic
admin credentials. Each scenario runs at desktop and mobile widths. Browser
errors, external requests and unmocked API requests fail the suite. Browser
HTTP(S) egress is also blocked by a loopback proxy with Chromium's loopback
bypass, and service workers are disabled. These are frontend checks against
mocked responses: they do not prove Go, PostgreSQL, provider ingestion, real
admin authorization or account integration. Keep existing integration gates
and the [accounts runbook](accounts.md#validation) for those requirements.
The fixture reads no deployment dotenv, starts no Go API/database, and has
empty internal authentication and analytics configuration.

## CLI exploration with the official skill

The global OpenCode setup uses `@playwright/cli@0.1.22` and Microsoft's
`skills/playwright-cli` at commit
`b85c7a736bb473bf55b584e54a09ffa698d6d871`, installed under
`~/.config/opencode/skills/playwright-cli/`. `UPSTREAM.md` records provenance
and `LICENSE` preserves the upstream license. OpenCode browser roles must
load `playwright-cli` before browser work. Restart OpenCode after installation
or permission changes. On another machine, install the pinned CLI:

```sh
npm install --global @playwright/cli@0.1.22
playwright-cli --help
```

Install the official skill in the agent's discovered skills directory,
retaining its references and license. Microsoft's `playwright-cli install
--skills` is also available; verify the resulting destination for the agent.
The CLI browser uses local Chrome in the example below; the test suite uses
its pinned downloaded Chromium independently.

For exploration, start only the repository fixture in one terminal:

```sh
npm --prefix web run test:browser:server
```

In another terminal, create a unique session owned by this task. Do not
attach to another browser or close all sessions. Keep screenshots, snapshots
and traces private and ignored; use synthetic credentials only:

```sh
umask 077
mkdir -p tmp/playwright
PLAYWRIGHT_CLI_SESSION="movieflow-$(date +%s)-$$"
export PLAYWRIGHT_CLI_SESSION
playwright-cli open http://127.0.0.1:13400/cinema/cinema-playwright --browser=chrome
# Read the snapshot; wait for the fixture hydration marker before interactions.
playwright-cli run-code "async page => await page.locator('html[data-playwright-ready=true]').waitFor()"
playwright-cli snapshot --depth=4
playwright-cli find Films
# Use refs from this session's latest snapshot or a stable role locator.
playwright-cli click "getByRole('navigation', { name: 'Vue de la programmation' }).getByRole('link', { name: 'Films', exact: true })"
playwright-cli console error
playwright-cli screenshot --filename=tmp/playwright/cli-cinema.png
playwright-cli close
```

Stop the fixture with Ctrl-C in its terminal when finished. `--headed` is
optional. CLI exploration and the automated suite must use different fixture
ports if run concurrently. Unlike the automated suite, manual CLI exploration
does not install its browser routing/egress guards; stay on the synthetic
fixture and do not follow external booking or footer links. Turn discoveries
into tracked `web/e2e/*.spec.ts` tests rather than leaving one-off scripts.

## Evidence and debugging

HTML reports and screenshots live under ignored `tmp/playwright/`. Traces
and failure screenshots are retained only for failed tests; successful
cinema layout tests retain desktop/mobile screenshots for inspection. Files
are created with a private umask. Open reports or traces locally:

```sh
npm --prefix web run test:browser:report
cd web
npx --no-install playwright show-trace ../tmp/playwright/results/<scenario>/trace.zip
```

Browser results are separate from the structured runner's `report.json`:
record the exact browser command, exit, selected scenarios and source scope.
The existing runner does not accept a `web-browser` check. Reuse relevant
verified canonical evidence after checking coverage, observed exits, artifact
completeness and source boundaries; ownership changes alone do not invalidate
it. A source fingerprint alone is not sufficient. Re-run affected checks
when code or assumptions change. Do not commit or upload private reports,
traces, snapshots, cookies or credentials.
