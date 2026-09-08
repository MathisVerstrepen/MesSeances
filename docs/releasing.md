# Development and release operation

MesSeances uses `dev` as its development integration branch. Feature pull requests target `dev`; release pull requests are same-repository `dev` to `main`. Only `main` is protected. Production deployment remains manual.

## One-time branch setup

Repository administrators create `dev` from current `main` once:

```sh
git fetch origin
git push origin origin/main:refs/heads/dev
```

Configure `main`, not `dev`, to require pull requests with zero mandatory approvals, conversation resolution, and these status checks:

- `Go CI / checks`
- `Go CI / integration`
- `Frontend CI / checks`
- `Release PR / Validate release metadata`

Block force pushes and branch deletion. Reviews may still be requested, but they are not mandatory while `MathisVerstrepen` is the repository's only collaborator; requiring one approval would prevent the owner from merging their own pull request. Repository settings are operator-owned and are not changed by repository automation.

## Feature work

Create each feature worktree from current remote `dev`:

```sh
git fetch origin
git worktree add ../movieflow-my-feature -b feat/my-feature origin/dev
cd ../movieflow-my-feature
```

Implement and verify the feature. Read `.github/PULL_REQUEST_TEMPLATE/feature.md` and prepare the feature PR body from that template, not the release template. With explicit push/PR authorization:

```sh
git push -u origin feat/my-feature
gh pr create --base dev --head feat/my-feature --title "feat: describe feature" --template feature.md
```

After merge, remove the worktree and local feature branch from the original checkout:

```sh
git worktree remove ../movieflow-my-feature
git branch -d feat/my-feature
```

`dev` is intentionally unprotected. Feature pull requests into `dev` are a required team convention rather than a GitHub rule. Require every registered check to pass before merging, including `Release automation / tests` alongside existing Go, integration, and frontend checks. This adds no branch-protection setting and does not rename existing checks.

## Release pull request

Choose a version matching strict non-`v` `X.Y.Z` syntax. Major, minor, and patch components have no leading zero unless they are exactly zero. Prerelease and build suffixes are not accepted. First release may use any valid strict stable version when no strict stable tags exist. Every later version must be numerically greater than all existing strict stable tags; unrelated `v`, prerelease, build-suffixed, and legacy tags are ignored.

Derive factual release notes from synchronized `main...dev` history before adding the changelog. For each included commit, use repository commit-to-pull-request association as evidence. Append a direct same-repository feature pull request link to every release-note bullet when that evidence establishes the corresponding pull request. Do not infer links from issue numbers, branch names, commit prose, or similar wording, and do not link a direct commit when no associated feature pull request exists. Multiple proven corresponding pull requests may be linked.

Use this exact bullet format when one corresponding feature pull request is proven:

```markdown
- Concise factual change ([#123](https://github.com/MathisVerstrepen/MesSeances/pull/123))
```

Use an unlinked bullet when no corresponding feature pull request is available:

```markdown
- Concise factual change
```

Create `docs/changelogs/X.Y.Z.md` using strict version from `Release X.Y.Z` exactly, with no `v`, extra title, introduction, or other prose. File must be an ordinary tracked repository file encoded as strict UTF-8, use LF line endings, end with exactly one newline, and contain only final release body. Create, validate, commit, and push this file to synchronized `dev` before creating release pull request. Then pass same file directly as pull request body:

```sh
version=0.7.0
git switch dev
git fetch origin main dev
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/dev)"
# Create and review docs/changelogs/$version.md here.
git add -- "docs/changelogs/$version.md"
git diff --cached --name-only
git commit -m "docs: add $version changelog"
git push origin dev
gh pr create \
  --base main \
  --head dev \
  --title "Release $version" \
  --body-file "docs/changelogs/$version.md"
```

Before commit, require staged changes contain exactly expected changelog path. Before push, require commit contains exactly that path and parent is previously synchronized `dev`. Recheck remote `dev` immediately before push and stop on a race instead of forcing. Never amend or force-push release preparation. If exact changelog is already tracked at synchronized `dev`, reuse it without creating another commit.

