#!/usr/bin/env bash
# UVP 绿色安装包 —— 启停脚本（start / stop / status / restart）
#
# 设计目标：目标机**零预装**。不需要 Docker、nginx、MySQL、Redis，
# 只要一个 x86_64 的 Linux + 可执行权限。
#
# ⛔⛔ 三条铁律（都是踩过的坑）：
#   1. **必须 cd 到安装目录再执行**。后端用相对路径找 config.yml、
#      version.json、SQLite 数据目录；cwd 不对就是「起了但页面打不开」。
#   2. **Redis 与后端有先后依赖**。Redis 没起来，后端启动阶段连缓存就失败。
#      但也别用固定 sleep 等—— 用「轮询 ping 通」代替，否则慢机器上必然踩空。
#   3. **启动失败要能自检**。光打一行 "start failed" 没人会知道是哪一步挂了。

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

BIN="$ROOT/bin"
DATA="$ROOT/data"
LOGS="$ROOT/logs"
RUN="$ROOT/run"
CONF="$ROOT/config/config.yml"

BACKEND_PID_FILE="$RUN/backend.pid"
REDIS_PID_FILE="$RUN/redis.pid"

# ------------------------------------------------------ 端口（可在目标机改）----
# ⛔⛔ 端口必须能在**目标机上**改，而不是只在打包时。
#   打包时传进去的 UVP_HTTP_PORT / UVP_REDIS_PORT 只作用于 build脚本，
#   那些环境变量不会跟着包走 —— 现场最常见的诉求恰恰是「8080 被占了，换一个」。
#   实测踩过：220 上 6379 已被开发环境占用，包里 Redis 起在 6379 直接
#   `bind: Address already in use`，而 config.yml 里写的是另一个端口，两边不一致。
#
# 优先级：环境变量 > 包内 config.env > 默认值。
# 首次运行把最终生效的端口写进 config.env，之后 start/stop/status 都以它为准 ——
# 这样"改端口"只发生一次，不会出现"改了A 处忘了 B 处"。
# python 解释器（端口同步要改config.yml；建库也要它）。
# ⛔ 不能写死路径：构建机是 macOS、目标机是 Ubuntu，写死本机路径必然 command not found。
PY_BIN=""
for _c in "${UVP_BUILD_PY:-}" python3 python; do
  if [ -n "$_c" ] && command -v "$_c" >/dev/null 2>&1; then PY_BIN="$(command -v "$_c")"; break; fi
done

ENV_FILE="$ROOT/config.env"
HTTP_PORT_DEFAULT=8280
REDIS_PORT_DEFAULT=6379

read_env_file() {
  # 读 config.env 的 KEY=VALUE；忽略注释与空行；取最后一条（后写的覆盖先写的）
  [ -f "$ENV_FILE" ] || return 0
  sed -n "s/^$1=//p" "$ENV_FILE" 2>/dev/null | tail -1
}

HTTP_PORT="${UVP_HTTP_PORT:-$(read_env_file UVP_HTTP_PORT)}"
HTTP_PORT="${HTTP_PORT:-$HTTP_PORT_DEFAULT}"
REDIS_PORT="${UVP_REDIS_PORT:-$(read_env_file UVP_REDIS_PORT)}"
REDIS_PORT="${REDIS_PORT:-$REDIS_PORT_DEFAULT}"
# ⛔ Redis 只监听回环：它没有密码，暴露到公网等于把后端的会话存储敞开。
#   后端与 Redis 同机，走回环即可。
REDIS_BIND="${UVP_REDIS_BIND:-127.0.0.1}"
BACKEND_LOG="$LOGS/backend.log"
REDIS_LOG="$LOGS/redis.log"

REDIS_BIN="$BIN/redis-server"
REDIS_CLI="$BIN/redis-cli"
SERVER_BIN="$BIN/uvp-server"

