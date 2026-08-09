#!/usr/bin/env bash
set -Eeuo pipefail
umask 027

ROOT="/opt/uvp-gb28181"
RELEASES="$ROOT/releases"
INCOMING="/home/uvp-deploy/incoming"
LOCK_FILE="/run/lock/uvp-gb28181-deploy.lock"
SHA="${1:-}"
ARCHIVE="${2:-}"
EXPECTED_SHA256="${3:-}"
CURRENT_FILE="$ROOT/current-release"
PREVIOUS=""
ACTIVATION_STARTED=0
AGENT_ACTIVATED=0

log() {
  printf '[%s] %s\n' "$(date -Is)" "$*"
}

fail() {
  log "ERROR: $*"
  exit 1
}

compose_for() {
  local release="$1"
  IMAGE_TAG="$release" UVP_ROOT="$ROOT" \
    docker compose -p uvp-gb28181 -f "$RELEASES/$release/compose.yml" "${@:2}"
}

rollback() {
  local exit_code=$?
  trap - ERR
  if [[ "$ACTIVATION_STARTED" == "1" && -n "$PREVIOUS" && -f "$RELEASES/$PREVIOUS/compose.yml" ]]; then
    log "Deployment failed; rolling back to $PREVIOUS"
    IMAGE_TAG="$PREVIOUS" UVP_ROOT="$ROOT" \
      docker compose -p uvp-gb28181 -f "$RELEASES/$PREVIOUS/compose.yml" \
      up -d --no-build --wait --wait-timeout 180 || true
  elif [[ "$ACTIVATION_STARTED" == "1" && -f "$ROOT/compose.yml" ]]; then
    log "Deployment failed on first managed release; restoring bootstrap images"
    IMAGE_TAG="test" UVP_ROOT="$ROOT" \
      docker compose -p uvp-gb28181 -f "$ROOT/compose.yml" \
      up -d --no-build --wait --wait-timeout 180 || true
  fi
  if [[ "$AGENT_ACTIVATED" == "1" ]]; then
    if [[ -n "$PREVIOUS" && -x "$RELEASES/$PREVIOUS/agent/uvp-firewall-agent" ]]; then
      install -m 0644 "$RELEASES/$PREVIOUS/agent/uvp-firewall-agent.service" /etc/systemd/system/uvp-firewall-agent.service
      ln -sfn "$RELEASES/$PREVIOUS/agent" "$ROOT/agent-current"
      systemctl daemon-reload || true
      systemctl restart uvp-firewall-agent || true
    else
      systemctl disable --now uvp-firewall-agent || true
      nft delete table inet uvp_sip_guard >/dev/null 2>&1 || true
      rm -f "$ROOT/agent-current"
    fi
  fi
  exit "$exit_code"
}
trap rollback ERR

[[ "$SHA" =~ ^[0-9a-f]{40}$ ]] || fail "Invalid Git revision"
[[ "$EXPECTED_SHA256" =~ ^[0-9a-f]{64}$ ]] || fail "Invalid archive checksum"
[[ "$ARCHIVE" == "$INCOMING/uvp-release-$SHA.tar.gz" ]] || fail "Unexpected archive path"
[[ -f "$ARCHIVE" ]] || fail "Release archive not found"
[[ -f "$ROOT/config/config.yml" ]] || fail "Runtime config is missing"
docker network inspect wvp_docker_compose_wvp-network >/dev/null 2>&1 || fail "Shared Docker network is missing"

install -d -m 0755 "$RELEASES"
exec 9>"$LOCK_FILE"
flock -n 9 || fail "Another deployment is already running"

AVAILABLE_KB=$(df -Pk "$ROOT" | awk 'NR==2 {print $4}')
(( AVAILABLE_KB >= 2097152 )) || fail "Less than 2 GiB disk space is available"

ACTUAL_SHA256=$(sha256sum "$ARCHIVE" | awk '{print $1}')
[[ "$ACTUAL_SHA256" == "$EXPECTED_SHA256" ]] || fail "Release checksum mismatch"

if tar -tzf "$ARCHIVE" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
  fail "Unsafe path found in release archive"
fi
if tar -tvzf "$ARCHIVE" | grep -Eq '^[lh]'; then
  fail "Symbolic and hard links are not allowed in release archives"
fi

