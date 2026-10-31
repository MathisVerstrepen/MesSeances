#!/bin/bash
# This account has Docker administrative access, equivalent to host root.
# Use a one-shot helper rather than changing sudo policy or Docker's daemon.
set -euo pipefail
test "$(hostname)" = Hogwarts-3
base=/home/hogwarts/config/gha-outrunner
case "${1:-}" in
  prepare) command=(/bin/bash "$base/deployment/install-host.sh") ;;
  build-image) command=(/usr/bin/python3 -u "$base/deployment/build-image.py") ;;
  resume-image) command=(/usr/bin/python3 -u "$base/deployment/build-image.py" --resume) ;;
  activate) command=(/usr/bin/python3 "$base/deployment/activate.py") ;;
  stop) command=(/bin/systemctl stop messeances-outrunner.service messeances-ci-watchdog.timer) ;;
  disable) command=(/bin/systemctl disable --now messeances-outrunner.service messeances-ci-watchdog.timer) ;;
  status) command=(/bin/systemctl --no-pager status messeances-outrunner.service messeances-ci-watchdog.timer) ;;
  *) echo 'Usage: admin.sh {prepare|build-image|resume-image|activate|stop|disable|status}' >&2; exit 2 ;;
esac
exec docker run --rm --privileged --pid=host --network=host \
  --mount type=bind,src=/,dst=/host \
  debian:bookworm-slim chroot /host "${command[@]}"