# ---- ZLM（二开版，平台靠它推流/取流/录像）----
ZLM_DIR="$BIN/zlm"
ZLM_BIN="$ZLM_DIR/MediaServer"
ZLM_PID_FILE="$RUN/zlm.pid"
ZLM_LOG="$LOGS/zlm.log"
# ZLM 端口：与后端的 httpport 段（18080）、hookport（8280）一起构成一段连续高位端口。
# ⛔ 改了 config.ini 里的端口就必须同步改这里，否则启停脚本探测的是旧端口，
#   表现为「服务起来了但 status 说没起」。
ZLM_HTTP_PORT="${UVP_ZLM_HTTP_PORT:-$(read_env_file UVP_ZLM_HTTP_PORT)}"
ZLM_HTTP_PORT="${ZLM_HTTP_PORT:-18080}"
ZLM_SSL_PORT="${UVP_ZLM_SSL_PORT:-$(read_env_file UVP_ZLM_SSL_PORT)}"
ZLM_SSL_PORT="${ZLM_SSL_PORT:-18443}"
ZLM_RTSP_PORT="${UVP_ZLM_RTSP_PORT:-$(read_env_file UVP_ZLM_RTSP_PORT)}"
ZLM_RTSP_PORT="${ZLM_RTSP_PORT:-10554}"

log()  { printf '[uvp] %s\n' "$*"; }
fail() { printf '[uvp][ERROR] %s\n' "$*" >&2; exit 1; }

ensure_dirs() {
  mkdir -p "$DATA" "$LOGS" "$RUN"
}

# ---------------------------------------------------------------- Redis ----

redis_healthy() {
  "$REDIS_CLI" -h "$REDIS_BIND" -p "$REDIS_PORT" ping 2>/dev/null | grep -q PONG
}

start_redis() {
  if redis_healthy; then
    log "Redis 已在运行（端口 $REDIS_PORT）"
    return 0
  fi
  [ -x "$REDIS_BIN" ] || fail "找不到可执行的 $REDIS_BIN"

  # ⛔ AOF 必须开：默认快照策略下，Redis 被 kill -9 时最多丢 1 分钟数据。
  #   平台的登录态、字典缓存都在里面，重启丢数据 = 用户要重新登录且功能异常。
  "$REDIS_BIN" \
    --port "$REDIS_PORT" \
    --bind "$REDIS_BIND" \
    --dir "$DATA/redis" \
    --appendonly yes \
    --appendfsync everysec \
    --save '' \
    --daemonize no \
    --protected-mode yes \
    >"$REDIS_LOG" 2>&1 &
  echo $! > "$REDIS_PID_FILE"

  # ⛔ 用轮询而不是 sleep：慢机器上固定 sleep 3s 可能不够，
  #   而本机（容器/慢盘）固定 sleep 又白等。
  for _ in $(seq 1 50); do
    if redis_healthy; then
      log "Redis 启动成功（端口 $REDIS_PORT，密码为空、仅监听回环）"
      return 0
    fi
    sleep 0.2
  done

  log "Redis 启动超时，最后 20 行日志："
  tail -20 "$REDIS_LOG" >&2 || true
  fail "Redis 未能启动"
}

stop_redis() {
  if [ -f "$REDIS_PID_FILE" ]; then
    local pid; pid="$(cat "$REDIS_PID_FILE")"
    if kill -0 "$pid" 2>/dev/null; then
      # ⛔ 用 SIGTERM 而不是 kill -9：Redis 收到 SIGTERM 会先执行最后的
      #   AOF flush 再退出；kill -9 会留下需要下次启动回放的 AOF 文件。
      kill -TERM "$pid" 2>/dev/null || true
      for _ in $(seq 1 30); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.2
      done
      kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f "$REDIS_PID_FILE"
  fi
}

# --------------------------------------------------------------- 后端 ----

