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
# ---- ZLM 端口 ----
# ⛔⛔ ZLM 端口的**唯一真源是 bin/zlm/config.ini**，下面这些变量只是"探测用镜像"。
#   只改变量不改 ini ⇒ 脚本探测 18081、MediaServer 却仍听 80 ⇒ 报「ZLM 启动失败」，
#   而真实原因是端口没写进配置文件。⚠️ 实测踩过这个（设了 18081 仍起不来）。
#   ⇒ sync_zlm_ports_ini() 负责写进去。
ZLM_INI="$ZLM_DIR/config.ini"
ZLM_HTTP_PORT="${UVP_ZLM_HTTP_PORT:-$(read_env_file UVP_ZLM_HTTP_PORT)}"
# ⛔ 默认值一律选**高位端口**，不用 ZLM 自带的 80/443/554。
#   1024 以下需要 CAP_NET_BIND_SERVICE，非 root 起不来（实测：
#   「Listen on :: 554 failed: permission denied」）。
#   绿色包的运行用户就是普通用户，所以默认值必须避开特权区。
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

# ---------------------------------------------------------- 进程兜底查找 ----

# stop_by_pidfile 先按 pid 文件停；**停不掉时按可执行文件绝对路径兜底**。
#
# ⛔⛔ 为什么必须有兜底（实测踩过）：pid 文件在 run/ 目录里，而 run/ 会随
#   「rm -rf 后重新解压」一起消失 —— 此时进程还活着（它持有的是已删除的目录），
#   pid 文件却没了，stop 就**静默地什么都不停**，用户以为停了其实还在跑。
#   症状是：重启后端口被占⇒ 新进程起不来，而 stop 全程报「已停止」。
#
# ⛔ 匹配必须用**绝对路径 + pgrep -f**，且要排除 grep 自身。
#   只按进程名匹配会误杀别人的同named 进程（这台机器上就有别人的 redis-server /
#   MediaServer 在跑，见验收记录）；按绝对路径只命中本包启动的。
stop_by_pidfile() {
  local pid_file="$1" exe_path="$2" label="$3"
  local pid=""
  if [ -f "$pid_file" ]; then
    pid="$(cat "$pid_file")"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null || true
      for _ in $(seq 1 40); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.25
      done
      kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f "$pid_file"
    return 0
  fi

  # 兜底：pid 文件不在了 ⇒ 按绝对路径找本包启动的进程
  #
  # ⛔⛔ 匹配用「路径**出现在**命令行里」而不是「路径**开头**」。
  #   实测 redis 被自己的守护逻辑改写了 argv[0]：
  #     /path/bin/redis-server  →  实际 cmdline 是「redis-server 127.0.0.1:16380」
  #   （只剩 basename），所以 ^绝对路径 锚定匹配不到，兜底静默失效 ——
  #   而症状又是「stop 说停了、进程还在」。
  #   ⇒ 去掉 ^ 锚定；安全性靠绝对路径本身足够独特来保证。
  if [ -n "$exe_path" ] && [ -x "$exe_path" ]; then
    local found
    found="$(pgrep -f "${exe_path}" 2>/dev/null | tr '\n' ' ')"
    if [ -n "${found// /}" ]; then
      log "${label} 的 pid 文件缺失，按可执行路径找到遗留进程（${found}），正在停止"
      # shellcheck disable=SC2086  # 上面已用 tr 转成空格分隔的列表
      kill -TERM $found 2>/dev/null || true
      for _ in $(seq 1 40); do
        pgrep -f "${exe_path}" >/dev/null 2>&1 || break
        sleep 0.25
      done
      pgrep -f "${exe_path}" >/dev/null 2>&1 && kill -KILL $(pgrep -f "${exe_path}") 2>/dev/null || true
    fi
  fi
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
  # ⛔ SIGTERM 而非 kill -9：Redis 收到 SIGTERM 会先执行最后的 AOF flush
  #   再退出；kill -9 会留下需要下次启动回放的 AOF 文件。
  stop_by_pidfile "$REDIS_PID_FILE" "$REDIS_BIN" "Redis"
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
  # ⛔ 一定要给足优雅退出时间：进程退出时要 flush SQLite 的 WAL、关闭 Redis
  #   连接。SIGKILL 会留下 -wal/-shm（下次启动能恢复，但期间数据处于
  #   「已提交、未 checkpoint」状态，出问题时难排查）。
  stop_by_pidfile "$BACKEND_PID_FILE" "$SERVER_BIN" "后端"
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

# ------------------------------------------------------------ nginx ----
# nginx 只做两件事：托管前端 + HTTPS 终止，后端仍是纯 HTTP 的
# @BACKEND_PORT@（二维码里填的就是它，App 因此完全不碰 TLS）。
NGINX_DIR="$BIN/nginx"
NGINX_BIN="$NGINX_DIR/sbin/nginx"
NGINX_CONF="$NGINX_DIR/conf/nginx.conf"
NGINX_PID_FILE="$RUN/nginx.pid"
NGINX_LOG="$LOGS/nginx.log"
NGINX_CONF_TEMPLATE="$NGINX_DIR/conf/nginx.conf.template"
NGINX_CRT="$NGINX_DIR/conf/uvp.crt"
NGINX_KEY="$NGINX_DIR/conf/uvp.key"

# HTTPS 端口：给客户换端口时只改这里（同时也在 config.env 里）
NGINX_HTTPS_PORT="${UVP_HTTPS_PORT:-$(read_env_file UVP_HTTPS_PORT)}"
NGINX_HTTPS_PORT="${NGINX_HTTPS_PORT:-443}"
NGINX_HTTP_PORT="${UVP_HTTP_PORT:-$(read_env_file UVP_HTTP_PORT)}"
NGINX_HTTP_PORT="${NGINX_HTTP_PORT:-80}"

# ------------------------------------------------------------ nginx ----
# nginx 只做两件事：托管前端 + HTTPS 终止，后端仍是纯 HTTP 的
# @BACKEND_PORT@（二维码里填的就是它，App 因此完全不碰 TLS）。
NGINX_DIR="$BIN/nginx"
NGINX_BIN="$NGINX_DIR/sbin/nginx"
NGINX_CONF="$NGINX_DIR/conf/nginx.conf"
NGINX_PID_FILE="$RUN/nginx.pid"
NGINX_LOG="$LOGS/nginx.log"
NGINX_CONF_TEMPLATE="$NGINX_DIR/conf/nginx.conf.template"
NGINX_CRT="$NGINX_DIR/conf/uvp.crt"
NGINX_KEY="$NGINX_DIR/conf/uvp.key"

# HTTPS 端口：给客户换端口时只改这里（同时也在 config.env 里）
NGINX_HTTPS_PORT="${UVP_HTTPS_PORT:-$(read_env_file UVP_HTTPS_PORT)}"
NGINX_HTTPS_PORT="${NGINX_HTTPS_PORT:-443}"
NGINX_HTTP_PORT="${UVP_HTTP_PORT:-$(read_env_file UVP_HTTP_PORT)}"
NGINX_HTTP_PORT="${NGINX_HTTP_PORT:-80}"

# ------------------------------------------------------------ nginx ----

# render_nginx_conf 从模板生成实际配置：把 @ROOT@ / @BACKEND_PORT@ / 端口
# 替换成真实值。
#
# ⛔ 为什么不直接把配置写死在包里：安装目录、客户选的端口都是运行期才知道的。
#   而每次start 都重写一遍也安全 —— 模板在包里是只读的，生成物在 run/ 下。
# ⛔ 用 sed 替换 @...@ 这种带@ 的标记：nginx 配置文件里 @ 有特殊含义吗？没有。
#   但 **# 和 & 在替换串里有特殊含义**，所以只出现固定文本，不会踩到。
render_nginx_conf() {
  [ -f "$NGINX_CONF_TEMPLATE" ] || {
    log "⚠️  缺少 nginx 配置模板（$NGINX_CONF_TEMPLATE），跳过 nginx"
    return 1
  }
  # ⛔⛔ 只做「@占位符@ → 字面值」这一种替换，不要去匹配 `listenNNNssl;` 这类文本。
  #   实测踩过：模式 `listen      80;` 会把 80 误换成**后端端口**（因为它同样匹配
  #   别的行），而带空格的 `listen      443 ssl;` 又匹配不上 ⇒ 一行都没换对。
  #   模板里端口已经写成 @HTTPS_PORT@ / @PLAIN_HTTP_PORT@，这里只负责填值。
  sed -e "s|@ROOT@|$ROOT|g" \
      -e "s|@BACKEND_PORT@|$HTTP_PORT|g" \
      -e "s|@HTTPS_PORT@|$NGINX_HTTPS_PORT|g" \
      -e "s|@PLAIN_HTTP_PORT@|$NGINX_HTTP_PORT|g" \
      "$NGINX_CONF_TEMPLATE" > "$NGINX_CONF"

  # ⛔ 渲染后自检：模板里不该再有 @...@。有就是漏了某个占位符，
  #   而 nginx 会把它当成一个畸形的路径/指令，报的错与真因完全无关。
  if grep -q '@[A-Z_]*@' "$NGINX_CONF"; then
    log "nginx 配置里仍有未替换的占位符："
    grep -n '@[A-Z_]*@' "$NGINX_CONF" >&2 || true
    fail "nginx 配置模板与本脚本不匹配（占位符未全部替换）"
  fi
  return 0
}

# ensure_self_signed_cert 没有证书时生成自签名。
#
# ⛔ 一次签发 10 年：客户现场一旦部署，几年内不会重签；
#   而短期证书过期会让整个系统突然"打不开"且原因极难自查。
# ⛔ CN 填 _（通配占位）：自签名证书浏览器本来就会警告，
#   写具体主机名反而制造「IP 访问 + CN 不匹配」这种更硬的拦截。
#   ⭐ 根本解法仍是换真证书 —— 交付说明里必须写清楚。
ensure_self_signed_cert() {
  [ -f "$NGINX_CRT" ] && [ -f "$NGINX_KEY" ] && return 0
  mkdir -p "$NGINX_DIR/conf"
  if command -v openssl >/dev/null 2>&1; then
    openssl req -x509 -nodes -newkey rsa:2048 \
      -keyout "$NGINX_KEY" -out "$NGINX_CRT" \
      -days 3650 -subj "/CN=uvp-local" \
      -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" >>"$NGINX_LOG" 2>&1 || {
      log "⚠️  自签证书生成失败，nginx 不会启动（详见 $NGINX_LOG）"
      return 1
    }
    log "已生成自签名证书（10 年有效；建议客户换用正式证书）"
    return 0
  fi
  log "⚠️  未找到 openssl，无法生成自签证书 —— 请自行放入 ${NGINX_CRT} 与 ${NGINX_KEY}"
  return 1
}

