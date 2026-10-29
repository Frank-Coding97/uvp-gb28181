#!/usr/bin/env bash
# UVP-GB28181 容器版 —— 首次安装
#
# 用法（离线，内网客户的默认方式）：
#   tar -xzf uvp-gb28181-docker-linux-amd64-<版本>.tar.gz
#   cd uvp-gb28181-docker-linux-amd64-<版本>
#   sudo ./install.sh
#
# 用法（在线，镜像从 Registry 拉取）：
#   sudo ./install.sh --online --registry registry.example.com/uvp --version 1.1.4
#
# 用法（端口段冲突，整段平移 —— 推荐）：
#   sudo ./install.sh --port-base 53000
#   ⇒ 53000 管理页 / 53001 HTTP / 53002 后端 / 53003 Redis / 53004 ZLM
#     53014-53063 RTP / 53064 SIP（SIP 是引导页默认值，由本脚本算出来注入）
#   ⛔ 只传 --https-port 会把**其它端口留在 51000 段**，形成混合段 ——
#      防火墙就得开两条规则。要换段请用 --port-base，或把 5 个端口全传。
#
# ⛔ 两个模式的区别**只有镜像从哪来**：在线 docker pull、离线 docker load，
#    之后的目录结构、配置生成、启动流程完全共用一套代码 ——
#    刻意不写两个脚本，否则两边行为迟早分叉（这正是绿色包曾经踩过的坑型）。
#
# 前置：Docker 20.10+ / Compose v2、python3（生成配置用）、openssl（生成证书用）。
set -Eeuo pipefail
umask 027

PKG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---- 默认值（都能被命令行参数覆盖）----
ROOT="${UVP_ROOT:-/opt/uvp-gb28181}"
VERSION="${UVP_VERSION:-}"
IMAGE_PREFIX="${UVP_IMAGE_PREFIX:-uvp-gb28181}"
MODE="offline"
REGISTRY=""
OFFLINE_IMAGES=""
# ⛔ 端口先全部留空，等参数解析完再统一派生。
#   原来是「解析前就给默认值」，于是 `--https-port 53000` 单独传的时候
#   会得到 53000 + 51001/51002/51003/51004 这种**混合段** —— 能跑，但
#   "整段连续、防火墙一条规则"的规划就破了，而且现场看不出来。
PORT_BASE="${UVP_PORT_BASE:-}"
HTTPS_PORT="${UVP_HTTPS_PORT:-}"
HTTP_PORT="${UVP_HTTP_PORT:-}"
BACKEND_PORT="${UVP_BACKEND_PORT:-}"
REDIS_PORT="${UVP_REDIS_PORT:-}"
ZLM_HTTP_PORT="${UVP_ZLM_HTTP_PORT:-}"
# SIP 端口不映射、不进 config.yml，只作为**引导页默认值**下发给前端。
SIP_PORT="${UVP_SIP_PORT:-}"
BUSINESS_UID="${UVP_BUSINESS_UID:-10001}"
BUSINESS_GID="${UVP_BUSINESS_GID:-10001}"
NO_START=0

while [ $# -gt 0 ]; do
  case "$1" in
    --root) ROOT="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --prefix) IMAGE_PREFIX="$2"; shift 2 ;;
    --online) MODE="online"; shift ;;
    --offline) MODE="offline"; shift ;;
    --registry) REGISTRY="$2"; shift 2 ;;
    --images) OFFLINE_IMAGES="$2"; shift 2 ;;
    --port-base) PORT_BASE="$2"; shift 2 ;;
    --https-port) HTTPS_PORT="$2"; shift 2 ;;
    --http-port) HTTP_PORT="$2"; shift 2 ;;
    --backend-port) BACKEND_PORT="$2"; shift 2 ;;
    --redis-port) REDIS_PORT="$2"; shift 2 ;;
    --zlm-http-port) ZLM_HTTP_PORT="$2"; shift 2 ;;
    --sip-port) SIP_PORT="$2"; shift 2 ;;
    --no-start) NO_START=1; shift ;;
    -h|--help) sed -n '2,/^# 前置/p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) printf '未知参数: %s\n' "$1" >&2; exit 2 ;;
  esac
done

# ---- 端口派生 ----
# 规划段（与 deploy/standalone/PORTS.md 一致，整段连续无空洞）：
#   base+0  管理页 HTTPS        base+1  明文 HTTP
#   base+2  后端 API            base+3  Redis（仅回环）
#   base+4  ZLM HTTP            base+4..+13  ZLM 各协议
#   base+14..+63  ZLM RTP 收流段
#   base+64 SIP（UDP + TCP 双栈）
#
# ⛔⛔ 顺序必须是「先 PORT_BASE 派生 → 再显式端口覆盖 → 最后落默认值」：
#   反过来的话 `--port-base` 会被默认值挡住，等于没生效。
#   显式端口优先（只覆盖被点名的那一个），没点名的才跟随段基址。
if [ -n "$PORT_BASE" ]; then
  [ -n "$HTTPS_PORT" ]    || HTTPS_PORT=$((PORT_BASE))
  [ -n "$HTTP_PORT" ]     || HTTP_PORT=$((PORT_BASE + 1))
  [ -n "$BACKEND_PORT" ]  || BACKEND_PORT=$((PORT_BASE + 2))
  [ -n "$REDIS_PORT" ]    || REDIS_PORT=$((PORT_BASE + 3))
  [ -n "$ZLM_HTTP_PORT" ] || ZLM_HTTP_PORT=$((PORT_BASE + 4))