backend_healthy() {
  # 用端口探测而不是 PID 文件：进程活着但端口没起来同样是不能用。
  # curl 可能没装，这里用 bash 的 /dev/tcp（无需外部依赖）。
  (exec 3<>"/dev/tcp/127.0.0.1/$HTTP_PORT") 2>/dev/null && exec 3<&- 3>&- && return 0
  return 1
}

start_backend() {
  if backend_healthy; then
    log "后端已在运行（端口 $HTTP_PORT）"
    return 0
  fi
  [ -x "$SERVER_BIN" ] || fail "找不到可执行的 $SERVER_BIN"
  [ -f "$CONF" ]    || fail "找不到配置文件 $CONF"
  [ -f "$ROOT/version.json" ] || fail "找不到 version.json（后端启动会读它，缺了日志里全是噪音）"

  # ⛔ 绝不能关掉子 shell 的 stdio：`uvp-ctl.sh start | tail` 这类用法会永远挂住
  #   （孤儿子 shell 攥着管道的写端），看起来像「启动卡死」。实测挂过 10 分钟以上。
  ( "$SERVER_BIN" >"$BACKEND_LOG" 2>&1 </dev/null & echo $! > "$BACKEND_PID_FILE" ) >/dev/null 2>&1

  for _ in $(seq 1 150); do
    if backend_healthy; then
      log "后端启动成功（端口 $HTTP_PORT）"
      return 0
    fi
    # 进程已死就别再等了
    if [ -f "$BACKEND_PID_FILE" ]; then
      local pid; pid="$(cat "$BACKEND_PID_FILE")"
      if ! kill -0 "$pid" 2>/dev/null; then
        log "后端进程已退出，最后 30 行日志："
        tail -30 "$BACKEND_LOG" >&2 || true
        rm -f "$BACKEND_PID_FILE"
        fail "后端启动失败（详见 $BACKEND_LOG）"
      fi
    fi
    sleep 0.2
  done

  log "后端启动超时，最后 30 行日志："
  tail -30 "$BACKEND_LOG" >&2 || true
  fail "后端未能在预期时间内监听端口 $HTTP_PORT"
}

stop_backend() {
  if [ -f "$BACKEND_PID_FILE" ]; then
    local pid; pid="$(cat "$BACKEND_PID_FILE")"
    if kill -0 "$pid" 2>/dev/null; then
      # ⛔ 一定要给足优雅退出时间：进程退出时要 flush SQLite 的 WAL、
      #   关闭 Redis 连接。SIGKILL 会留下 -wal/-shm 文件（下次启动能恢复，
      #   但期间数据处于「已提交、未 checkpoint」状态，出问题时难排查）。
      kill -TERM "$pid" 2>/dev/null || true
      for _ in $(seq 1 60); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.25
      done
      kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f "$BACKEND_PID_FILE"
  fi
}

# ---------------------------------------------------------------- 入口 ----

check_preconditions() {
  ensure_dirs
  case "$(uname -m)" in
    x86_64|amd64) : ;;
    *) fail "本包只支持 x86_64，当前架构 $(uname -m)" ;;
  esac
  [ -f "$CONF" ] || fail "缺少 $CONF —— 解压是否完整？"
}

# ---------------------------------------------------------------- ZLM ----

zlm_healthy() {
  (exec 3<>"/dev/tcp/127.0.0.1/$ZLM_HTTP_PORT") 2>/dev/null && exec 3<&- 3>&- && return 0
  return 1
}

