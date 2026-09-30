#!/usr/bin/env bash
set -euo pipefail
: "${ORCA_SSH_PUBLIC_KEY:?Missing SSH public key}"
install -d -m 700 -o mathis -g mathis /home/mathis/.ssh
printf '%s\n' "$ORCA_SSH_PUBLIC_KEY" > /home/mathis/.ssh/authorized_keys
chown mathis:mathis /home/mathis/.ssh/authorized_keys
chmod 600 /home/mathis/.ssh/authorized_keys
ssh-keygen -A >&2
exec /usr/sbin/sshd -D -e
