#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "$0")/common.sh"
payload_values
owned "$instance-postgres"
docker stop "$resource_id" "$instance-postgres" >&2
printf '{}\n'
