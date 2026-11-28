#!/usr/bin/env bash
# UVP-GB28181 绿色安装包 —— 把本包注册成 systemd 服务
#
# 用法（在**包根目录**执行，与 uvp-gb28181-ctl.sh 同级）：
#   ./uvp-gb28181-service-install.sh          注册 + 开机自启（**不**立即启动）
#   ./uvp-gb28181-service-install.sh --now    注册并立即接管（先停手工实例再启动）
#   ./uvp-gb28181-service-uninstall.sh        卸载（只摘服务注册，不动数据）
#
# 非 root 运行会自动用 sudo 重新执行；装出来的服务名是 uvp-gb28181。
#
# ⛔⛔ 四条设计约束（改这个脚本前先读完，全是踩过的）：
#
#   1. **服务以「安装者」身份跑，不能写死 root。**
#      手工 `./uvp-gb28181-ctl.sh start` 时，进程属当前用户、data/ 与 logs/ 也归当前用户。
#      unit 里若不写 User=，systemd 默认以 root 跑 ⇒ 同一份 data/ 的属主在两种启动方式
#      之间来回跳，之后就出现「能读不能写」，而现象是页面报错，跟权限毫无关联。
#      ⇒ 这里取 SUDO_USER（由 sudo 触发时）否则取当前用户，写进 unit 的 User=。
#
#   2. **Type=oneshot + RemainAfterExit=yes。**
#      ctl start 的动作是「把四个后台进程拉起来，然后自己退出」，它不是常驻进程。
#      用 Type=simple/forking 会让 systemd 以为主进程已死 ⇒ status 一直 inactive/failed。
#      oneshot 的语义正好：跑一条命令改变状态，进程由脚本自己管。
#      ⛔ 连带副作用：oneshot **不允许 Restart=**（systemd 会直接拒绝整个 unit），别加。
#
#   3. **install 默认只 enable、不 start。**
#      现场极可能正手工跑着这套服务，直接 start 会撞端口（ctl 的端口预检会拒绝启动）。
#      要立即接管用 --now，它会先把手工实例停掉。
#
#   4. **凡是调用 ctl 的动作，都必须以 User= 的身份执行。**
#      ctl 一开头就跑目录迁移 + 建目录（logs/app、logs/zlm…）。以 root 调它，
#      这些目录的属主会变成 root，之后服务以普通用户跑就写不进日志。
#      ⇒ 本脚本自己**绝不以 root 直接调 ctl**；--now 里那次 stop 用 as_user 降权。

set -Eeuo pipefail

UNIT_NAME="uvp-gb28181.service"
UNIT_PATH="/etc/systemd/system/${UNIT_NAME}"
UPDATER_UNIT_NAME="uvp-gb28181-updater.service"
UPDATER_UNIT_PATH="/etc/systemd/system/${UPDATER_UNIT_NAME}"
# 主机防火墙代理通过 systemd 获得管理 nftables 所需的能力
# ⇒ 单独一个 unit。名字必须与 uvp-gb28181-ctl.sh 里的 FIREWALL_AGENT_UNIT_NAME 逐字一致。
FIREWALL_UNIT_NAME="uvp-gb28181-firewall-agent.service"
FIREWALL_UNIT_PATH="/etc/systemd/system/${FIREWALL_UNIT_NAME}"

log()  { printf '[uvp-service] %s\n' "$*"; }
fail() { printf '[uvp-service][ERROR] %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'USAGE'
用法：./uvp-gb28181-service-install.sh [--now | --firewall-only]

  （无参数）   注册为 systemd 服务并设为开机自启，不立即启动
  --now       注册后立即接管：先停掉手工跑着的实例，再 systemctl start
  --firewall-only  只注册并启动主机防火墙代理
  -h, --help  显示本帮助

卸载：./uvp-gb28181-service-uninstall.sh
USAGE
}

WANT_NOW=0
FIREWALL_ONLY=0
while [ $# -gt 0 ]; do
  case "$1" in
    --now)     WANT_NOW=1 ;;
    --firewall-only) FIREWALL_ONLY=1 ;;
    -h|--help) usage; exit 0 ;;
    *)         fail "未知参数：$1（只支持 --now、--firewall-only 与 -h）" ;;
  esac
  shift
done
if [ "$WANT_NOW" = 1 ] && [ "$FIREWALL_ONLY" = 1 ]; then
  fail "--now 与 --firewall-only 不能同时使用"
fi

SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
ROOT="$(dirname "$SELF")"
CTL="$ROOT/uvp-gb28181-ctl.sh"