start_zlm() {
  if zlm_healthy; then
    log "ZLM 已在运行（HTTP 端口 ${ZLM_HTTP_PORT}）"
    return 0
  fi
  if [ ! -x "$ZLM_BIN" ]; then
    # ⛔ ZLM 缺失不阻断启动：平台核心（设备管理、实时预览列表、录像查询）
    #   不依赖它，但推流/取流/录像会失败。让用户能先进去看页面，别卡在这一步。
    log "⚠️  未找到 ZLM（$ZLM_BIN），跳过启动。系统可访问，但推流/取流/录像不可用。"
    return 0
  fi

  # ⛔⛔ LD_LIBRARY_PATH 是**必须**的：MediaServer 动态链接 ffmpeg 的 7 个 .so
  #   （libavformat/libavfilter/libswscale/libpostproc 等），包内自带在 lib/。
  #   漏了它就是启动瞬间 `error while loading shared libraries: libavfilter.so.9`。
  # ⛔ 配置文件路径要**绝对路径**：MediaServer 的 WorkingDir 敏感，
  #   相对路径在不同启动方式下会解析到不同地方 ⇒ 端口/secret 全部走默认值。
  ( cd "$ZLM_DIR" && LD_LIBRARY_PATH="$ZLM_DIR/lib" \
      "$ZLM_BIN" -c "$ZLM_DIR/config.ini" -l 0 >"$ZLM_LOG" 2>&1 </dev/null &
    echo $! > "$ZLM_PID_FILE" ) >/dev/null 2>&1

  for _ in $(seq 1 100); do
    if zlm_healthy; then
      log "ZLM 启动成功（HTTP ${ZLM_HTTP_PORT} / RTSP ${ZLM_RTSP_PORT}）"
      return 0
    fi
    if [ -f "$ZLM_PID_FILE" ]; then
      local pid; pid="$(cat "$ZLM_PID_FILE")"
      if ! kill -0 "$pid" 2>/dev/null; then
        log "ZLM 进程已退出，最后 20 行日志："
        tail -20 "$ZLM_LOG" >&2 || true
        rm -f "$ZLM_PID_FILE"
        fail "ZLM 启动失败（详见 ${ZLM_LOG}）。常见原因：lib/ 下缺 ffmpeg 运行时库"
      fi
    fi
    sleep 0.2
  done

  log "ZLM 启动超时，最后 20 行日志："
  tail -20 "$ZLM_LOG" >&2 || true
  fail "ZLM 未能在预期时间内监听端口 ${ZLM_HTTP_PORT}"
}

stop_zlm() {
  if [ -f "$ZLM_PID_FILE" ]; then
    local pid; pid="$(cat "$ZLM_PID_FILE")"
    if kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null || true
      for _ in $(seq 1 40); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.25
      done
      kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f "$ZLM_PID_FILE"
  fi
}

# ------------------------------------------------------------ 自动建库 ----

# ensure_database 在启动后端**之前**建库。
#
# 为什么必须自动做，而不是让用户手动跑 init-database.sh：
#   绿色包的契约是「解压即用」。参照EasyGBS/LiveGBS 的形态，用户装完只做一件事：
#   执行 start 然后打开浏览器。多一条"记得先建库"的步骤，就有一批用户会漏掉 ——
#   而漏掉的症状是**页面能开、登录页报"no such table"**，
#   用户完全联想不到是因为少跑了一个脚本（这个坑记忆里记录过：
#   解开了、页面打不开、日志只有一行 startup failed）。
#
# ⛔ 幂等靠「sys_users 表是否存在」判断，不是「文件是否存在」：
#   建库中途断电会留下半成品文件，只看文件存在会让后续跳过建库，
#   表现是"文件在、库是空的"。
ensure_database() {
  local db_path="${UVP_DB_PATH:-./data/uvp.db}"
  local baseline="$ROOT/resource/baseline/baseline.sql"

  [ -f "$baseline" ] || fail "缺少建库脚本 $baseline（包不完整？请重新解压）"

  # ⛔ python 路径不能写死：构建机是 macOS、目标机是 Ubuntu，
  #   写死本机路径的话包发到客户机上必然 command not found。
  local py=""
  if [ -n "${UVP_BUILD_PY:-}" ] && [ -x "${UVP_BUILD_PY}" ]; then
    py="$UVP_BUILD_PY"
  else
    local c
    for c in python3 python; do
      if command -v "$c" >/dev/null 2>&1; then py="$(command -v "$c")"; break; fi
    done
  fi
  [ -n "$py" ] || fail "找不到 python3 —— 本包建库需要它（Ubuntu/Debian 请先 apt install python3）"

  # 已建好就直接跳过（幂等）
  if [ -f "$db_path" ]; then
    local has_table
    has_table="$("$py" - "$db_path" <<'PY' 2>/dev/null || true
import sqlite3, sys
try:
    db = sqlite3.connect(sys.argv[1])
    print(db.execute("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='sys_users'").fetchone()[0])
    db.close()
except Exception:
    print(0)
PY
)"
    if [ "$has_table" = "1" ]; then
      log "数据库已就绪（$db_path）"
      return 0
    fi
    log "检测到 $db_path 存在但库不完整（可能是上次建库中断），将重建"
  fi

  log "首次运行：正在初始化数据库（111 张表，约需数秒）…"
  # ⛔ executescript 交给 SQLite 自己处理语句边界。千万别改成
  #   「按分号裸切」或「逐行 complete_statement」—— 种子数据里有转义换行
  #   （MySQL 的 \n），前一种会切坏语句、后一种会把前一句的尾随注释算进缓冲区，
  #   两者都会报出与真实原因无关的错误（实测两种都踩过）。
  "$py" - "$baseline" "$db_path" <<'PY'