nginx_healthy() {
  [ -f "$NGINX_PID_FILE" ] || return 1
  local pid; pid="$(cat "$NGINX_PID_FILE")"
  kill -0 "$pid" 2>/dev/null
}

start_nginx() {
  if [ ! -x "$NGINX_BIN" ]; then
    log "⚠️  未找到 nginx（$NGINX_BIN），跳过。系统仍可通过 http://127.0.0.1:${HTTP_PORT} 访问"
    return 0
  fi
  if nginx_healthy; then
    log "nginx 已在运行（端口 ${NGINX_HTTPS_PORT}）"
    return 0
  fi

  render_nginx_conf || return 0
  ensure_self_signed_cert || return 0

  # ⛔ 先 -t 校验再起：nginx 配置写错时的表现是「反复重启、端口时有时无」，
  #   远不如一次性报清楚。-t 失败必须把错误原样打出来。
  if ! "$NGINX_BIN" -p "$ROOT" -c "$NGINX_CONF" -t >>"$NGINX_LOG" 2>&1; then
    log "nginx 配置校验失败，最后 10 行："
    tail -10 "$NGINX_LOG" >&2 || true
    fail "nginx 配置有误（详见 ${NGINX_LOG}）"
  fi

  "$NGINX_BIN" -p "$ROOT" -c "$NGINX_CONF" >>"$NGINX_LOG" 2>&1

  for _ in $(seq 1 50); do
    if nginx_healthy; then
      log "nginx 启动成功（HTTPS ${NGINX_HTTPS_PORT}，前端已托管，反代到 127.0.0.1:${HTTP_PORT}）"
      return 0
    fi
    sleep 0.2
  done
  log "nginx 启动超时，最后 10 行日志："
  tail -10 "$NGINX_LOG" >&2 || true
  fail "nginx 未能在预期时间内启动"
}