if [[ -f "$CURRENT_FILE" ]]; then
  PREVIOUS=$(tr -d '[:space:]' < "$CURRENT_FILE")
fi

TEMP_RELEASE="$RELEASES/.${SHA}.tmp"
FINAL_RELEASE="$RELEASES/$SHA"
rm -rf "$TEMP_RELEASE"
install -d -m 0755 "$TEMP_RELEASE"
tar -xzf "$ARCHIVE" --no-same-owner --no-same-permissions -C "$TEMP_RELEASE"

for required in \
  compose.yml \
  backend/Dockerfile \
  backend/uvp-gb28181 \
  agent/uvp-firewall-agent \
  agent/uvp-firewall-agent.service \
  agent/uvp-firewall-agent.default \
  backend/resource/database/uvp-gb28181.sql \
  clickhouse/init/01-sip-trace.sh \
  frontend/Dockerfile \
  frontend/nginx.conf \
  frontend/dist/index.html; do
  [[ -e "$TEMP_RELEASE/$required" ]] || fail "Release is missing $required"
done

rm -rf "$FINAL_RELEASE"
mv "$TEMP_RELEASE" "$FINAL_RELEASE"
chmod 0755 "$FINAL_RELEASE/backend/uvp-gb28181"

log "Validating release $SHA"
compose_for "$SHA" config --quiet

log "Activating firewall agent for $SHA"
install -m 0644 "$FINAL_RELEASE/agent/uvp-firewall-agent.service" /etc/systemd/system/uvp-firewall-agent.service
if [[ ! -e /etc/default/uvp-firewall-agent ]]; then
  install -m 0644 "$FINAL_RELEASE/agent/uvp-firewall-agent.default" /etc/default/uvp-firewall-agent
fi
ln -sfn "$FINAL_RELEASE/agent" "$ROOT/agent-current"
AGENT_ACTIVATED=1
systemctl daemon-reload
systemctl enable uvp-firewall-agent
systemctl restart uvp-firewall-agent
systemctl is-active --quiet uvp-firewall-agent
for _ in {1..50}; do
  [[ -S /run/uvp/firewall-agent.sock ]] && break
  sleep 0.1
done
[[ -S /run/uvp/firewall-agent.sock ]] || fail "Firewall agent socket was not created"

log "Building runtime images for $SHA"
compose_for "$SHA" build --pull

log "Activating release $SHA"
ACTIVATION_STARTED=1
compose_for "$SHA" up -d --wait --wait-timeout 180

for url in \
  http://127.0.0.1:56000/healthz \
  http://127.0.0.1:56000/ \
  http://127.0.0.1:56000/account \
  http://127.0.0.1:56000/css/loading.css \
  http://127.0.0.1:56000/system/; do
  curl --fail --location --silent --show-error --retry 5 --retry-delay 2 --retry-all-errors \
    --max-redirs 5 --max-time 15 "$url" >/dev/null
done
for url in \
  https://www.uvplatform.cn/healthz \
  https://www.uvplatform.cn/ \
  https://www.uvplatform.cn/system/; do
  curl --fail --location --silent --show-error --retry 5 --retry-delay 2 --retry-all-errors \
    --max-redirs 5 --max-time 20 "$url" >/dev/null
done

printf '%s\n' "$SHA" > "$CURRENT_FILE.tmp"
mv "$CURRENT_FILE.tmp" "$CURRENT_FILE"
ln -sfn "$FINAL_RELEASE" "$ROOT/current"
rm -f "$ARCHIVE"
ACTIVATION_STARTED=0
AGENT_ACTIVATED=0

log "Release $SHA is healthy"

mapfile -t OLD_RELEASES < <(
  find "$RELEASES" -mindepth 1 -maxdepth 1 -type d -name '[0-9a-f]*' -printf '%T@ %p\n' \
    | sort -rn \
    | awk 'NR > 5 {print $2}'
)
for old_release in "${OLD_RELEASES[@]}"; do
  old_name=$(basename "$old_release")
  if [[ "$old_name" != "$SHA" && "$old_name" != "$PREVIOUS" ]]; then
    rm -rf "$old_release"
    docker image rm "uvp-gb28181-backend:$old_name" "uvp-gb28181-frontend:$old_name" >/dev/null 2>&1 || true
  fi
done

docker builder prune -f --filter 'until=168h' >/dev/null 2>&1 || true
log "Deployment completed"
