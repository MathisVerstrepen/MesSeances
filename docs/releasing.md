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

Create the selectable release template with GitHub CLI from repository root:

```sh
version=0.7.0
gh pr create \
  --base main \
  --head dev \
  --title "$version - $(date -u +%F)" \
  --template .github/PULL_REQUEST_TEMPLATE/release.md
```

Browser flow uses [the release comparison page](https://github.com/MathisVerstrepen/MesSeances/compare/main...dev?expand=1&template=release.md). Select `release.md` if GitHub presents a template chooser, set base to `main` and compare to `dev`, then set title to `X.Y.Z - YYYY-MM-DD` using current UTC date.

Body must contain only these headings in this exact order:

```markdown
## Changed

- None

## Added

- Added an example

## Improved

- None

## Fixed

- Fixed an example
```

Every section needs at least one `- ` Markdown bullet. Use `- None` as sole bullet when a section has no entries. Do not add an introduction, extra headings, continuation paragraphs, or combine `- None` with another bullet.

Open-PR validation requires title date to equal current UTC date whenever validation runs. Before merging an older open release PR, update title date to current UTC date; editing title starts validation again. Publisher compares title date with GitHub's immutable UTC `merged_at` date, not rerun date. This keeps initial merge strict while allowing deterministic recovery reruns on later days.

## Publication behavior

Merging a valid same-repository `dev` to `main` pull request runs one serialized release workflow. When release tooling exists on the pull request's pre-merge `main` base, validation, finalization, and promotion execute that exact base version. For the one-time bootstrap where the exact base lacks the tooling path, read-only validation may execute the exact non-fork `dev` head only after event repository, branch, state, and SHA checks pass. Privileged finalization and promotion never execute unmerged head tooling: each may execute the exact merge commit only after GitHub API data confirms a merged same-repository `dev` to `main` pull request with matching base, head, and merge SHAs and confirms that merge commit is current protected `main`. Any mismatch stops before Python. No separate manual tooling seed on `main` is required. Release automation then re-fetches pull request and commit data through GitHub API, revalidates repository, branches, merge state, merge SHA, title, body, and all tags, then:

1. Creates lightweight Git tag `X.Y.Z` at exact merge commit without ever moving or deleting a tag.
2. Creates or reconciles stable GitHub Release with same name, exact validated pull request body, `prerelease=false`, and latest-release status.
3. Builds and pushes `ghcr.io/<owner>/<repo>-api:<version>` and `ghcr.io/<owner>/<repo>-web:<version>` for `linux/amd64`.
4. Resolves both versioned manifests, then repoints both `latest` aliases from those manifests without rebuilding.

Workflow uses only repository `GITHUB_TOKEN`. Tag, Release, and image work stays in closed-pull-request workflow because events created by `GITHUB_TOKEN` do not reliably start downstream workflows. No step deploys or restarts production. Operator selects `IMAGE_TAG=<version>` and performs existing production deployment manually.

## Failure recovery

Validation failure makes no changes. Correct title/body or release source branches, update from tags if version is stale, and wait for `Release PR / Validate release metadata` to pass before merge.

Publication is idempotent for same version and merge commit. Find run and failed jobs:

```sh
gh run list --workflow release.yml --limit 10
gh run view RUN_ID --json jobs --jq '.jobs[] | [.databaseId, .name, .conclusion] | @tsv'
```

- If tag exists at merge commit but Release creation or image build failed, rerun failed jobs: `gh run rerun RUN_ID --failed`.
- If both versioned images exist and only `Promote latest aliases` failed, rerun that job without rebuilding: `gh run rerun RUN_ID --job JOB_ID`. Promotion rechecks newest tag and resolves both version manifests before touching either alias.
- If Release exists with wrong name, body, draft state, stable state, or latest status, rerun reconciles it to validated pull request and marks it latest.
- If tag resolves to another commit, stop. Automation intentionally never moves or deletes tags. Investigate repository history, leave collided tag untouched, and use a numerically newer release through a new `dev` to `main` pull request after `dev` has another commit.
- If a newer strict stable tag now exists, stale run cannot update image `latest` aliases. Recover newest release from its own workflow run.
- If one `latest` alias changed before second alias command failed, rerun promotion job. Both version manifests are revalidated before aliases are repointed again.
- During first-release bootstrap, a failed finalization or promotion rerun can use merge-commit tooling only while that exact merge commit remains current protected `main`. If `main` advanced or protection is missing, automation stops before Python; do not seed, copy, or run tooling manually with workflow credentials. Inspect repository state and recover through a newly reviewed release path.

Never delete or force-update a release tag to make a failed run pass.