# ---------------------------------------------------------- 提权（sudo 重入）----
# ⛔ 用 exec 重入而不是 `sudo "$0" "$@"`：这样 sudo 会设好 SUDO_USER，
#   下面据此决定「服务以谁的身份跑」（见头部约束 1）。
if [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 \
    || fail "需要 root 权限，而本机没有 sudo —— 请直接用 root 账号执行"
  log "需要 root 权限，正在通过 sudo 重新执行…"
  exec sudo -- "$SELF" "$@"
fi

# ------------------------------------------------------------ 前置检查 ----
[ -f "$CTL" ] || fail "找不到 ${CTL} —— 本脚本必须与 uvp-gb28181-ctl.sh 放在同一个目录（包根）里执行"
[ -x "$CTL" ] || fail "${CTL} 没有可执行权限，请先执行 chmod +x"
command -v systemctl >/dev/null 2>&1 \
  || fail "本机没有 systemctl —— 这不是 systemd 系统，无法注册为服务（请继续用 uvp-gb28181-ctl.sh 启停）"
[ -d /run/systemd/system ] \
  || fail "systemd 未作为 init 运行（/run/systemd/system 不存在）—— 容器内通常如此，无法注册系统服务"

RUN_USER="${SUDO_USER:-$(id -un)}"
if [ "$FIREWALL_ONLY" = 1 ] && [ -d "$ROOT/run" ]; then
  RUN_USER="$(stat -c %U "$ROOT/run")"
fi
[ -n "$RUN_USER" ] || RUN_USER="root"
id "$RUN_USER" >/dev/null 2>&1 \
  || fail "解析出的运行用户「${RUN_USER}」在本机不存在 —— 请改用 sudo -u <用户名> 执行本脚本"
RUN_GROUP="$(id -gn "$RUN_USER")"

log "安装目录：${ROOT}"
log "运行身份：${RUN_USER}"

# ⛔ 路径含空格时：ExecStart= 能用引号兜住，但 WorkingDirectory= 不行（见 unit 里的注释）
#   ⇒ 与其让 systemd 抛一个看不懂的 bad-setting，不如在这里先说清楚。
case "$ROOT" in
  *[[:space:]]*)
    log "⚠️ 安装路径含空格（${ROOT}）：服务单元的 WorkingDirectory= 表达不了这种情况，"
    log "   建议把包挪到无空格的路径下再注册服务。"
    ;;
esac

install_firewall_unit() {
  local agent="$ROOT/bin/uvp-firewall-agent" tmp_unit
  if [ -e "$ROOT/current/bin/uvp-firewall-agent" ]; then
    agent="$ROOT/current/bin/uvp-firewall-agent"
  fi
  [ -x "$agent" ] || return 1

  tmp_unit="$(mktemp)"
  cat > "$tmp_unit" <<UNIT
[Unit]
Description=UVP GB28181 主机防火墙代理（nftables）
After=local-fs.target
Before=${UNIT_NAME}

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_GROUP}
WorkingDirectory=${ROOT}
ExecStart=/bin/bash "${CTL}" serve-firewall-agent
Restart=on-failure
RestartSec=2s
NoNewPrivileges=true
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNIT
  if ! install -m 0644 "$tmp_unit" "$FIREWALL_UNIT_PATH"; then
    rm -f "$tmp_unit"
    return 1
  fi
  rm -f "$tmp_unit"
  log "已写入主机防火墙代理单元：${FIREWALL_UNIT_PATH}"
}

start_firewall_unit() {
  local socket="$ROOT/run/firewall-agent.sock" attempt
  systemctl enable --now "$FIREWALL_UNIT_NAME" || return 1
  for attempt in {1..30}; do
    if systemctl is-active --quiet "$FIREWALL_UNIT_NAME" && [ -S "$socket" ]; then
      log "主机防火墙代理已启动（${FIREWALL_UNIT_NAME}）"
      return 0
    fi
    sleep 0.2
  done
  systemctl --no-pager --full status "$FIREWALL_UNIT_NAME" >&2 || true
  return 1
}

if [ "$FIREWALL_ONLY" = 1 ]; then
  command -v nft >/dev/null 2>&1 || fail "找不到 nft，请先安装 nftables"
  firewall_unit_root="$(systemctl show -p WorkingDirectory --value "$FIREWALL_UNIT_NAME" 2>/dev/null || true)"
  if [ -n "$firewall_unit_root" ] && [ "$firewall_unit_root" != "$ROOT" ]; then
    fail "防火墙代理已注册到另一个绿色包目录：$firewall_unit_root"
  fi
  if [ -S "$ROOT/run/firewall-agent.sock" ] && ! systemctl is-active --quiet "$FIREWALL_UNIT_NAME" &&
    ss -xlH | grep -F -- "$ROOT/run/firewall-agent.sock" >/dev/null; then
    fail "包内已有前台防火墙代理，请结束该进程后重试"
  fi
  install_firewall_unit || fail "找不到包内防火墙代理"
  systemctl daemon-reload
  start_firewall_unit || fail "主机防火墙代理未就绪"
  exit 0
