#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "$0")/common.sh"
image="$(value baseImage ORCA_DOCKER_BASE_IMAGE)"
repo_url="$(value repoUrl ORCA_DOCKER_REPO_URL)"
repo_ref="$(value repoRef ORCA_DOCKER_REPO_REF)"
config_source="${ORCA_OPENCODE_CONFIG_SOURCE:-$HOME/.config/opencode}"
context="$(mktemp -d "$local_dir/build.XXXXXX")"
trap 'rm -rf -- "$context"' EXIT
mkdir -p "$context/bin" "$context/opencode"
cp "$script_dir/Dockerfile" "$context/Dockerfile"
cp "$script_dir/entrypoint.sh" "$context/bin/orca-docker-ssh-entrypoint"
cp "$script_dir/dev.sh" "$context/bin/orca-dev"
for tool in opencode bun rtk codebase-memory-mcp; do
  cp "$(command -v "$tool")" "$context/bin/$tool"
done
# Copy active configuration byte-for-byte, excluding credentials and runtime history.
files=(opencode.jsonc MASTER.md agents commands dcp.jsonc internal package.json package-lock.json bun.lock plugins skills templates themes tools tui.json)
tar -C "$config_source" --exclude=node_modules --exclude=.git --exclude=.agents --exclude=.env --exclude=tmp -cf - "${files[@]}" | tar -C "$context/opencode" -xf -
chmod +x "$context/bin/"*
export GH_TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-$(gh auth token)}}"
docker build --secret id=github_token,env=GH_TOKEN --build-arg "REPO_URL=$repo_url" \
  --build-arg "REPO_REF=$repo_ref" --tag "$image" "$context" >&2
unset GH_TOKEN
merge_state "$(node -e 'console.log(JSON.stringify({baseImage:process.argv[1],snapshotId:"",authSourceSnapshotId:""}))' "$image")"
