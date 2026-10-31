#!/bin/bash
# Runs inside the image-building VM. No GitHub or production credentials.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
export PATH=/usr/local/go/bin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"

curl -fsSL --retry 3 https://go.dev/dl/go1.26.0.linux-amd64.tar.gz -o go.tar.gz
echo 'aac1b08a0fb0c4e0a7c1555beb7b59180b05dfc5a3d62e40e9de90cd42f88235  go.tar.gz' | sha256sum -c -
tar -xzf go.tar.gz -C /usr/local
curl -fsSL --retry 3 https://nodejs.org/dist/v22.23.1/node-v22.23.1-linux-x64.tar.xz -o node.tar.xz
echo '9749e988f437343b7fa832c69ded82a312e41a03116d766797ac14f6f9eee578  node.tar.xz' | sha256sum -c -
tar -xJf node.tar.xz -C /usr/local --strip-components=1
npm install --global npm@10.9.8
# Let the unprivileged runner execute the existing setup-node/npm workflow.
chown -R runner:runner /usr/local/lib/node_modules /usr/local/bin

install -d -o runner -g runner /opt/actions-runner /opt/hostedtoolcache
curl -fsSL --retry 3 https://github.com/actions/runner/releases/download/v2.338.2/actions-runner-linux-x64-2.338.2.tar.gz -o runner.tar.gz
echo 'baebf204307bac6193480852732f0aba2ae97b6c5597c244f4d5558390147c40  runner.tar.gz' | sha256sum -c -
tar -xzf runner.tar.gz -C /opt/actions-runner
chown -R runner:runner /opt/actions-runner

cat > /usr/local/bin/start-github-runner <<'RUNNER'
#!/bin/bash
set -euo pipefail
# QEMU's guest agent calls this as guest root. Drop to runner before starting
# workflow code. This receives only a single-use JIT configuration.
export HOME=/home/runner USER=runner LOGNAME=runner
export PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:/home/runner/go/bin
export RUNNER_TOOL_CACHE=/opt/hostedtoolcache AGENT_TOOLSDIRECTORY=/opt/hostedtoolcache
cd /opt/actions-runner
exec setpriv --reuid=runner --regid=runner --init-groups ./run.sh "$@"
RUNNER
chmod 0755 /usr/local/bin/start-github-runner
systemctl enable --now docker qemu-guest-agent
# No network login is required: administration uses the local guest agent.
systemctl disable --now ssh.socket ssh.service || true
systemctl mask ssh.socket ssh.service
usermod --lock ubuntu || true

# Preload public dependencies. Each job writes to its own disposable overlay.
runuser -u runner -- env HOME=/home/runner PATH="$PATH" bash -euc '
  cd /home/runner
  git clone --quiet --depth 1 --branch dev https://github.com/MathisVerstrepen/MesSeances.git image-warmup
  cd image-warmup/api
  go mod download
  GOFLAGS=-tags=nodynamic go test -run "^$" ./...
  cd ../web
  npm ci --no-audit --no-fund
  npx --no-install playwright install chromium
  cd /home/runner
  rm -rf image-warmup
'
npx --yes playwright@1.63.0 install-deps chromium
docker pull postgres:18-alpine
apt-get clean
install -d /var/lib/messeances-ci
date -u +%FT%TZ > /var/lib/messeances-ci/image-ready
