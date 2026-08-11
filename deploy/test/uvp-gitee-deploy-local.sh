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
ALLOW_DATABASE_CHANGES=0

fail() {
  printf '[%s] ERROR: %s\n' "$(date -Is)" "$*" >&2
  exit 1
}

for arg in "$@"; do
  case "$arg" in
    --allow-database-changes) ALLOW_DATABASE_CHANGES=1 ;;
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

previous=""
if [[ -e "$ROOT/current-release" && ! -f "$ROOT/current-release" ]]; then
  fail "release marker must be a regular file: $ROOT/current-release"
fi
if [[ -f "$ROOT/current-release" ]]; then
  previous=$(tr -d '[:space:]' < "$ROOT/current-release")
fi
if [[ "$ALLOW_DATABASE_CHANGES" != "1" && ! "$previous" =~ ^[0-9a-f]{40}$ ]]; then
  fail "no valid release baseline; apply and review migrations, then run manually with --allow-database-changes for the first deployment"
fi
if [[ "$ALLOW_DATABASE_CHANGES" != "1" ]]; then
  if ! git -C "$SOURCE_ROOT" diff --quiet "$previous" "$SHA" -- server/resource/database; then
    fail "database files changed since $previous; apply and review migrations separately before manual deployment"
  fi
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
