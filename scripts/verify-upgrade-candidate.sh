#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/verify-upgrade-candidate.sh --binary /absolute/path/to/uvp-gb28181 [options]

Options:
  --binary PATH       Linux backend executable to verify (required).
  --baseline PATH     SQLite baseline.sql. Defaults to the repository baseline.
  --build-id VALUE    Build ID embedded in the executable and version.json.
                      Default: upgrade-runtime-fixture-20261016
  --version VALUE     Candidate version written to version.json.
                      Default: 0.0.0-candidate-fixture
  --task-id VALUE     Candidate task ID (32 lowercase hexadecimal characters).
                      Default: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  --http-port VALUE   Loopback HTTP port. Defaults to an available port.
  --sip-port VALUE    Unused SIP port. Defaults to an available port.
  --workdir PATH      Empty fixture directory retained after the check.
  --startup-timeout S Maximum seconds to wait for the candidate HTTP server.
                      Default: 60
  -h, --help          Show this help.

The check starts a real backend process in candidate mode against an isolated
SQLite baseline and configuration. It then clears the gate atomically and
verifies that business startup is admitted without restarting the process.
USAGE
}

fail() {
  printf 'verify-upgrade-candidate: %s\n' "$*" >&2
  exit 1
}

SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY=""
BASELINE="$SCRIPT_ROOT/../server/resource/database/sqlitebaseline/baseline.sql"
BUILD_ID="upgrade-runtime-fixture-20261016"
VERSION="0.0.0-candidate-fixture"
TASK_ID="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
HTTP_PORT=""
SIP_PORT=""
WORKDIR=""
STARTUP_TIMEOUT=60
AUTO_WORKDIR=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)
      [[ $# -ge 2 ]] || fail "--binary requires a path"
      BINARY="$2"
      shift 2
      ;;
    --baseline)
      [[ $# -ge 2 ]] || fail "--baseline requires a path"
      BASELINE="$2"
      shift 2
      ;;
    --build-id)
      [[ $# -ge 2 ]] || fail "--build-id requires a value"
      BUILD_ID="$2"
      shift 2
      ;;
    --version)
      [[ $# -ge 2 ]] || fail "--version requires a value"
      VERSION="$2"
      shift 2
      ;;
    --task-id)
      [[ $# -ge 2 ]] || fail "--task-id requires a value"
      TASK_ID="$2"
      shift 2
      ;;
    --http-port)
      [[ $# -ge 2 ]] || fail "--http-port requires a port"
      HTTP_PORT="$2"
      shift 2
      ;;
    --sip-port)
      [[ $# -ge 2 ]] || fail "--sip-port requires a port"
      SIP_PORT="$2"
      shift 2
      ;;
    --workdir)
      [[ $# -ge 2 ]] || fail "--workdir requires a path"
      WORKDIR="$2"
      shift 2
      ;;
    --startup-timeout)
      [[ $# -ge 2 ]] || fail "--startup-timeout requires seconds"
      STARTUP_TIMEOUT="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[[ -n "$BINARY" ]] || fail "--binary is required"
[[ -x "$BINARY" ]] || fail "backend binary is not executable: $BINARY"
[[ -f "$BASELINE" ]] || fail "SQLite baseline does not exist: $BASELINE"
command -v python3 >/dev/null 2>&1 || fail "python3 is required"
[[ "$TASK_ID" =~ ^[0-9a-f]{32}$ ]] || fail "--task-id must be 32 lowercase hexadecimal characters"
[[ "$STARTUP_TIMEOUT" =~ ^[1-9][0-9]*$ ]] || fail "--startup-timeout must be a positive integer"

BINARY="$(cd "$(dirname "$BINARY")" && pwd)/$(basename "$BINARY")"
BASELINE="$(cd "$(dirname "$BASELINE")" && pwd)/$(basename "$BASELINE")"
if [[ -z "$WORKDIR" ]]; then
  WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/uvp-upgrade-candidate.XXXXXX")"
  AUTO_WORKDIR=1
else
  [[ ! -e "$WORKDIR" || -d "$WORKDIR" ]] || fail "--workdir is not a directory: $WORKDIR"
  mkdir -p "$WORKDIR"
  WORKDIR="$(cd "$WORKDIR" && pwd)"
  [[ -z "$(find "$WORKDIR" -mindepth 1 -maxdepth 1 -print -quit)" ]] || fail "--workdir must be empty: $WORKDIR"
fi

choose_port() {
  python3 - <<'PY'
import socket
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
}

if [[ -z "$HTTP_PORT" ]]; then HTTP_PORT="$(choose_port)"; fi
if [[ -z "$SIP_PORT" ]]; then SIP_PORT="$(choose_port)"; fi
[[ "$HTTP_PORT" =~ ^[1-9][0-9]*$ && "$HTTP_PORT" -le 65535 ]] || fail "invalid --http-port"
[[ "$SIP_PORT" =~ ^[1-9][0-9]*$ && "$SIP_PORT" -le 65535 ]] || fail "invalid --sip-port"
[[ "$HTTP_PORT" != "$SIP_PORT" ]] || fail "HTTP and SIP ports must differ"

SERVER_PID=""
SERVER_STDOUT="$WORKDIR/backend.stdout.log"
SERVER_STDERR="$WORKDIR/backend.stderr.log"
READINESS_BODY="$WORKDIR/readiness.candidate.json"
NORMAL_BODY="$WORKDIR/config.normal.json"

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    for _ in {1..50}; do
      kill -0 "$SERVER_PID" 2>/dev/null || break
      sleep 0.1
    done
    kill -KILL "$SERVER_PID" 2>/dev/null || true
  fi
  if [[ "$AUTO_WORKDIR" = "1" ]]; then
    WORKDIR_TO_REMOVE="$WORKDIR" python3 - <<'PY'
import os
import shutil
shutil.rmtree(os.environ["WORKDIR_TO_REMOVE"], ignore_errors=True)
PY
  fi
}
trap cleanup EXIT INT TERM

WORKDIR="$WORKDIR" BASELINE="$BASELINE" VERSION="$VERSION" BUILD_ID="$BUILD_ID" TASK_ID="$TASK_ID" HTTP_PORT="$HTTP_PORT" SIP_PORT="$SIP_PORT" python3 - <<'PY'
import hashlib
import json
import os
import pathlib
import sqlite3
from datetime import datetime, timezone

root = pathlib.Path(os.environ["WORKDIR"])
(root / "config").mkdir(parents=True)
(root / "data").mkdir(parents=True)
(root / "resource" / "public").mkdir(parents=True)
(root / "resource" / "logs").mkdir(parents=True)
(root / "resource" / "public" / "uploads").mkdir(parents=True)

db_path = root / "data" / "uvp.db"
with sqlite3.connect(db_path) as connection:
    connection.executescript(pathlib.Path(os.environ["BASELINE"]).read_text(encoding="utf-8"))
    connection.execute("CREATE TABLE gb_schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)")
    connection.execute(
        "INSERT INTO gb_schema_migrations (version, applied_at) VALUES (?, ?)",
        ("fixture-baseline", "2026-10-16T00:00:00Z"),
    )
    connection.commit()
    snapshot = {}
    for table in ("sys_jobs", "sys_job_results", "gb_device", "gb_channel", "gb_sip_config", "gb_cascade_platform", "gb_schema_migrations"):
        snapshot[table] = connection.execute("SELECT COUNT(*) FROM \"%s\"" % table).fetchone()[0]
    versions = [row[0] for row in connection.execute("SELECT version FROM gb_schema_migrations ORDER BY version")]

digest = hashlib.sha256("\x00".join(["sqlite", *versions]).encode()).hexdigest()
(root / "candidate.before.json").write_text(json.dumps(snapshot, sort_keys=True) + "\n", encoding="utf-8")
(root / "migration.digest").write_text(digest + "\n", encoding="utf-8")

config = f'''server:
  appdebug: false
  cachetype: memory
  syslog: false
  notcheckuser: []
system:
  systemname: UVP candidate fixture
safe:
  loginlockthreshold: 3
  loginlockexpire: 60
  loginlockduration: 600
  minpasswordlength: 8
  requirespecialchar: false
captcha:
  open: false
  length: 4
httpserver:
  port: "127.0.0.1:{os.environ["HTTP_PORT"]}"
  allowcrossdomain: false
  serverrootpath: /public
  serverroot: ./resource/public
  read_timeout: 5
  write_timeout: 30
  idle_timeout: 30
  handler_timeout: 30
  trustedproxies: []
processauthority:
  state_dir: ""
upgrade:
  deployment: standalone
  release_source: ""
openapi:
  enabled: false
  play_enabled: false
token:
  jwttokensignkey: candidate-fixture-secret
  jwttokenexpire: 3600
  jwttokenrefreshexpire: 7200
  cachekeyprefix: "uvp-candidate:"
  isCache: false
logs:
  outputs: [stdout]
  stdoutformat: console
  level: error
  routes: false
  maxsize: 5
  maxbackups: 1
  maxage: 1
redis:
  host: 127.0.0.1
  port: 6379
  password: ""
  indexdb: 1
casbin:
  autoloadpolicyseconds: 120
  tableprefix: sys_
  tablename: casbin_rule
  modelconfig: |
    [request_definition]
    r = sub, obj, act, dom
    [policy_definition]
    p = sub, obj, act, dom
    [role_definition]
    g = _, _, _
    [policy_effect]
    e = some(where (p.eft == allow))
    [matchers]
    m = g(r.sub, p.sub, r.dom) && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act) && (r.dom == p.dom || p.dom == "*")
gormv2:
  usedbtype: sqlite
  mysql:
    isinitglobalgormmysql: 0
  sqlserver:
    isinitglobalgormsqlserver: 0
  postgresql:
    isinitglobalgormpostgresql: 0
  sqlite:
    isinitglobalgormsqlite: 1
    write:
      database: data/uvp.db
upload:
  upload_type: local
  max_size: 10
  local_path: ./resource/public/uploads
  allowed_types: [.jpg, .png, .txt]
  chunk_max_size: 5120
  max_chunk_size: 5
  chunk_allowed_types: [.jpg, .png, .txt]
scheduler:
  job_results_buffer_size: 1000
gb28181:
  enabled: false
  sip_command_timeout_sec: 10
  sip:
    ip: 127.0.0.1
    advertiseip: 127.0.0.1
    port: {os.environ["SIP_PORT"]}
    transport: [udp, tcp]
    domain: 3402000000
    serverid: 34020000002000000001
    password: fixture
  device:
    keepalive_interval: 60
    keepalive_timeout_count: 3
    keepalive_grace_seconds: 15
    offline_scan_interval: 30
    sync_channels_on_online: false
    online_on_heartbeat: true
  record_query:
    timezone: Asia/Shanghai
    timeout_sec: 15
    max_range_hours: 24
    max_active_queries: 8
    max_records_per_query: 100
    result_ttl_sec: 60
  playback:
    media_wait_sec: 2
    idle_timeout_sec: 10
    max_session_sec: 60
  play:
    reconcile_interval_sec: 0
    request_timeout_ms: 1000
  recording:
    plan_enabled: false
    reconcile_interval_sec: 0
  zlm:
    host: 127.0.0.1
    receivehost: 127.0.0.1
    playbackhost: 127.0.0.1
    httpport: 18080
    secret: fixture
    rtpport: 40000
  media:
    hookhost: 127.0.0.1
    hookport: 8280
'''
(root / "config" / "config.yml").write_text(config, encoding="utf-8")
(root / "version.json").write_text(json.dumps({"name": "uvp-gb28181", "version": os.environ["VERSION"], "build_id": os.environ["BUILD_ID"]}) + "\n", encoding="utf-8")

gate_dir = root / "updater-gate"
gate_dir.mkdir()
gate_dir.chmod(0o750)
gate = gate_dir / "gate.json"
gate.write_text(json.dumps({"mode": "candidate", "task_id": os.environ["TASK_ID"], "build_id": os.environ["BUILD_ID"], "updated_at": datetime.now(timezone.utc).isoformat()}) + "\n", encoding="utf-8")
gate.chmod(0o640)
PY

request_status() {
  local url="$1" method="$2" body="$3" output="$4"
  URL="$url" METHOD="$method" BODY="$body" OUTPUT="$output" python3 - <<'PY'
import os
import urllib.error
import urllib.request

request = urllib.request.Request(
    os.environ["URL"],
    data=os.environ["BODY"].encode() if os.environ["BODY"] else None,
    method=os.environ["METHOD"],
    headers={"Content-Type": "application/json", "Accept": "application/json"},
)
status = 0
payload = b""
try:
    with urllib.request.urlopen(request, timeout=3) as response:
        status = response.status
        payload = response.read()
except urllib.error.HTTPError as error:
    status = error.code
    payload = error.read()
except Exception as error:
    payload = str(error).encode()
with open(os.environ["OUTPUT"], "wb") as output:
    output.write(payload)
print(status)
PY
}

assert_no_listener() {
  PORT="$1" python3 - <<'PY'
import os
import socket

port = int(os.environ["PORT"])
for sock_type, label in ((socket.SOCK_STREAM, "TCP"), (socket.SOCK_DGRAM, "UDP")):
    with socket.socket(socket.AF_INET, sock_type) as sock:
        try:
            sock.bind(("127.0.0.1", port))
        except OSError as error:
            raise SystemExit("SIP %s port %d is already listening: %s" % (label, port, error))
PY
}

wait_for_status() {
  local url="$1" wanted="$2" timeout="$3" output="$4"
  local started now status
  started="$(date +%s)"
  while :; do
    status="$(request_status "$url" GET "" "$output")"
    [[ "$status" = "$wanted" ]] && return 0
    if [[ -n "$SERVER_PID" ]] && ! kill -0 "$SERVER_PID" 2>/dev/null; then
      printf 'backend exited while waiting for %s (HTTP %s)\n' "$wanted" "$status" >&2
      tail -n 80 "$SERVER_STDERR" >&2 || true
      tail -n 40 "$SERVER_STDOUT" >&2 || true
      return 1
    fi
    now="$(date +%s)"
    (( now - started >= timeout )) && return 1
    sleep 0.2
  done
}

cd "$WORKDIR"
UVP_VERSION_PATH="$WORKDIR/version.json" \
UVP_UPDATER_GATE_PATH="$WORKDIR/updater-gate/gate.json" \
UVP_PROCESS_AUTHORITY_DIR="$WORKDIR/data/process-authority" \
  "$BINARY" >"$SERVER_STDOUT" 2>"$SERVER_STDERR" &
SERVER_PID=$!

BASE_URL="http://127.0.0.1:$HTTP_PORT"
READINESS_URL="$BASE_URL/internal/updater/readiness"
wait_for_status "$READINESS_URL" 200 "$STARTUP_TIMEOUT" "$READINESS_BODY" || fail "candidate readiness did not become available"

EXPECTED_DIGEST="$(cat "$WORKDIR/migration.digest")"
python3 - "$READINESS_BODY" "$VERSION" "$BUILD_ID" "$TASK_ID" "$EXPECTED_DIGEST" <<'PY'
import json
import sys

payload = json.loads(open(sys.argv[1], encoding="utf-8").read())
expected = {
    "version": sys.argv[2],
    "build_id": sys.argv[3],
    "task_id": sys.argv[4],
    "database_ready": True,
    "migration_digest": sys.argv[5],
    "database_dialect": "sqlite",
}
for key, value in expected.items():
    if payload.get(key) != value:
        raise SystemExit("candidate readiness %s = %r, want %r" % (key, payload.get(key), value))
print("candidate readiness identity verified")
PY

assert_no_listener "$SIP_PORT"

status="$(request_status "$BASE_URL/api/config/get" GET "" "$WORKDIR/public.candidate.body")"
[[ "$status" = "503" ]] || fail "candidate public ingress returned HTTP $status, want 503"
status="$(request_status "$BASE_URL/api/login" POST '{"username":"candidate","password":"candidate"}' "$WORKDIR/login.candidate.body")"
[[ "$status" = "503" ]] || fail "candidate business write returned HTTP $status, want 503"

python3 - "$WORKDIR/data/uvp.db" "$WORKDIR/candidate.before.json" <<'PY'
import json
import sqlite3
import sys

db, snapshot_path = sys.argv[1:]
expected = json.load(open(snapshot_path, encoding="utf-8"))
with sqlite3.connect(db) as connection:
    for table, wanted in expected.items():
        actual = connection.execute("SELECT COUNT(*) FROM \"%s\"" % table).fetchone()[0]
        if actual != wanted:
            raise SystemExit("candidate startup changed %s count from %d to %d" % (table, wanted, actual))
    if expected["sys_jobs"] != 0:
        raise SystemExit("fixture must start with zero system jobs")
print("candidate startup left business tables and migration ledger unchanged")
PY

WORKDIR="$WORKDIR" python3 - <<'PY'
import json
import os
import pathlib
from datetime import datetime, timezone

root = pathlib.Path(os.environ["WORKDIR"])
gate = root / "updater-gate" / "gate.json"
temporary = gate.with_name(".gate.json.normal.tmp")
temporary.write_text(json.dumps({"mode": "normal", "updated_at": datetime.now(timezone.utc).isoformat()}) + "\n", encoding="utf-8")
temporary.chmod(0o640)
os.replace(temporary, gate)
PY

wait_for_status "$BASE_URL/api/config/get" 200 "$STARTUP_TIMEOUT" "$NORMAL_BODY" || fail "business ingress did not recover after clearing candidate gate"
status="$(request_status "$BASE_URL/api/login" POST '{"username":"candidate","password":"candidate"}' "$WORKDIR/login.normal.body")"
[[ "$status" != "503" && "$status" != "0" ]] || fail "business write remained blocked after gate clear (HTTP $status)"
status="$(request_status "$READINESS_URL" GET "" "$WORKDIR/readiness.normal.body")"
[[ "$status" = "503" ]] || fail "candidate readiness remained exposed after gate clear (HTTP $status)"

python3 - "$WORKDIR/data/uvp.db" <<'PY'
import sqlite3
import sys

with sqlite3.connect(sys.argv[1]) as connection:
    jobs = connection.execute("SELECT COUNT(*) FROM sys_jobs").fetchone()[0]
if jobs <= 0:
    raise SystemExit("business jobs were not initialized after gate clear")
print("business jobs initialized only after gate clear: %d" % jobs)
PY

assert_no_listener "$SIP_PORT"
printf 'upgrade candidate lifecycle verified: %s\n' "$WORKDIR"
printf 'candidate readiness: %s\n' "$READINESS_BODY"
printf 'backend logs: %s, %s\n' "$SERVER_STDOUT" "$SERVER_STDERR"
