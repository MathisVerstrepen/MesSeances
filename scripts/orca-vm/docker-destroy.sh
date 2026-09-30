#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "$0")/common.sh"
payload_values
owned "$instance-postgres"
port="$(docker port "$resource_id" 22/tcp)"; port="${port##*:}"
host_key="$(docker exec "$resource_id" cat /etc/ssh/ssh_host_ed25519_key.pub 2>/dev/null || true)"
if [ -z "$host_key" ]; then
  host_key="$(node -e 'process.stdout.write(JSON.parse(process.argv[1]).recipeResult.userData.hostKey ?? "")' "$payload")"
  port="$(node -e 'process.stdout.write(String(JSON.parse(process.argv[1]).recipeResult.userData.sshPort ?? ""))' "$payload")"
fi
docker rm -f "$resource_id" >&2
docker rm -fv "$instance-postgres" >&2
docker network rm "$instance" >&2
if [ -n "$host_key" ]; then node "$script_dir/known-hosts.mjs" remove "$port" "$host_key"; fi
printf '{}\n'
