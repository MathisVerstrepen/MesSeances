#!/bin/bash
# Run only inside a disposable runner VM, never against the production host.
set -euo pipefail
grep -Eq 'KVM|QEMU|Standard PC' /sys/class/dmi/id/product_name
test "$(id -un)" = runner
test "$(nproc)" = 8
test ! -e /home/hogwarts
test ! -e /run/credentials/messeances-outrunner.service/github-token
test ! -e /var/run/libvirt/libvirt-sock
test -z "$(docker ps --format '{{.Names}}')"
test "$(node --version)" = v22.23.1
test "$(npm --version)" = 10.9.8
go version | grep -q 'go1.26.0 '
python3 -c 'import sys; assert sys.version_info >= (3, 12)'
curl -fsS --max-time 15 https://api.github.com/zen >/dev/null
for target in 10.231.64.1:22 65.109.65.105:22 172.17.0.1:2375 169.254.169.254:80; do
  if timeout 2 bash -c "echo >/dev/tcp/${target%:*}/${target##*:}" 2>/dev/null; then
    echo "Unexpected network access: $target" >&2
    exit 1
  fi
done
echo 'Disposable VM, tools, empty guest Docker daemon, and network isolation verified.'
