#!/usr/bin/env bash
set -Eeuo pipefail
umask 027

ROOT="${UVP_ROOT:-/opt/uvp-gb28181}"
SOURCE_ROOT="${UVP_SOURCE_ROOT:-$ROOT/source}"
INCOMING="${UVP_INCOMING:-/home/uvp-deploy/incoming}"
DEPLOY_SCRIPT="${UVP_DEPLOY_SCRIPT:-/usr/local/sbin/deploy-uvp}"
GIT_REMOTE="${UVP_GIT_REMOTE:-origin}"
GIT_REF="${UVP_GIT_REF:-develop}"
LOCK_FILE="${UVP_LOCAL_DEPLOY_LOCK:-/run/lock/uvp-gitee-local-deploy.lock}"
SHA=""

fail() {
  printf '[%s] ERROR: %s\n' "$(date -Is)" "$*" >&2
  exit 1
}

for arg in "$@"; do
  case "$arg" in
    --*) fail "unknown option $arg" ;;
    *) [[ -z "$SHA" ]] && SHA="$arg" || fail "duplicate revision" ;;
  esac
done

[[ "$SHA" =~ ^[0-9a-f]{40}$ ]] || fail "invalid Git revision"
[[ -d "$SOURCE_ROOT/.git" ]] || fail "source repository is missing: $SOURCE_ROOT"
[[ -x "$DEPLOY_SCRIPT" ]] || fail "deploy script is missing: $DEPLOY_SCRIPT"
install -d -m 0750 "$INCOMING" "$ROOT/builds"
exec 9>"$LOCK_FILE"
flock -n 9 || fail "another local deployment is running"

git -C "$SOURCE_ROOT" fetch --no-tags "$GIT_REMOTE" "$GIT_REF"
fetched_sha=$(git -C "$SOURCE_ROOT" rev-parse FETCH_HEAD)
[[ "$fetched_sha" == "$SHA" ]] || fail "fetched $fetched_sha does not match webhook $SHA"

if [[ -e "$ROOT/current-release" && ! -f "$ROOT/current-release" ]]; then
  fail "release marker must be a regular file: $ROOT/current-release"
fi

# 损坏的 current-release 会导致回退定位失败:校验 40 位 SHA 与 release 目录
previous_sha=""
if [[ -f "$ROOT/current-release" ]]; then
  previous_sha=$(tr -d '[:space:]' < "$ROOT/current-release")
  [[ "$previous_sha" =~ ^[0-9a-f]{40}$ ]] || fail "corrupted current-release marker: not a 40-char SHA"
  [[ -d "$ROOT/builds/$previous_sha" ]] || fail "current-release points to missing build: $previous_sha"
fi

build_root="$ROOT/builds/$SHA"
rm -rf "$build_root"
cleanup() {
  if [[ -d "$build_root" ]]; then
    git -C "$SOURCE_ROOT" worktree remove --force "$build_root" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
git -C "$SOURCE_ROOT" worktree add --detach "$build_root" "$SHA" >/dev/null

archive=$(UVP_SOURCE_ROOT="$build_root" \
  "$build_root/deploy/test/build-release.sh" "$SHA" "$INCOMING")
checksum=$(sha256sum "$archive" | awk '{print $1}')
"$DEPLOY_SCRIPT" "$SHA" "$archive" "$checksum"
printf '[%s] deployment completed sha=%s\n' "$(date -Is)" "$SHA"
