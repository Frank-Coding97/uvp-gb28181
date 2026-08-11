#!/usr/bin/env bash
set -Eeuo pipefail

SOURCE_ROOT="${1:-/opt/uvp-gb28181/source}"
REPO_URL="${UVP_GITEE_REPO_URL:-https://gitee.com/Frank-Coding/uvp-gb28181.git}"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

[[ "$(id -u)" == "0" ]] || fail "run as root"
[[ -f /etc/uvp-gitee-webhook.env ]] || fail "create /etc/uvp-gitee-webhook.env first"
command -v go >/dev/null || fail "Go is required"
command -v pnpm >/dev/null || fail "pnpm is required"

if ! getent group uvp-deploy >/dev/null; then
  groupadd --system uvp-deploy
fi
if ! id uvp-deploy >/dev/null 2>&1; then
  useradd --system --gid uvp-deploy --home-dir /var/lib/uvp-gitee-deployer --create-home --shell /usr/sbin/nologin uvp-deploy
fi

install -d -o uvp-deploy -g uvp-deploy -m 0750 /var/lib/uvp-gitee-deployer/queue
install -d -o uvp-deploy -g uvp-deploy -m 0750 /home/uvp-deploy/incoming
install -d -m 0755 /usr/local/libexec
install -d -m 0755 "$(dirname "$SOURCE_ROOT")"

if [[ ! -d "$SOURCE_ROOT/.git" ]]; then
  [[ ! -e "$SOURCE_ROOT" ]] || fail "source root exists but is not a Git repository: $SOURCE_ROOT"
  git clone --no-checkout "$REPO_URL" "$SOURCE_ROOT"
fi
git -C "$SOURCE_ROOT" remote set-url origin "$REPO_URL"
git -C "$SOURCE_ROOT" fetch --no-tags origin develop
git -C "$SOURCE_ROOT" checkout --detach FETCH_HEAD
chown -R uvp-deploy:uvp-deploy "$SOURCE_ROOT"

go build -trimpath -ldflags='-s -w' -o /usr/local/libexec/uvp-gitee-webhook "$SOURCE_ROOT/deploy/gitee-webhook"
install -m 0755 "$SOURCE_ROOT/deploy/test/uvp-gitee-deploy-local.sh" /usr/local/sbin/uvp-gitee-deploy-local
install -m 0755 "$SOURCE_ROOT/deploy/test/uvp-gitee-deploy-worker.sh" /usr/local/sbin/uvp-gitee-deploy-worker
install -m 0755 "$SOURCE_ROOT/deploy/test/deploy-uvp.sh" /usr/local/sbin/deploy-uvp
install -m 0644 "$SOURCE_ROOT/deploy/test/uvp-gitee-webhook.service" /etc/systemd/system/uvp-gitee-webhook.service
install -m 0644 "$SOURCE_ROOT/deploy/test/uvp-gitee-deploy.path" /etc/systemd/system/uvp-gitee-deploy.path
install -m 0644 "$SOURCE_ROOT/deploy/test/uvp-gitee-deploy.service" /etc/systemd/system/uvp-gitee-deploy.service

systemctl daemon-reload
systemctl enable --now uvp-gitee-webhook.service
systemctl enable --now uvp-gitee-deploy.path
systemctl is-active --quiet uvp-gitee-webhook.service
systemctl is-active --quiet uvp-gitee-deploy.path
printf 'installed Gitee deployer; configure HTTPS reverse proxy and Gitee WebHook next\n'
