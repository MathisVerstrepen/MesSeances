#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "$0")/common.sh"
source "$script_dir/catalogue-copy.sh"
image="$(value snapshotId ORCA_DOCKER_SNAPSHOT_ID)"
[ -n "$image" ] || { printf 'Build base snapshot, then run docker-base-auth.sh start and finish.\n' >&2; exit 1; }
catalogue_source
instance="$(node -e 'const crypto=require("crypto");const s=[process.env.ORCA_RECIPE_ID??"local-docker",process.env.ORCA_VM_INSTANCE_ID??crypto.randomUUID()].join("-").toLowerCase().replace(/[^a-z0-9-]/g,"-");console.log("messeances-orca-"+s.slice(0,90))')"
resource_id=""; created_network=; created_database=; created_container=
cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    if [ -n "$created_container" ]; then
      docker logs "$instance" >&2 || true
      docker rm -f "$instance" >&2 || true
    fi
    [ -z "$created_database" ] || docker rm -fv "$instance-postgres" >&2 || true
    [ -z "$created_network" ] || docker network rm "$instance" >&2 || true
    if [ -n "${ssh_port:-}" ] && [ -n "${host_key:-}" ]; then
      node "$script_dir/known-hosts.mjs" remove "$ssh_port" "$host_key" || true
    fi
  fi
}
trap cleanup EXIT
docker image inspect "$image" --format '{{json .Config.Entrypoint}}' >&2
ensure_key
network_prefix="$(value networkPrefix ORCA_DOCKER_NETWORK_PREFIX)"
for attempt in {1..8}; do
  subnet="$(node -e 'const p=process.argv[1]||"10.240";if(!/^\d{1,3}\.\d{1,3}$/.test(p)||p.split(".").some(n=>Number(n)>255))throw new Error("Invalid networkPrefix");const n=require("crypto").randomInt(4096);console.log(`${p}.${n>>4}.${(n&15)*16}/28`)' "$network_prefix")"
  if docker network create --subnet "$subnet" --label fr.messeances.orca=local-docker "$instance" >&2; then
    created_network=1; break
  fi
done
[ -n "$created_network" ] || { printf 'Could not allocate an isolated Docker subnet\n' >&2; exit 1; }
docker run -d --name "$instance-postgres" --network "$instance" --network-alias postgres \
  --label fr.messeances.orca=local-docker --label "fr.messeances.orca.instance=$instance" \
  --cpus "$(value postgresCpus ORCA_DOCKER_POSTGRES_CPUS)" --memory "$(value postgresMemory ORCA_DOCKER_POSTGRES_MEMORY)" \
  -e POSTGRES_DB=movieflow -e POSTGRES_USER=movieflow -e POSTGRES_PASSWORD=movieflow \
  --health-cmd 'pg_isready -U movieflow -d movieflow' --health-interval 2s --health-retries 30 \
  "$(value postgresImage ORCA_DOCKER_POSTGRES_IMAGE)" >&2
created_database=1
resource_id="$(docker run -d --name "$instance" --network "$instance" \
  --label fr.messeances.orca=local-docker --label "fr.messeances.orca.instance=$instance" \
  --cpus "$(value cpus ORCA_DOCKER_CPUS)" --memory "$(value memory ORCA_DOCKER_MEMORY)" \
  -p 127.0.0.1::22 -p 127.0.0.1::3000 -p 127.0.0.1::8080 \
  -e "ORCA_SSH_PUBLIC_KEY=$public_key" "$image")"
created_container=1
for attempt in {1..60}; do
  health="$(docker inspect --format '{{.State.Health.Status}}' "$instance-postgres")"
  [ "$health" != healthy ] || break
  [ "$health" != unhealthy ] || { docker logs "$instance-postgres" >&2; exit 1; }
  sleep 1
done
[ "$health" = healthy ] || { printf 'PostgreSQL startup timed out\n' >&2; exit 1; }
# Authenticated Git credentials never enter image, argv, or recipe JSON.
export GH_TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-$(gh auth token)}}"
printf '%s\n' "$GH_TOKEN" | docker exec -i --user mathis "$resource_id" bash -c '
  set -euo pipefail; read -r GH_TOKEN; export GH_TOKEN GIT_TERMINAL_PROMPT=0;
  askpass=$(mktemp); trap '\''rm -f "$askpass"'\'' EXIT;
  printf "%s\n" "#!/bin/sh" '\''case "$1" in *Username*) printf "%s\n" x-access-token;; *) printf "%s\n" "$GH_TOKEN";; esac'\'' > "$askpass";
  chmod 700 "$askpass"; export GIT_ASKPASS="$askpass";
  git -C /home/mathis/projects/messeances fetch origin' >&2
unset GH_TOKEN
copy_catalogue "$instance-postgres"
validate_catalogue
result