fi

HTTPS_PORT="${HTTPS_PORT:-51000}"
HTTP_PORT="${HTTP_PORT:-51001}"
BACKEND_PORT="${BACKEND_PORT:-51002}"
REDIS_PORT="${REDIS_PORT:-51003}"
ZLM_HTTP_PORT="${ZLM_HTTP_PORT:-51004}"
# SIP 端口默认取段内最后一位。⛔ 不写死 51064：段一平移，烤进前端产物的
# 51064 就落在段外，防火墙一条规则覆盖不到 ⇒ 设备注册不通。
SIP_PORT="${SIP_PORT:-$((HTTPS_PORT + 64))}"

log()  { printf '[uvp-install] %s\n' "$*"; }
fail() { printf '[uvp-install][ERROR] %s\n' "$*" >&2; exit 1; }

# ⛔⛔ 找命令**不能只靠 command -v**：
#   通过 ssh 的非登录 shell、cron、以及某些加固过的系统里 PATH 可能不含 /usr/bin，
#   实测在 220 上 `command -v python3` 是空的，而 /usr/bin/python3 明明在。
#   只判 command -v 会给出"没装 python3"这种**假结论**，让人跑去装一个已有的东西。
find_tool() {
  local name="$1"; shift
  if command -v "$name" >/dev/null 2>&1; then printf '%s\n' "$name"; return 0; fi
  local p
  for p in "$@"; do
    if [ -x "$p" ]; then printf '%s\n' "$p"; return 0; fi
  done
  return 1
}

# detect_lan_ip 取本机**对外物理网卡**的 IPv4。
# 这个地址有三个用途：证书 SAN、安装完成时打印的访问地址、扫码配网基址，
# 取错了就是「浏览器说证书不匹配」或「照给的地址打不开」。
#
# ⛔⛔ 不要用 `hostname -I | awk '{print $1}'`：它把 docker0(172.17.x) / virbr0
#   排在**最前面**，而容器版宿主机**必定装了 Docker** ⇒ 几乎必然踩到这个坑。
# ⭐ 首选 `ip route get 1.1.1.1`：内核回答「去这个地址走哪张网卡」，
#   虚拟网桥没有默认路由，天然被排除。
#   ⚠️ 但装了 VPN/隧道时默认路由会落到 tun/wg 上，所以还要再看一眼接口名，
#      是虚拟的就丢弃、改走枚举。
# 兜底：枚举 scope global 地址并按接口名排除虚拟设备；再不行用 hostname -I 挑内网段。
# 全都取不到返回空串，调用方据此降级成 127.0.0.1，不编地址。
# ⛔ 这段与 deploy/standalone/uvp-gb28181-ctl.sh 的 detect_lan_ip 是同一套判据，
#    改一边要同步另一边，否则两种形态在同一台机器上会给出不同的访问地址。
detect_lan_ip() {
  local ip="" dev="" line
  if command -v ip >/dev/null 2>&1; then
    line="$(ip route get 1.1.1.1 2>/dev/null | head -1 || true)"
    dev="$(printf '%s' "$line" | awk '{for(i=1;i<=NF;i++) if($i=="dev"){print $(i+1); exit}}')"
    ip="$(printf '%s' "$line"  | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}')"
    case "$dev" in
      docker*|br-*|virbr*|veth*|tun*|tap*|zt*|wg*|kube*|flannel*|dummy*) ip="" ;;
    esac
    if [ -z "$ip" ]; then
      ip="$(ip -4 -o addr show scope global 2>/dev/null \
            | awk '{print $2, $4}' \
            | awk '$1 !~ /^(docker|br-|virbr|veth|tun|tap|zt|wg|kube|flannel|dummy|lo)/ {print $2; exit}' \
            | cut -d/ -f1 || true)"
    fi
  fi
  if [ -z "$ip" ] && command -v hostname >/dev/null 2>&1; then
    ip="$(hostname -I 2>/dev/null | tr ' ' '\n' \
          | awk '/^192\.168\./ || /^10\./ {print; exit}' || true)"
    if [ -z "$ip" ]; then
      ip="$(hostname -I 2>/dev/null | tr ' ' '\n' \
            | awk '/^[0-9]+\.[0-9]/ && !/^127\./ {print; exit}' || true)"
    fi
  fi
  printf '%s' "$ip"
}

# ================================================================ 预检 ====
log "预检运行环境"

DOCKER_BIN="$(find_tool docker /usr/bin/docker /usr/local/bin/docker)" \
  || fail "需要 Docker（20.10+）。离线包不自带 Docker，请先安装。"
"$DOCKER_BIN" compose version >/dev/null 2>&1 || fail "需要 Docker Compose v2（docker compose version 无输出）。
  老式的 docker-compose（v1，Python 版）不支持本项目用到的 compose 规范，请先升级。"
PY="$(find_tool python3 /usr/bin/python3 /usr/local/bin/python3)" \
  || fail "需要 python3（生成配置 + 建库都要用它）。
  Debian/Ubuntu: apt install -y python3    RHEL/麒麟: yum install -y python3"
OPENSSL_BIN="$(find_tool openssl /usr/bin/openssl)" \
  || fail "需要 openssl（生成自签证书）。
  Debian/Ubuntu: apt install -y openssl    RHEL/麒麟: yum install -y openssl"