fi

# --------------------------------------------------- 生成 unit 并落盘 ----
TMP_UNIT="$(mktemp)"
trap 'rm -f "$TMP_UNIT"' EXIT

cat > "$TMP_UNIT" <<UNIT
[Unit]
Description=UVP GB28181 平台（绿色包：nginx + 后端 + Redis + ZLM）
Requires=${UPDATER_UNIT_NAME}
After=${UPDATER_UNIT_NAME}
# 主机防火墙代理要先就绪：后端起 SIP 时会探测它的 socket，先有就能一上线即「内核级封禁可用」。
# ⛔ 用 Wants 而不是 Requires：代理起不来（目标机没 nftables）不该拖死整个平台。
Wants=${FIREWALL_UNIT_NAME}
After=${FIREWALL_UNIT_NAME}
# 要监听端口，等网络就绪更稳（缺这条时可能抢在网络配置完成前启动）
After=network-online.target
Wants=network-online.target

[Service]
# ⛔ oneshot + RemainAfterExit：见脚本头部约束 2，改成 simple/forking 会让 systemd
#   误判服务已死；同时 oneshot 禁用 Restart=，不要加。
Type=oneshot
RemainAfterExit=yes

# ⛔ User= 必须是「安装者」而不是 root：见头部约束 1。
User=${RUN_USER}
# ⛔⛔ WorkingDirectory= 这里**绝对不能加引号**（实测 2026-10-10）：
#   systemd 对 WorkingDirectory= 用的是**原样字符串**，不像 ExecStart= 那样做引号剥离。
#   写成 WorkingDirectory="/opt/x" 会被判
#     「path is not absolute: "/opt/x"」⇒ bad-setting，整个 unit 拒绝启动。
#   （代价：安装路径含空格时这里表达不了 ⇒ 下面单独给了告警。）
WorkingDirectory=${ROOT}

ExecStart=/bin/bash "${CTL}" start
ExecStop=/bin/bash "${CTL}" stop
ExecReload=/bin/bash "${CTL}" restart

# ctl start 要跑迁移、建库、依次拉起四个进程，给足时间（默认 90s 偏紧）
TimeoutStartSec=300
TimeoutStopSec=120
# ⛔ 不设 KillMode=none：ctl stop 若漏杀（ZLM 曾漏），让 systemd 按 cgroup 兜底清理是好事

[Install]
WantedBy=multi-user.target
UNIT

install -m 0644 "$TMP_UNIT" "$UNIT_PATH"
log "已写入服务单元：${UNIT_PATH}"

# 助手必须独立于业务 unit，业务 stop/restart 时继续持有任务、锁和回退能力。
TMP_UPDATER_UNIT="$(mktemp)"
trap 'rm -f "$TMP_UNIT" "$TMP_UPDATER_UNIT"' EXIT
cat > "$TMP_UPDATER_UNIT" <<UNIT
[Unit]
Description=UVP GB28181 服务升级助手
After=local-fs.target
Before=${UNIT_NAME}

[Service]
Type=simple
# The helper must survive and control the business unit while it is stopped.
# It is root-owned; every application command is launched with the registered
# RUN_USER identity by the updater adapter.
User=root
Group=root
WorkingDirectory=${ROOT}
Environment=UVP_UPGRADE_BUSINESS_UID=$(id -u "$RUN_USER")
Environment=UVP_UPGRADE_BUSINESS_GID=$(id -g "$RUN_USER")
Environment=UVP_UPGRADER_ALLOWED_UID=$(id -u "$RUN_USER")
# 前台 exec 到助手，避免 Type=simple 只跟踪到已经退出的后台启动脚本。
ExecStart=/bin/bash "${CTL}" serve-updater
Restart=on-failure
RestartSec=3s
TimeoutStartSec=30s
TimeoutStopSec=45s

[Install]
WantedBy=multi-user.target
UNIT
install -m 0644 "$TMP_UPDATER_UNIT" "$UPDATER_UNIT_PATH"
log "已写入独立升级助手单元：${UPDATER_UNIT_PATH}"

install -d -m 0755 -o "$RUN_USER" -g "$RUN_GROUP" "$ROOT/run" "$ROOT/logs"
if install_firewall_unit; then
  FIREWALL_UNIT_AVAILABLE=1
