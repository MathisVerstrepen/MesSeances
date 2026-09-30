#!/usr/bin/env bash
set -euo pipefail

catalogue_source() {
  source_container="$(value sourcePostgresContainer ORCA_DOCKER_SOURCE_POSTGRES_CONTAINER)"
  if [ -z "$source_container" ]; then
    local project
    project="$(value sourceComposeProject ORCA_DOCKER_SOURCE_COMPOSE_PROJECT)"
    project="${project:-movieflow}"
    source_container="$(docker ps --filter "label=com.docker.compose.project=$project" \
      --filter label=com.docker.compose.service=postgres --format '{{.ID}}')"
  fi
  [ -n "$source_container" ] && [[ "$source_container" != *$'\n'* ]] || {
    printf 'Expected one running source PostgreSQL container; start local Compose postgres or set ORCA_DOCKER_SOURCE_POSTGRES_CONTAINER.\n' >&2
    return 1
  }
  [ "$(docker inspect --format '{{.State.Running}}' "$source_container")" = true ] || {
    printf 'Source PostgreSQL container is not running.\n' >&2; return 1;
  }
}

# Subshell owns only temporary archives. The caller owns workspace cleanup.
copy_catalogue() (
  set -euo pipefail
  local target="$1" archives table count
  owned "$target"
  [ "$(docker inspect --format '{{index .Config.Labels "fr.messeances.orca.instance"}}' "$target")" = "$instance" ] || return 1
  [ "$(docker exec "$target" psql -X -U movieflow -d movieflow -v ON_ERROR_STOP=1 -Atc \
    "SELECT count(*) FROM pg_tables WHERE schemaname='public'")" = 0 ] || {
    printf 'Refusing to seed a non-empty database.\n' >&2; return 1;
  }
  archives="$(mktemp -d "$local_dir/catalogue.XXXXXXXX")"
  trap 'rm -rf -- "$archives"' EXIT
  local tables=()
  while IFS= read -r table; do
    [[ "$table" =~ ^[a-z][a-z0-9_]+$ ]] || return 1
    tables+=("--table=public.$table")
  done < "$script_dir/catalogue-tables.txt"

  printf 'Copying local catalogue into workspace PostgreSQL...\n' >&2
  # Native source tools avoid host/client PostgreSQL version mismatch. Schema
  # contains definitions only; data has an explicit allowlist, including sequences.
  docker exec "$source_container" sh -ceu \
    'exec pg_dump -w -U "$POSTGRES_USER" -d "$POSTGRES_DB" "$@"' sh \
    --format=custom --schema=public --schema-only --no-owner --no-acl \
    --no-publications --no-subscriptions > "$archives/schema.dump"
  docker exec "$source_container" sh -ceu \
    'exec pg_dump -w -U "$POSTGRES_USER" -d "$POSTGRES_DB" "$@"' sh \
    --format=custom --section=data --strict-names --no-large-objects \
    "${tables[@]}" > "$archives/data.dump"
  docker exec -i "$target" pg_restore -U movieflow -d movieflow \
    --exit-on-error --single-transaction --no-owner --no-acl --clean --if-exists \
    --section=pre-data < "$archives/schema.dump" >&2
  docker exec -i "$target" pg_restore -U movieflow -d movieflow \
    --exit-on-error --single-transaction --no-owner --no-acl < "$archives/data.dump" >&2
  docker exec -i "$target" pg_restore -U movieflow -d movieflow \
    --exit-on-error --single-transaction --no-owner --no-acl --section=post-data < "$archives/schema.dump" >&2

  count="$(docker exec "$target" psql -X -U movieflow -d movieflow -v ON_ERROR_STOP=1 -Atc \
    "SELECT count(*) FROM showtimes s JOIN schedule_snapshot p ON p.version=s.generation_id
     WHERE s.start_time > now() AND EXISTS (SELECT 1 FROM public_movies)
       AND EXISTS (SELECT 1 FROM movie_enrichment_state WHERE singleton)
       AND EXISTS (SELECT 1 FROM theater_location_state WHERE singleton)")"
  [ "$count" -gt 0 ] || {
    printf 'Source catalogue has no usable published future showtimes; refresh local catalogue first.\n' >&2; return 1;
  }
  docker exec "$target" psql -X -U movieflow -d movieflow -v ON_ERROR_STOP=1 -c ANALYZE >&2
  printf 'Catalogue restored: %s future showtimes in active publication.\n' "$count" >&2
)

validate_catalogue() {
  # Reuse actual startup: embedded migrations, snapshot invariants and public API.
  # No sync schedules or credentials were copied; only the disposable DB changes.
  docker exec -i --user mathis "$resource_id" bash -s -- "$(value projectRoot ORCA_DOCKER_PROJECT_ROOT)" <<'SH' >&2
set -euo pipefail
cd "$1/api"
log="$(mktemp)"
pid=""
cleanup() {
  local status=$?
  [ -z "$pid" ] || kill -- "-$pid" 2>/dev/null || true
  [ -z "$pid" ] || wait "$pid" 2>/dev/null || true
  [ "$status" = 0 ] || cat "$log" >&2
  rm -f "$log"
}
trap cleanup EXIT
setsid env PORT=8080 go run -tags=nodynamic ./cmd/api > "$log" 2>&1 &
pid=$!
for attempt in {1..120}; do
  kill -0 "$pid" 2>/dev/null || { printf 'Catalogue API validation exited.\n' >&2; exit 1; }
  if curl --fail --silent --max-time 5 http://localhost:8080/healthz >/dev/null; then
    curl --fail --silent --max-time 10 http://localhost:8080/api/v1/movies >/dev/null
    if ! curl --fail --silent --max-time 5 http://localhost:8080/readyz >/dev/null; then
      printf 'Warning: copied catalogue serves data but /readyz is not ready; source publication may exceed the 24-hour freshness limit. Refresh the local source explicitly.\n' >&2
    fi
    printf 'Catalogue migrations and public API validated.\n' >&2
    exit 0
  fi
  sleep 1
done
printf 'Catalogue API validation timed out.\n' >&2
exit 1
SH
}
