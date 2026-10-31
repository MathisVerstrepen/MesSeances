#!/usr/bin/python3
"""Build, validate and seal a credential-free Ubuntu runner image as host root."""
import base64
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import time
import xml.etree.ElementTree as ET

BASE = Path('/home/hogwarts/config/gha-outrunner')
DOMAIN = 'mfci-image-build'
IMAGE = BASE / 'images/runner-building.qcow2'
FINAL = BASE / 'images/runner.qcow2'


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=60).stdout


def virsh(*args):
    return run('virsh', '-c', 'qemu:///system', *args)


def agent(command):
    return json.loads(virsh('qemu-agent-command', DOMAIN, json.dumps(command)))['return']


def guest(script, timeout=60):
    pid = agent({'execute': 'guest-exec', 'arguments': {
        'path': '/bin/bash', 'arg': ['-c', script], 'capture-output': True,
    }})['pid']
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        status = agent({'execute': 'guest-exec-status', 'arguments': {'pid': pid}})
        if status.get('exited'):
            stdout = base64.b64decode(status.get('out-data', '')).decode()
            stderr = base64.b64decode(status.get('err-data', '')).decode()
            if status.get('exitcode') != 0:
                raise RuntimeError(f'Guest command failed: {stdout}\n{stderr}')
            return stdout
        time.sleep(1)
    raise TimeoutError('Guest command timed out')


def domain_xml():
    domain = ET.Element('domain', type='kvm')
    ET.SubElement(domain, 'name').text = DOMAIN
    ET.SubElement(domain, 'memory', unit='MiB').text = '8192'
    ET.SubElement(domain, 'vcpu').text = '4'
    os = ET.SubElement(domain, 'os', firmware='efi')
    ET.SubElement(os, 'type', arch='x86_64', machine='q35').text = 'hvm'
    ET.SubElement(os, 'boot', dev='hd')
    features = ET.SubElement(domain, 'features')
    ET.SubElement(features, 'acpi')
    ET.SubElement(features, 'apic')
    ET.SubElement(domain, 'cpu', mode='host-passthrough')
    devices = ET.SubElement(domain, 'devices')
    ET.SubElement(devices, 'controller', type='scsi', model='virtio-scsi')
    disk = ET.SubElement(devices, 'disk', type='file', device='disk')
    ET.SubElement(disk, 'driver', name='qemu', type='qcow2')
    ET.SubElement(disk, 'source', file=str(IMAGE))
    ET.SubElement(disk, 'target', dev='sda', bus='scsi')
    cdrom = ET.SubElement(devices, 'disk', type='file', device='cdrom')
    ET.SubElement(cdrom, 'driver', name='qemu', type='raw')
    ET.SubElement(cdrom, 'source', file=str(BASE / 'images/seed.iso'))
    ET.SubElement(cdrom, 'target', dev='sdb', bus='sata')
    ET.SubElement(cdrom, 'readonly')
    interface = ET.SubElement(devices, 'interface', type='network')
    ET.SubElement(interface, 'source', network='default')
    ET.SubElement(interface, 'model', type='virtio')
    channel = ET.SubElement(devices, 'channel', type='unix')
    ET.SubElement(channel, 'target', type='virtio', name='org.qemu.guest_agent.0')
    ET.SubElement(devices, 'serial', type='pty')
    return ET.tostring(domain, encoding='unicode')


def configure_network():
    # cloud-init binds its generated network config to the build VM's MAC.
    # Clones have new MACs, so replace it with a generic DHCP interface match.
    network = base64.b64encode((BASE / 'deployment/guest-network.yaml').read_bytes()).decode()
    guest('set -eu; rm -f /etc/netplan/50-cloud-init.yaml; '
          'install -m 600 /dev/null /etc/netplan/90-ci.yaml; '
          f'echo {network} | base64 -d > /etc/netplan/90-ci.yaml; netplan apply', timeout=90)


