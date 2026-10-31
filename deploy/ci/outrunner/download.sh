#!/bin/bash
# Public upstream assets only. Run through admin.sh for root-owned upgrades.
set -euo pipefail
umask 077
base=/home/hogwarts/config/gha-outrunner
mkdir -p "$base/bin" "$base/downloads"
cd "$base/downloads"
archive=outrunner_1.2.0_linux_amd64.tar.gz
release=https://github.com/NetwindHQ/gha-outrunner/releases/download/v1.2.0
curl -fsSL --retry 3 "$release/$archive" -o "$archive.part"
curl -fsSL --retry 3 "$release/checksums.txt" -o checksums.txt
mv "$archive.part" "$archive"
echo "68a9112d00a80cc00d86fde979b737e99c853db4866f94123d94a9cea26fa224  $archive" | sha256sum -c -
sha256sum --ignore-missing -c checksums.txt
tar -xzf "$archive" -C "$base/bin" outrunner
chmod 0750 "$base/bin/outrunner"
curl -fsSL --retry 3 https://cloud-images.ubuntu.com/noble/current/SHA256SUMS -o ubuntu-SHA256SUMS.part
curl -fsSL --retry 3 https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img -o ubuntu-noble.qcow2.part
mv ubuntu-SHA256SUMS.part ubuntu-SHA256SUMS
mv ubuntu-noble.qcow2.part ubuntu-noble.qcow2
# The builder compares the image's SHA256 against this upstream manifest.
"$base/bin/outrunner" --version
