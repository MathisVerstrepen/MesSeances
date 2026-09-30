#!/usr/bin/env bash
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
cd "$root"
if [ ! -f deploy/.env ]; then
  cp deploy/.env.example deploy/.env
  chmod 600 deploy/.env
fi
export DATABASE_URL="${DATABASE_URL:-postgres://movieflow:movieflow@postgres:5432/movieflow?sslmode=disable}"
export HOST=0.0.0.0
go -C api mod download
npm --prefix web ci
pg_isready -h postgres -U movieflow -d movieflow
mkdir -p api/bin
api_pid=; web_pid=
cleanup() {
  trap - EXIT INT TERM
  for pid in "$api_pid" "$web_pid"; do
    if [ -n "$pid" ]; then kill -TERM -- "-$pid" 2>/dev/null || true; fi
  done
  wait || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
setsid bash -c 'cd api && exec go run github.com/air-verse/air@v1.61.7 -c .air.toml' & api_pid=$!
setsid npm --prefix web run dev -- --port 3000 & web_pid=$!
set +e
wait -n "$api_pid" "$web_pid"
status=$?
exit "$status"
