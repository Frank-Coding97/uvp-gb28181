#!/usr/bin/env bash
set -Eeuo pipefail
umask 027

QUEUE_DIR="${UVP_GITEE_QUEUE_DIR:-/var/lib/uvp-gitee-deployer/queue}"
FAILED_DIR="${UVP_GITEE_FAILED_DIR:-/var/lib/uvp-gitee-deployer/failed}"
LOCK_FILE="${UVP_GITEE_WORKER_LOCK:-/run/lock/uvp-gitee-deploy-worker.lock}"
DEPLOY_LOCAL="${UVP_GITEE_LOCAL_DEPLOY:-/usr/local/sbin/uvp-gitee-deploy-local}"
export UVP_SKIP_TESTS="${UVP_SKIP_TESTS:-1}"

fail() {
  printf '[%s] ERROR: %s\n' "$(date -Is)" "$*" >&2
  exit 1
}

install -d -m 0750 "$QUEUE_DIR" "$FAILED_DIR"
exec 9>"$LOCK_FILE"
flock -n 9 || exit 0

latest_job=$(find "$QUEUE_DIR" -maxdepth 1 -type f -name '*.json' -printf '%T@ %p\n' | sort -nr | awk 'NR == 1 {print $2}')
[[ -n "$latest_job" ]] || exit 0

while IFS= read -r stale_job; do
  [[ "$stale_job" == "$latest_job" ]] || rm -f -- "$stale_job"
done < <(find "$QUEUE_DIR" -maxdepth 1 -type f -name '*.json' -print)

processing_job="$latest_job.processing"
mv -- "$latest_job" "$processing_job"
failed_job="$FAILED_DIR/$(basename "$processing_job").failed"
sha=$(python3 - "$processing_job" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    job = json.load(handle)
print(job.get("sha", ""))
PY
)

if [[ ! "$sha" =~ ^[0-9a-f]{40}$ ]]; then
  mv -- "$processing_job" "$failed_job"
  fail "invalid queued revision"
fi

if "$DEPLOY_LOCAL" "$sha"; then
  rm -f -- "$processing_job"
  printf '[%s] queued deployment completed sha=%s\n' "$(date -Is)" "$sha"
else
  mv -- "$processing_job" "$failed_job"
  fail "queued deployment failed sha=$sha"
fi
