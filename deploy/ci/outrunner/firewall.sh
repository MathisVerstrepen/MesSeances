#!/bin/bash
set -euo pipefail
rules=/home/hogwarts/config/gha-outrunner/config/firewall.nft
transaction=$(mktemp)
trap 'rm -f "$transaction"' EXIT
for family in inet bridge; do
  if nft list table "$family" messeances_ci >/dev/null 2>&1; then
    printf 'delete table %s messeances_ci\n' "$family" >> "$transaction"
  fi
done
cat "$rules" >> "$transaction"
nft --check --file "$transaction"
nft --file "$transaction"
