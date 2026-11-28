#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/verify-upgrade-preflight.sh --binary /absolute/path/to/uvp-gb28181 [options]

Options:
  --binary PATH       Release-built backend executable to verify (required).
  --build-id VALUE    Embedded build ID used to build the executable.
                      Default: preflight-fixture-build
  --version VALUE     Version written to fixture version.json.
                      Default: 0.0.0-preflight
  --workdir PATH      Empty directory used for the isolated fixture. It is
                      retained after the check for inspection.
  -h, --help          Show this help.

Build a Linux fixture binary, then run the check:
  cd server
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
    -ldflags '-X uvplatform.com/uvp-gb28181/app/global/app.BuildID=preflight-fixture-build' \
    -o /tmp/uvp-preflight/uvp-gb28181 .
  cd ..
  scripts/verify-upgrade-preflight.sh \
    --binary /tmp/uvp-preflight/uvp-gb28181 \
    --workdir /tmp/uvp-upgrade-preflight-fixture
USAGE
}

fail() {
  printf 'verify-upgrade-preflight: %s\n' "$*" >&2
  exit 1
}

BINARY=""
BUILD_ID="preflight-fixture-build"
VERSION="0.0.0-preflight"
WORKDIR=""
AUTO_WORKDIR=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)
      [[ $# -ge 2 ]] || fail "--binary requires a path"
      BINARY="$2"
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
    --workdir)
      [[ $# -ge 2 ]] || fail "--workdir requires a path"
      WORKDIR="$2"
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
BINARY="$(cd "$(dirname "$BINARY")" && pwd)/$(basename "$BINARY")"
[[ -n "$BUILD_ID" ]] || fail "--build-id must not be empty"
[[ -n "$VERSION" ]] || fail "--version must not be empty"
command -v python3 >/dev/null 2>&1 || fail "python3 is required to create and inspect the SQLite fixture"

if [[ -z "$WORKDIR" ]]; then
  WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/uvp-upgrade-preflight.XXXXXX")"
  AUTO_WORKDIR=1
else
  [[ ! -e "$WORKDIR" ]] || [[ -d "$WORKDIR" ]] || fail "--workdir is not a directory: $WORKDIR"
  mkdir -p "$WORKDIR"
  WORKDIR="$(cd "$WORKDIR" && pwd)"
  [[ -z "$(find "$WORKDIR" -mindepth 1 -maxdepth 1 -print -quit)" ]] || fail "--workdir must be empty: $WORKDIR"
fi

BEFORE_MANIFEST="$(mktemp "${TMPDIR:-/tmp}/uvp-upgrade-preflight-before.XXXXXX")"
AFTER_MANIFEST="$(mktemp "${TMPDIR:-/tmp}/uvp-upgrade-preflight-after.XXXXXX")"
STDOUT_FILE="$(mktemp "${TMPDIR:-/tmp}/uvp-upgrade-preflight-stdout.XXXXXX")"
STDERR_FILE="$(mktemp "${TMPDIR:-/tmp}/uvp-upgrade-preflight-stderr.XXXXXX")"

cleanup() {
  rm -f -- "$BEFORE_MANIFEST" "$AFTER_MANIFEST" "$STDOUT_FILE" "$STDERR_FILE"
  if [[ "$AUTO_WORKDIR" = "1" ]]; then
    rm -rf -- "$WORKDIR"
  fi
}
trap cleanup EXIT

mkdir -p "$WORKDIR/config" "$WORKDIR/data"

WORKDIR="$WORKDIR" BUILD_ID="$BUILD_ID" VERSION="$VERSION" python3 - <<'PY'
import json
import os
import sqlite3

root = os.environ["WORKDIR"]
db_path = os.path.join(root, "data", "preflight.db")
with open(os.path.join(root, "config", "config.yml"), "w", encoding="utf-8") as config:
    config.write("""gormv2:
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
      database: data/preflight.db
""")

connection = sqlite3.connect(db_path)
connection.execute("CREATE TABLE gb_schema_migrations (version TEXT NOT NULL, applied_at DATETIME)")
connection.execute(
    "INSERT INTO gb_schema_migrations (version, applied_at) VALUES (?, ?)",
    ("2026-10-16-preflight-fixture.sql", "2026-10-16T00:00:00Z"),
)
connection.execute("CREATE TABLE preflight_sentinel (id INTEGER PRIMARY KEY, value TEXT NOT NULL)")
connection.execute("INSERT INTO preflight_sentinel (id, value) VALUES (1, 'must-not-change')")
connection.commit()
connection.close()

with open(os.path.join(root, "version.json"), "w", encoding="utf-8") as version:
    json.dump(
        {
            "name": "uvp-gb28181",
            "version": os.environ["VERSION"],
            "build_id": os.environ["BUILD_ID"],
        },
        version,
    )
    version.write("\n")
PY

python3 - "$WORKDIR" "$BEFORE_MANIFEST" <<'PY'
import hashlib
import json
import os
import sys

root, destination = sys.argv[1:]
manifest = {}
for directory, _, files in os.walk(root):
    for name in sorted(files):
        path = os.path.join(directory, name)
        relative = os.path.relpath(path, root)
        with open(path, "rb") as source:
            manifest[relative] = hashlib.sha256(source.read()).hexdigest()
with open(destination, "w", encoding="utf-8") as output:
    json.dump(manifest, output, sort_keys=True)
PY

python3 - "$BINARY" "$WORKDIR" "$STDOUT_FILE" "$STDERR_FILE" <<'PY'
import os
import signal
import subprocess
import sys

binary, root, stdout_path, stderr_path = sys.argv[1:]
environment = os.environ.copy()
environment["UVP_VERSION_PATH"] = os.path.join(root, "version.json")
process = subprocess.Popen(
    [binary, "-upgrade-preflight"],
    cwd=root,
    env=environment,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    start_new_session=True,
)
try:
    stdout, stderr = process.communicate(timeout=20)
except subprocess.TimeoutExpired:
    os.killpg(process.pid, signal.SIGKILL)
    process.wait()
    raise SystemExit("upgrade preflight timed out")

with open(stdout_path, "wb") as output:
    output.write(stdout)
with open(stderr_path, "wb") as output:
    output.write(stderr)

if process.returncode != 0:
    sys.stderr.buffer.write(stderr)
    raise SystemExit("upgrade preflight exited with status %d" % process.returncode)

try:
    os.killpg(process.pid, 0)
except ProcessLookupError:
    pass
else:
    os.killpg(process.pid, signal.SIGKILL)
    raise SystemExit("upgrade preflight left a background process in its process group")
PY

python3 - "$STDOUT_FILE" "$VERSION" "$BUILD_ID" <<'PY'
import json
import re
import sys

path, version, build_id = sys.argv[1:]
payload = open(path, "r", encoding="utf-8").read()
decoder = json.JSONDecoder()
try:
    value, end = decoder.raw_decode(payload.lstrip())
except json.JSONDecodeError as error:
    raise SystemExit("upgrade preflight stdout is not JSON: %s" % error)
if payload.lstrip()[end:].strip():
    raise SystemExit("upgrade preflight stdout contains more than one JSON value")
if not isinstance(value, dict):
    raise SystemExit("upgrade preflight stdout is not a JSON object")
expected = {
    "current_version": version,
    "current_build_id": build_id,
    "database_dialect": "sqlite",
}
for key, wanted in expected.items():
    if value.get(key) != wanted:
        raise SystemExit("upgrade preflight %s = %r, want %r" % (key, value.get(key), wanted))
for key in ("config_digest", "migration_digest"):
    if not re.fullmatch(r"[0-9a-f]{64}", value.get(key, "")):
        raise SystemExit("upgrade preflight %s is not a SHA-256 digest" % key)
PY

python3 - "$WORKDIR" "$BEFORE_MANIFEST" "$AFTER_MANIFEST" <<'PY'
import hashlib
import json
import os
import sys

root, source, destination = sys.argv[1:]
with open(source, "r", encoding="utf-8") as input_file:
    before = json.load(input_file)
after = {}
for directory, _, files in os.walk(root):
    for name in sorted(files):
        path = os.path.join(directory, name)
        relative = os.path.relpath(path, root)
        with open(path, "rb") as input_file:
            after[relative] = hashlib.sha256(input_file.read()).hexdigest()
if before != after:
    raise SystemExit("upgrade preflight changed the isolated fixture tree")
for suffix in ("-wal", "-shm", "-journal"):
    if os.path.exists(os.path.join(root, "data", "preflight.db" + suffix)):
        raise SystemExit("upgrade preflight created SQLite sidecar " + suffix)
with open(destination, "w", encoding="utf-8") as output:
    json.dump(after, output, sort_keys=True)
PY

if [[ "$AUTO_WORKDIR" = "0" ]]; then
  cp "$STDOUT_FILE" "$WORKDIR/preflight.stdout.json"
  cp "$STDERR_FILE" "$WORKDIR/preflight.stderr.log"
  cp "$BEFORE_MANIFEST" "$WORKDIR/preflight.before.json"
  cp "$AFTER_MANIFEST" "$WORKDIR/preflight.after.json"
fi

printf 'upgrade preflight verified: %s\n' "$WORKDIR"
printf 'fixture database: %s\n' "$WORKDIR/data/preflight.db"