CURL_BIN="$(find_tool curl /usr/bin/curl)" || CURL_BIN=""
log "  docker=$DOCKER_BIN python3=$PY openssl=$OPENSSL_BIN"

# ⛔ 必须是绝对真实目录：软链接会让后面的 inode 判定与卷挂载行为变得不可预测
[[ "$ROOT" == /* ]] || fail "安装根必须是绝对路径：$ROOT"

if [ -e "$ROOT" ] && [ -n "$(ls -A "$ROOT" 2>/dev/null)" ]; then
  fail "安装根已存在且不为空：$ROOT
  已装过就用升级流程，别重复安装（重复安装会把旧数据目录的权限重刷一遍）。"
fi

# ---- 版本 ----
if [ -z "$VERSION" ]; then
  if [ -f "$PKG_DIR/version.json" ]; then
    VERSION="$(grep -m1 '"version"' "$PKG_DIR/version.json" | cut -d'"' -f4 || true)"
  fi
fi
[ -n "$VERSION" ] || fail "拿不到版本号：显式传 --version"
log "  版本 ${VERSION}"

# ---- 端口取值校验 ----
# ⛔ 放在这里而不是派生处：上面还没定义 fail()。拼错一个非数字的端口，
#    会让后面的 python 预检在 int() 上抛 ValueError，报的是一大段 traceback，
#    现场完全看不出"是你把端口写成 5l000 了"。
for _port_value in "$HTTPS_PORT" "$HTTP_PORT" "$BACKEND_PORT" "$REDIS_PORT" "$ZLM_HTTP_PORT" "$SIP_PORT"; do
  case "$_port_value" in
    ''|*[!0-9]*) fail "端口必须是整数，当前值：${_port_value}" ;;
  esac
  [ "$_port_value" -ge 1 ] && [ "$_port_value" -le 65535 ] \
    || fail "端口必须落在 1-65535，当前值：${_port_value}"
done
unset _port_value

# ⛔ 端口不成段要**大声提示**：功能上能跑（各端口独立），但规划里"整段连续、
#    防火墙一条规则覆盖"就不再成立，客户现场常常在开完防火墙后才发现
#    "有一半端口没放行"。
if [ "$HTTP_PORT" -ne $((HTTPS_PORT + 1)) ] \
  || [ "$BACKEND_PORT" -ne $((HTTPS_PORT + 2)) ] \
  || [ "$REDIS_PORT" -ne $((HTTPS_PORT + 3)) ] \
  || [ "$ZLM_HTTP_PORT" -ne $((HTTPS_PORT + 4)) ]; then
  printf '\n[uvp-install][WARN] 端口不成段：%s/%s/%s/%s/%s（SIP %s）\n' \
    "$HTTPS_PORT" "$HTTP_PORT" "$BACKEND_PORT" "$REDIS_PORT" "$ZLM_HTTP_PORT" "$SIP_PORT" >&2
  printf '  现在能用，但防火墙要按实际端口逐个放行，不能再用一条规则覆盖整段。\n' >&2
  printf '  想整段平移请用：./install.sh --port-base <段基址>（只传 --https-port 会造成混合段）\n\n' >&2
fi

# ---- 端口预检 ----
# ⛔⛔ 这一步不能省：容器全部走 **host 网络**，端口直接占宿主机，
#    没有任何一层会帮你挡冲突。等到 compose up 才发现，容器会陷入
#    重启循环，而日志里只有 "address already in use"，现场很难定位是谁占了。
#
# ⛔⛔ 检查**整段**而不是只查 5 个平台端口：段里还住着 ZLM 各协议（base+4..+13）、
#    RTP 收流段（base+14..+63）和 SIP（base+64）。这些端口不写进 compose 的
#    ports 段（host 网络不需要映射），所以 compose 自己完全不校验它们；
#    漏检的后果是 ZLM 启动 1 秒就自杀（UDP 绑定失败），日志只有一行
#    "bind: address already in use"，而现场根本不知道要去查 100 个 RTP 端口。
#
# ⛔⛔⛔ 判据是「有没有人在**监听**」，**不是**「能不能 bind」。
#    这条踩得很惨：`net.ipv4.ip_local_port_range` 默认 32768-60999，
#    **把 51000-51064 和 53000-53064 整段都包在里面** —— 任何一条出站连接
#    （比如某个进程连着本机 Redis/MySQL）都会临时占用段内一个端口，
#    这时 bind 会 EADDRINUSE，但那不是"端口被服务占着"，几秒后就释放。
#    实测：220 上查 54000 段，54006/54012/54024 报占用，ss 一看全是
#    `ESTAB ... :54006 ... :6379` 这种出站连接。
#    只查 5 个端口时撞上的概率低，扩到 65 个端口后概率放大十几倍 ⇒
#    安装会被一条临时连接莫名拦下，现场完全无法理解。
#    ⇒ 用 ss 枚举 LISTEN/UNCONN 状态，只认**真占着端口**的服务。
SEG_END=$((HTTPS_PORT + 64))
log "预检端口占用（整段 ${HTTPS_PORT}-${SEG_END}，按 ss 监听状态判定；ss 不可用时回退逐个试绑）"
"$PY" - "$HTTPS_PORT" "$SEG_END" "$HTTP_PORT" "$BACKEND_PORT" "$REDIS_PORT" "$ZLM_HTTP_PORT" "$SIP_PORT" <<'PORTS_PY'
import socket
import subprocess
import sys

start, end = int(sys.argv[1]), int(sys.argv[2])
# 整段 + 所有「被显式点名、可能落在段外」的端口（比如客户坚持 SIP 用 5060）。
wanted = list(range(start, end + 1)) + [int(p) for p in sys.argv[3:]]
seen, ports = set(), []
for port in wanted:
    if 1 <= port <= 65535 and port not in seen:
        seen.add(port)
        ports.append(port)

busy = []


def listened(*flags):
    """ss 里处于监听/绑定状态的本地端口集合；ss 不可用或解析失败返回 None。"""
    try:
        result = subprocess.run(
            ["ss", "-H", "-n", *flags],
            capture_output=True, text=True, timeout=15,
        )
    except Exception:
        return None
    if result.returncode != 0:
        return None
    found = set()
    for line in result.stdout.splitlines():
        # 列序：State Recv-Q Send-Q Local:Port Peer:Port [Process]
        fields = line.split()
        if len(fields) < 4:
            continue
        try:
            found.add(int(fields[3].rsplit(":", 1)[-1]))
        except ValueError:
            continue
    return found


tcp_listening = listened("-ltn")
udp_listening = listened("-lun")

if tcp_listening is not None and udp_listening is not None:
    busy = [f"TCP {p}" for p in ports if p in tcp_listening]
    busy += [f"UDP {p}" for p in ports if p in udp_listening]
else:
    # 没有 ss（极简镜像 / 裁剪过的系统）才回退到试绑。
    # ⛔ 这条分支会有上面说的"临时连接误报"，但总比完全不检查好。
    for port in ports:
        for proto, kind in ((socket.SOCK_STREAM, "TCP"), (socket.SOCK_DGRAM, "UDP")):
            s = socket.socket(socket.AF_INET, proto)
            s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            try:
                s.bind(("0.0.0.0", port))
            except OSError:
                busy.append(f"{kind} {port}")
            finally:
                s.close()

if busy:
    print("以下端口已被占用： " + ", ".join(busy), file=sys.stderr)
    print("换整段：./install.sh --port-base <段基址>；查占用：ss -ltnp", file=sys.stderr)
    sys.exit(1)
print(f"  {len(ports)} 个端口空闲")
PORTS_PY

# ============================================================ 目录布局 ====
log "创建安装目录 $ROOT"
mkdir -p "$ROOT"/{config/tls,data/{database,redis,uploads,zlm-www,process-authority,updater-state},logs/{backend,nginx,zlm},run/nginx}

# ⛔ 业务目录属主必须是业务 UID/GID：容器以该 UID 跑，目录属主不对的话
#    写入会静默失败（表现为"能起来、但 SQLite 建库失败"，日志里只有 permission denied 一行）。
#
# ⛔ 但 Linux 上**只有 root 能 chown**（普通用户连"改成自己"都不行）。
#    非 root 安装时目录本来就是当前用户的 ⇒ 直接跳过，
#    前提是业务 UID 就是当前 uid（不一致就让它早点报错，而不是等到容器里才失败）。
if [ "$(id -u)" != "0" ]; then
  if [ "${BUSINESS_UID}" != "$(id -u)" ]; then
    fail "非 root 安装时业务 UID 必须是当前用户（当前 $(id -u)，指定的是 ${BUSINESS_UID}）。
  要用别的 UID 跑容器，请以 root 执行本脚本。"
  fi
  log "  非 root 安装：数据目录属主为当前用户 ${BUSINESS_UID}"
fi
chown_biz() {
  [ "$(id -u)" = "0" ] || return 0
  chown "$@"
}
chown_biz -R "${BUSINESS_UID}:${BUSINESS_GID}" "$ROOT"/data "$ROOT"/logs "$ROOT"/run
chmod 0750 "$ROOT/config"
chmod 0755 "$ROOT/data/uploads"

# ⛔⛔ 进程授权锁目录必须 **0700**（属主 = 容器运行 UID，其它人零权限）。
#    后端启动时会逐项校验这个目录：属主不是当前 euid、或 mode 的其它位非 0，
#    都直接拒绝启动 —— 而错误只有一行
#      `phase=process_authority reason=state_unavailable`（**不含原因**）。
#    装到 /opt 下用 umask 027 建出来的目录是 0750 ⇒ 必定卡在这里。
#    （判据见 server/app/openapi/processauthority/local_lock_unix.go 的 secureLocalFile）
chmod 0700 "$ROOT/data/process-authority" "$ROOT/data/updater-state"

# ============================================================ 配置文件 ====
# ZLM API secret：**固定值，不随机**。
# ⛔ 它会被 ZLM 持久化进容器里的 config.ini，随机生成后再改就得两边同时改，
#    漏一边就是「节点在线但点播全失败」。这里与绿色包用同一个值，
#    跨形态迁移时不必再动 ZLM 侧配置。
# ⚠️ 它同时是 hook 回调 URL 里 cap 参数的派生输入（zlm.secret + mediaServerId），
#    所以 change 它等于让所有既有 hook 失效 —— 除非有明确理由，不要改。
ZLM_SECRET="${UVP_ZLM_SECRET:-uvp-9f3c7a1e5d84b206c1a9e4f7b2d6c805}"
# ⛔ config.yml 由**仓库里那份 make-config.py**生成，不在本脚本里手搓：
#    绿色包与容器版共用同一个生成器 ⇒ 两边的配置项永远不会分叉。
MAKE_CONFIG=""
for candidate in "$PKG_DIR/tools/make-config.py" "$PKG_DIR/../standalone/make-config.py"; do
  [ -f "$candidate" ] && MAKE_CONFIG="$candidate" && break
done
[ -n "$MAKE_CONFIG" ] || fail "找不到 make-config.py（应在包内 tools/ 下）"

CONFIG_SOURCE=""
for candidate in "$PKG_DIR/config/config.example.yml" "$PKG_DIR/../../server/config/config.example.yml"; do
  [ -f "$candidate" ] && CONFIG_SOURCE="$candidate" && break
done
[ -n "$CONFIG_SOURCE" ] || fail "找不到 config.example.yml"

log "生成 config.yml"
if [ -n "${UVP_UPGRADE_TRUSTED_KEYS:-}" ]; then
  log "将发行公钥写入 upgrade.trusted_keys"
else
  log "未设置 UVP_UPGRADE_TRUSTED_KEYS：默认空 trusted_keys 配置下离线升级不可用；可在 config.yml 配置 trusted_keys 后重新注册。upgrade.release_source 仍可单独配置。"
fi
"$PY" "$MAKE_CONFIG" \
  --source "$CONFIG_SOURCE" \
  --target "$ROOT/config/config.yml" \
  --http-port "$BACKEND_PORT" \
  --redis-port "$REDIS_PORT" \
  --db-path "./data/uvp.db" \
  --log-path "./resource/logs/uvp-gb28181.log" \
  --zlm-secret "$ZLM_SECRET" \
  --zlm-media-server-id "uvp-media-server-0001" \
  --zlm-api-port "$ZLM_HTTP_PORT" \
  --hook-port "$BACKEND_PORT" \
  --upgrade-trusted-keys "${UVP_UPGRADE_TRUSTED_KEYS:-}" || fail "config.yml 生成失败"
chown_biz "${BUSINESS_UID}:${BUSINESS_GID}" "$ROOT/config/config.yml"
chmod 0640 "$ROOT/config/config.yml"

# ================================================================ 证书 ====
# ⛔ SAN 里必须带 IP：浏览器的证书校验只认 SAN（不看 CN），
#    只写 CN=<IP> 的话用 IP 访问照样 ERR_CERT_COMMON_NAME_INVALID。
log "生成自签证书（含本机 IP 的 SAN）"
TLS_DIR="$ROOT/config/tls"
# ⛔⛔ QR_HOST 必须在这里落地。它有三个下游用途：证书 CN/SAN、安装完成提示里的
#   访问地址、以及扫码配网的基址。原先这个变量**从来没被赋值**，
#   在 set -u 下装到这一步直接 `QR_HOST: unbound variable` 中断 ——
#   而报错位置指向 openssl 调用行，现场完全看不出「是变量没定义」。
#   取不到 IP 时降级成 127.0.0.1：证书只对本机有效，但安装能走完，
#   总比装到一半失败强（提示里也会如实显示 127.0.0.1，不编造地址）。
QR_HOST="$(detect_lan_ip)"
if [ -z "$QR_HOST" ]; then
  QR_HOST="127.0.0.1"
  printf '[uvp-install][WARN] 探测不到本机对外 IP，证书与访问地址按 127.0.0.1 生成。\n' >&2
  printf '  局域网的其它电脑要访问时，请手工用 --port-base 之外的证书配置（见手册「正式证书」一节）。\n' >&2
else
  log "  本机 IP ${QR_HOST}（证书 SAN 与访问地址都用它）"
fi
"$OPENSSL_BIN" req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -subj "/CN=${QR_HOST}" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1,IP:${QR_HOST}" \
  -keyout "$TLS_DIR/uvp.key" -out "$TLS_DIR/uvp.crt" >/dev/null 2>&1 \
  || fail "证书生成失败"
chmod 0644 "$TLS_DIR/uvp.crt"
chmod 0600 "$TLS_DIR/uvp.key"
log "  证书：$TLS_DIR/uvp.crt（自签，浏览器首次访问需选择「继续访问」）"

# ================================================================ 建库 ====
# ⛔⛔ 这一步**不能省，也不能交给后端自己做**：
#   绿色包是 ctl.sh 用 python 读 baseline.sql 建好库再拉起后端（后端不会自己建 SQLite 库）。
#   容器版必须照做，否则后端起来后日志刷一堆 "no such table"，
#   而页面只是"登录失败"，与真因完全对不上。
BASELINE=""
for candidate in "$PKG_DIR/resource/baseline/baseline.sql" \
                 "$PKG_DIR/../../server/resource/database/sqlitebaseline/baseline.sql"; do
  [ -f "$candidate" ] && BASELINE="$candidate" && break
done
[ -n "$BASELINE" ] || fail "找不到 baseline.sql（应在包内 resource/baseline/ 下）"

log "初始化数据库 $ROOT/data/database/uvp.db"
PYTHONIOENCODING=utf-8 "$PY" - "$BASELINE" "$ROOT/data/database/uvp.db" <<'DB_INIT_PY' || fail "建库失败"
import sqlite3
import sys

# ⛔⛔ split_sql 必须与 server/resource/database/sqlitebaseline/generate.py
#    里的同名函数**逐字一致**，也与绿色包 ctl.sh 里内联的那份一致。
#    理由：baseline.sql 里既有 MySQL 转义换行、又有菜单标题/回调 URL 里的分号，
#    想当然的切法会切坏语句，而报错与真实原因毫无关���。
#    ⛔ 改任何一处，三处都要同步（契约锁：scripts/standalone-baseline-loader.test.mjs）。
def split_sql(text):
    statements = []
    buffer = []
    quote = None
    i, n = 0, len(text)

    while i < n:
        ch = text[i]

        if quote is None and ch == "-" and text.startswith("--", i):
            nl = text.find("\n", i)
            i = n if nl < 0 else nl + 1
            continue
        if quote is None and ch == "/" and text.startswith("/*", i):
            end = text.find("*/", i)
            i = n if end < 0 else end + 2
            continue

        if quote is not None:
            if ch == "\\" and quote == "'":
                buffer.append(ch); i += 1
                if i < n:
                    buffer.append(text[i]); i += 1
                continue
            if ch == quote:
                if i + 1 < n and text[i + 1] == quote:
                    buffer.append(ch); buffer.append(text[i + 1]); i += 2
                    continue
                quote = None
            buffer.append(ch); i += 1
            continue

        if ch in "'\"":
            quote = ch; buffer.append(ch); i += 1; continue
        if ch == ";":
            statements.append("".join(buffer)); buffer = []; i += 1; continue
        buffer.append(ch); i += 1

    tail = "".join(buffer).strip()
    if tail:
        statements.append(tail)
    return [s.strip() for s in statements if s.strip()]


baseline_sql, db_path = sys.argv[1], sys.argv[2]

with open(baseline_sql, encoding="utf-8") as _fh:
    sql = _fh.read()
stmts = split_sql(sql)
if not stmts:
    raise SystemExit("建库失败：从 baseline.sql 里切不出任何语句（文件被截断？）")

db = sqlite3.connect(db_path)
# ⛔ 幂等判据是「sys_users 表在不在」，不是「文件在不在」：
#   建库中途失败会留下半成品文件，只看文件存在会让后续跳过建库，
#   表现为"文件在、库是空的"。
try:
    has_table = db.execute(
        "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='sys_users'"
    ).fetchone()[0]
except Exception:
    has_table = 0
if has_table:
    print("  数据库已就绪，跳过初始化")
    db.close()
    sys.exit(0)

# ⛔ 整库放进**一个事务**：逐条提交要 fsync 几百次（实测 2.5s vs 0.4s），
#   更重要的是原子 —— 中途失败整库回滚，不会留下"表建了一半"的库。
db.isolation_level = None
db.execute("BEGIN")
for s in stmts:
    try:
        db.execute(s)
    except sqlite3.Error as exc:
        # 索引名在 SQLite 里是库级唯一，而基线从 MySQL 转来时同名索引会出现在
        # 多张表上 ⇒ 重复的那几条 CREATE INDEX 会被静默跳过，属**预期行为**，不是错误。
        if "CREATE INDEX" in s.upper() or "CREATE UNIQUE INDEX" in s.upper():
            continue
        db.execute("ROLLBACK")
        raise SystemExit("建库失败：%s\n  语句：%s" % (exc, s[:200]))
db.execute("COMMIT")
db.close()
print("  建库完成（%d 条语句）" % len(stmts))
DB_INIT_PY
chown_biz "${BUSINESS_UID}:${BUSINESS_GID}" "$ROOT/data/database/uvp.db" 2>/dev/null || true

# ================================================================ 镜像 ====
if [ "$MODE" = "offline" ]; then
  if [ -z "$OFFLINE_IMAGES" ]; then
    OFFLINE_IMAGES="$(ls "$PKG_DIR"/images/*.tar 2>/dev/null | head -1 || true)"
  fi
  [ -n "$OFFLINE_IMAGES" ] && [ -f "$OFFLINE_IMAGES" ] || fail "离线模式：找不到镜像归档（应在 $PKG_DIR/images/ 下）"
  log "导入离线镜像 $OFFLINE_IMAGES"
  "$DOCKER_BIN" load -i "$OFFLINE_IMAGES" || fail "镜像导入失败"
else
  [ -n "$REGISTRY" ] || fail "在线模式必须传 --registry"
  log "拉取镜像（registry=$REGISTRY）"
  for comp in backend frontend zlm redis; do
    "$DOCKER_BIN" pull "${REGISTRY}/${IMAGE_PREFIX}-${comp}:${VERSION}" \
      || fail "拉取 ${comp} 镜像失败"
    # 统一成安装脚本使用的本地 tag，让 compose.yml 不关心镜像来自哪个仓库。
    "$DOCKER_BIN" tag "${REGISTRY}/${IMAGE_PREFIX}-${comp}:${VERSION}" "${IMAGE_PREFIX}-${comp}:${VERSION}"
  done
fi

# ⛔ 镜像到位后必须**按 tag 逐个确认存在**：上面任一环节静默失败的话，
#    compose up 会用一个不存在的镜像起容器，报错信息完全指不到根因。
for comp in backend frontend zlm redis; do
  "$DOCKER_BIN" image inspect "${IMAGE_PREFIX}-${comp}:${VERSION}" >/dev/null 2>&1 \
    || fail "镜像 ${IMAGE_PREFIX}-${comp}:${VERSION} 不存在（导入或拉取环节有问题）"
done
log "  四个镜像均已就绪"

# ============================================================== 密钥 ====
# ⛔⛔ 两个密钥必须**每套部署各自一份、且持久化**，不能放进镜像、也不能让后端临时生成：
#
#   1) UVP_OPENAPI_MASTER_KEY：OpenAPI 客户端凭据的加密主密钥。
#      格式必须是 **32 字节的无填充 base64url（43 字符）** —— 后端用
#      base64.RawURLEncoding.Strict() 解码（server/app/openapi/routes/runtime.go），
#      标准 base64 的 + / = 会被直接拒。
#      换掉它 ⇒ 已签发的客户端凭据全部解不开，必须重新下发。
#
#   2) UVP_SIP_TRACE_ENCRYPTION_KEY：SIP 报文诊断的加密密钥。
#      这里生成**标准 base64（44 字符，带 = padding）**：后端 decodeEncryptionKey()
#      先试原文 32 字节、再试 base64.StdEncoding、最后 hex；
#      用 token_urlsafe 那种无 padding 的 url-safe 串会让 StdEncoding 解码失败
#      ⇒ 报文诊断静默降级为 degraded（且只有一行 error 日志）。
#
#   ⛔ 这两项缺任意一个，后端会 panic
#      "OpenAPI initialization failed; ingress remains closed" ——
#      因为 InitializeRuntimeWithBindings() 把**所有**失败原因都压成同一个
#      auth.ErrUnavailable，日志里根本看不出是缺密钥。
#      所以 compose.yml 里用 ${VAR:?} 让缺失在解析阶段就报出来。
log "生成部署密钥"
OPENAPI_MASTER_KEY="${UVP_OPENAPI_MASTER_KEY:-}"
SIP_TRACE_KEY="${UVP_SIP_TRACE_ENCRYPTION_KEY:-}"
if [ -z "$OPENAPI_MASTER_KEY" ]; then
  OPENAPI_MASTER_KEY="$("$PY" -c 'import secrets; print(secrets.token_urlsafe(32))')"
fi
if [ -z "$SIP_TRACE_KEY" ]; then
  SIP_TRACE_KEY="$("$PY" -c 'import base64, secrets; print(base64.b64encode(secrets.token_bytes(32)).decode())')"
fi
[ "${#OPENAPI_MASTER_KEY}" = "43" ] \
  || fail "UVP_OPENAPI_MASTER_KEY 必须是 32 字节的无填充 base64url（43 字符），当前 ${#OPENAPI_MASTER_KEY} 字符。
  生成：python3 -c 'import secrets; print(secrets.token_urlsafe(32))'"
log "  密钥已生成（OpenAPI 43 字符 / SIP trace ${#SIP_TRACE_KEY} 字符）"

# =========================================================== 编排文件 ====
log "写入 compose.yml 与 .env"
cp "$PKG_DIR/compose.yml" "$ROOT/compose.yml"
cat > "$ROOT/.env" <<ENVFILE
UVP_ROOT=${ROOT}
UVP_VERSION=${VERSION}
UVP_IMAGE_PREFIX=${IMAGE_PREFIX}
UVP_BUSINESS_UID=${BUSINESS_UID}
UVP_BUSINESS_GID=${BUSINESS_GID}
UVP_HTTPS_PORT=${HTTPS_PORT}
UVP_HTTP_PORT=${HTTP_PORT}
UVP_BACKEND_PORT=${BACKEND_PORT}
UVP_REDIS_PORT=${REDIS_PORT}
UVP_ZLM_HTTP_PORT=${ZLM_HTTP_PORT}
# ⛔ SIP 端口**不进 compose 的 ports**（host 网络不需要映射），它只被后端读去
#    当作引导页默认值（compose.yml 里 UVP_DEFAULT_SIP_PORT）。⛔ 别以为写在这里
#    就等于开了端口 —— 服务端的监听是 SIP 运行时按 gb_sip_config 里的值绑的。
UVP_SIP_PORT=${SIP_PORT}
# ⛔ 节点标识必须与 config.yml 的 gb28181.zlm.mediaserverid 一致：
#    它同时是 hook URL 的 node 参数和 cap 的派生输入（见 zlm-entrypoint.sh）。
UVP_ZLM_MEDIA_SERVER_ID=uvp-media-server-0001
# ⛔⛔ 必须与 config.yml 里 gb28181.zlm.secret 逐字一致 —— 上面 make-config.py
#    那行传的就是这个值，两处是同一份契约。不一致时后端调 ZLM API 全部 401，
#    平台里表现为「媒体节点在线、点播回放全失败」，而容器状态看起来完全正常。
UVP_ZLM_SECRET=${ZLM_SECRET}
UVP_UPDATER_SOCKET_DIR=/run/uvp-updater
UVP_REDIS_MAXMEMORY=512mb
# ⛔ 密钥写进 .env：升级时不要覆盖这个文件里的这两行，换了就等于把所有
#    已签发的 OpenAPI 客户端凭据作废。备份数据目录时它随 $ROOT 一起走。
UVP_OPENAPI_MASTER_KEY=${OPENAPI_MASTER_KEY}
UVP_SIP_TRACE_ENCRYPTION_KEY=${SIP_TRACE_KEY}
ENVFILE
# ⛔ 0640：密钥在里面，不能让同机其它用户读到（compose 以 root/当前用户读即可）
chmod 0640 "$ROOT/.env"

# ⛔ 必须先 config 一次：compose 的变量替换错误（比如少了 UVP_ROOT）
#    在 up 阶段才报的话，报的是一堆看不懂的服务名，而不是"哪个变量没设"。
"$DOCKER_BIN" compose --env-file "$ROOT/.env" -f "$ROOT/compose.yml" -p uvp-gb28181 config >/dev/null \
  || fail "compose.yml 校验失败（变量替换或语法有误，见上）"

# =========================================================== 升级助手 ====
# ⛔⛔ 这一段的位置**不能挪到启动之后**：
#   compose.yml 给后端设了 UVP_UPDATER_GATE_PATH，而后端启动时要求该文件
#   已经存在（缺失直接 `startup dependency failed: configured updater gate is missing`）。
#   gate 是宿主 updater 启动时创建的 ⇒ 必须"先装助手、再 compose up"。
#
# ⛔ 为什么助手必须跑在宿主机、而不是容器里：
#   升级要做的事是「停掉这几个容器 → 换 compose.yml → 重新起」。
#   干这件事的进程如果本身就在被停的容器里，它会在第一步把自己杀掉，
#   升级永远停在半路。所以它是 systemd 服务（uvp-updater.service），
#   容器里的后端只是通过 /run/uvp-updater/updater.sock 发指令。
UPGRADE_INSTALL="$PKG_DIR/tools/uvp-compose-upgrade-install.sh"
if [ ! -f "$UPGRADE_INSTALL" ]; then
  log "⚠️ 包内没有 tools/uvp-compose-upgrade-install.sh：跳过升级助手（页面内升级不可用）"
elif [ "$(id -u)" != "0" ]; then
  # 装 systemd 服务必须 root。非 root 安装（只用于本地试用）就不装，
  # 明确告知，别让人以为装好了。
  log "⚠️ 非 root 安装：跳过升级助手 ——「服务升级」页会显示助手未运行，只能换镜像手工升级"
else
  log "安装升级助手（uvp-updater.service，页面内升级靠它）"
  UVP_ROOT="$ROOT" \
  UVP_BUSINESS_UID="$BUSINESS_UID" \
  UVP_BUSINESS_GID="$BUSINESS_GID" \
  UVP_BACKEND_PORT="$BACKEND_PORT" \
  UVP_UPDATER_SOURCE="$PKG_DIR/updater/uvp-updater" \
    bash "$UPGRADE_INSTALL" || fail "升级助手安装失败（详见上面的报错）"
fi

if [ "$NO_START" = "1" ]; then
  log "已跳过启动（--no-start）。启动命令："
  log "  "$DOCKER_BIN" compose --env-file $ROOT/.env -f $ROOT/compose.yml -p uvp-gb28181 up -d"
  exit 0
fi

# ================================================================ 启动 ====
log "启动服务（首次建库约 1 分钟）"
"$DOCKER_BIN" compose --env-file "$ROOT/.env" -f "$ROOT/compose.yml" -p uvp-gb28181 up -d \
  || fail "启动失败（docker compose logs 查看各容器日志）"

# ⛔ 首次启动要建库 + 生成权限，这一段时间里后端健康检查是失败的。
#    不等就直接打印地址的话，客户打开页面是 502，会误判成"装坏了"。
log "等待后端就绪（最多 5 分钟）"
# ⛔ 探测用 python 而不是 curl：curl 不是每台机器都有（220 上就没有），
#    而 python3 是本脚本的既有前置依赖，用它探测不引入新依赖。
probe_https() {
  "$PY" - "$1" <<'READY_PY'
import ssl
import sys
import urllib.request

context = ssl.create_default_context()
context.check_hostname = False
context.verify_mode = ssl.CERT_NONE
try:
    urllib.request.urlopen("https://127.0.0.1:%s/healthz" % sys.argv[1], timeout=5, context=context).read()
except Exception:
    sys.exit(1)
sys.exit(0)
READY_PY
}
ready=0
for _ in $(seq 1 60); do
  if probe_https "$HTTPS_PORT"; then
    ready=1; break
  fi
  sleep 5
done

if [ "$ready" != "1" ]; then
  printf '\n后端未在预期时间内就绪，最近日志如下：\n' >&2
  "$DOCKER_BIN" compose -f "$ROOT/compose.yml" -p uvp-gb28181 logs --tail 40 backend >&2 || true
  fail "启动未完成。完整日志：docker compose -f $ROOT/compose.yml -p uvp-gb28181 logs"
fi

cat <<SUMMARY

安装完成。

  访问地址   https://${QR_HOST}:${HTTPS_PORT}
  初始账号   admin / 123456（登录后会强制要求改密码）

  ⚠️ 首次进入后会被要求配置 SIP（国标设备注册用），引导页里
     **SIP 端口默认已预填为 ${SIP_PORT}**（= 端口段 ${HTTPS_PORT} 的最后一位）。
     设备侧的 SIP 服务器端口必须填同一个数字（国标设备出厂默认 5060，不会自动跟随）。
     防火墙要放行：TCP ${HTTPS_PORT}-${SEG_END} 与 UDP ${HTTPS_PORT}-${SEG_END}。

  常用命令（在 $ROOT 下执行）：
    docker compose -p uvp-gb28181 ps            # 看各容器状态
    docker compose -p uvp-gb28181 logs -f backend
    docker compose -p uvp-gb28181 down          # 停止（数据保留在 $ROOT/data）

  数据都在 $ROOT 下：config/ data/ logs/ —— 迁移整机只需搬这一个目录。
SUMMARY