def main():
    resume = sys.argv[1:] == ['--resume']
    if sys.argv[1:] and not resume:
        raise RuntimeError('Only --resume is supported')
    if FINAL.exists() or (IMAGE.exists() and not resume) or DOMAIN in virsh('list', '--all', '--name').splitlines():
        raise RuntimeError('Existing image/build domain; refusing to overwrite')
    cloud = BASE / 'downloads/ubuntu-noble.qcow2'
    sums = (BASE / 'downloads/ubuntu-SHA256SUMS').read_text().splitlines()
    expected = next(line.split()[0] for line in sums
                    if line.split()[-1].lstrip('*') == 'noble-server-cloudimg-amd64.img')
    with cloud.open('rb') as stream:
        actual = hashlib.file_digest(stream, 'sha256').hexdigest()
    if actual != expected:
        raise RuntimeError('Ubuntu image checksum mismatch')
    if not resume:
        run('qemu-img', 'convert', '-f', 'qcow2', '-O', 'qcow2', str(cloud), str(IMAGE))
        run('qemu-img', 'resize', str(IMAGE), '40G')
    elif not IMAGE.is_file() or IMAGE.is_symlink():
        raise RuntimeError('Resume requires a regular unfinished build image')
    bootstrap = (BASE / 'deployment/guest-bootstrap.sh').read_text()
    config = {
        'hostname': DOMAIN, 'ssh_pwauth': False, 'disable_root': True,
        'groups': ['docker'],
        'users': [{'name': 'runner', 'uid': 1001, 'groups': ['sudo', 'docker'],
                   'shell': '/bin/bash', 'lock_passwd': True,
                   'sudo': ['ALL=(ALL) NOPASSWD:ALL']}],
        'package_update': True,
        'packages': ['ca-certificates', 'curl', 'git', 'jq', 'python3', 'python3-venv',
                     'make', 'gcc', 'g++', 'libc6-dev', 'pkg-config', 'unzip', 'zip',
                     'zstd', 'docker.io', 'qemu-guest-agent', 'sudo', 'libicu74',
                     'libssl3t64', 'libkrb5-3', 'libunwind8', 'nftables',
                     'netcat-openbsd', 'dnsutils'],
        'write_files': [{'path': '/root/guest-bootstrap.sh', 'permissions': '0700',
                         'content': bootstrap}],
        'runcmd': [['bash', '/root/guest-bootstrap.sh']],
    }
    user_data = BASE / 'images/user-data'
    user_data.write_text('#cloud-config\n' + json.dumps(config) + '\n')
    metadata = BASE / 'images/meta-data'
    metadata.write_text('instance-id: mfci-image-v1\nlocal-hostname: mfci-image-build\n')
    if not resume:
        run('cloud-localds', str(BASE / 'images/seed.iso'), str(user_data), str(metadata))
    xml = BASE / 'images/build.xml'
    xml.write_text(domain_xml())
    virsh('create', str(xml))
    print('Image-building VM started.', flush=True)
    try:
        deadline = time.monotonic() + 1800
        while time.monotonic() < deadline:
            try:
                if guest('test -f /var/lib/messeances-ci/image-ready && echo ready').strip() == 'ready':
                    break
            except (subprocess.CalledProcessError, RuntimeError):
                pass
            time.sleep(10)
        else:
            print(guest('tail -60 /var/log/cloud-init-output.log'), flush=True)
            raise TimeoutError('Image bootstrap did not complete within 30 minutes')
        configure_network()
        print(guest('PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin; go version; node --version; npm --version; docker --version; python3 --version'), flush=True)
        print(guest('''set -eu
curl -fsS --retry 5 --retry-all-errors --retry-delay 2 --retry-max-time 30 --max-time 15 https://api.github.com/zen >/dev/null
for target in 10.231.64.1:22 65.109.65.105:22 172.17.0.1:2375 169.254.169.254:80; do
  if timeout 2 bash -c "echo >/dev/tcp/${target%:*}/${target##*:}" 2>/dev/null; then
    echo "Unexpected network access: $target"; exit 1
  fi
done
echo 'Guest internet and network isolation checks passed'
'''), flush=True)
        guest('''set -eu
touch /etc/cloud/cloud-init.disabled
rm -rf /var/lib/cloud/instances /var/lib/cloud/instance /root/guest-bootstrap.sh
rm -f /etc/ssh/ssh_host_* /root/.ssh/authorized_keys /home/runner/.ssh/authorized_keys
truncate -s 0 /etc/machine-id
rm -f /var/lib/dbus/machine-id
ln -s /etc/machine-id /var/lib/dbus/machine-id
sync
''')
        # libvirt requires its shutdown API rather than a raw guest-shutdown RPC.
        virsh('shutdown', DOMAIN, '--mode', 'agent')
        for _ in range(60):
            if DOMAIN not in virsh('list', '--name').splitlines():
                break
            time.sleep(1)
        else:
            raise RuntimeError('Image VM did not shut down cleanly')
        IMAGE.rename(FINAL)
        FINAL.chmod(0o444)
        manifest = {'ubuntu_sha256': actual, 'outrunner': '1.2.0', 'runner': '2.338.2',
                    'go': '1.26.0', 'node': '22.23.1', 'npm': '10.9.8',
                    'created_at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}
        (BASE / 'images/manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        print('Validated runner image sealed successfully.', flush=True)
    finally:
        if DOMAIN in virsh('list', '--all', '--name').splitlines():
            virsh('destroy', DOMAIN)


if __name__ == '__main__':
    main()
