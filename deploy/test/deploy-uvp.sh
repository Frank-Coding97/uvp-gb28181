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
  backend/resource/database/uvp-gb28181.sql \
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

log "Building runtime images for $SHA"
compose_for "$SHA" build --pull

log "Activating release $SHA"
ACTIVATION_STARTED=1
compose_for "$SHA" up -d --wait --wait-timeout 180

curl --fail --silent --show-error --retry 5 --retry-delay 2 --retry-all-errors \
  --max-time 15 http://127.0.0.1:56000/healthz >/dev/null
curl --fail --silent --show-error --retry 5 --retry-delay 2 --retry-all-errors \
  --max-time 20 https://www.uvplatform.cn/healthz >/dev/null

printf '%s\n' "$SHA" > "$CURRENT_FILE.tmp"
mv "$CURRENT_FILE.tmp" "$CURRENT_FILE"
ln -sfn "$FINAL_RELEASE" "$ROOT/current"
rm -f "$ARCHIVE"
ACTIVATION_STARTED=0

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