import sqlite3
import sys
from pathlib import Path

baseline_path, db_path = Path(sys.argv[1]), Path(sys.argv[2])
sql = baseline_path.read_text(encoding="utf-8")

# ⛔ 外键必须显式打开：SQLite 的 foreign_keys PRAGMA **默认为 OFF**，
#    建表语句里那些 REFERENCES 在默认连接下全是摆设，
#    表现为「引用完整性悄悄没了」——开发库验过、客户机上数据却能写成孤儿。
db = sqlite3.connect(db_path)
db.execute("PRAGMA foreign_keys = ON")
try:
    db.executescript(sql)
except Exception as exc:
    db.close()
    raise SystemExit(f"建库失败: {type(exc).__name__}: {exc}")

tables = db.execute(
    "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
).fetchone()[0]
admins = db.execute("SELECT count(*) FROM sys_users WHERE username='admin'").fetchone()[0]
integrity = db.execute("PRAGMA integrity_check").fetchone()[0]
db.close()

if integrity != "ok":
    raise SystemExit(f"建库后自检失败: integrity_check={integrity}")
if admins != 1:
    raise SystemExit(f"建库后自检失败: admin 账号数={admins}（应为 1）")
print(f"           {tables} 张表已建好，管理员账号就位")
PY
}

sync_ports_into_config() {
  # 把生效端口写回 config.env 与 config.yml。
  #
  # ⛔⛔ 不做这一步就会出现"Redis 听 A 端口、后端连 B 端口"这种**静默错配**：
  #   Redis 用 --port 参数启动（读 $REDIS_PORT），而后端连哪个端口是读 config.yml 的
  #   redis.port。两者不同源 ⇒ 用户改了 env 变量后，后端连不上Redis，
  #   症状是「启动日志只说 cache 连不上」，完全看不出是端口没同步。
  #
  # 首次运行（config.env 还不存在）用打包时写入 config.yml 的值作为权威，
  # 之后一律以 config.env 为准。
  if [ ! -f "$ENV_FILE" ]; then
    local yml_http yml_redis
    yml_http="$(sed -n 's/^[[:space:]]*port:[[:space:]]*:\([0-9]\+\).*/\1/p' "$CONF" | head -1)"
    yml_redis="$(awk '/^redis:/{f=1;next} f&&/^[^[:space:]]/{exit} f&&/^[[:space:]]+port:/{print $2;exit}' "$CONF")"
    HTTP_PORT="${yml_http:-$HTTP_PORT}"
    REDIS_PORT="${yml_redis:-$REDIS_PORT}"
  fi

  printf 'UVP_HTTP_PORT=%s\nUVP_REDIS_PORT=%s\n' "${HTTP_PORT}" "${REDIS_PORT}" > "$ENV_FILE"

  # config.yml 里同步：后端是通过这两个键读端口的
  if [ -f "$CONF" ]; then
    "$PY_BIN" - "$CONF" "$HTTP_PORT" "$REDIS_PORT" <<'PY'
import re, sys
path, http_port, redis_port = sys.argv[1], sys.argv[2], sys.argv[3]
lines = open(path, encoding="utf-8").read().split("\n")

def replace_in_section(section, key, value):
    """只在指定段落内替换末级键 —— host/port 这类键在 gormv2 各段下重名，
    全局替换会把 mysql 的也改掉。找不到段落或键就返回 False，不猜。"""
    start = None
    for i, line in enumerate(lines):
        if line.strip() == f"{section}:":
            start = i
            break
    if start is None:
        return False
    indent_of = len(lines[start]) - len(lines[start].lstrip())
    end = len(lines)
    for i in range(start + 1, len(lines)):
        line = lines[i]
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if len(line) - len(line.lstrip()) <= indent_of:
            end = i
            break
    pat = re.compile(rf"^(?P<ind>\s*){key}\s*:\s*.*?(?P<trail>\s*)$")
    for i in range(start + 1, end):
        m = pat.match(lines[i])
        if m:
            lines[i] = f"{m.group('ind')}{key}: {value}"
            return True
    return False

# 只在值真的变了才写盘，避免无谓地改 mtime（运维会误以为配置被动过）
dirty = False
if replace_in_section("httpserver", "port", f":{http_port}"):
    dirty = True
if replace_in_section("redis", "port", redis_port):
    dirty = True
if dirty:
    open(path, "w", encoding="utf-8").write("\n".join(lines))
PY
  fi
  log "生效端口：HTTP ${HTTP_PORT} / Redis ${REDIS_PORT}（已写入 config.env）"
}

