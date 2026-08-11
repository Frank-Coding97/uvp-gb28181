#!/usr/bin/env bash
set -Eeuo pipefail

SOURCE_ROOT="${UVP_SOURCE_ROOT:-$(pwd)}"
SHA="${1:-}"
OUTPUT_DIR="${2:-$SOURCE_ROOT/release-output}"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

[[ "$SHA" =~ ^[0-9a-f]{40}$ ]] || fail "invalid Git revision"
[[ -d "$SOURCE_ROOT/server" && -d "$SOURCE_ROOT/web" ]] || fail "invalid source root"
[[ -x "$SOURCE_ROOT/server/bin/uvp-gb28181-linux-amd64" ]] || fail "backend binary is missing"
[[ -x "$SOURCE_ROOT/server/bin/uvp-firewall-agent-linux-amd64" ]] || fail "firewall agent binary is missing"
[[ -f "$SOURCE_ROOT/web/dist/index.html" ]] || fail "frontend build is missing"

release_dir=$(mktemp -d "${TMPDIR:-/tmp}/uvp-release.XXXXXX")
cleanup() {
  rm -rf "$release_dir"
}
trap cleanup EXIT

mkdir -p \
  "$release_dir/backend/resource/public" \
  "$release_dir/agent" \
  "$release_dir/frontend" \
  "$OUTPUT_DIR"

install -m 0755 "$SOURCE_ROOT/server/bin/uvp-gb28181-linux-amd64" "$release_dir/backend/uvp-gb28181"
install -m 0755 "$SOURCE_ROOT/server/bin/uvp-firewall-agent-linux-amd64" "$release_dir/agent/uvp-firewall-agent"
install -m 0644 "$SOURCE_ROOT/deploy/test/uvp-firewall-agent.service" "$release_dir/agent/uvp-firewall-agent.service"
install -m 0644 "$SOURCE_ROOT/deploy/test/uvp-firewall-agent.default" "$release_dir/agent/uvp-firewall-agent.default"
cp -a "$SOURCE_ROOT/server/resource/database" "$release_dir/backend/resource/database"
if [[ -d "$SOURCE_ROOT/server/resource/public" ]]; then
  cp -a "$SOURCE_ROOT/server/resource/public/." "$release_dir/backend/resource/public/"
fi
cp "$SOURCE_ROOT/deploy/test/backend.Dockerfile" "$release_dir/backend/Dockerfile"
cp -a "$SOURCE_ROOT/web/dist" "$release_dir/frontend/dist"
cp "$SOURCE_ROOT/deploy/test/frontend.Dockerfile" "$release_dir/frontend/Dockerfile"
cp "$SOURCE_ROOT/deploy/test/nginx.conf" "$release_dir/frontend/nginx.conf"
cp "$SOURCE_ROOT/deploy/test/compose.yml" "$release_dir/compose.yml"

archive="$OUTPUT_DIR/uvp-release-$SHA.tar.gz"
tar -czf "$archive" -C "$release_dir" .
test -s "$archive"
printf '%s\n' "$archive"
