#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "$0")/common.sh"
payload_values
owned "$instance-postgres"
docker start "$instance-postgres" "$resource_id" >&2
result