case "${1:-start}" in
  start)
    check_preconditions
    # ⛔ 必须先建库再起 Redis/后端：后端启动时会立刻查库，
    #   库不存在就是「页面能开、登录报 no such table」。
    ensure_database
    # ⛔ 端口必须先同步进 config.yml 再起服务：Redis 读 --port 参数、
    #   后端读 config.yml，两处不同源就是「Redis 起来了但后端连不上」。
    if [ -z "$PY_BIN" ]; then
      fail "找不到 python3 —— 改端口需要它（Ubuntu/Debian 请先 apt install python3）"
    fi
    sync_ports_into_config
    start_zlm# ⛔ ZLM 要在**后端之前**起来：后端启动后会立即注册 hook、
                   #   校验 ZLM 连通性，ZLM 没起就报连接失败。
    start_redis          # ⛔ 顺序不能反：后端启动时要连缓存
    start_backend
    log "启动完成 → http://127.0.0.1:${HTTP_PORT}"
    printf'首次登录账号 admin，密码见交付说明（登录后请立即修改）\n'
    ;;
  stop)
    stop_backend
    stop_zlm
    stop_redis
    log "已停止"
    ;;
  restart)
    "$0" stop; "$0" start
    ;;
  status)
    redis_healthy    && log "Redis:  运行中（端口 ${REDIS_PORT}）"  || log "Redis:  未运行"
    zlm_healthy      && log "ZLM:    运行中（端口 ${ZLM_HTTP_PORT}）" || log "ZLM:    未运行"
    backend_healthy && log "后端:  运行中（端口 ${HTTP_PORT}）"    || log "后端:  未运行"
    ;;
  *)
    echo "用法: $0 {start|stop|restart|status}" >&2
    exit 2
    ;;
esac