stop_nginx() {
  stop_by_pidfile "$NGINX_PID_FILE" "$NGINX_BIN" "nginx"
}

# ---------------------------------------------------- ZLM 端口同步到 config.ini ----

# sync_zlm_ports_ini 把端口写进 bin/zlm/config.ini 的对应段。
#
# ⛔⛔ 必须**按段落**改，绝不能全局替换 `port=` —— config.ini 里有 11 个段各带
#   port/sslport（http / rtmp / rtp_proxy / rtc / srt / rtsp / shell / onvif …），
#   全局替换会把它们全改成同一个值，表现为「一堆协议抢同一个端口」。
# ⛔ 不能用 configparser：这份 ini **有重名段**（[general] 出现两次），
#   标准库直接抛 DuplicateSectionError —— 实测过。
#   ⇒ 行扫描：记住当前段名，只在目标段内改目标键。
sync_zlm_ports_ini() {
  [ -f "$ZLM_INI" ] || return 0      # 没有 ini 就用 ZLM 默认值，不阻塞启动

  "$PY_BIN" - "$ZLM_INI" "$ZLM_HTTP_PORT" "$ZLM_SSL_PORT" "$ZLM_RTSP_PORT" <<'ZLM_INI_PY'
import re
import sys

path, http_port, ssl_port, rtsp_port = sys.argv[1:5]

# 段名（小写） → {键: 新值}
targets = {
    "http": {"port": http_port, "sslport": ssl_port},
    "rtsp": {"port": rtsp_port, "sslport": "0"},   # sslport=0 表示不启用
}

section = None
changed = []
out = []
for line in open(path, encoding="utf-8").read().splitlines():
    stripped = line.strip()
    if stripped.startswith("[") and stripped.endswith("]"):
        section = stripped[1:-1].strip().lower()
        out.append(line)
        continue
    m = re.match(r"^(\s*)([a-z_]+)(\s*=\s*)(.*)$", line)
    if m and section in targets and m.group(2) in targets[section]:
        indent, key, eq, old_val = m.groups()
        new_val = targets[section][key]
        if old_val.strip() != new_val:
            changed.append("[%s] %s: %s -> %s" % (section, key, old_val.strip(), new_val))
        out.append("%s%s%s%s" % (indent, key, eq, new_val))
        continue
    out.append(line)

if changed:
    open(path, "w", encoding="utf-8").write("\n".join(out) + "\n")
    print("   ZLM config.ini: " + "; ".join(changed))
else:
    print("   ZLM config.ini: 端口已与目标一致，无需改动")
ZLM_INI_PY
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
  stop_by_pidfile "$ZLM_PID_FILE" "$ZLM_BIN" "ZLM"
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
  # nginx 端口也持久化，否则 stop/status 阶段读到的是默认值
  printf 'UVP_HTTPS_PORT=%s\nUVP_HTTP_PORT_80=%s\n' "${NGINX_HTTPS_PORT}" "${NGINX_HTTP_PORT}" >> "$ENV_FILE"

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
    # ⛔ ZLM 端口也要同步：它的真源是 config.ini，不是本脚本里的变量。
    sync_zlm_ports_ini
    # ⛔ ZLM 要在**后端之前**起来：后端启动后会立即注册 hook、
    #   校验 ZLM 连通性，ZLM 没起就报连接失败。
    start_zlm
    # ⛔ nginx 在最后起：它要反代后端，后端得先在监听。
    start_nginx
    start_redis          # ⛔ 顺序不能反：后端启动时要连缓存
    start_backend
    log "启动完成 → http://127.0.0.1:${HTTP_PORT}"
    printf '首次登录账号 admin，密码见交付说明（登录后请立即修改）\n'
    ;;
  stop)
    stop_nginx
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
    if [ -x "$NGINX_BIN" ]; then
      nginx_healthy  && log "nginx:  运行中（HTTPS ${NGINX_HTTPS_PORT}）" || log "nginx:  未运行"
    else
      log "nginx:  未安装（可直接访问 http://127.0.0.1:${HTTP_PORT}）"
    fi
    ;;
  *)
    echo "用法: $0 {start|stop|restart|status}" >&2
    exit 2
    ;;
esac