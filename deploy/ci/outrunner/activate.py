#!/usr/bin/python3
"""Validate repository admission policy and private credentials before activation."""
import json
from pathlib import Path
import stat
import subprocess
import urllib.request

BASE = Path('/home/hogwarts/config/gha-outrunner')


def main():
    token_file = BASE / 'secrets/github-token'
    info = token_file.lstat()
    if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600:
        raise RuntimeError('Token must be a regular file with permissions 0600')
    if not (BASE / 'images/runner.qcow2').is_file():
        raise RuntimeError('Validated runner image is missing')
    token = token_file.read_text().strip()
    request = urllib.request.Request(
        'https://api.github.com/repos/MathisVerstrepen/MesSeances/actions/permissions/fork-pr-contributor-approval',
        headers={'Authorization': 'Bearer ' + token, 'Accept': 'application/vnd.github+json'},
    )
    with urllib.request.urlopen(request, timeout=20) as response:
        policy = json.load(response)
    if policy.get('approval_policy') != 'all_external_contributors':
        raise RuntimeError('All external contributors must require workflow approval')
    subprocess.run(['systemctl', 'restart', 'messeances-ci-firewall.service'], check=True)
    subprocess.run(['systemctl', 'enable', '--now', 'messeances-outrunner.service',
                    'messeances-ci-watchdog.timer'], check=True)
    print('Controller and VM watchdog activated.')


if __name__ == '__main__':
    main()