else
  FIREWALL_UNIT_AVAILABLE=0
  log "包内没有主机防火墙代理，跳过该单元"
fi

# --------------------------------------------------- 生效 + 开机自启 ----
systemctl daemon-reload

systemctl enable "$UPDATER_UNIT_NAME" 2>&1 | sed 's/^/    /' || fail "升级助手 enable 失败"
systemctl enable "$UNIT_NAME" 2>&1 | sed 's/^/    /' || fail "systemctl enable 失败"
log "已设为开机自启"

systemctl start "$UPDATER_UNIT_NAME" 2>&1 | sed 's/^/    /' || fail "升级助手启动失败"

# ---- 主机防火墙代理：清手工实例 → enable --now → 起不来只告警 ----
if [ "${FIREWALL_UNIT_AVAILABLE:-1}" != "0" ]; then
  # ⛔ 手工 start（root 直接跑 ctl）时已有代理占着同一个 socket。systemd 再起一个会
  #   把老进程的 socket 摘掉（代理会先 os.Remove 再 listen）⇒ 两个代理抢同一张 nft 表。
  #   先按 pid 文件结束手工实例，再交给 systemd。
  _fw_pidfile="$ROOT/run/firewall-agent.pid"
  if [ -f "$_fw_pidfile" ]; then
    _fw_pid="$(cat "$_fw_pidfile" 2>/dev/null || true)"
    if [ -n "$_fw_pid" ] && kill -0 "$_fw_pid" 2>/dev/null; then
      log "结束手工启动的防火墙代理（PID ${_fw_pid}），改由 systemd 托管"
      kill -TERM "$_fw_pid" 2>/dev/null || true
      for _ in {1..20}; do kill -0 "$_fw_pid" 2>/dev/null || break; sleep 0.1; done
      kill -KILL "$_fw_pid" 2>/dev/null || true
    fi
    rm -f "$_fw_pidfile"
  fi

  start_firewall_unit || log "主机防火墙代理启动失败，应用层防护可继续运行"
fi

if systemctl is-active --quiet "$UNIT_NAME" 2>/dev/null; then
  log "服务当前处于运行中，单元文件已更新 —— 让改动生效：systemctl restart ${UNIT_NAME}"
fi

# --------------------------------------------------- --now 立即接管 ----
# as_user 以「服务将来的运行身份」执行命令（见头部约束 4）。
as_user() {
  if [ "$RUN_USER" = "root" ]; then
    "$@"
  elif command -v runuser >/dev/null 2>&1; then
    runuser -u "$RUN_USER" -- "$@"
  else
    su -s /bin/bash -c "$(printf '%q ' "$@")" "$RUN_USER"
  fi
}

# _ctl_running 判断本包是否已有实例在跑（pid 文件存在且进程活着）。
# ⛔ 不用 `ctl status`：它会打一堆日志，这里只要一个是/否。
_ctl_running() {
  local name f pid
  for name in backend zlm redis nginx; do
    f="$ROOT/run/${name}.pid"
    [ -f "$f" ] || continue
    pid="$(cat "$f" 2>/dev/null || true)"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
  done
  return 1
}

if [ "$WANT_NOW" = "1" ]; then
  # ⛔ 手工实例必须先停：ctl start 第一步是端口预检，端口被自己占着会直接拒绝启动。
  if _ctl_running; then
    log "检测到手工启动的实例，先以 ${RUN_USER} 身份停止它"
    as_user /bin/bash "$CTL" stop \
      || fail "手工实例停止失败 —— 请手动执行 ./uvp-gb28181-ctl.sh stop 后重试"
  fi
  log "启动服务…"
  if ! systemctl start "$UNIT_NAME"; then
    systemctl --no-pager --full status "$UNIT_NAME" || true
    fail "systemctl start 失败（详细见上面输出与 ${ROOT}/logs/）"
  fi
  log "服务已启动"
fi

# ------------------------------------------------------------ 收尾提示 ----
cat <<DONE

服务已注册（${UNIT_NAME}，运行身份 ${RUN_USER}，工作目录 ${ROOT}）。
后续统一用 systemctl 管理：

  systemctl start   ${UNIT_NAME}
  systemctl stop    ${UNIT_NAME}
  systemctl restart ${UNIT_NAME}
  systemctl status  ${UNIT_NAME}
  journalctl -u ${UNIT_NAME} -n 100     # 启停过程的输出；业务日志仍在 logs/ 下

⛔ 注册之后不要再用 ./uvp-gb28181-ctl.sh start|stop 手工启停：两条路管的是同一批进程，
   混用会出现「systemctl status 说没在跑、其实在跑」。要临时手工操作，先 systemctl stop。

DONE
