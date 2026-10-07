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

# 端口与 Redis 配置集中在这里，客户改这一个文件即可
HTTP_PORT="${UVP_HTTP_PORT:-8280}"
REDIS_PORT="${UVP_REDIS_PORT:-6379}"
REDIS_BIND="127.0.0.1"
# ⛔ Redis 只监听回环：它没有密码，暴露到公网等于把后端的会话存储敞开。
#   后端与 Redis 同机，走回环即可。
BACKEND_LOG="$LOGS/backend.log"
REDIS_LOG="$LOGS/redis.log"

REDIS_BIN="$BIN/redis-server"
REDIS_CLI="$BIN/redis-cli"
SERVER_BIN="$BIN/uvp-server"

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

case "${1:-start}" in
  start)
    check_preconditions
    ensure_database# ⛔ 必须先建库再起 Redis/后端：后端启动时会立刻查库，
                    #   库不存在就是「页面能开、登录报 no such table」。
    start_redis          # ⛔ 顺序不能反：后端启动时要连缓存
    start_backend
    log "启动完成 → http://127.0.0.1:${HTTP_PORT}"
    printf'首次登录账号 admin，密码见交付说明（登录后请立即修改）\n'
    ;;
  stop)
    stop_backend
    stop_redis
    log "已停止"
    ;;
  restart)
    "$0" stop; "$0" start
    ;;
  status)
    redis_healthy    && log "Redis:  运行中（端口 ${REDIS_PORT}）"  || log "Redis:  未运行"
    backend_healthy && log "后端:  运行中（端口 ${HTTP_PORT}）"    || log "后端:  未运行"
    ;;
  *)
    echo "用法: $0 {start|stop|restart|status}" >&2
    exit 2
    ;;
esac