#!/bin/bash
# Run as root only after reviewing the files in this directory.
set -euo pipefail
base=/home/hogwarts/config/gha-outrunner
source_dir=$base/deployment
test "$(id -u)" = 0
test -x "$base/bin/outrunner"

# Refuse to take over an existing hypervisor's default network.
if command -v virsh >/dev/null && virsh -c qemu:///system net-info default >/dev/null 2>&1; then
  virsh -c qemu:///system net-dumpxml default | grep -q "virbr-mfci" || {
    echo 'Existing unrelated default libvirt network; refusing to replace it.' >&2
    exit 1
  }
fi

export DEBIAN_FRONTEND=noninteractive
test ! -e /usr/sbin/policy-rc.d || {
  echo 'Existing service-start policy; review required before package installation.' >&2
  exit 1
}
# Keep new libvirt services stopped until our network is defined. The stock
# subnet overlaps Docker networks already in use on this server.
printf '#!/bin/sh\nexit 101\n' > /usr/sbin/policy-rc.d
chmod 0755 /usr/sbin/policy-rc.d
trap 'rm -f /usr/sbin/policy-rc.d' EXIT
apt-get update -qq
apt-get install -y --no-install-recommends qemu-system-x86 qemu-utils \
  libvirt-daemon-system libvirt-clients ovmf cloud-image-utils dnsmasq-base \
  nftables acl python3
rm /usr/sbin/policy-rc.d
trap - EXIT
if systemctl is-active --quiet dbus.service; then
  # Load newly installed D-Bus policies without restarting existing connections.
  systemctl reload dbus.service
fi
# Remove only the distribution's new default-network autostart link, before
# libvirtd starts. No VM or network was present at the initial preflight.
if test -L /etc/libvirt/qemu/networks/autostart/default.xml; then
  if grep -q "virbr0" /etc/libvirt/qemu/networks/default.xml; then
    unlink /etc/libvirt/qemu/networks/autostart/default.xml
  fi
fi
if ! id outrunner >/dev/null 2>&1; then
  useradd --system --user-group --home-dir "$base/state" --shell /usr/sbin/nologin outrunner
fi
usermod -aG libvirt outrunner
# Use filesystem permissions on the local management socket. This avoids an
# interactive Polkit dependency for the headless controller; no TCP API opens.
install -d -m 0755 /etc/systemd/system/libvirtd.socket.d
printf '[Socket]\nSocketGroup=libvirt\nSocketMode=0660\n' > /etc/systemd/system/libvirtd.socket.d/messeances-ci.conf
if ! grep -q '^# MesSeances CI local socket authentication' /etc/libvirt/libvirtd.conf; then
  printf '\n# MesSeances CI local socket authentication\nauth_unix_rw = "none"\nunix_sock_group = "libvirt"\nunix_sock_rw_perms = "0660"\n' >> /etc/libvirt/libvirtd.conf
fi
install -d -m 0755 /usr/local/libexec/messeances-outrunner
install -m 0755 "$source_dir/firewall.sh" "$source_dir/cleanup.py" "$source_dir/watchdog.py" /usr/local/libexec/messeances-outrunner/
for unit in messeances-ci-firewall.service messeances-outrunner.service messeances-ci-watchdog.service messeances-ci-watchdog.timer; do
  install -m 0644 "$source_dir/$unit" "/etc/systemd/system/$unit"
done
install -m 0640 -o root -g outrunner "$source_dir/config.yml" "$base/config/config.yml"
install -m 0644 -o root -g root "$source_dir/firewall.nft" "$base/config/firewall.nft"
chown root:outrunner "$base/bin/outrunner"
chmod 0750 "$base/bin/outrunner"
install -d -m 0750 -o outrunner -g outrunner "$base/state" "$base/state/overlays" "$base/state/leases"
install -d -m 0750 -o root -g libvirt-qemu "$base/images"
for dir in /home/hogwarts /home/hogwarts/config "$base" "$base/config" "$base/bin"; do
  setfacl -m u:outrunner:--x,u:libvirt-qemu:--x "$dir"
done
setfacl -m u:outrunner:r-x "$base/images"
setfacl -m u:libvirt-qemu:--x "$base/state"
setfacl -m u:libvirt-qemu:rwx,d:u:libvirt-qemu:rwx "$base/state/overlays"

systemctl daemon-reload
systemctl enable --now libvirtd.service
systemctl enable --now messeances-ci-firewall.service
# The distribution may have installed its unused default network with libvirt.
if virsh -c qemu:///system net-info default >/dev/null 2>&1; then
  if ! virsh -c qemu:///system net-dumpxml default | grep -q 'virbr-mfci'; then
    test -z "$(virsh -c qemu:///system list --all --name)"
    virsh -c qemu:///system net-destroy default 2>/dev/null || true
    virsh -c qemu:///system net-undefine default
  fi
fi
if ! virsh -c qemu:///system net-info default >/dev/null 2>&1; then
  virsh -c qemu:///system net-define "$source_dir/network.xml"
fi
virsh -c qemu:///system net-autostart default
if ! virsh -c qemu:///system net-info default | grep -q 'Active:.*yes'; then
  virsh -c qemu:///system net-start default
fi
# Controller activation is separate: credentials and a validated image are required.
systemd-analyze verify /etc/systemd/system/messeances-{ci-firewall,outrunner}.service
echo 'Hypervisor and firewall prepared; controller activation remains disabled.'
