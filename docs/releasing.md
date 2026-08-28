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
git worktree add ../movieflow-my-feature -b feature/my-feature origin/dev
cd ../movieflow-my-feature
```

Implement and verify the feature, then push it and open a pull request into `dev`:

```sh
git push -u origin feature/my-feature
gh pr create --base dev --head feature/my-feature --title "feat: describe feature" --body "Describe the change and its validation."
```

After merge, remove the worktree and local feature branch from the original checkout:

```sh
git worktree remove ../movieflow-my-feature
git branch -d feature/my-feature
```

`dev` is intentionally unprotected. Feature pull requests into `dev` are a required team convention rather than a GitHub rule.

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

Create `docs/changelogs/X.Y.Z.md` using title version exactly, with no `v`, extra title, introduction, or other prose. File must be an ordinary tracked repository file encoded as strict UTF-8, use LF line endings, end with exactly one newline, and contain only final release body. Create, validate, commit, and push this file to synchronized `dev` before creating release pull request. Then pass same file directly as pull request body:

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
  --title "$version - $(date -u +%F)" \
  --body-file "docs/changelogs/$version.md"
```

Before commit, require staged changes contain exactly expected changelog path. Before push, require commit contains exactly that path and parent is previously synchronized `dev`. Recheck remote `dev` immediately before push and stop on a race instead of forcing. Never amend or force-push release preparation. If exact changelog is already tracked at synchronized `dev`, reuse it without creating another commit.

Browser flow uses [the release comparison page](https://github.com/MathisVerstrepen/MesSeances/compare/main...dev?expand=1&template=release.md). Select `release.md` if GitHub presents a template chooser, set base to `main` and compare to `dev`, set title to `X.Y.Z - YYYY-MM-DD` using current UTC date, remove all template comments, and paste exact changelog content as body. Verify body and file still match byte-for-byte after creation.

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

Open-PR validation requires title date to equal current UTC date whenever validation runs. Before merging an older open release PR, update title date to current UTC date; editing title starts validation again. Publisher compares title date with GitHub's immutable UTC `merged_at` date, not rerun date. This keeps initial merge strict while allowing deterministic recovery reruns on later days.

Validation derives only `docs/changelogs/<strict-title-version>.md` after title and body grammar pass. It reads GitHub Contents API and recursive Git tree at exact pull request head SHA, not runner working tree or branch name, and rejects missing files, symlinks, submodules, wrong path/ref/type/mode metadata, malformed or truncated responses, invalid base64 or UTF-8, wrong newline form, and any body mismatch.

## Publication behavior

Merging a valid same-repository `dev` to `main` pull request runs one serialized release workflow. When release tooling exists on the pull request's pre-merge `main` base, validation, finalization, and promotion execute that exact base version. For the one-time bootstrap where the exact base lacks the tooling path, read-only validation may execute the exact non-fork `dev` head only after event repository, branch, state, and SHA checks pass. Privileged finalization and promotion never execute unmerged head tooling: each may execute the exact merge commit only after GitHub API data confirms a merged same-repository `dev` to `main` pull request with matching base, head, and merge SHAs and confirms that merge commit is current protected `main`. Any mismatch stops before Python. No separate manual tooling seed on `main` is required. Release automation then re-fetches pull request and commit data through GitHub API; revalidates repository, branches, merge state, merge SHA, title, body, and all tags; and re-reads exact changelog from exact merge commit before any tag or Release write. This merge-commit recheck prevents a head-only or stale file from becoming release record. Automation then:

1. Creates lightweight Git tag `X.Y.Z` at exact merge commit without ever moving or deleting a tag.
2. Creates or reconciles stable GitHub Release with same name, exact validated pull request body, `prerelease=false`, and latest-release status.
3. Builds and pushes `ghcr.io/<owner>/<repo>-api:<version>` and `ghcr.io/<owner>/<repo>-web:<version>` for `linux/amd64`.
4. Resolves both versioned manifests, then repoints both `latest` aliases from those manifests without rebuilding.

Workflow uses only repository `GITHUB_TOKEN`. Tag, Release, and image work stays in closed-pull-request workflow because events created by `GITHUB_TOKEN` do not reliably start downstream workflows. No step deploys or restarts production. Operator selects `IMAGE_TAG=<version>` and performs existing production deployment manually.

## Failure recovery

Validation failure makes no tag or Release changes. For missing or mismatched changelog, do not merge: verify strict title version, inspect tracked file at reported head SHA, and restore exact body/file equality with LF and one final newline through reviewed `dev` history. If changelog is correct and only open pull request body differs, replace body with exact file content. For malformed GitHub metadata or an inaccessible/truncated contents response, stop and retry validation only after provider state is unambiguous. For stale version, choose numerically newer version and corresponding filename. Wait for `Release PR / Validate release metadata` to pass before merge.

Publication is idempotent for same version and merge commit. Find run and failed jobs:

```sh
gh run list --workflow release.yml --limit 10
gh run view RUN_ID --json jobs --jq '.jobs[] | [.databaseId, .name, .conclusion] | @tsv'
```

- If tag exists at merge commit but Release creation or image build failed, rerun failed jobs: `gh run rerun RUN_ID --failed`.
- If both versioned images exist and only `Promote latest aliases` failed, rerun that job without rebuilding: `gh run rerun RUN_ID --job JOB_ID`. Promotion rechecks newest tag and resolves both version manifests before touching either alias.
- If Release exists with wrong name, body, draft state, stable state, or latest status, rerun reconciles it to validated pull request and marks it latest.
- If publication fails changelog recheck before tag creation, compare pull request body with `docs/changelogs/X.Y.Z.md` at immutable merge commit. A transient API failure may be retried after GitHub state is healthy. If merge-commit file is correct but merged pull request body was changed, restore exact body before operator-authorized rerun. If immutable merge content is wrong, do not rewrite history or create tag; correct `dev` through normal review and use a new numerically greater release pull request.
- If tag resolves to another commit, stop. Automation intentionally never moves or deletes tags. Investigate repository history, leave collided tag untouched, and use a numerically newer release through a new `dev` to `main` pull request after `dev` has another commit.
- If a newer strict stable tag now exists, stale run cannot update image `latest` aliases. Recover newest release from its own workflow run.
- If one `latest` alias changed before second alias command failed, rerun promotion job. Both version manifests are revalidated before aliases are repointed again.
- During first-release bootstrap, a failed finalization or promotion rerun can use merge-commit tooling only while that exact merge commit remains current protected `main`. If `main` advanced or protection is missing, automation stops before Python; do not seed, copy, or run tooling manually with workflow credentials. Inspect repository state and recover through a newly reviewed release path.

Never delete or force-update a release tag to make a failed run pass.
