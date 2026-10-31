#!/usr/bin/python3
"""Remove only our transient domains and their overlays; fail on uncertainty."""
from pathlib import Path
import re
import stat
import subprocess

BASE = Path('/home/hogwarts/config/gha-outrunner/state/overlays')
NAME = re.compile(r'messeances-vps-[0-9a-f]{8}\Z')


def virsh(*args):
    return subprocess.run(
        ['virsh', '--connect', 'qemu:///system', *args],
        check=True, capture_output=True, text=True, timeout=30,
    ).stdout


def main():
    domains = virsh('list', '--all', '--name').splitlines()
    for name in domains:
        if NAME.fullmatch(name):
            virsh('destroy', name)
    remaining = set(virsh('list', '--all', '--name').splitlines())
    for path in BASE.glob('messeances-vps-*.qcow2'):
        if not NAME.fullmatch(path.stem) or path.stem in remaining:
            continue
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode):
            raise RuntimeError('Unexpected overlay file type')
        path.unlink()


if __name__ == '__main__':
    main()
