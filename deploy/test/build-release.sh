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
actual_sha=$(git -C "$SOURCE_ROOT" rev-parse HEAD)
[[ "$actual_sha" == "$SHA" ]] || fail "source HEAD $actual_sha does not match $SHA"

if [[ "${UVP_SKIP_TESTS:-0}" != "1" ]]; then
  (cd "$SOURCE_ROOT/server" && go test ./... -count=1)
fi

mkdir -p "$SOURCE_ROOT/server/bin"
# -p 2 / GOMAXPROCS=2 / max-old-space-size keep the build inside the
# production server's memory budget (4C/3.8G with ~1.3G available)
(cd "$SOURCE_ROOT/server" && \
  env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOMAXPROCS=2 \
  go build -p 2 -trimpath -ldflags='-s -w' -o bin/uvp-gb28181-linux-amd64 main.go)
(cd "$SOURCE_ROOT/server" && \
  env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOMAXPROCS=2 \
  go build -p 2 -trimpath -ldflags='-s -w' -o bin/uvp-firewall-agent-linux-amd64 ./cmd/uvp-firewall-agent)

# pnpm progress goes to stderr: the caller captures stdout via command
# substitution and treats it as the archive path
(cd "$SOURCE_ROOT/web" && pnpm install --frozen-lockfile >&2)
(cd "$SOURCE_ROOT/web" && NODE_OPTIONS=--max-old-space-size=1536 pnpm run build:prod >&2)

UVP_SOURCE_ROOT="$SOURCE_ROOT" \
  "$SOURCE_ROOT/deploy/test/assemble-release.sh" "$SHA" "$OUTPUT_DIR"
