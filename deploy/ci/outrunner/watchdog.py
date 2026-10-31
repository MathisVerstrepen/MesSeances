#!/usr/bin/python3
"""Bound VM lifetime if GitHub completion events or controller cleanup are lost."""
import json
from pathlib import Path
import re
import subprocess
import time

LEASES = Path('/home/hogwarts/config/gha-outrunner/state/leases')
NAME = re.compile(r'messeances-vps-[0-9a-f]{8}\Z')
MAX_SECONDS = 45 * 60


def virsh(*args):
    return subprocess.run(
        ['virsh', '-c', 'qemu:///system', *args],
        check=True, capture_output=True, text=True, timeout=30,
    ).stdout.strip()


def main():
    now = time.time()
    names = {n for n in virsh('list', '--name').splitlines() if NAME.fullmatch(n)}
    for name in names:
        lease = LEASES / name
        if lease.is_symlink():
            raise RuntimeError('Unexpected lease symlink')
        uuid = virsh('domuuid', name)
        if not lease.exists():
            lease.write_text(json.dumps({'uuid': uuid, 'created_at': now}))
        record = json.loads(lease.read_text())
        if record['uuid'] != uuid:
            raise RuntimeError('Unexpected domain identity change')
        if now - record['created_at'] > MAX_SECONDS:
            # Recheck the UUID immediately before destroying the exact domain.
            if virsh('domuuid', name) != uuid:
                raise RuntimeError('Domain identity changed before disposal')
            virsh('destroy', uuid)
            print('Disposed expired CI VM:', name)
    for lease in LEASES.iterdir():
        if NAME.fullmatch(lease.name) and lease.name not in names and not lease.is_symlink():
            lease.unlink()


if __name__ == '__main__':
    main()
