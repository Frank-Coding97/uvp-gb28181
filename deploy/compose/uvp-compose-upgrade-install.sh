#!/usr/bin/env bash
set -Eeuo pipefail
umask 027

# Register one Compose project with the host-side updater. The helper is kept
# outside application containers so it remains available while a release stops
# and recreates those containers.
ROOT="${UVP_ROOT:-/opt/uvp-gb28181}"
# ⛔⛔ 容器版的编排文件就放在 $ROOT 根下，**没有 current/ 目录**：
#   current/ 是绿色包/systemd 那套「多版本并存 + 软链切换」的布局，
#   compose 版是 compose.yml + .env 平铺。原来这里默认为 $ROOT/current，
#   在容器版上必然报 "UVP_COMPOSE_PROJECT_DIR must be a real directory"。
PROJECT_DIR="${UVP_COMPOSE_PROJECT_DIR:-$ROOT}"
COMPOSE_FILE="${UVP_COMPOSE_FILE:-$PROJECT_DIR/compose.yml}"
# ⛔ 容器版的库固定在 $ROOT/data/database/uvp.db（compose 把该目录挂到 /app/data，
#   config.yml 里写的是 ./data/uvp.db）。留空的话回滚阶段会"没有库可恢复"，
#   而升级中途失败的恢复恰恰最需要它。
SQLITE_DATABASE="${UVP_COMPOSE_SQLITE_DATABASE:-$ROOT/data/database/uvp.db}"
PROJECT="${UVP_COMPOSE_PROJECT:-uvp-gb28181}"
MIGRATION_SERVICE="${UVP_COMPOSE_MIGRATION_SERVICE:-backend}"
# ⛔⛔ readiness 探针必须落在**后端容器对外暴露的那个端口**上（容器用 host 网络，
#   所以宿主 127.0.0.1:后端端口 就是它）。原来写死 56001 是另一套内部拓扑的端口，
#   在容器版上探针永远连不通 ⇒ 升级做完 Validate 阶段超时 ⇒ 回滚。
BACKEND_PORT="${UVP_BACKEND_PORT:-51002}"
READINESS_URL="${UVP_COMPOSE_READINESS_URL:-http://127.0.0.1:${BACKEND_PORT}/internal/updater/readiness}"
BUSINESS_UID="${UVP_BUSINESS_UID:-10001}"
BUSINESS_GID="${UVP_BUSINESS_GID:-10001}"
# ⛔⛔ socket 对端校验的是「连上来的进程 UID」，而容器里的后端是用 BUSINESS_UID
#   跑的（不是 root）。原来默认 0 ⇒ 容器发来的每个请求都会被判
#   "peer uid not allowed"，表现是页面里「检查更新/导入」全部失败，
#   而 service 状态是 active、socket 也在，看不出是被授权规则挡了。
ALLOWED_UID="${UVP_UPDATER_ALLOWED_UID:-$BUSINESS_UID}"
SOCKET_DIR="${UVP_UPDATER_SOCKET_DIR:-/run/uvp-updater}"
UPDATER_UNIT="${UVP_UPDATER_UNIT:-uvp-updater.service}"
UPDATER_STATE=""
UPDATER_BIN=""
UPDATER_REGISTRATION=""

log() {
  printf '[uvp-compose-upgrade] %s\n' "$*"
}

fail() {
  printf '[uvp-compose-upgrade][ERROR] %s\n' "$*" >&2
  exit 1
}

