#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "$0")/common.sh"
base_image="$(value baseImage ORCA_DOCKER_BASE_IMAGE)"
auth_image="$(value authImage ORCA_DOCKER_AUTH_IMAGE)"
name=messeances-orca-auth
case "${1:-}" in
  start)
    docker run -d --name "$name" --label fr.messeances.orca=local-docker \
      --cpus "$(value cpus ORCA_DOCKER_CPUS)" --memory "$(value memory ORCA_DOCKER_MEMORY)" \
      --entrypoint sleep "$base_image" infinity >&2
    config_source="${ORCA_OPENCODE_CONFIG_SOURCE:-$HOME/.config/opencode}"
    if [ -f "$config_source/.env" ]; then
      # Config credentials belong only in the explicitly authenticated image.
      docker cp "$config_source/.env" "$name:/home/mathis/.config/opencode/.env" >&2
      docker exec "$name" chown mathis:mathis /home/mathis/.config/opencode/.env
      docker exec "$name" chmod 600 /home/mathis/.config/opencode/.env
    fi
    printf 'Run in your terminal: docker exec -it --user mathis %s opencode auth login\n' "$name" >&2
    printf '{"authContainer":"%s"}\n' "$name"
    ;;
  finish)
    owned "$name"
    # Auth list has no authenticated exit status. Validate exact credential records without printing them.
    docker exec --user mathis "$name" node -e '
      const fs=require("fs"); const d=JSON.parse(fs.readFileSync("/home/mathis/.local/share/opencode/auth.json"));
      for (const id of ["openai","commandcode"]) {
        const c=d[id]; if (!c || !(c.type==="api" && c.key || c.type==="oauth" && c.access && c.refresh && c.expires>Date.now())) throw new Error(`Missing valid ${id} authentication`);
      }' >&2
    docker exec "$name" bash -c 'rm -f /etc/ssh/ssh_host_*; test ! -d /home/mathis/.config/orca' >&2
    docker stop "$name" >&2
    docker commit --change='ENTRYPOINT ["/usr/local/bin/orca-docker-ssh-entrypoint"]' \
      "$name" "$auth_image" >&2
    snapshot_id="$(docker image inspect --format '{{.Id}}' "$auth_image")"
    base_id="$(docker image inspect --format '{{.Id}}' "$base_image")"
    merge_state "$(node -e 'console.log(JSON.stringify({snapshotId:process.argv[1],authSourceSnapshotId:process.argv[2]}))' "$snapshot_id" "$base_id")"
    docker rm "$name" >&2
    ;;
  cancel) owned "$name"; docker rm -f "$name" >&2; printf '{}\n' ;;
  *) printf 'Usage: %s start|finish|cancel\n' "$0" >&2; exit 1 ;;
esac
