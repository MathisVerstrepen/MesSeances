#!/usr/bin/env bash
# CI-only prebuilt installer. Local source builds remain managed by validate.py.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  printf 'Usage: %s CACHE_DIR BIN_DIR\n' "$0" >&2
  exit 2
fi

version=2.13.1
# From the upstream golangci-lint-2.13.1-checksums.txt release asset.
case "$(uname -s):$(uname -m)" in
  Linux:x86_64)
    arch=amd64
    asset_id=522396872
    checksum=b17bfbc9d4aaa48be7f4f1ce3240bc3d8200c870c072bacf15c26219e2cfb9cc
    ;;
  Linux:aarch64)
    arch=arm64
    asset_id=522396912
    checksum=908317c23db18448f924e853b3d8a659fd919614cd438f224810a4053daa2607
    ;;
  *) printf 'Unsupported CI linter platform\n' >&2; exit 1 ;;
esac

mkdir -p -- "$1" "$2"
cache_dir="$(cd -- "$1" && pwd)"
bin_dir="$(cd -- "$2" && pwd)"
name="golangci-lint-$version-linux-$arch"
archive="$cache_dir/$name.tar.gz"
download=""
binary=""
cleanup() {
  [ -z "$download" ] || rm -f -- "$download"
  [ -z "$binary" ] || rm -f -- "$binary"
}
trap cleanup EXIT

verify_archive() {
  printf '%s  %s\n' "$checksum" "$1" | sha256sum --check --status
}

if [ -f "$archive" ]; then
  # An invalid cache is an integrity failure, not a reason to retry execution.
  verify_archive "$archive" || { printf 'Cached linter checksum mismatch\n' >&2; exit 1; }
else
  download="$(mktemp "$cache_dir/.download.XXXXXX")"
  # curl retries timeouts, HTTP 429 and transient 5xx with exponential backoff.
  # At most five attempts, each bounded to 45s; no retries of lint itself.
  if http_status="$(curl --disable --fail --location --silent --show-error \
    --proto '=https' --proto-redir '=https' \
    --connect-timeout 10 --max-time 45 \
    --retry 4 --retry-max-time 180 --retry-connrefused \
    --output "$download" --write-out '%{http_code}' \
    "https://github.com/golangci/golangci-lint/releases/download/v$version/$name.tar.gz")"; then
    :
  else
    download_status=$?
    case "$download_status:$http_status" in
      22:408|22:429|22:500|22:502|22:503|22:504|5:*|6:*|7:*|18:*|28:*|52:*|55:*|56:*)
        # The asset API offers a separate route when release URLs are down.
        # IDs are pinned with the version/checksum; gh reads auth from its
        # environment, never argv. Both routes must pass the same digest gate.
        printf 'Release download unavailable; trying pinned GitHub API asset\n' >&2
        timeout 90s gh api --hostname github.com \
          "repos/golangci/golangci-lint/releases/assets/$asset_id" \
          --header 'Accept: application/octet-stream' > "$download"
        ;;
      *) exit "$download_status" ;;
    esac
  fi
  verify_archive "$download" || { printf 'Downloaded linter checksum mismatch\n' >&2; exit 1; }
  mv -- "$download" "$archive"
fi

# Extract only the pinned archive member, never arbitrary archive paths.
binary="$(mktemp "$bin_dir/.golangci-lint.XXXXXX")"
tar -xzf "$archive" -O "$name/golangci-lint" > "$binary"
chmod 755 "$binary"
actual_version="$("$binary" version)"
if [[ "$actual_version" != "golangci-lint has version $version "* ]]; then
  printf 'Installed linter version mismatch\n' >&2
  exit 1
fi
mv -- "$binary" "$bin_dir/golangci-lint"
printf 'Verified golangci-lint %s\n' "$version"