Browser flow uses [the release comparison page](https://github.com/MathisVerstrepen/MesSeances/compare/main...dev?expand=1&template=release.md). Select `release.md` if GitHub presents a template chooser, set base to `main` and compare to `dev`, set title to exact `Release X.Y.Z`, remove all template comments, and paste exact changelog content as body. Verify body and file still match byte-for-byte after creation.

Body must contain only these headings in this exact order:

```markdown
## Changed

- None

## Added

- Added an example ([#123](https://github.com/MathisVerstrepen/MesSeances/pull/123))

## Improved

- None

## Fixed

- Fixed an example
```

Every section needs at least one `- ` Markdown bullet. Use `- None` as sole bullet when a section has no entries. Do not link `- None`. Do not add an introduction, extra headings, continuation paragraphs, or combine `- None` with another bullet. Pull request body and changelog must be exact string and byte matches, including LF line endings and one final newline.

Validation derives only `docs/changelogs/<strict-title-version>.md` after title and body grammar pass. It reads GitHub Contents API and recursive Git tree at exact pull request head SHA, not runner working tree or branch name, and rejects missing files, symlinks, submodules, wrong path/ref/type/mode metadata, malformed or truncated responses, invalid base64 or UTF-8, wrong newline form, and any body mismatch.

## Publication behavior

Merging a valid same-repository `dev` to `main` pull request runs one serialized release workflow. Before selecting any tooling, including present base tooling, validation checks the supported event/action, open/unmerged state, same-repository non-fork dev/main identity, both SHAs, and exact base checkout. Finalization and promotion each check the closed/merged event identity and all three SHAs, then require fresh GitHub PR number/base/head/merge equality and current protected `main == MERGE_SHA`. API errors, malformed metadata, and mismatches stop before Python.

After these guards, ordinary tooling at the exact pre-merge base is preferred. Only an absent base tooling path permits fallback: exact non-fork head for read-only validation, exact merge commit for finalization/promotion. Existing nonregular or symlink paths fail, never fall back. Selected checkout SHA and ordinary tooling file are verified before execution. Unsafe-but-present base tooling must be rejected by operator preflight, not opportunistically replaced by dev or merge tooling.

Python publication independently requires event-bound `--base-sha`, `--head-sha`, and `--merge-sha`; it has no configurable branch overrides or unbound compatibility mode. It revalidates exact PR number, same-repository non-fork dev/main identity, literal closed/merged state, all expected SHAs, and fresh current protected main. Strict title/body, numeric tag progression, merge commit, and exact merge changelog checks still precede writes. Immediately before the first write and before a subsequent Release create, it re-reads bound PR identity/metadata and protected main and stops on observed changes. These reads are not an atomic lock; concurrent changes can still occur after a read.

Both immutable records are preflighted before any write. Existing tag must be the exact lightweight commit ref at merge SHA; annotated tags are rejected even when they dereference to that commit. Existing Release must have a positive non-boolean integer ID, exact tag/name/body, literal `draft=false` and `prerelease=false`, and the same ID from `/releases/latest`. Any mismatch stops without PATCH or DELETE. A Release without its tag is inconsistent. Exact existing tag and Release succeed using GET only; an exact tag with no Release permits only Release creation. With both absent, automation:

1. Creates lightweight Git tag `X.Y.Z` at exact merge commit without ever moving or deleting a tag.
2. Creates a stable GitHub Release with the same name, exact validated pull request body, `draft=false`, `prerelease=false`, and latest-release status.
3. Builds and pushes `ghcr.io/<owner>/<repo>-api:<version>` and `ghcr.io/<owner>/<repo>-web:<version>` for `linux/amd64`.
4. Resolves both versioned manifests, then repoints both `latest` aliases from those manifests without rebuilding.

Workflow uses only repository `GITHUB_TOKEN`. Tag, Release, and image work stays in closed-pull-request workflow because events created by `GITHUB_TOKEN` do not reliably start downstream workflows. No step deploys or restarts production. Operator selects `IMAGE_TAG=<version>` and performs existing production deployment manually.

Successful creates require fresh canonical read-back: tag GET, then Release GET and latest GET. An explicit 422 create collision permits one bounded read-back only, accepting exact canonical state without a second POST or reconciliation. Other errors and uncertain writes stop. Exact-object validation is not permission to rerun workflows or recover through agent mutations.

## Repair integration and blocked bootstrap

A reviewed release-automation repair normally reaches `dev` through a conventional feature PR. That alone does not unblock publication. When protected main contains unsafe present tooling, the next release would select that old base; missing-tooling fallback cannot bootstrap this repair. A direct maintenance PR to main is not a compliant shortcut under the current release-only validator. Keep main integration and publication blocked until an operator-approved, independently justified route preserves protection and all retained release prerequisites. Do not disable publication, weaken validation or the release skill, bypass protection, or publish with unsafe tooling to seed the repair.

Before any later publication authorization is exercised, freshly inspect exact protected-main workflows/tooling, active workflow IDs, refs, protection, and checks using the complete publish skill preflight. Repair integration success is not release readiness.

## Focused automation tests

From repository root with Python 3.13, Bash, and jq available:

```sh
PYTHONDONTWRITEBYTECODE=1 python -m unittest discover -s scripts/tests
```

The stdlib suite uses queued HTTP fixtures and isolated executed workflow-shell fixtures; it never publishes or contacts GitHub. `.github/workflows/release-tests.yml` runs it on every push and pull request to dev/main with read-only contents permission, no persisted checkout credentials, and no secrets or path filters. `make check` remains the application gate and does not run this suite.

## Failure recovery

Validation failure makes no tag or Release changes. Do not merge. Inspect strict title/version, exact head changelog/body equality, metadata, and checks read-only; report the mismatch for operator review. Do not automatically edit metadata, retry validation, select another version, or prepare another release to recover a failed attempt.

For publication failures, identify the exact PR, base/head/merge SHAs, version, correlated workflow run, and failed jobs. Read-only diagnostics include:

```sh
gh run list --workflow release.yml --limit 10
gh run view RUN_ID --json jobs --jq '.jobs[] | [.databaseId, .name, .conclusion] | @tsv'
```

- No writes: report the failed guard, metadata/changelog mismatch, immutable-record conflict, API error, or main/PR drift. Do not infer that a transient failure permits a retry.
- Tag-only or uncertain tag create: inspect the exact ref and target read-only, report confirmed versus uncertain effects, and leave it untouched. A later failure does not roll back the tag.
- Release-created or uncertain Release create: inspect Release and latest objects read-only. Report mismatched fields or unsuccessful canonical read-back without editing, deleting, or reconciling either record.
- Partial versioned images: inspect both version manifests and build-job conclusions. Report which images are confirmed, absent, or unknown; do not rebuild or rerun.
- Partial latest aliases: inspect both alias and version digests. Report each result separately; do not repoint either alias. Promotion is not atomic across images.
- Advanced or unprotected main, changed PR metadata, or a newer stable tag: report the observed race and any earlier writes. Stop; do not manually execute selected tooling or recover through another release.

Escalate failures, uncertainty, and partial publication to the operator. Never automatically rerun or dispatch a workflow, retry an uncertain mutation, edit merged metadata, create a replacement release, or promise rollback. Never delete or force-update a release tag to make a failed run pass. A later invocation repeats full preflight and may only monitor a uniquely correlated merged publication read-only under the release skill's safeguards.