require_absolute_directory() {
  local value="$1" label="$2"
  [[ "$value" == /* ]] || fail "$label must be an absolute path"
  [[ -d "$value" && ! -L "$value" ]] || fail "$label must be a real directory: $value"
  case "$value" in
    *$'\n'*|*$'\r'*|*[[:space:]]*) fail "$label must not contain whitespace" ;;
  esac
}

[[ "$(id -u)" == "0" ]] || fail "run this installer as root"
command -v docker >/dev/null 2>&1 || fail "docker is required"
command -v systemctl >/dev/null 2>&1 || fail "systemd is required for the host updater"
[[ "$ALLOWED_UID" =~ ^[0-9]+$ ]] || fail "UVP_UPDATER_ALLOWED_UID must be a numeric host UID"
[[ "$BUSINESS_UID" =~ ^[1-9][0-9]*$ && "$BUSINESS_GID" =~ ^[1-9][0-9]*$ ]] || fail "UVP_BUSINESS_UID/GID must be positive numeric host IDs"
[[ "$SOCKET_DIR" == /* && "$SOCKET_DIR" != *[[:space:]]* ]] || fail "UVP_UPDATER_SOCKET_DIR must be an absolute path without whitespace"
[[ "$UPDATER_UNIT" =~ ^[A-Za-z0-9_.@-]+\.service$ ]] || fail "UVP_UPDATER_UNIT must be a service unit name"

require_absolute_directory "$ROOT" "UVP_ROOT"
require_absolute_directory "$PROJECT_DIR" "UVP_COMPOSE_PROJECT_DIR"
[[ "$COMPOSE_FILE" == /* && -f "$COMPOSE_FILE" && ! -L "$COMPOSE_FILE" ]] \
  || fail "UVP_COMPOSE_FILE must be a real absolute file: $COMPOSE_FILE"
case "$COMPOSE_FILE" in
  *$'\n'*|*$'\r'*|*[[:space:]]*) fail "UVP_COMPOSE_FILE must not contain whitespace" ;;
esac
[[ -f "$ROOT/config/config.yml" && ! -L "$ROOT/config/config.yml" ]] \
  || fail "application config is missing: $ROOT/config/config.yml"

UPDATER_STATE="$ROOT/data/updater-state"
UPDATER_BIN="$ROOT/updater/uvp-updater"
UPDATER_REGISTRATION="$ROOT/config/updater-registration.json"

docker compose --project-directory "$PROJECT_DIR" --project-name "$PROJECT" -f "$COMPOSE_FILE" config --quiet

if [[ ! -x "$UPDATER_BIN" ]]; then
  [[ -x "$PROJECT_DIR/updater/uvp-updater" ]] \
    || fail "stable updater is missing: $UPDATER_BIN (the first Compose release must contain updater/uvp-updater)"
  install -d -m 0755 "$ROOT/updater"
  install -m 0755 "$PROJECT_DIR/updater/uvp-updater" "$UPDATER_BIN"
fi
[[ -f "$UPDATER_BIN" && ! -L "$UPDATER_BIN" ]] || fail "updater binary must be a real file: $UPDATER_BIN"
install -d -m 0700 "$UPDATER_STATE"
install -d -m 0750 -o root -g "$BUSINESS_GID" "$SOCKET_DIR"

registration_tmp="$(mktemp "$ROOT/config/.updater-registration.XXXXXX")"
cleanup() {
  rm -f "$registration_tmp"
}
trap cleanup EXIT

"$UPDATER_BIN" register \
  --config-dir "$ROOT/config" \
  --registration "$registration_tmp" \
  --deployment compose \
  --root "$ROOT" \
  --state "$UPDATER_STATE" \
  --project-dir "$PROJECT_DIR" \
  --project "$PROJECT" \
  --migration-service "$MIGRATION_SERVICE" \
  --compose-file "$COMPOSE_FILE" \
  --sqlite-database "$SQLITE_DATABASE" \
  --readiness-url "$READINESS_URL" \
  --allow-uid "$ALLOWED_UID" \
  --business-uid "$BUSINESS_UID" \
  --business-gid "$BUSINESS_GID" \
  --external-backup-max-bytes "${UVP_UPGRADE_EXTERNAL_BACKUP_MAX_BYTES:-8589934592}"
chmod 0600 "$registration_tmp"
mv -f "$registration_tmp" "$UPDATER_REGISTRATION"
trap - EXIT

unit_path="/etc/systemd/system/$UPDATER_UNIT"
unit_tmp="$(mktemp)"
cleanup_unit() {
  rm -f "$unit_tmp"
}
trap cleanup_unit EXIT
cat > "$unit_tmp" <<UNIT
[Unit]
Description=UVP GB28181 Docker Compose upgrade helper
After=local-fs.target docker.service
Wants=docker.service

[Service]
Type=simple
User=root
Group=root
Environment=TZ=Asia/Shanghai
ExecStart=$UPDATER_BIN serve --socket $SOCKET_DIR/updater.sock --registration $UPDATER_REGISTRATION
Restart=on-failure
RestartSec=3s
RuntimeDirectoryPreserve=yes
NoNewPrivileges=false
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=$ROOT $SOCKET_DIR

[Install]
WantedBy=multi-user.target
UNIT
install -m 0644 "$unit_tmp" "$unit_path"

systemctl daemon-reload
systemctl enable "$UPDATER_UNIT"
systemctl restart "$UPDATER_UNIT"
systemctl is-active --quiet "$UPDATER_UNIT" || fail "updater helper is not active"

for _ in {1..50}; do
  [[ -S "$SOCKET_DIR/updater.sock" ]] && break
  sleep 0.1
done
[[ -S "$SOCKET_DIR/updater.sock" ]] || fail "updater socket was not created"

gate_dir="$ROOT/updater-gate"
gate="$gate_dir/gate.json"
[[ "$(stat -c '%a:%u:%g' "$gate_dir")" == "750:0:$BUSINESS_GID" ]] \
  || fail "updater gate directory was not protected as root:$BUSINESS_GID mode 0750"
[[ "$(stat -c '%a:%u:%g' "$gate")" == "640:0:$BUSINESS_GID" ]] \
  || fail "updater gate file was not protected as root:$BUSINESS_GID mode 0640"

log "Compose project is registered; start it with the registered docker compose command"
