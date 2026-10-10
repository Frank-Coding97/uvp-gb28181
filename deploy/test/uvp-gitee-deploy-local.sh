#!/usr/bin/env bash
set -Eeuo pipefail
umask 027

ROOT="${UVP_ROOT:-/opt/uvp-gb28181}"
SOURCE_ROOT="${UVP_SOURCE_ROOT:-$ROOT/source}"
INCOMING="${UVP_INCOMING:-/home/uvp-deploy/incoming}"
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
# (与 deploy-uvp.sh 一致,marker 指向 releases/<sha> 而非 builds/<sha>)
previous_sha=""
if [[ -f "$ROOT/current-release" ]]; then
  previous_sha=$(tr -d '[:space:]' < "$ROOT/current-release")
  [[ "$previous_sha" =~ ^[0-9a-f]{40}$ ]] || fail "corrupted current-release marker: not a 40-char SHA"
  [[ -f "$ROOT/releases/$previous_sha/compose.yml" ]] || fail "current-release points to missing release compose: $previous_sha"
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

# deploy 脚本与 build 脚本一样随被部署 commit 从 worktree 检出,避免固定
# 路径 /usr/local/sbin/deploy-uvp 与仓库版本漂移;UVP_DEPLOY_SCRIPT 可显式覆盖
DEPLOY_SCRIPT="${UVP_DEPLOY_SCRIPT:-$build_root/deploy/test/deploy-uvp.sh}"
[[ -x "$DEPLOY_SCRIPT" ]] || fail "deploy script is missing: $DEPLOY_SCRIPT"

archive=$(UVP_SOURCE_ROOT="$build_root" \
  "$build_root/deploy/test/build-release.sh" "$SHA" "$INCOMING")
checksum=$(sha256sum "$archive" | awk '{print $1}')
"$DEPLOY_SCRIPT" "$SHA" "$archive" "$checksum"
printf '[%s] deployment completed sha=%s\n' "$(date -Is)" "$SHA"
