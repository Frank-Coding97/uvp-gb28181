#!/usr/bin/env bash
# UVP 绿色安装包 —— 启停脚本（start / stop / status / restart）
#
# 设计目标：目标机**零预装**。不需要 Docker、nginx、MySQL、Redis，
# 只要一个 x86_64 或 aarch64（ARM64）的 Linux + 可执行权限。
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

# 稳定入口位于安装根；升级后 current 指向 releases/<task_id>，程序和随包
# 依赖从活动版本读取，config/data/logs/run 仍固定在安装根，避免切版本丢数据。
ACTIVE_ROOT="$ROOT"
if [ -L "$ROOT/current" ]; then
  ACTIVE_ROOT="$(readlink -f "$ROOT/current" 2>/dev/null || true)"
  if [ -z "$ACTIVE_ROOT" ]; then
    printf '[uvp][ERROR] current 版本指针不可解析\n' >&2
    exit 1
  fi
fi

# ------------------------------------------------------------ 输出样式 ----
#
# ⛔⛔ 颜色只在**交互终端**下开。输出被重定向到文件、经管道、或进 systemd journal
#   时一律不加转义序列 —— 否则日志里混进 \033[..m，grep/比对/贴给客户看全是噪声。
#   想显式关掉（比如录屏、或终端配色奇怪）：NO_COLOR=1 启动即可（见 no-color.org）。
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_TAG=$'\033[36m'   # [uvp] 标签：青色
  C_OK=$'\033[32m'    # 成功标记：绿色
  C_ERR=$'\033[31m'   # 错误标记：红色
  C_RST=$'\033[0m'
else
  C_TAG=''; C_OK=''; C_ERR=''; C_RST=''
fi

log()  { printf '%s[uvp]%s %s\n' "$C_TAG" "$C_RST" "$*"; }
fail() { printf '%s[uvp][ERROR]%s %s\n' "$C_ERR" "$C_RST" "$*" >&2; exit 1; }

# ⭐ 两级缩进约定（改宽度要连 server/cmd/uvp-setup 里的 indent 常量一起改）：
#   [uvp] xxx        —— 阶段/事件行（由 log 输出）
#         ✓ xxx       —— 该阶段的**结果**行（6 格缩进 = "[uvp] " 的宽度）
#         · xxx       —— 结果之下的补充明细（不抢眼，长行让它自然折行）
#   这么分是因为原来的输出里有的行有 [uvp] 前缀、有的没有，读者无法判断
#   哪一行是"步骤"哪一行是"结论"，扫一眼全是等价的碎句子。
det()  { printf '      %s\n' "$*"; }
ok()   { printf '      %s✓%s %s\n' "$C_OK" "$C_RST" "$*"; }

# ⛔ 本脚本从 2026-10-19 起**不再内嵌任何 Python**：建库、端口同步、secret 对齐、
#   读配置、重置口令全部由包内 `bin/uvp-setup`（纯 Go、静态链接）完成。
#   原因：客户那台 CentOS 7 只带 python2.7，装 Python3 要装一堆依赖 ——
#   而"解压即用"是这个包唯一的卖点。
# ⛔ 也别再把这类逻辑写回 shell 的 heredoc：Python 有"整段一次性编译"的坑
#   （一句语法不认识 ⇒ 一行都不执行、报错行还指向那句语法），
#   而 Go 二进制没有解释器版本这一层，编译期就能发现问题。

# ------------------------------------------------------ 横幅 / 收尾汇总 ----

# print_banner 打印启动横幅（版本号从 version.json 取，取不到就不显示）。
# ⛔ 刻意**不画「左右都封口的方框」**：中文是双宽字符，bash 里没有 wcwidth，
#   任何"左边框 + 右边框"的框子在换字体/换终端后都会歪（右边线对不齐）。
#   用上下两条等长横线做边界，中间行不做右对齐 —— 怎么换都不会歪。
print_banner() {
  local ver="" title
  if [ -f "$ACTIVE_ROOT/version.json" ]; then
    ver="$(grep -m1 '"version"' "$ACTIVE_ROOT/version.json" 2>/dev/null | cut -d'"' -f4 || true)"
  fi
  title="UVP-GB28181  统一视频接入平台"
  if [ -n "$ver" ]; then title="$title  版本号 v$ver"; fi
  printf '\n%s============================================================%s\n' "$C_TAG" "$C_RST"
  printf '%s  %s%s\n' "$C_TAG" "$title" "$C_RST"
  printf '%s============================================================%s\n\n' "$C_TAG" "$C_RST"
  return 0
}

# detect_lan_ip 取本机**对外物理网卡**的 IPv4，用于给出"别的电脑能访问"的地址。
#
# ⛔ 不用 `hostname -I | awk '{print $1}'`：它会把 docker0 / virbr0 排在前面
#   （172.17.0.1 之类），用户照着**虚拟网卡**的地址是打不开的 —— 而且这个坑
#   在装了 Docker 的机器上几乎必然踩到。
# ⭐ 首选 `ip route get`：它问内核"去 1.1.1.1 走哪张网卡"，答的就是默认路由网卡，
#   虚拟网桥/容器网卡没有默认路由 ⇒ 天然被排除。
#   ⚠️ 但装了 VPN/隧道时默认路由**会**落到 tun/wg 上，所以还要再看一眼接口名，
#      是虚拟的就丢弃、改走枚举。
# 兜底：枚举 scope global 的地址并按接口名排除虚拟设备；再不行用 hostname -I。
# 全都取不到就返回空串（调用方据此降级成只显示 127.0.0.1，不编地址）。
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
    # ⛔ hostname -I 会把 docker0(172.17.x) 排在**最前面** ⇒ 直接取第一个会拿到
    #   容器网段地址（用户照它访问必然打不开）。这里**优先挑 192.168./10. 这类
    #   常见内网地址**，实在没有才退回"第一个非 127"。
    #   之所以敢这么挑：能走到这一步说明机器上连 `ip` 命令都没有（极罕见），
    #   没法再按接口名精确排除虚拟网卡，只能靠地址段兜。
    ip="$(hostname -I 2>/dev/null | tr ' ' '\n' \
          | awk '/^192\.168\./ || /^10\./ {print; exit}' || true)"
    if [ -z "$ip" ]; then
      ip="$(hostname -I 2>/dev/null | tr ' ' '\n' \
            | awk '/^[0-9]+\.[0-9]/ && !/^127\./ {print; exit}' || true)"
    fi
  fi
  printf '%s' "$ip"
}

# _firewall_hint 按本机装了哪个防火墙工具，给一条**可直接粘贴**的放行命令。
# 都没装 / 认不出来就返回空串（调用方据此不打这一段，不猜）。
_firewall_hint() {
  local from="$1" to="$2"
  if command -v ufw >/dev/null 2>&1; then
    printf 'sudo ufw allow %s:%s/tcp && sudo ufw allow %s:%s/udp' "$from" "$to" "$from" "$to"
  elif command -v firewall-cmd >/dev/null 2>&1; then
    printf 'sudo firewall-cmd --permanent --add-port=%s-%s/tcp --add-port=%s-%s/udp && sudo firewall-cmd --reload' \
           "$from" "$to" "$from" "$to"
  elif command -v iptables >/dev/null 2>&1; then
    printf 'sudo iptables -I INPUT -p tcp --dport %s:%s -j ACCEPT && sudo iptables -I INPUT -p udp --dport %s:%s -j ACCEPT' \
           "$from" "$to" "$from" "$to"
  fi
  return 0
}

# print_port_plan 打印**按用途分类**的端口规划 + 防火墙放行提示。
#
# ⛔ 之前端口信息散在三处（预检一行、ZLM ini 同步一坨"我改了哪 12 个键"、
#   config.yml 写入一行），而且讲的都是"我做了什么"，用户真正要知道的是
#   "现在开了哪些口、要不要在防火墙上放行"。⇒ 这里给一次**只读快照**，
#   以"是否需要对局域网开放"分类，最后附放行命令。
print_port_plan() {
  local to="${ZLM_RTP_RANGE#*-}" fw="" fw_to=""
  # 段起点取两个 nginx 端口的**较小值**，不写死、也不假设"HTTPS 一定是第一个"。
  # ⛔ 2026-10-19 端口互换后就栽在这：原来直接拿 $NGINX_HTTPS_PORT 当起点，
  #   于是提示变成「连续段 51001-51064」—— 默认入口 51000 反倒被排除在"规划段"之外，
  #   客户照着放行就会漏掉自己正在用的那个端口。互换了顺序也照样对。
  local fw_from="$NGINX_HTTP_PORT"
  if [ "$NGINX_HTTPS_PORT" -lt "$fw_from" ]; then
    fw_from="$NGINX_HTTPS_PORT"
  fi
  # 国标信令端口由**引导页**录入（存 gb_sip_config 表），本脚本无从得知；
  # 这里按规划段末尾的预留位（51064）给出提示，放行命令也把它带上。
  local sip_hint=51064

  # ⛔ 段尾取 max(RTP 段尾, SIP 预留位)：SIP 默认 51064 正好在 RTP 段之后一个，
  #   只放到 RTP 段尾会把 SIP 漏掉 —— 而 SIP 恰恰是**设备接入**能不能通的
  #   关键。放行给**一整段**：规划段本来就是连续无空洞的，一条命令比逐条好记。
  fw_to="$to"
  if [ "$fw_to" -lt "$sip_hint" ]; then fw_to="$sip_hint"; fi

  printf '%s[uvp]%s 端口规划（连续段 %s-%s）\n' "$C_TAG" "$C_RST" "$fw_from" "$fw_to"
  ok "需要对局域网开放"
  if [ "$NGINX_HTTPS_ENABLED" = "1" ]; then
    det "  · 管理页面   ${NGINX_HTTPS_PORT}/tcp（HTTPS） · ${NGINX_HTTP_PORT}/tcp（HTTP 自动跳转）"
  else
    det "  · 管理页面   ${NGINX_HTTP_PORT}/tcp（HTTP；HTTPS 已关闭）"
  fi
  det "  · 后端接口   ${HTTP_PORT}/tcp（API + 设备/App 接入地址）"
  det "  · 国标信令   ${sip_hint}/udp（默认值；实际端口以引导页录入为准）"
  det "  · 媒体服务   ${ZLM_HTTP_PORT}/tcp · ${ZLM_RTSP_PORT}/tcp · ${ZLM_RTMP_PORT}/tcp · ${ZLM_SSL_PORT}/tcp"
  det "  · WebRTC     ${ZLM_RTC_PORT}/tcp · ${ZLM_SIGNALING_PORT}/tcp · ${ZLM_SIGNALING_SSL_PORT}/tcp"
  det "  · 其它媒体   ${ZLM_SRT_PORT}/udp（SRT） · ${ZLM_ONVIF_PORT}/tcp（ONVIF） · ${ZLM_ICE_PORT}/udp（STUN）"
  det "  · 媒体收流   ${ZLM_RTP_RANGE} UDP+TCP（成对占用）"
  ok "仅本机，无需放行"
  det "  · Redis      ${REDIS_PORT}/tcp（只监听 ${REDIS_BIND}）"

  fw="$(_firewall_hint "$fw_from" "$fw_to")"
  if [ -n "$fw" ]; then
    ok "防火墙放行（本机若开了防火墙，执行下面这条）"
    det "  · ${fw}"
  fi
  return 0
}

# print_start_summary 打印启动收尾：访问地址（优先给局域网地址）+ 登录信息。
print_start_summary() {
  local lan lan_url local_url
  local scheme="https"
  [ -x "$NGINX_BIN" ] && [ "$NGINX_HTTPS_ENABLED" = "1" ] || scheme="http"
  local plain_port="$NGINX_HTTP_PORT"
  [ -x "$NGINX_BIN" ] || plain_port="$HTTP_PORT"

  if [ "$scheme" = "https" ]; then
    local_url="https://127.0.0.1:${NGINX_HTTPS_PORT}"
  else
    local_url="http://127.0.0.1:${plain_port}"
  fi

  printf '%s[uvp]%s 启动完成\n' "$C_TAG" "$C_RST"

  lan="$(detect_lan_ip)"
  if [ -n "$lan" ]; then
    if [ "$scheme" = "https" ]; then
      lan_url="https://${lan}:${NGINX_HTTPS_PORT}"
    else
      lan_url="http://${lan}:${plain_port}"
    fi
    ok "管理页面  ${lan_url}"
    det "  · 本机访问  ${local_url}"
  else
    ok "管理页面  ${local_url}"
    det "  · 未识别到对外网卡地址，其它电脑请用本机 IP 访问"
  fi
  local demo_pw=""
  demo_pw="$(read_conf_scalar demoaccount defaultpassword 2>/dev/null || true)"
  if [ -n "$demo_pw" ]; then
    det "  · 登录账号  admin / ${demo_pw}（登录后请立即修改）"
  else
    # ⛔ 读不到就**不编**一个口令出来：只说"见交付说明"（包的 README 里写着）。
    det "  · 登录账号  admin（密码见 README/交付说明，登录后请立即修改）"
  fi
  return 0
}

# ------------------------------------------------------------ 目录布局 ----
# ⛔ 布局固定，不要再往别处塞东西（2026-10-10 整理）：
#   bin/       只有**我们自己的程序**（uvp-server）—— 不再放第三方依赖
#   vendor/    第三方运行时，每个组件自成一体（自带 conf/lib/www）；
#              升级某个组件 = 整目录替换，互不污染
#   config/    全部配置（config.yml + config.env）
#   data/      可写数据（SQLite / Redis / 运行期密钥）
#   logs/      全部日志（含 app/ 后端业务日志、zlm/ ZLM 自带滚动日志）
#   resource/  只读静态资源（前端产物 + 上传目录 + 建库 SQL）
BIN="$ACTIVE_ROOT/bin"
VENDOR="$ACTIVE_ROOT/vendor"
DATA="$ROOT/data"
LOGS="$ROOT/logs"
RUN="$ROOT/run"
CONF="$ROOT/config/config.yml"
RESOURCE="$ACTIVE_ROOT/resource"

BACKEND_PID_FILE="$RUN/backend.pid"
REDIS_PID_FILE="$RUN/redis.pid"
UPDATER_PID_FILE="$RUN/updater.pid"
UPDATER_SOCKET="$RUN/updater.sock"
UPDATER_STATE="$ROOT/updater-state"
UPDATER_GATE_PATH="$ROOT/updater-gate/gate.json"
UPDATER_REGISTRATION="$ROOT/config/updater-registration.json"

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

ENV_FILE="$ROOT/config/config.env"

# ------------------------------------------------ 旧布局迁移（升级兼容）----
#
# ⛔⛔ 为什么必须有这一段：包升级 = **原地 tar 覆盖**，而 tar 不会删掉旧目录。
#   于是升级后「旧布局残留 + 新布局就位」共存，脚本只读新路径 ⇒
#   现场症状是「端口设置/密钥/历史日志像丢了」——其实文件都还在，只是没人读。
#
# ⛔ 迁移只搬「用户数据」，**不搬程序**：
#   · 程序（redis/nginx/zlm）：新包自带 vendor/ 版本 ⇒ 旧的直接删掉；
#   · 数据（config.env、历史日志、自签证书）：新包不带 ⇒ 必须搬过去。
# ⛔ 幂等：旧布局不存在时全程零动作，普通启动没有任何额外开销。

# _merge_env_file 把 $1 的键值行覆盖到 $2 上（旧键优先，新文件独有的键保留）。
# ⛔ 为什么不是直接覆盖：新包的 config.env 带**新生成的密钥**，
#   而旧的带**用户改过的端口**（带 UVP_USER_SET_ 标记）⇒ 两边都有价值，必须合。
_merge_env_file() {
  local old="$1" new="$2" tmp line k
  tmp="$(mktemp "${new}.merge.XXXXXX")"
  cp "$new" "$tmp"
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in ''|'#'*) continue ;; esac
    k="${line%%=*}"
    [ -n "$k" ] || continue
    # ⛔ grep 无匹配时返回 1，在 set -e 下会把脚本带走 ⇒ 必须 || true
    grep -v "^${k}=" "$tmp" > "${tmp}.x" || true
    mv "${tmp}.x" "$tmp"
    printf '%s\n' "$line" >> "$tmp"
  done < "$old"
  mv "$tmp" "$new"
}

migrate_legacy_layout() {
  local changed=0 stamp cfg_dir
  stamp="$(date +%Y%m%d%H%M%S)"
  cfg_dir="$(dirname "$ENV_FILE")"

  # ① config.env：包根 → config/
  # ⛔ 备份也放进 config/（而不是留在包根）：顶层只该有 uvp-gb28181-ctl.sh、
  #   uvp-gb28181-service-{install,uninstall}.sh、version.json 与目录，
  #   一个游离的 config.env.legacy-* 会让人以为「这文件能删吗」。
  if [ -f "$ROOT/config.env" ]; then
    mkdir -p "$cfg_dir"
    cp -a "$ROOT/config.env" "$cfg_dir/config.env.legacy-${stamp}" 2>/dev/null || true
    if [ -f "$ENV_FILE" ]; then
      _merge_env_file "$ROOT/config.env" "$ENV_FILE"
      log "已合并旧 config.env → config/config.env（旧文件留作 config/config.env.legacy-${stamp}）"
    else
      mv "$ROOT/config.env" "$ENV_FILE"
      log "已迁移 config.env → config/config.env"
    fi
    rm -f "$ROOT/config.env"
    changed=1
  fi

  # ② 第三方程序目录 bin/* → vendor/：新包已带 vendor/ 版本，删旧件；
  #    但**运行期数据**（ZLM 历史日志、nginx 自签证书）先搬走再删。
  if [ -d "$ROOT/bin/zlm" ] && [ -x "$VENDOR/zlm/MediaServer" ]; then
    if [ -d "$ROOT/bin/zlm/log" ]; then
      mkdir -p "$LOGS/zlm"
      cp -a "$ROOT/bin/zlm/log/." "$LOGS/zlm/" 2>/dev/null || true
      log "已搬运 ZLM 历史日志 → logs/zlm/"
    fi
    rm -rf "$ROOT/bin/zlm"
    changed=1
  fi
  if [ -d "$ROOT/bin/nginx" ] && [ -x "$VENDOR/nginx/sbin/nginx" ]; then
    local f
    for f in uvp.crt uvp.key; do
      if [ -f "$ROOT/bin/nginx/conf/$f" ] && [ ! -f "$VENDOR/nginx/conf/$f" ]; then
        mkdir -p "$VENDOR/nginx/conf"
        cp -a "$ROOT/bin/nginx/conf/$f" "$VENDOR/nginx/conf/$f"
        log "已搬运自签证书 $f → vendor/nginx/conf/"
      fi
    done
    rm -rf "$ROOT/bin/nginx"
    changed=1
  fi
  # ⛔ 用 if 而不是 `[ -f x ] && rm`：后者条件为假时整条返回 1，
  #   在 set -e 下会直接把脚本带走（本项目踩过同型坑）。
  local rb
  for rb in redis-server redis-cli; do
    if [ -f "$ROOT/bin/$rb" ]; then
      rm -f "$ROOT/bin/$rb"
      changed=1
    fi
  done

  # ③ 后端业务日志 resource/logs → logs/app/
  if [ -d "$ROOT/resource/logs" ]; then
    mkdir -p "$LOGS/app"
    cp -a "$ROOT/resource/logs/." "$LOGS/app/" 2>/dev/null || true
    rm -rf "$ROOT/resource/logs"
    log "已迁移后端业务日志 → logs/app/"
    changed=1
  fi

  # ④ 空壳 scripts/（只删**空**目录，非空一律不动）
  if [ -d "$ROOT/scripts" ] && [ -z "$(ls -A "$ROOT/scripts" 2>/dev/null)" ]; then
    rmdir "$ROOT/scripts"
    changed=1
  fi

  # ⑤ 旧版入口名 uvp-ctl.sh → uvp-gb28181-ctl.sh（2026-10-10 改名）
  # ⛔ 为什么必须删：升级是原地 tar 覆盖，tar 不会删旧文件 ⇒ 老实例顶层会同时
  #   留着旧脚本和新脚本两个「看起来都能用」的入口。而旧脚本引用的是**旧布局**
  #   路径（bin/nginx、bin/zlm、包根的 config.env），迁移完成后那些路径都没了，
  #   敲它只会报一堆「找不到文件」，纯粹是误导。
  # ⛔ 删除的前提是**新入口已就位**——能跑到这里说明正在跑的就是新脚本自己。
  #   要回退只能重新解压旧版包，所以这里不留备份（留一份坏的入口比不留给的坑大）。
  if [ -f "$ROOT/uvp-ctl.sh" ]; then
    rm -f "$ROOT/uvp-ctl.sh"
    log "已删除旧版入口 uvp-ctl.sh（本包入口为 ./uvp-gb28181-ctl.sh）"
    changed=1
  fi

  if [ "$changed" = "1" ]; then
    log "旧布局迁移完成（已在 config/ 下落盘，下次启动不会再跑）"
  fi
  return 0
}

migrate_legacy_layout

upgrade_password_env_name() {
  local configured=""
  if [ -z "${UVP_UPGRADE_DB_PASSWORD_ENV:-}" ]; then
    configured="$(read_env_file UVP_UPGRADE_DB_PASSWORD_ENV 2>/dev/null || true)"
  fi
  printf '%s' "${UVP_UPGRADE_DB_PASSWORD_ENV:-${configured:-UVP_UPGRADE_DB_PASSWORD}}"
}

upgrade_external_backup_max_bytes() {
  local configured=""
  if [ -z "${UVP_UPGRADE_EXTERNAL_BACKUP_MAX_BYTES:-}" ]; then
    configured="$(read_env_file UVP_UPGRADE_EXTERNAL_BACKUP_MAX_BYTES 2>/dev/null || true)"
  fi
  printf '%s' "${UVP_UPGRADE_EXTERNAL_BACKUP_MAX_BYTES:-${configured:-8589934592}}"
}

ensure_updater_registration() {
  [ -x "$UPDATER_BIN" ] || fail "找不到升级助手：$UPDATER_BIN"
  mkdir -p "$ROOT/config" "$UPDATER_STATE" "$RUN"
  chmod 0700 "$UPDATER_STATE" "$RUN"
  local password_env max_backup_bytes
  password_env="$(upgrade_password_env_name)"
  max_backup_bytes="$(upgrade_external_backup_max_bytes)"
  case "$password_env" in
    ''|[!A-Za-z_]*|*[!A-Za-z0-9_]*) fail "外置数据库口令环境变量名无效：$password_env" ;;
  esac
  local business_uid business_gid allowed_uid
  business_uid="${UVP_UPGRADE_BUSINESS_UID:-$(id -u "${SUDO_USER:-$(id -un)}")}" \
    || fail "无法解析业务用户 UID"
  business_gid="${UVP_UPGRADE_BUSINESS_GID:-$(id -g "${SUDO_USER:-$(id -un)}")}" \
    || fail "无法解析业务用户 GID"
  allowed_uid="${UVP_UPGRADER_ALLOWED_UID:-$business_uid}"
  [[ "$business_uid" =~ ^[0-9]+$ && "$business_gid" =~ ^[0-9]+$ && "$allowed_uid" =~ ^[0-9]+$ ]] \
    || fail "升级助手业务 UID/GID 必须是数字"
  "$UPDATER_BIN" register \
    --config-dir "$ROOT/config" \
    --registration "$UPDATER_REGISTRATION" \
    --deployment standalone \
    --root "$ROOT" \
    --state "$UPDATER_STATE" \
    --control "$ROOT/uvp-gb28181-ctl.sh" \
    --allow-uid "$allowed_uid" \
    --business-uid "$business_uid" \
    --business-gid "$business_gid" \
    --password-env "$password_env" \
    --external-backup-max-bytes "$max_backup_bytes" \
    || fail "无法生成升级助手注册文件"
}

prepare_updater() {
  ensure_updater_registration
  # 外置数据库备份口令只通过环境变量交给独立助手；默认不从
  # config.yml 读取并导出，避免升级助手启动时把配置口令带入进程环境。
  # systemd 可用 EnvironmentFile 注入，绿色包也可在 config.env 中显式设置。
  local password_env; password_env="$(upgrade_password_env_name)"
  case "$password_env" in
    ''|[!A-Za-z_]*|*[!A-Za-z0-9_]*) fail "外置数据库口令环境变量名无效：$password_env" ;;
  esac
  if [ -z "$(printenv "$password_env" 2>/dev/null || true)" ]; then
    local configured_password; configured_password="$(read_env_file "$password_env")"
    if [ -n "$configured_password" ]; then
      export "$password_env=$configured_password"
    fi
  fi
}

start_updater() {
  # systemd starts the root-owned helper in its own unit before the business
  # unit. A non-root business process must reuse that socket and never try to
  # rewrite the administrator-owned registration or state directory.
  if [ -S "$UPDATER_SOCKET" ] && [ -f "$UPDATER_PID_FILE" ]; then
    local running_pid; running_pid="$(cat "$UPDATER_PID_FILE" 2>/dev/null || true)"
    if [ -n "$running_pid" ] && kill -0 "$running_pid" 2>/dev/null; then
      log "升级助手已在运行"
      return 0
    fi
  fi
  prepare_updater
  if [ -S "$UPDATER_SOCKET" ] && [ -f "$UPDATER_PID_FILE" ]; then
    local pid; pid="$(cat "$UPDATER_PID_FILE" 2>/dev/null || true)"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      log "升级助手已在运行"
      return 0
    fi
  fi
  rm -f "$UPDATER_SOCKET"
  ( "$UPDATER_BIN" serve --socket "$UPDATER_SOCKET" --state "$UPDATER_STATE" --registration "$UPDATER_REGISTRATION" >"$LOGS/updater.log" 2>&1 & echo $! > "$UPDATER_PID_FILE" ) >/dev/null 2>&1
  for _ in $(seq 1 30); do
    [ -S "$UPDATER_SOCKET" ] && return 0
    sleep 0.1
  done
  tail -30 "$LOGS/updater.log" >&2 || true
  fail "升级助手未能启动"
}

# serve_updater is the systemd entrypoint. It intentionally replaces the
# shell so Type=simple tracks the helper itself instead of a launcher that
# has already exited after backgrounding it.
serve_updater() {
  prepare_updater
  if [ -S "$UPDATER_SOCKET" ]; then
    local pid=""
    if [ -f "$UPDATER_PID_FILE" ]; then
      pid="$(cat "$UPDATER_PID_FILE" 2>/dev/null || true)"
    fi
    if [ -n "$pid" ] && [ "$pid" != "$$" ] && kill -0 "$pid" 2>/dev/null; then
      fail "升级助手已在运行（PID ${pid}）"
    fi
    rm -f "$UPDATER_SOCKET"
  fi
  printf '%s\n' "$$" > "$UPDATER_PID_FILE"
  exec "$UPDATER_BIN" serve --socket "$UPDATER_SOCKET" --state "$UPDATER_STATE" --registration "$UPDATER_REGISTRATION"
}

# ⛔ 端口规划：整段 51000-51064 **连续无空洞**（老板 2026-10-10 定）。
#   顺序：对外在前（nginx → ZLM），内部随后，RTP 动态段收尾，末尾 51064 给 SIP。
#   ⚠️ SIP（51064）不在下面的预检清单里：它由**引导页**录入并写进 gb_sip_config 表，
#      本脚本无从知道现场最终选了哪个端口；保存时若端口被占，引导页会报 bind 失败。
HTTP_PORT_DEFAULT=51002
REDIS_PORT_DEFAULT=51003

read_env_file() {
  # 读 config.env 的 KEY=VALUE；忽略注释与空行；取最后一条（后写的覆盖先写的）
  [ -f "$ENV_FILE" ] || return 0
  sed -n "s/^$1=//p" "$ENV_FILE" 2>/dev/null | tail -1
}

# read_port_env 读**端口类**配置：只认带 UVP_USER_SET_ 标记的键。
#
# ⛔⛔ 为什么端口要单独一套读法（这一轮踩了一整天才搞清）：
#   普通键（如 ZLM 各段）config.env 里有就该用；但端口不行 ——
#   **老包写的 config.env 里全是旧默认值**（8280/6379/443/80）。
#   若无条件读取，客户升级包后端口永远停在旧值上 ⇒ 包里代码是新的、
#   配置是旧的，两者**静默共存**，而且没有任何提示。
#
#   而"这个值是用户设的还是默认值留下的"**无法从值本身判断**：
#   我试过硬编码「上一版默认值」去比 —— 那玩意儿会随我改版本而漂移
#   （改了默认值忘了改判据），而且调试残留值也会被误判成用户设置，
#   两次都栽了。
#   ⇒ 唯一可靠的做法：**写入时就把意图记下来**。老包没有标记，
#   天然按新默认值处理，正好符合"跟着升级"的诉求。
read_port_env() {
  [ -f "$ENV_FILE" ] || return 0
  if [ "$(sed -n "s/^UVP_USER_SET_$1=//p" "$ENV_FILE" 2>/dev/null | tail -1)" = "1" ]; then
    sed -n "s/^$1=//p" "$ENV_FILE" 2>/dev/null | tail -1
  fi
}

HTTP_PORT="${UVP_HTTP_PORT:-$(read_port_env UVP_HTTP_PORT)}"
HTTP_PORT="${HTTP_PORT:-$HTTP_PORT_DEFAULT}"
REDIS_PORT="${UVP_REDIS_PORT:-$(read_port_env UVP_REDIS_PORT)}"
REDIS_PORT="${REDIS_PORT:-$REDIS_PORT_DEFAULT}"
# ⛔ Redis 只监听回环：它没有密码，暴露到公网等于把后端的会话存储敞开。
#   后端与 Redis 同机，走回环即可。
REDIS_BIND="${UVP_REDIS_BIND:-127.0.0.1}"
BACKEND_LOG="$LOGS/backend.log"
REDIS_LOG="$LOGS/redis.log"

# ---- 自家程序 / 第三方运行时（2026-10-10 起分家）----
SERVER_BIN="$BIN/uvp-server"
# 安装助手：建库 / 端口同步 / secret 对齐 / 读配置 / 证书 / 重置口令全走它。
# ⛔ 它是**纯 Go 静态二进制**（CGO_ENABLED=0），不依赖 python、openssl、libc 之外的任何东西。
SETUP_BIN="$BIN/uvp-setup"
UPDATER_BIN="$ROOT/updater/uvp-updater"
REDIS_BIN="$VENDOR/redis/redis-server"
REDIS_CLI="$VENDOR/redis/redis-cli"

# ---- ZLM（二开版，平台靠它推流/取流/录像）----
ZLM_DIR="$VENDOR/zlm"
ZLM_BIN="$ZLM_DIR/MediaServer"
ZLM_PID_FILE="$RUN/zlm.pid"
ZLM_LOG="$LOGS/zlm.log"
# ⛔ ZLM 自带日志（按天滚动的那份）由命令行 `--log-dir` 指定，
#   **不是配置文件项**（见 ZLM server/main.cpp 的 CMD_main 定义）。
#   ⚠️ 它的默认值是 `exeDir() + "log/"` —— 取的是**可执行文件所在目录**，
#   所以即使 cd 走了也照样落在 vendor/zlm/log ⇒ 必须显式指到 logs/zlm。
ZLM_NATIVE_LOG_DIR="$LOGS/zlm"
# ---- ZLM 端口 ----
# ⛔⛔ ZLM 端口的**唯一真源是 vendor/zlm/config.ini**，下面这些变量只是"探测用镜像"。
#   只改变量不改 ini ⇒ 脚本探测 18081、MediaServer 却仍听 80 ⇒ 报「ZLM 启动失败」，
#   而真实原因是端口没写进配置文件。⚠️ 实测踩过这个（设了 18081 仍起不来）。
#   ⇒ sync_zlm_ports_ini() 负责写进去。
ZLM_INI="$ZLM_DIR/config.ini"
ZLM_HTTP_PORT="${UVP_ZLM_HTTP_PORT:-$(read_env_file UVP_ZLM_HTTP_PORT)}"
# ⛔ 默认值一律选**高位端口**，不用 ZLM 自带的 80/443/554。
#   1024 以下需要 CAP_NET_BIND_SERVICE，非 root 起不来（实测：
#   「Listen on :: 554 failed: permission denied」）。
#   绿色包的运行用户就是普通用户，所以默认值必须避开特权区。
ZLM_HTTP_PORT="${ZLM_HTTP_PORT:-51004}"
ZLM_SSL_PORT="${UVP_ZLM_SSL_PORT:-$(read_env_file UVP_ZLM_SSL_PORT)}"
ZLM_SSL_PORT="${ZLM_SSL_PORT:-51007}"
ZLM_RTSP_PORT="${UVP_ZLM_RTSP_PORT:-$(read_env_file UVP_ZLM_RTSP_PORT)}"
ZLM_RTSP_PORT="${ZLM_RTSP_PORT:-51005}"
# 其余对外段也统一到规划段（PORTS.md）
ZLM_RTMP_PORT="${UVP_ZLM_RTMP_PORT:-$(read_env_file UVP_ZLM_RTMP_PORT)}"
ZLM_RTMP_PORT="${ZLM_RTMP_PORT:-51006}"
ZLM_RTC_PORT="${UVP_ZLM_RTC_PORT:-$(read_env_file UVP_ZLM_RTC_PORT)}"
ZLM_RTC_PORT="${ZLM_RTC_PORT:-51008}"
# ⛔ rtp_proxy 端口必须 = RTP 动态段的起点（同一个值，改一个必须改另一个）
ZLM_RTP_PROXY_PORT="${UVP_ZLM_RTP_PROXY_PORT:-$(read_env_file UVP_ZLM_RTP_PROXY_PORT)}"
ZLM_RTP_PROXY_PORT="${ZLM_RTP_PROXY_PORT:-51014}"
# ⛔⛔ RTP 动态端口段必须改！ZLM 默认是 49152-65535，那是 Linux 的
#   **临时端口范围**（客户端出站 connect 随机占用它）⇒ 两者抢端口，
#   表现为「偶发 bind 失败 / 偶发推流失败」，重启就好、复现极难。
# ⭐ 段长 = 默认 50 个（UDP+TCP 成对占用 ⇒ 50 路并发收流）；不够时改这个键放大，
#   但**注意别越过 51063**（51064 是 SIP 预留位，见 PORTS.md）。
ZLM_RTP_RANGE="${UVP_ZLM_RTP_RANGE:-$(read_env_file UVP_ZLM_RTP_RANGE)}"
ZLM_RTP_RANGE="${ZLM_RTP_RANGE:-51014-51063}"
# WebRTC 信令（键名不是 port，按"段名+port"匹配抓不到，实测漏掉导致 ZLM 起不来）
ZLM_SIGNALING_PORT="${UVP_ZLM_SIGNALING_PORT:-$(read_env_file UVP_ZLM_SIGNALING_PORT)}"
ZLM_SIGNALING_PORT="${ZLM_SIGNALING_PORT:-51009}"
ZLM_SIGNALING_SSL_PORT="${UVP_ZLM_SIGNALING_SSL_PORT:-$(read_env_file UVP_ZLM_SIGNALING_SSL_PORT)}"
ZLM_SIGNALING_SSL_PORT="${ZLM_SIGNALING_SSL_PORT:-51010}"
# SRT / onvif：容器默认 9000/3702 在目标机上极易被占，一并挪进规划段
ZLM_SRT_PORT="${UVP_ZLM_SRT_PORT:-$(read_env_file UVP_ZLM_SRT_PORT)}"
ZLM_SRT_PORT="${ZLM_SRT_PORT:-51011}"
ZLM_ONVIF_PORT="${UVP_ZLM_ONVIF_PORT:-$(read_env_file UVP_ZLM_ONVIF_PORT)}"
ZLM_ONVIF_PORT="${ZLM_ONVIF_PORT:-51012}"
# STUN/TURN（icePort / iceTcpPort）：容器默认 3478，UDP 上极易与其它服务冲突
ZLM_ICE_PORT="${UVP_ZLM_ICE_PORT:-$(read_env_file UVP_ZLM_ICE_PORT)}"
ZLM_ICE_PORT="${ZLM_ICE_PORT:-51013}"
# ⛔⛔ TURN 一律**关掉**（`[rtc] enableTurn=0`，老板 2026-10-10 定）。
#   不是"用不到"，而是**根本没接上**：平台的 ICE 下发只有 STUN 一条路 ——
#   `app/gb28181/talk/ice.go` 的 BuildICEServers() 只构造 `"stun:" + endpoint`
#   （该文件注释原文："TURN 凭据在启用中继时再补，**当前不下发**"），
#   全仓 `server/` + `web/src/` 搜 `turn:` **零命中** ⇒ 浏览器永远拿不到
#   中继地址与凭据，ZLM 这个中继服务是**死的**，收益为 0。
#   而它的端口池 `[rtc] port_range` 默认落在 Linux **临时端口区**（49152-65535，
#   客户端出站 connect 会随机占用同一段）—— 留着就等于埋雷：哪天有人把
#   enableTurn 打开，就会和出站连接抢端口，表现为偶发 bind 失败、重启就好。
#   ⇒ 关掉后端口规划仍是 51000-51064，不必再为它划段（原「51065-51099」方案作废）。
#   ⚠️ 将来真要开中继（浏览器在严格 NAT 后面 + 必须用 WebRTC 低延迟），**三步缺一不可**：
#      ① 把 server/cmd/uvp-setup/portsync.go 里 `[rtc] enableTurn` 那条的值改成 "1"
#         （原来还有个 ZLM_ENABLE_TURN 变量在这里，但 Python 内嵌段下线后它没有消费者，
#          留着只会让人以为改它能生效 —— 实际写入 config.ini 的是 Go 里那条 edit）；
#      ② `[rtc] port_range` 挪出临时端口区（如 51065-51099）；
#      ③ 防火墙上放开该段 UDP。只做 ① 等于又埋一次同样的雷。
#   ⚠️ 不给它做 config.env 开关：config.env 里的键**不会自动成为环境变量**
#      （见本文件 392 行附近的说明），写进去会让运维以为改了却没生效。

# ⛔ ZLM 的证书文件：由 nginx 的 crt+key 合成（见 ensure_tls_material），
#   启动时用 `-s` 指给 ZLM。放在 data/secrets/ 而不是 vendor/zlm/ ——
#   vendor/ 是"整目录替换"的第三方件，不该往里写运行期生成的东西。
ZLM_SSL_PEM="$DATA/secrets/zlm-ssl.pem"

ensure_dirs() {
  # ⛔ logs/app 与 logs/zlm 必须显式建：后端与 ZLM 都不会自己创建父目录，
  #   目录不存在时后端启动阶段直接报「open log file failed」退出。
  mkdir -p "$DATA" "$LOGS" "$RUN" "$LOGS/app" "$LOGS/zlm"
}

# ------------------------------------------------------------ 端口预检 ----

# check_ports_free 在**启动任何组件之前**检查端口占用。
#
# ⛔⛔ 为什么必须有这一步（今天真实踩到）：
#   规划到 30000 段时，30000/30001 已被同机另一套服务（livesms）占着。
#   而 nginx 的健康检查只看「端口能不能连」⇒ 别人的服务在监听，
#   就被判成「我的 nginx 起来了」⇒ status 报「运行中」而实际**根本没起**。
#   症状：浏览器访问 30000 通的是**别人的服务**，页面完全不对，
#   而包这边所有自检都显示正常 —— 没有任何一处会报错。
#   ⭐ 这类「静默指向别人的服务」比直接启动失败危险得多：
#   它把**错误的响应**喂给了客户，看起来像系统有响应、只是内容不对。
#
# ⇒ 所以占用检测放在**起服务之前**，且要**明确告诉用户哪个端口被谁占了**。
#   只报「启动失败」没用 —— 用户需要知道去停掉谁、或者改哪个端口。
check_ports_free() {
  local busy="" busy_count=0
  local cannot_judge=0

  # ⛔ 缺 ss 时**必须先告知**：port_holder 一律返回 2（无法判断），下面的循环会
  #   一个占用都报不出来 —— 不说明的话，用户看到的是"预检通过"，而事实是
  #   "一个端口都没检查"（详见 note_no_ss_once 的注释）。
  #   放在这里而不是 port_holder 里：只有当前 shell 才能让"只提示一次"生效。
  note_no_ss_once
  has_ss || cannot_judge=1

  # ---- 逐个检查单端口 ----
  # $1=默认端口 $2=协议 tcp|udp $3=用途 $4=可覆盖的变量名
  # ⛔ 用**实际生效值**而不是默认值：用户改过 config.env 或传了环境变量时，
  #   查默认值等于查了一个没在用的端口 —— 检测形同虚设。
  _check_one() {
    local eff="${!4:-$1}" holder
    holder="$(port_holder "$eff" "$2")" || return 0
    busy_count=$(( busy_count + 1 ))
    busy="${busy}
  ${eff}/$2  $3
      └─ 已被占用：${holder}"
  }

  # 顺序与 PORTS.md 的规划段一致（51000-51064 连续无空洞；51064 SIP 由引导页自检）
  #
  # ⛔⛔ 2026-10-19 端口互换：**51000 是明文入口、51001 才是 HTTPS**（上一版相反）。
  #   这里的端口字面量只是「变量为空时的兜底」，但仍必须与真实默认值一致：
  #   否则哪天变量没取到，预检就去查一个根本没人监听的端口 ⇒ 报"通过"，
  #   而真正的监听端口可能正被别的程序占着（正是这套预检要防的事）。
  if [ "$NGINX_HTTPS_ENABLED" = "1" ]; then
    _check_one 51000 tcp "HTTP（默认入口 + 明文 API）" NGINX_HTTP_PORT
    _check_one 51001 tcp "HTTPS（浏览器访问管理页面）" NGINX_HTTPS_PORT
  else
    _check_one 51000 tcp "HTTP（浏览器访问管理页面 + 明文 API）" NGINX_HTTP_PORT
  fi
  _check_one 51002 tcp "后端 HTTP（API + 扫码接入地址）"  HTTP_PORT
  _check_one 51003 tcp "Redis（仅回环）"                  REDIS_PORT
  _check_one 51004 tcp "ZLM HTTP API（平台开关流）"       ZLM_HTTP_PORT
  _check_one 51005 tcp "ZLM RTSP（设备拉流播放）"         ZLM_RTSP_PORT
  _check_one 51006 tcp "ZLM RTMP"                         ZLM_RTMP_PORT
  _check_one 51007 tcp "ZLM HTTPS"                        ZLM_SSL_PORT
  _check_one 51008 tcp "ZLM WebRTC 媒体"                  ZLM_RTC_PORT
  _check_one 51009 tcp "ZLM WebRTC 信令 WS"               ZLM_SIGNALING_PORT
  _check_one 51010 tcp "ZLM WebRTC 信令 WSS"              ZLM_SIGNALING_SSL_PORT
  _check_one 51011 udp "ZLM SRT"                          ZLM_SRT_PORT
  _check_one 51012 tcp "ZLM ONVIF"                        ZLM_ONVIF_PORT
  _check_one 51013 udp "ZLM STUN/TURN"                    ZLM_ICE_PORT

  # ---- RTP 动态段：整段都要查（UDP/TCP 成对占用）----
  # ⛔ 只查起点是不够的：段内任一端口被占，ZLM 都会在 bind 那一个时退出。
  #   （实测就栽在这类"段中间被占"上。）
  local from="${ZLM_RTP_RANGE%-*}" to="${ZLM_RTP_RANGE#*-}" i proto holder
  for (( i = from; i <= to; i++ )); do
    for proto in tcp udp; do
      holder="$(port_holder "$i" "$proto")" || continue
      busy_count=$(( busy_count + 1 ))
      busy="${busy}
  ${i}/${proto}  ZLM RTP 动态段
      └─ 已被占用：${holder}"
    done
  done

  if [ "$busy_count" -gt 0 ]; then
    log "端口预检失败：${busy_count} 个端口已被占用"
    printf '%s\n' "$busy" >&2
    printf '\n处理办法（按推荐顺序）：\n' >&2
    printf '  1) 停掉占用者 —— 若那不是本系统，说明机器上还有另一套服务在用这些端口。\n' >&2
    printf '  2) 换端口 —— 编辑 config.env 里的对应键，常用键名：\n' >&2
    printf '     UVP_HTTPS_PORT / UVP_HTTP_PORT / UVP_ZLM_HTTP_PORT / UVP_ZLM_RTSP_PORT ...\n' >&2
    printf '  3) 当前端口规划见 deploy/standalone/PORTS.md（规划段 51000-51064，连续无空洞）。\n' >&2
    printf '     ⚠️ SIP（51064）由引导页录入、不在此预检内：若它被占，保存引导页时会报 bind 失败。\n' >&2
    printf '\n  ⚠️ 为什么必须在启动前发现：若端口被别人占着而我们照常启动，\n' >&2
    printf '     nginx 会连到**别人的服务**上，而 status 仍报「运行中」——\n' >&2
    printf '     客户看到的是别的系统的页面，全程却没有任何报错。\n' >&2
    fail "端口被占用，未启动任何服务"
  fi
  # ⛔ 段尾只到 RTP 段尾 —— SIP（默认 51064）**不在本预检内**：它的端口值由
  #   **引导页**录入（存 gb_sip_config 表），出包脚本与启动脚本都无从得知。
  #   完整端口清单在收尾的「端口规划」里给出，这里只报结论。
  # ⛔ 缺 ss 时**不能**报"通过"：那是把"没检查"说成"没问题" ——
  #   宁可少报一句，也不能给一个假的绿灯（上面已用 stderr 说明预检被跳过）。
  if [ "$cannot_judge" -eq 1 ]; then
    log "端口预检：已跳过（本机没有 ss，见上面那条提示）"
    return 0
  fi
  ok "端口预检通过（${NGINX_HTTPS_PORT}-${ZLM_RTP_RANGE#*-} 无冲突）"
}

# has_ss 探测本机有没有 ss（由 iproute2 提供）。
#
# ⛔⛔ 缺它时**绝不能静默放行**：port_holder 的"查不到"会被调用方解释成"端口空闲"，
#   于是整个端口预检变成"全部通过"，而真实情况是**根本没法判断**。
#   最典型的现场是目标机没装 iproute2 —— 预检全绿，真起服务时 nginx 连到别人的
#   服务上，status 反而报「运行中」（见 check_ports_free 顶部那段）。
#   绿色包契约不要求目标机装 iproute2，所以这里只**明确告知已跳过**，不阻断安装。
has_ss() { command -v ss >/dev/null 2>&1; }

_no_ss_noted=""

# note_no_ss_once 就"缺 ss、预检已跳过"告知用户，**只告知一次**。
#
# ⛔⛔ 这个函数只能由 check_ports_free 在**当前 shell**里调，不能写在 port_holder 里：
#   所有调用点都是 `holder="$(port_holder …)"`，命令替换会 fork 子 shell ⇒
#   子 shell 里设的 `_no_ss_noted=1` **传不回父 shell**，于是每查一个端口就重复
#   打一遍（RTP 段 50 个端口 × 2 协议 ⇒ 一百多行）。这条是实测出来的。
# ⛔ 提示走 **stderr**：check_ports_free 的调用方会把 stdout 归到"明细"里，
#   而这条是"需要人看一眼"的告知，必须跳出来。
note_no_ss_once() {
  has_ss && return 0
  [ -z "$_no_ss_noted" ] || return 0
  _no_ss_noted=1
  printf '%s[uvp]%s ℹ️  本机没有 ss（未装 iproute2），**端口预检已跳过**。\n' \
    "$C_TAG" "$C_RST" >&2
  printf '     · 不影响安装：服务照常启动，只是不再提前发现端口冲突。\n' >&2
  printf '     · 若启动后页面打不开或内容不对，请先怀疑端口被别的程序占了。\n' >&2
  printf '     · 想恢复预检：apt install iproute2（Debian/Ubuntu）或 yum install iproute（RHEL/CentOS/麒麟）。\n' >&2
}

# port_holder 返回占用某端口的进程描述；返回码 0=被占用，1=空闲，2=无法判断。
#
# ⛔ 不用 lsof：绿色包不保证目标机装了它。
# ⛔⛔ ss 非 root 时**看不到别人的进程名** —— 而"被谁占的"恰恰是用户最需要的信息
#   （今天就靠它才发现 30000 已被 livesms 占用）。所以先用 ss 判"有没有人听"
#   （这一步非 root 也可靠），再尝试用 sudo -n 拿名字；拿不到就如实说
#   "需 root 才能看到名字"，**绝不编一个名字出来**。
port_holder() {
  local port="$1" proto="$2" flag="-tln"
  [ "$proto" = "udp" ] && flag="-uln"

  # ⛔ 没有 ss 就返回"无法判断"(2)，而不是"空闲"(1)：后者会让预检假装全绿。
  #   （对应的"已跳过"提示由 check_ports_free 统一打，理由见 note_no_ss_once。）
  has_ss || return 2

  if ! ss "$flag" 2>/dev/null | awk -v pat=":$port\$" '$4 ~ pat {found=1} END{exit !found}'; then
    return 1
  fi

  # 有人听⇒ 尝试拿名字。sudo 用 -n（免密），失败就算了，不提示输密码。
  local out
  out="$(sudo -n ss "$flag" -p 2>/dev/null \
        | awk -v pat=":$port\$" '$4 ~ pat' \
        | grep -oE 'users:\(\("[^"]+"' | head -1 | sed 's/users:((\"//; s/"$//')"
  if [ -n "$out" ]; then
    printf '%s' "$out"
    return 0
  fi

  # 拿不到名字时，先判断是不是**本系统自己**（最常见的自占用，值得精确指认）
  case "$port" in
    "$HTTP_PORT")      printf '%s' "本包的后端（先执行 ./uvp-gb28181-ctl.sh stop）"; return 0 ;;
    "$REDIS_PORT")     printf '%s' "本包的 Redis（先执行 ./uvp-gb28181-ctl.sh stop）";  return 0 ;;
    "$ZLM_HTTP_PORT")  printf '%s' "本包的 ZLM（先执行 ./uvp-gb28181-ctl.sh stop）";    return 0 ;;
    "$NGINX_HTTPS_PORT") printf '%s' "本包的 nginx（先执行 ./uvp-gb28181-ctl.sh stop）"; return 0 ;;
  esac

  # ⛔ 格式串里有两个 %s 就必须给两个参数 —— 少给一个不会报错，
  #   只会把剩下的 %s 原样打给用户（实测输出过
  #   「看名字：sudo ss %s -p | grep %s）-tln30001」这种）。
  #   ⇒ 提示文本直接内插，不走 printf 的占位符。
  printf '某个进程（看名字：sudo ss %s -p | grep %s）' "$flag" "$port"
  return 0
}
# ------------------------------------------------------ 外部命令能力探测 ----

# has_pgrep 探测本机有没有 pgrep（由 procps / procps-ng 提供）。
#
# ⛔⛔ 为什么必须探测：绿色包契约是「目标机零预装」，而**最小化容器镜像
#   （python:*-slim / alpine 等）默认不带 procps ⇒ 没有 pgrep**。
#   2026-10-12 实测：容器里 ZLM 明明在 51004 监听（`ss -lntp` 可见），
#   脚本却报「ZLM 未能在预期时间内监听端口 51004」；紧接着 stop 报「已停止」
#   而 ZLM 还在跑 —— 两件怪事同源：判活与「找进程」都只靠 pgrep。
#   ⇒ pgrep 只是**择优路径**：缺了也能用 /proc 扫描拿到同样结论
#     （见 proc_pids_by_exe），所以缺 pgrep **不影响功能**，只影响速度。
has_pgrep() { command -v pgrep >/dev/null 2>&1; }

# proc_pids_by_exe 按**可执行文件绝对路径**列出本包进程的 pid（每行一个）。
#
# ⛔ 为什么不能只有 pgrep 一条路：ZLM 会**daemon 化**（自己 fork，pidfile 里
#   记的那个父进程 pid 随即失效），此时 stop 只能靠「按绝对路径找进程」兜底；
#   而没装 procps 的目标机/容器里根本没有 pgrep ⇒ 兜底整体失效 ⇒
#   **stop 说「已停止」、ZLM 却还在监听**，下次 start 反被端口预检拦下。
#   /proc 由内核提供、不需要任何外部命令，正好把这条路补上。
#   有 pgrep 时仍优先用 pgrep（一次调用，胜过遍历整个 /proc）。
proc_pids_by_exe() {
  local exe="$1" d pid arg p
  # ⛔⛔ 必须排除「调用者自身」：本函数的 argv 文本里可能就写着这个路径
  #   （例如 `bash -c '<脚本正文>'` 这种调用方式），不排除就会把自己算成
  #   目标进程 —— 判活恒为真、stop 直接自杀（2026-10-12 在 Linux 上跑契约
  #   测试时抓到：PIDS 里冒出来的正是测试进程自己的 pid）。
  #   $$ 是脚本主进程、$BASHPID 是当前（子）shell；命令替换会 fork 子 shell，
  #   两者 pid 不同，所以两个都要排。
  if has_pgrep; then
    while IFS= read -r p; do
      [ -n "$p" ] || continue
      if [ "$p" = "$$" ] || [ "$p" = "$BASHPID" ]; then continue; fi
      printf '%s\n' "$p"
    done < <(pgrep -f "$exe" 2>/dev/null || true)
    return 0
  fi
  for d in /proc/[0-9]*; do
    pid="${d#/proc/}"
    if [ "$pid" = "$$" ] || [ "$pid" = "$BASHPID" ]; then continue; fi
    if [ -r "$d/cmdline" ]; then
      # cmdline 是 NUL 分隔的 argv；用 bash 内建 read -d '' 逐段比对，
      # 全程不调外部命令（tr/grep 在精简系统里也不保证存在）。
      while IFS= read -r -d '' arg; do
        case "$arg" in
          *"$exe"*) printf '%s\n' "$pid"; break ;;
        esac
      done < "$d/cmdline" || true
    fi
  done
  return 0
}

# proc_alive_by_exe 该绝对路径的进程是否存在（判活用）。
proc_alive_by_exe() {
  local pids
  pids="$(proc_pids_by_exe "$1")"
  [ -n "$pids" ]
}

# note_no_pgrep_once 缺 pgrep 时提示**一次**（轮询里会被反复调用，不能刷屏）。
# 这是「告知」不是「告警」：功能不受影响，只是走了 /proc 慢路径。
_no_pgrep_noted=""
note_no_pgrep_once() {
  if has_pgrep; then return 0; fi
  [ -z "$_no_pgrep_noted" ] || return 0
  _no_pgrep_noted=1
  log "ℹ️  本机没有 pgrep（未装 procps/procps-ng），已改用 /proc 扫描识别本包进程。
     · 功能不受影响，仅在进程数极多的机器上略微变慢。
     · 想走更快的路径可安装：apt install procps（Debian/Ubuntu）或 yum install procps-ng（RHEL/CentOS/麒麟）。"
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
    # ⛔⛔ pid 已失效时**不能直接 return** —— 必须继续往下走兜底。
    #   实测踩过：ZLM 会 **daemon 化**（自己 fork，ppid 变成 1），
    #   start_zlm 里 `echo $!` 记到的是那个短命的父进程 pid。
    #   ⇒ 第一次 stop 时 kill -0 <父pid> 已失败 ⇒ 原实现直接 return 0，
    #     而**真正在监听端口的 ZLM 永远停不掉**，
    #     症状是「stop 说已停止、端口还占着、再 start 被端口预检拦下」。
    #   ⇒ pid 文件的 pid 失效**不等于**服务停了，两种情况要分开处理。
  fi

  # 兜底：pid 文件不在、或里面那个 pid 已失效 ⇒ 按绝对路径找本包启动的进程
  #
  # ⛔⛔ 匹配用「路径**出现在**命令行里」而不是「路径**开头**」。
  #   实测 redis 被自己的守护逻辑改写了 argv[0]：
  #     /path/bin/redis-server  →  实际 cmdline 是「redis-server 127.0.0.1:16380」
  #   （只剩 basename），所以 ^绝对路径 锚定匹配不到，兜底静默失效 ——
  #   而症状又是「stop 说停了、进程还在」。
  #   ⇒ 去掉 ^ 锚定；安全性靠绝对路径本身足够独特来保证。
  if [ -n "$exe_path" ] && [ -x "$exe_path" ]; then
    local found
    # ⛔⛔ 这里**不能**依赖 pgrep 是否存在：ZLM daemon 化后 pidfile 里的 pid 失效，
    #   按路径找进程就是唯一的希望；而目标机（容器/精简系统）没装 procps 时没有
    #   pgrep ⇒ 原实现直接「找不到就走人」⇒ stop 报「已停止」而进程还活着，
    #   下次 start 反被端口预检拦下（2026-10-12 容器实测）。
    #   proc_pids_by_exe 在缺 pgrep 时改读 /proc，两种环境都能拿到结果。
    # ⛔ 必须带 `|| true`：**"没找到进程"是这里的正常情况**（服务本来就没跑），
    #   而 pgrep 无匹配时退出码是 1，`var="$(cmd)"` 这条赋值语句的退出码
    #   就等于 cmd 的⇒ `set -e` 直接把整个脚本杀掉。
    #   实测症状：stop **零输出、退出码 1**，而且恰好停在"兜底什么都没找到"这条路
    #   —— 也就是说**越是该静默的情况，它越会崩**。
    found="$(proc_pids_by_exe "${exe_path}" || true)"
    if [ -n "${found//$'\n'/ }" ]; then
      log "${label} 的 pid 文件缺失，按可执行路径找到遗留进程（${found//$'\n'/ }），正在停止"
      # shellcheck disable=SC2086  # found 是换行分隔的 pid 列表，需按 IFS 分词
      kill -TERM $found 2>/dev/null || true
      for _ in $(seq 1 40); do
        proc_alive_by_exe "${exe_path}" || break
        sleep 0.25
      done
      local still
      still="$(proc_pids_by_exe "${exe_path}" || true)"
      if [ -n "$still" ]; then
        # shellcheck disable=SC2086  # 同上，仍是换行分隔的 pid 列表
        kill -KILL $still 2>/dev/null || true
      fi
    fi
  fi
}

# ---------------------------------------------------------------- Redis ----

redis_healthy() {
  "$REDIS_CLI" -h "$REDIS_BIND" -p "$REDIS_PORT" ping 2>/dev/null | grep -q PONG
}

start_redis() {
  if redis_healthy; then
    log "Redis 已在运行（端口 ${REDIS_PORT}）"
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
      ok "Redis 启动成功（端口 ${REDIS_PORT}）"
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
    log "后端已在运行（端口 ${HTTP_PORT}）"
    return 0
  fi
  [ -x "$SERVER_BIN" ] || fail "找不到可执行的 $SERVER_BIN"
  [ -f "$CONF" ]    || fail "找不到配置文件 $CONF"
  [ -f "$ACTIVE_ROOT/version.json" ] || fail "找不到 version.json（后端启动会读它，缺了日志里全是噪音）"

  # ---- 把「后端要读的环境变量」从 config.env 显式导出到子进程 ----
  #
  # ⛔⛔ 为什么必须显式导出：`read_env_file` 只是**按需取值**，
  #   `config.env` 里的东西并不会自动成为进程环境变量 ——
  #   而 SIP 报文诊断的加密密钥是走 `os.Getenv(envName)` 读的（trace/crypto.go），
  #   不导出 ⇒ 后端永远读不到 ⇒ 报文诊断无法落库（fail-closed，不存明文，但功能不可用）。
  #
  # ⛔ 键名与 config.yml 里的 `gb28181.trace.encryption_key_env` **必须一致**，
  #   这里只认这一条 —— 别顺手加更多变量：config.env 是明文文件，
  #   往里放不该落盘的东西会随包分发。
  #
  # 用法（config.env 里加一行，重启即生效）：
  #   UVP_SIP_TRACE_ENCRYPTION_KEY=<32 字节，原文 / base64 / hex 均可>
  # 密钥缺失时的表现：报文诊断整体不落库（安全，但菜单点进去是空的）。
  SIP_TRACE_KEY_ENV="UVP_SIP_TRACE_ENCRYPTION_KEY"
  sip_trace_key="$(read_env_file "$SIP_TRACE_KEY_ENV")"
  if [ -n "$sip_trace_key" ]; then
    export "$SIP_TRACE_KEY_ENV=$sip_trace_key"
  fi

  # ---- OpenAPI 主密钥 ----
  #⛔ 同一个坑（read_env_file 只是按需取值、不会让 config.env 成为进程环境变量）。
  #   后端用 os.Getenv 读它，缺失时 OpenAPI 装配直接 ErrUnavailable ⇒ 客户端页 503，
  #   而报错**不说是哪一道检查没过**。格式：32 字节**无填充 base64url**。
  OPENAPI_MASTER_KEY_ENV="UVP_OPENAPI_MASTER_KEY"
  openapi_master_key="$(read_env_file "$OPENAPI_MASTER_KEY_ENV")"
  if [ -n "$openapi_master_key" ]; then
    export "$OPENAPI_MASTER_KEY_ENV=$openapi_master_key"
  fi
  export UVP_STANDALONE_ROOT="$ROOT"
  export UVP_VERSION_PATH="$ACTIVE_ROOT/version.json"
  export UVP_UPDATER_SOCKET="$UPDATER_SOCKET"
  export UVP_UPDATER_STATE="$UPDATER_STATE"
  export UVP_UPDATER_GATE_PATH="$UPDATER_GATE_PATH"

  # ⛔ 绝不能关掉子 shell 的 stdio：`uvp-gb28181-ctl.sh start | tail` 这类用法会永远挂住
  #   （孤儿子 shell 攥着管道的写端），看起来像「启动卡死」。实测挂过 10 分钟以上。
  ( "$SERVER_BIN" >"$BACKEND_LOG" 2>&1 </dev/null & echo $! > "$BACKEND_PID_FILE" ) >/dev/null 2>&1

  for _ in $(seq 1 150); do
    if backend_healthy; then
      ok "后端启动成功（端口 ${HTTP_PORT}）"
      return 0
    fi
    # 进程已死就别再等了
    if [ -f "$BACKEND_PID_FILE" ]; then
      local pid; pid="$(cat "$BACKEND_PID_FILE")"
      if ! kill -0 "$pid" 2>/dev/null; then
        log "后端进程已退出，最后 30 行日志："
        tail -30 "$BACKEND_LOG" >&2 || true
        rm -f "$BACKEND_PID_FILE"
        fail "后端启动失败（详见 ${BACKEND_LOG}）"
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

migrate_backend() {
  check_preconditions
  [ -x "$SERVER_BIN" ] || fail "找不到后端可执行文件：$SERVER_BIN"
  # Keep database migration in the application migration runner. The updater
  # invokes this fixed subcommand only after the service has stopped; it never
  # accepts SQL, paths, or command text from the upgrade request.
  ensure_database
  sync_ports_into_config
  export UVP_STANDALONE_ROOT="$ROOT"
  export UVP_VERSION_PATH="$ACTIVE_ROOT/version.json"
  export UVP_UPDATER_STATE="$UPDATER_STATE"
  export UVP_UPDATER_GATE_PATH="$UPDATER_GATE_PATH"
  log "执行应用迁移"
  "$SERVER_BIN" -migrate-up
}

# ---------------------------------------------------------------- 入口 ----

check_preconditions() {
  ensure_dirs
  # ⛔ 架构要**两头都查**，缺一头都会在现场留一个难懂的症状：
  #   ① 宿主 uname -m 必须落在本包支持的两族里（x86_64 / aarch64）；
  #   ② 包内后端二进制的 ELF e_machine 必须与宿主一致 —— 拿错架构的包时，
  #      后端起不来只会打一行 `Exec format error`，没人会往"包拿错了"想。
  #      e_machine 在 ELF 头 offset 18（2 字节，小端）：3e00=x86-64，b700=AArch64。
  #      od 读不到时（缺 coreutils 等）**只跳过 ②**，不让它把 ① 的正确判断变成误报。
  local host_arch want_elf pkg_elf
  host_arch="$(uname -m)"
  case "$host_arch" in
    x86_64|amd64)  want_elf="3e00" ;;
    aarch64|arm64) want_elf="b700" ;;
    *) fail "本包只支持 x86_64 / aarch64，当前架构 ${host_arch}" ;;
  esac
  if [ -r "$SERVER_BIN" ]; then
    pkg_elf="$(od -An -tx1 -j18 -N2 "$SERVER_BIN" 2>/dev/null | tr -d ' \n')"
    if [ -n "$pkg_elf" ] && [ "$pkg_elf" != "$want_elf" ]; then
      fail "包内后端二进制的 ELF 架构（e_machine=0x${pkg_elf}）与本机（${host_arch}）不匹配 —— 是不是拿错了架构的包？"
    fi
  fi
  [ -f "$CONF" ] || fail "缺少 $CONF —— 解压是否完整？"
  check_setup_bin
}

# check_setup_bin 确认包内安装助手可用。
#
# ⛔ 它是启动链上**唯一**的外部能力来源（建库/端口同步/secret/证书/重置口令），
#   缺了就等于整个包不可用，必须在开动任何组件之前说清楚 —— 而不是等到
#   "页面能开、登录报 no such table" 才让人去猜。
# ⛔ 不能给它 chmod +x 兜底：目标机若挂载了 noexec（不少政企的安全基线会这么做），
#   chmod 之后照样不能执行，反而把真因藏起来。这里只报清楚。
check_setup_bin() {
  [ -f "$SETUP_BIN" ] || fail "缺少 ${SETUP_BIN} —— 包不完整（请重新解压整个压缩包）"
  [ -x "$SETUP_BIN" ] || fail "${SETUP_BIN} 没有执行权限 —— 多半是解压工具丢了权限位。
      请执行：chmod +x ${SETUP_BIN} 后重试"
}

# ------------------------------------------------------------ nginx ----
# nginx 只做两件事：托管前端 + HTTPS 终止，后端仍是纯 HTTP 的
# @BACKEND_PORT@（二维码里填的就是它，App 因此完全不碰 TLS）。
NGINX_DIR="$VENDOR/nginx"
NGINX_BIN="$NGINX_DIR/sbin/nginx"
NGINX_CONF="$RUN/nginx.conf"
NGINX_PID_FILE="$RUN/nginx.pid"
NGINX_LOG="$LOGS/nginx.log"
NGINX_CONF_TEMPLATE="$NGINX_DIR/conf/nginx.conf.template"
# Certificates belong to the installation, not an interchangeable release.
NGINX_CRT="$ROOT/vendor/nginx/conf/uvp.crt"
NGINX_KEY="$ROOT/vendor/nginx/conf/uvp.key"

# HTTPS 端口：给客户换端口时只改这里（同时也在 config.env 里）
#
# ⛔⛔ 2026-10-19 端口重排（老板定）：**HTTP 51000 / HTTPS 51001**。
#   上一版正好相反（HTTPS 51000 / HTTP 51001）。换回来不是为了好看：
#   客户拿到的地址里 51000 是最短最好记的那个，明文是默认入口，
#   该给它。⇒ 两个默认值**成对**改，只改一个就等于让两个 listen 抢同一端口。
NGINX_HTTPS_PORT_DEFAULT=51001
NGINX_HTTP_PORT_DEFAULT=51000
NGINX_HTTPS_PORT="${UVP_HTTPS_PORT:-$(read_port_env UVP_HTTPS_PORT)}"
NGINX_HTTPS_PORT="${NGINX_HTTPS_PORT:-$NGINX_HTTPS_PORT_DEFAULT}"
# ⛔ 默认不开 HTTPS：新装的管理页走明文 HTTP，由管理员在「系统配置 → HTTPS」
#   里自行开启。后端 uvp-setup https-render 首次启动会把这份默认落盘到
#   data/https/settings.json，之后的启停与端口规划都以落盘值为准。
#   （存量实例已落盘的不受影响；不要为了"默认开"改回 1。）
NGINX_HTTPS_ENABLED=0
if [ -f "$DATA/https/settings.json" ]; then
  # ⛔ 分两条语句：写成一整行时命令替换的退出码会被 read 吞掉，
  #   配置文件坏掉也会"读成 0 0"继续往下跑，最后 nginx 去监听 0 号端口。
  _https_state="$("$SETUP_BIN" https-state --root "$ROOT" --default-port "$NGINX_HTTPS_PORT")" \
    || fail "读取 HTTPS 配置失败（${DATA}/https/settings.json）"
  read -r NGINX_HTTPS_ENABLED NGINX_HTTPS_PORT <<< "$_https_state"
fi
# ⛔⛔ 变量名必须与后端的 UVP_HTTP_PORT 区分开。实测踩过：nginx 的明文端口
#   曾经也叫 UVP_HTTP_PORT，于是后端设 8390 时nginx 也去 bind 8390 ⇒
#   `bind() to 0.0.0.0:8390 failed: Address already in use`，
#   而报错完全看不出是「两个组件抢同一个端口」。
NGINX_HTTP_PORT="${UVP_NGINX_HTTP_PORT:-$(read_port_env UVP_NGINX_HTTP_PORT)}"
NGINX_HTTP_PORT="${NGINX_HTTP_PORT:-$NGINX_HTTP_PORT_DEFAULT}"

# ---- 存量实例的 HTTPS 端口迁移（端口重排的一次性收尾）----
#
# ⛔⛔ 为什么必须有这一段：`data/https/settings.json` 在**首次启动时无条件落盘**
#   （httpsconfig.RenderStartup），存量实例里存的是上一版的 HTTPS 端口 51000。
#   而 51000 现在成了本包的**明文 HTTP 端口** ⇒ 两个 listen 抢同一端口：
#     · HTTPS 开着   → nginx `duplicate listen` / bind 失败，页面直接打不开；
#     · HTTPS 关着   → Render 把两段都渲染成 51000，同样重复。
#   若交给 https-render 报错，客户看到的是「HTTPS 端口不能与 HTTP 端口相同」，
#   完全指不到"升级导致的"这一层。⇒ 在读出来之后、渲染 nginx 之前就地纠正。
#
# ⛔ 只在**落盘端口 == 本包明文端口**时才动：这是唯一无法共存的组合。
#   用户手工设过别的值（如 8443）一律不碰 —— 那是他的选择，不是历史残留。
if [ "$NGINX_HTTPS_PORT" = "$NGINX_HTTP_PORT" ]; then
  log "HTTPS 端口 ${NGINX_HTTPS_PORT} 与本包明文端口相同（旧版遗留），自动迁移到 ${NGINX_HTTPS_PORT_DEFAULT}"
  "$SETUP_BIN" https-repair-port --root "$ROOT" \
    --conflict "$NGINX_HTTP_PORT" --fallback "$NGINX_HTTPS_PORT_DEFAULT" \
    || fail "HTTPS 端口迁移失败（${DATA}/https/settings.json）—— 两个监听不能同端口"
  NGINX_HTTPS_PORT="$NGINX_HTTPS_PORT_DEFAULT"
fi

# ⛔ nginx 的目录与端口**只在上面那段定义一次**，此处不再重复
#   （原有一份完全相同的目录变量定义块，2026-10-10 删除）。
# ⛔ nginx 端口只在上面「# ---- nginx ----」段里定义一次。
# ⛔⛔ 这里曾有过第二组定义（旧版 443/80，且用 UVP_HTTP_PORT 而非
#   UVP_NGINX_HTTP_PORT）—— bash 里**后定义覆盖先定义**，于是：
#   ① nginx 的明文端口被读成**后端**的端口（UVP_HTTP_PORT）；
#   ② 改了 30000/30001 的新默认值被这组旧的重新盖回 443/80。
#   症状是「明明改了端口，nginx 还是绑443/80」且毫无提示。
#   ⇒ 端口变量必须单点定义；要改就改那一处。

# ------------------------------------------------------------ nginx ----

# resolve_nginx_worker 决定 nginx 的 `user` 指令该写什么（返回「用户 组」两个名字）。
#
# ⛔⛔ 为什么必须显式声明、且必须把**组**也写出来：
#   源码编译的 nginx 默认 user/group 都是 nobody，运行期按**名字**解析；
#   而 Debian 系（Ubuntu / 统信 UOS）里 nobody 用户的**主组叫 nogroup**，
#   系统里根本没有 nobody 这个组 ⇒ 报 `getgrnam("nobody") failed`，nginx 起不来。
#   现场症状极具误导性：ZLM / Redis / 后端全部正常，**只有 nginx 挂**。
# ⛔ 只写 `user nobody;` 同样不行：nginx 在只给一个参数时会去查
#   「与用户同名的组」（不是该用户的主组），Debian 上照样失败（2026-10-11 实测）。
# ⇒ 用 `id -gn` 问出该用户**实际的主组名**填进去，跨发行版通用：
#     RPM 系（openEuler/麒麟）  → nobody 的主组是 nobody  ⇒ `user nobody nobody;`
#     Debian 系（Ubuntu/UOS）   → nobody 的主组是 nogroup ⇒ `user nobody nogroup;`
# ⛔ 不要"猜"发行版再分支 —— 同一发行版不同版本也可能不一样，问系统才是准的。
resolve_nginx_worker() {
  local u grp
  for u in nobody nginx www-data daemon; do
    grp="$(id -gn "$u" 2>/dev/null)" || continue
    [ -n "$grp" ] || continue
    printf '%s %s' "$u" "$grp"
    return 0
  done
  return 1
}

# render_nginx_conf 从模板生成实际配置：把 @ROOT@ / @BACKEND_PORT@ / 端口
# 替换成真实值。
#
# ⛔ 为什么不直接把配置写死在包里：安装目录、客户选的端口都是运行期才知道的。
#   而每次start 都重写一遍也安全 —— 模板在包里是只读的，生成物在 run/ 下。
# ⛔ 用 sed 替换 @...@ 这种带@ 的标记：nginx 配置文件里 @ 有特殊含义吗？没有。
#   但 **# 和 & 在替换串里有特殊含义**，所以只出现固定文本，不会踩到。
render_nginx_conf() {
  [ -f "$NGINX_CONF_TEMPLATE" ] || {
    log "⚠️  缺少 nginx 配置模板（${NGINX_CONF_TEMPLATE}），跳过 nginx"
    return 1
  }

  # ⛔ nginx 工作进程身份：见 resolve_nginx_worker 的注释（Debian 系缺 nobody 组）。
  #   填进去的是「整行指令」，因为取值失败时要整行换成注释（不能让
  #   `user ;` 这种半截指令进配置 —— 那会是一个与真因无关的语法错误）。
  local worker_line worker_pair
  if worker_pair="$(resolve_nginx_worker)"; then
    worker_line="user ${worker_pair};"
  else
    worker_line="# ⚠️ 未设置 user：本机没有可用的 nginx 运行账号（试过 nobody/nginx/www-data/daemon）"
    log "⚠️  未找到可用的 nginx 运行账号，将保留 nginx 编译默认值（若发行版缺 nobody 组会启动失败）"
  fi

  # ⛔⛔ 只做「@占位符@ → 字面值」这一种替换，不要去匹配 `listenNNNssl;` 这类文本。
  #   实测踩过：模式 `listen      80;` 会把 80 误换成**后端端口**（因为它同样匹配
  #   别的行），而带空格的 `listen      443 ssl;` 又匹配不上 ⇒ 一行都没换对。
  #   模板里端口已经写成 @HTTPS_PORT@ / @PLAIN_HTTP_PORT@，这里只负责填值。
  sed -e "s|@ROOT@|$ROOT|g" \
      -e "s|@ACTIVE_ROOT@|$ACTIVE_ROOT|g" \
      -e "s|@BACKEND_PORT@|$HTTP_PORT|g" \
      -e "s|@HTTPS_PORT@|$NGINX_HTTPS_PORT|g" \
      -e "s|@PLAIN_HTTP_PORT@|$NGINX_HTTP_PORT|g" \
      -e "s|@NGINX_WORKER_DIRECTIVE@|$worker_line|g" \
      "$NGINX_CONF_TEMPLATE" > "$NGINX_CONF"
  "$BIN/uvp-setup" https-render --root "$ROOT" || fail "应用 HTTPS 配置失败"


  # ⛔ 渲染后自检：模板里不该再有 @...@。有就是漏了某个占位符，
  #   而 nginx 会把它当成一个畸形的路径/指令，报的错与真因完全无关。
  if grep -q '@[A-Z_]*@' "$NGINX_CONF"; then
    log "nginx 配置里仍有未替换的占位符："
    grep -n '@[A-Z_]*@' "$NGINX_CONF" >&2 || true
    fail "nginx 配置模板与本脚本不匹配（占位符未全部替换）"
  fi
  return 0
}

# ensure_self_signed_cert 没有证书时安装证书。
#
# ⛔⛔ 2026-10-19 起：**证书随包自带**（resource/cert/uvp.crt + uvp.key），
#   安装时只做"拷贝"，不再现场生成。原因：
#   ① 客户那台 CentOS 7 上什么都可能没有，而 openssl 也不是必装件；
#   ② 现场生成 = 又一个可能失败/超时的步骤，失败了还得靠日志才看得出；
#   ③ 内置证书的**有效期起点可以写在过去**（见构建仓里那份），
#      现场机器时间不准（内网机器很常见）也不会被判"证书尚未生效"。
#   ⚠️ 代价：所有实例共用同一把私钥 ⇒ 它只能用于"内网自签、浏览器点继续"。
#   需要每台机器一把唯一私钥的客户，执行：
#       ./bin/uvp-setup certgen --dir vendor/nginx/conf --zlm-pem data/secrets/zlm-ssl.pem --force
#   之后 restart 即可（本函数只在证书缺失时才动手，不会覆盖它）。
# ⛔ 生成成功**不出声**（老板 2026-10-10 定：这属于"本该就有"的步骤，
#   写在启动输出里是噪音）；失败才报，且必须先试 nginx 再报。
BUNDLED_CRT="$RESOURCE/cert/uvp.crt"
BUNDLED_KEY="$RESOURCE/cert/uvp.key"
ensure_self_signed_cert() {
  [ -f "$NGINX_CRT" ] && [ -f "$NGINX_KEY" ] && return 0
  mkdir -p "$(dirname "$NGINX_CRT")"

  # ① 包内自带（正常路径）
  if [ -f "$BUNDLED_CRT" ] && [ -f "$BUNDLED_KEY" ]; then
    cp -f "$BUNDLED_CRT" "$NGINX_CRT" || { log "⚠️  写入 ${NGINX_CRT} 失败"; return 1; }
    cp -f "$BUNDLED_KEY" "$NGINX_KEY" || { log "⚠️  写入 ${NGINX_KEY} 失败"; return 1; }
    chmod 644 "$NGINX_CRT" 2>/dev/null || true
    chmod 600 "$NGINX_KEY" 2>/dev/null || true
    return 0
  fi

  # ② 兜底：包内的 Go 助手现场签一张（老包没有 resource/cert 时走这里）。
  if [ -x "$SETUP_BIN" ]; then
    if "$SETUP_BIN" certgen --dir "$(dirname "$NGINX_CRT")" --cn uvp-local --days 3650 \
         >>"$NGINX_LOG" 2>&1; then
      return 0
    fi
    log "⚠️  自签证书生成失败，nginx 不会启动（详见 ${NGINX_LOG}）"
    return 1
  fi
  log "⚠️  包内既没有证书（${BUNDLED_CRT}）也没有 ${SETUP_BIN} —— 请重新解压完整安装包"
  return 1
}

# 首次初始化时由 Nginx 证书合成 ZLM 独立 PEM（私钥在前）。
# 后续证书更新由 HTTPS 管理功能显式同步，不在普通启动时自动覆盖。
ensure_tls_material() {
  # 首次初始化后独立保存，页面未选择同步时，重启不得覆盖 ZLM。
  [ -s "$ZLM_SSL_PEM" ] && return 0
  ensure_self_signed_cert || return 1
  [ -f "$NGINX_KEY" ] && [ -f "$NGINX_CRT" ] || return 0

  mkdir -p "$(dirname "$ZLM_SSL_PEM")"
  local tmp="${ZLM_SSL_PEM}.tmp"
  cat "$NGINX_KEY" "$NGINX_CRT" > "$tmp" 2>/dev/null || { rm -f "$tmp"; return 0; }
  chmod 600 "$tmp" 2>/dev/null || true
  # 幂等：内容没变就不动它（免得每次 start 都改 mtime，运维会以为配置又被动过）
  if cmp -s "$tmp" "$ZLM_SSL_PEM"; then
    rm -f "$tmp"
  else
    mv -f "$tmp" "$ZLM_SSL_PEM"
  fi
  return 0
}

# nginx_healthy 以「**HTTPS 端口能连上**」为准，不看 pid 文件。
#
# ⛔⛔ 实测：nginx 启动时若 bind 失败，master 会退出，但已经 fork 出来的 worker
#   还活着并继续持有端口 —— 此刻 pid 文件是**0 字节**（master 没来得及写），
#   而 8443 照样能curl 通。于是「按 pid 判活」会误报"未运行"，
#   而 stop 也停不掉（stop_by_pidfile 依赖 pid 文件）。
#   ⭐ 端口能连 = 真的在服务，这才是用户关心的。
# nginx_healthy 要求**端口通**且**是本包的 nginx 在服务**。
#
# ⛔⛔ 只探端口会把**别人的服务**误认成自己的（今天真实踩到）：
#   规划到 30000 段时 30000/30001 已被同机另一套服务（livesms）占着，
#   而"端口能连"对它一样成立 ⇒ status 报「运行中」而我们的 nginx 根本没起。
#   最坏情况：浏览器打开 30000，看到的是**别的系统**的页面，
#   而包这边所有自检都显示正常 —— 全程零报错，最难查的一类问题。
#   ⭐ 与 ZLM 同一个道理：端口可连≠ 服务是我们的。判据必须是**进程**。
nginx_healthy() {
  # ⛔⛔ 关闭 fd 必须写进**子 shell**。
  #   实测踩过：写成裸的 `exec 3<&- 3>&- 2>/dev/null || true` 时，
  #   它会把**整个脚本的 shell 替换掉** —— 在 `set -Eeuo pipefail` 下，
  #   `exec` 作用于当前 shell 且关闭一个从未打开的 fd 会中断执行，
  #   后面的语句（包括调用方 `stop_nginx` 的收尾）**全部不再执行**。
  #
  #   症状极具误导性：stop 的退出码是 1、**一行输出都没有**，
  #   而 ZLM 还在跑 —— 看起来像"stop 失败了"，实际是"stop 把自己杀了"。
  #   只有用探针插进 || 分支（发现 close 之后那句没打）才定位到。
  local entry_port="$NGINX_HTTPS_PORT"
  [ "$NGINX_HTTPS_ENABLED" = "1" ] || entry_port="$NGINX_HTTP_PORT"
  (exec 3<>"/dev/tcp/127.0.0.1/$entry_port") 2>/dev/null || {
    ( exec 3<&- 3>&- ) 2>/dev/null || true
    return 1
  }
  exec 3<&- 3>&-
  # 按可执行文件**绝对路径**匹配：按名字会命中同机其它 nginx
  # （这台机器上就Docker 里跑了 27 天的 nginx）。缺 pgrep 时自动走 /proc。
  note_no_pgrep_once
  proc_alive_by_exe "$NGINX_BIN"
}

start_nginx() {
  if [ ! -x "$NGINX_BIN" ]; then
    log "⚠️  未找到 nginx（${NGINX_BIN}），跳过。系统仍可通过 http://127.0.0.1:${HTTP_PORT} 访问"
    return 0
  fi

  # Upgrade archives can carry a rendered config from their build/install path.
  # Refresh it before the health check so a reused process cannot preserve stale
  # absolute paths for the next restart.
  render_nginx_conf || return 0

  if nginx_healthy; then
    log "nginx 已在运行（端口 ${NGINX_HTTPS_PORT}）"
    return 0
  fi

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
      ok "nginx 启动成功（HTTPS 开启=${NGINX_HTTPS_ENABLED}，HTTP ${NGINX_HTTP_PORT}，HTTPS ${NGINX_HTTPS_PORT}）"
      return 0
    fi
    sleep 0.2
  done
  log "nginx 启动超时，最后 10 行日志："
  tail -10 "$NGINX_LOG" >&2 || true
  fail "nginx 未能在预期时间内启动"
}

stop_nginx() {
  # ⛔ 必须走 nginx 自己的 \`-s quit\`：它会让 master 通知**所有 worker** 退出。
  #   直接 kill master 的话，已经 fork 的 worker 会继续占着端口变成孤儿
  #   （实测过：kill 掉 master 后 curl 8443 仍然 200）。
  if [ -x "$NGINX_BIN" ] && [ -f "$NGINX_CONF" ]; then
    "$NGINX_BIN" -p "$ROOT" -c "$NGINX_CONF" -s quit >/dev/null 2>&1 || true
    for _ in $(seq 1 40); do
      nginx_healthy || break
      sleep 0.25
    done
  fi
  # 兜底：端口还没放掉就按可执行路径清（pid 文件可能不可靠）
  nginx_healthy && stop_by_pidfile "" "$NGINX_BIN" "nginx"
  rm -f "$NGINX_PID_FILE"
}

# ---------------------------------------------------- ZLM 端口同步到 config.ini ----

# sync_zlm_ports_ini 把端口写进 vendor/zlm/config.ini 的对应段。
#
# ⛔⛔ 必须**按段落**改，绝不能全局替换 `port=` —— config.ini 里有 11 个段各带
#   port/sslport（http / rtmp / rtp_proxy / rtc / srt / rtsp / shell / onvif …），
#   全局替换会把它们全改成同一个值，表现为「一堆协议抢同一个端口」。
# ⛔ 不能用 configparser：这份 ini **有重名段**（[general] 出现两次），
#   标准库直接抛 DuplicateSectionError —— 实测过。
#   ⇒ 行扫描：记住当前段名，只在目标段内改目标键。
# ------------------------------------------------------ ZLM secret 对齐 ----

# sync_zlm_secret 把平台侧与 ZLM 侧的 API secret 对齐。
#
# ⛔⛔⛔ 这是绿色包最隐蔽的一个坑（实测踩了一整轮）：
#   config.example.yml 里 zlm.secret 写的是占位符 `CHANGE_ME`，注释也明说
#   「**部署时填 ZLM 实际 secret**」—— 而绿色包**从来没做过这一步**。
#
#   为什么它比看起来严重得多：
#   · ZLM 侧 secret 在 vendor/zlm/config.ini 里（构建 ZLM 时随机生成）；
#   · 平台侧在 config.yml 里（种子/示例给的是 CHANGE_ME）；
#   · 两者对不上 ⇒ ZLM 每次 API 调用都返回 `{"code":-100,"msg":"Please login first"}`。
#
#   症状链条极长、且每一环都不指向真因：
#     ZLM 节点配置恢复失败（error 被脱敏）→ zlmRegistry 就绪但不可用
#     → setupRecordCacheRuntime() 的四个前置条件之一不满足
#     → **录像缓存服务未装配** → 页面报 503。
#   而ZLM 进程本身活得好好的（curl getServerConfig 用对secret 就是 200）。
#
#   ⚠️ 最坑的是：这跟"录像缓存"八竿子打不着。录像缓存只是**第一个撞上**的
#   受害者，同一个 secret 还会让点播、录像查询、hook 全线失效。
#
# ⇒ 判据：**以 ZLM 自己的 config.ini 为准**（它是真正在跑的那个进程的密钥），
#   把 config.yml 里的值对齐过去。反过来（改 ZLM 的）会让已在跑的 ZLM 失联。
sync_zlm_secret() {
  [ -f "$ZLM_INI" ] || return 0
  [ -f "$CONF" ] || return 0

  # ⛔ 取值必须分成两条语句（先声明、再赋值）：写成 `local rc="$(...)"` 会把命令替换
  #   的退出码吞掉（local 自身恒返回 0），于是"对齐失败"被当成成功继续往下跑 ——
  #   而这条链一断，平台调 ZLM 全部返回 -100，症状却指向"录像缓存未装配"。
  local rc=0
  "$SETUP_BIN" secret-sync --ini "$ZLM_INI" --yml "$CONF" || rc=$?
  [ "$rc" -eq 0 ] || fail "ZLM secret 对齐失败（退出码 ${rc}）—— 平台调 ZLM 会全部被拒"
}

# ---------------------------------------------------- ZLM 端口同步到 config.ini ----

# sync_zlm_ports_ini 把端口写进 vendor/zlm/config.ini 的对应段。
#
# ⛔⛔ 必须**按段落**改，绝不能全局替换 `port=` —— config.ini 里有 11 个段各带
#   port/sslport（http / rtmp / rtp_proxy / rtc / srt / rtsp / shell / onvif …），
#   全局替换会把它们全改成同一个值，表现为「一堆协议抢同一个端口」。
# ⛔ 不能用 ini 解析库：这份 ini **有重名段**（[general] 出现两次），
#   标准 ini 库会直接抛"重复段"错误（Linux 版当年用 Python configparser 实测踩过）。
#   ⇒ uvp-setup 里同样是行扫描：记住当前段名，只在目标段内改目标键
#     （实现见 server/cmd/uvp-setup/portsync.go 的 syncZLMIni）。
sync_zlm_ports_ini() {
  [ -f "$ZLM_INI" ] || return 0      # 没有 ini 就用 ZLM 默认值，不阻塞启动

  # ⛔ 这里的每个键都必须真的写进 config.ini：ZLM 只认文件，不认我们的变量。
  #   漏一个的后果是「平台按 A 端口调它、它却听 B 端口」⇒ 推流/取流静默失败。
  #   ⛔ [rtc] 的两个媒体键不叫 port（是 tcpPort / signalingPort），
  #     RTP 段的真源是 [rtp_proxy] 而不是 [rtp] —— 这些都由 uvp-setup 处理。
  local rc=0
  "$SETUP_BIN" ports-sync --ini "$ZLM_INI" \
    --zlm-http "$ZLM_HTTP_PORT" \
    --zlm-ssl "$ZLM_SSL_PORT" \
    --zlm-rtsp "$ZLM_RTSP_PORT" \
    --zlm-rtmp "$ZLM_RTMP_PORT" \
    --zlm-rtc "$ZLM_RTC_PORT" \
    --zlm-signaling "$ZLM_SIGNALING_PORT" \
    --zlm-signaling-ssl "$ZLM_SIGNALING_SSL_PORT" \
    --zlm-srt "$ZLM_SRT_PORT" \
    --zlm-onvif "$ZLM_ONVIF_PORT" \
    --zlm-ice "$ZLM_ICE_PORT" \
    --zlm-rtp-proxy "$ZLM_RTP_PROXY_PORT" \
    --zlm-rtp-range "$ZLM_RTP_RANGE" || rc=$?
  [ "$rc" -eq 0 ] || fail "ZLM 端口同步失败（退出码 ${rc}）—— 端口跑出规划段会导致收流全废"
}


# ---------------------------------------------------------------- ZLM ----

# port_in_use_by_other 判断某端口是否被**非本包**的进程占用。
# ⛔ 用 ss 而不是 lsof：绿色包不保证目标机装了 lsof，而 ss 是 iproute2 自带的。
#   只能看端口是否有人听（不解析进程归属，归属交给调用方自己的 pgrep 判）。
port_in_use_by_other() {
  ss -tln 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${1}$"
}

# zlm_healthy 要求**端口通**且**是本包的 ZLM 在服务**。
#
# ⛔⛔ 端口通不足以证明是"我的 ZLM"：实测机器上有别人的 MediaServer 占着同一端口时，
#   我们的 ZLM 起来后bind 失败、**1 秒后正常析构退出**（日志里是 ~EventPoller/~Logger，
#   不是崩溃），而这里只探测端口 ⇒ 立刻误判"启动成功"。
#   症状最坑的地方：status 随后又报「未运行」（那时对方也退了），
#   于是**启动成功、状态未运行**，两个输出互相矛盾且都"有依据"。
#   ⇒ 必须同时确认两件事：① 端口在听 ② 本包路径的 MediaServer 进程活着。
zlm_healthy() {
  # ⛔ close 同样必须包进子 shell —— 理由见 nginx_healthy 处的注释。
  (exec 3<>"/dev/tcp/127.0.0.1/$ZLM_HTTP_PORT") 2>/dev/null || {
    ( exec 3<&- 3>&- ) 2>/dev/null || true
    return 1
  }
  exec 3<&- 3>&-
  # ⛔ 端口被谁占着也要分清：机器上可能跑着别的 MediaServer（容器内的、
  #   用相对路径 -c ../conf/config.ini 启动），按可执行文件**绝对路径**匹配
  #   只会命中本包启动的。缺 pgrep 时 proc_pids_by_exe 自动改走 /proc。
  note_no_pgrep_once
  proc_alive_by_exe "$ZLM_BIN"
}

start_zlm() {
  if zlm_healthy; then
    log "ZLM 已在运行（HTTP 端口 ${ZLM_HTTP_PORT}）"
    return 0
  fi
  if [ ! -x "$ZLM_BIN" ]; then
    # ⛔ ZLM 缺失不阻断启动：平台核心（设备管理、实时预览列表、录像查询）
    #   不依赖它，但推流/取流/录像会失败。让用户能先进去看页面，别卡在这一步。
    log "⚠️  未找到 ZLM（${ZLM_BIN}），跳过启动。系统可访问，但推流/取流/录像不可用。"
    return 0
  fi

  # ⛔⛔ LD_LIBRARY_PATH 是**必须**的：MediaServer 动态链接 ffmpeg 的 7 个 .so
  #   （libavformat/libavfilter/libswscale/libpostproc 等），包内自带在 lib/。
  #   漏了它就是启动瞬间 `error while loading shared libraries: libavfilter.so.9`。
  # ⛔ 配置文件路径要**绝对路径**：MediaServer 的 WorkingDir 敏感，
  #   相对路径在不同启动方式下会解析到不同地方 ⇒ 端口/secret 全部走默认值。
  # ⛔ 仍要 `cd "$ZLM_DIR"`：config.ini 里 `[http] rootPath=./www` 是
  #   **相对工作目录**的，不 cd 就取不到 ZLM 自带的 web 页面。
  # ⛔ `--log-dir` 必须显式给：该参数默认取**可执行文件目录**（不是 cwd），
  #   不指定的话 ZLM 自带日志会一直落在 vendor/zlm/log，与"日志统一在 logs/"
  #   的约定不符（ZLM 没有这个配置项，只能走命令行）。
  # 证书独立保存；首次初始化由 Nginx 证书合成，后续仅按页面同步选项更新。
  #   不传 `-s` 时 ZLM 会去找 exeDir()/default.pem，而绿色包里没这个文件
  #   ⇒ HTTPS(51007)/WSS(51010) 握手失败。文件不存在就退化成不传（不硬给）。
  local zlm_ssl=()
  if [ -f "$ZLM_SSL_PEM" ]; then zlm_ssl=(-s "$ZLM_SSL_PEM"); fi
  ( cd "$ZLM_DIR" && LD_LIBRARY_PATH="$ZLM_DIR/lib" \
      "$ZLM_BIN" -c "$ZLM_DIR/config.ini" -l 0 --log-dir "$ZLM_NATIVE_LOG_DIR/" \
        ${zlm_ssl[@]+"${zlm_ssl[@]}"} >"$ZLM_LOG" 2>&1 </dev/null &
    echo $! > "$ZLM_PID_FILE" ) >/dev/null 2>&1

  for _ in $(seq 1 100); do
    if zlm_healthy; then
      ok "ZLM 启动成功（HTTP ${ZLM_HTTP_PORT} / RTSP ${ZLM_RTSP_PORT}）"
      return 0
    fi
    if [ -f "$ZLM_PID_FILE" ]; then
      local pid; pid="$(cat "$ZLM_PID_FILE")"
      if ! kill -0 "$pid" 2>/dev/null; then
        rm -f "$ZLM_PID_FILE"
        # ⛔⛔ 排障体验（2026-10-12 客户 Ubuntu 16 实机踩到，查了半天）：
        #   ZLM 启动失败时，会先打一行 `E ... Start server failed: <原因>`
        #   （ZLMediaKit server/main.cpp 的 catch 分支），**然后 sleep(1)**，
        #   最后随静态对象析构刷出几十行 `~EventPoller` / `>logWriter` 收尾日志。
        #   ⇒ 只 `tail -20` 会被那堆析构日志**整整占满**，真正的原因行被挤出窗口，
        #     用户看到的就只有"一堆 poller 日志 + 一句笼统的兜底话"，无从排障。
        #   ⇒ 所以先把带错误关键字的行单独挑出来打，再补 tail 兜底。
        local zlm_err
        zlm_err="$(grep -nE 'Start server failed|Listen on .*failed|already in use|error while loading shared libraries' "$ZLM_LOG" 2>/dev/null | head -6 || true)"
        if [ -n "$zlm_err" ]; then
          log "ZLM 日志里的关键错误行（下面这句才是真正原因）："
          printf '%s\n' "$zlm_err" >&2
        fi
        log "ZLM 最后 20 行日志："
        tail -20 "$ZLM_LOG" >&2 || true
        # ⛔ 区分两种退出：端口被别人占着 vs 自身起不来。
        #   这两种的处置完全不同（前者要换端口，后者要查库），混为一谈会误导排障。
        if port_in_use_by_other "$ZLM_HTTP_PORT"; then
          fail "ZLM 端口 ${ZLM_HTTP_PORT} 已被**其他进程**占用（多半是另一套 ZLM），
   本包的 ZLM 无法bind 而退出。处理：停掉占用者，或用 UVP_ZLM_HTTP_PORT 换一个端口。"
        fi
        # ⛔ 不要在这里断言"就是缺 ffmpeg 库"——那是众多可能之一，写死会把排障带偏
        #   （客户 2026-10-12 就被这句误导成"缺依赖"）。指向上面那行具体错误即可。
        fail "ZLM 启动失败（详见 ${ZLM_LOG}）。
   处理：看上面「关键错误行」——
     · `Listen on :: <端口> failed: address already in use` ⇒ 该端口被别的程序占了，
       停掉占用者，或用 UVP_ZLM_*_PORT 换一个端口后重试；
     · `error while loading shared libraries: xxx` ⇒ 这才是缺运行时库。"
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
  local baseline="$RESOURCE/baseline/baseline.sql"

  [ -f "$baseline" ] || fail "缺少建库脚本 ${baseline}（包不完整？请重新解压）"
  mkdir -p "$(dirname "$db_path")"

  # 幂等（看 sys_users 在不在）、进度、建库后对账、重名索引告警都在 uvp-setup 里，
  # 与 Windows 版**同一份实现**：同一份 baseline.sql、同一种语句切分、
  # 同样「PRAGMA 必须在事务外」、同样把被静默跳过的重名索引显式报出来。
  #
  # ⛔ 不接管道（tee/tail）：`set -o pipefail` 下管道会把真实退出码盖掉，
  #   而这里必须让建库失败**直接中止启动** —— 否则页面能开、登录报 no such table。
  if ! "$SETUP_BIN" db-init --baseline "$baseline" --db "$db_path"; then
    fail "数据库初始化失败（详见上面的输出；只在还没录入业务数据时才可删库重来）"
  fi
}


sync_ports_into_config() {
  # 把生效端口写回 config.env（持久化）、config.yml（后端读）、config.ini（ZLM 真源）。
  #
  # ⛔⛔ 不做这一步就会出现"Redis 听 A 端口、后端连 B 端口"这种**静默错配**：
  #   Redis 用 --port 参数启动（读 $REDIS_PORT），而后端连哪个端口是读 config.yml 的
  #   redis.port。两者不同源 ⇒ 用户改了 env 变量后，后端连不上 Redis，
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

  # ⛔ 只有「用户显式设过的」端口才写 UVP_USER_SET_ 标记：不带标记的老包值一律按
  #   新默认值处理，这正好与"升级后跟着新端口规划走"的诉求一致。
  #   判据 = 环境变量显式传入，或文件里已经有标记（用户手工改过）。
  local user_set="" port_key
  for port_key in UVP_HTTP_PORT UVP_REDIS_PORT UVP_HTTPS_PORT UVP_NGINX_HTTP_PORT; do
    if [ -n "${!port_key:-}" ] || [ "$(read_env_file "UVP_USER_SET_$port_key")" = "1" ]; then
      user_set="${user_set:+$user_set,}$port_key"
    fi
  done

  local -a args=(ports-sync
    --env "$ENV_FILE" --yml "$CONF"
    --http "$HTTP_PORT" --redis "$REDIS_PORT"
    --https "$NGINX_HTTPS_PORT" --nginx-http "$NGINX_HTTP_PORT"
    --zlm-http "$ZLM_HTTP_PORT" --zlm-ssl "$ZLM_SSL_PORT"
    --zlm-rtsp "$ZLM_RTSP_PORT" --zlm-rtmp "$ZLM_RTMP_PORT"
    --zlm-rtc "$ZLM_RTC_PORT" --zlm-signaling "$ZLM_SIGNALING_PORT"
    --zlm-signaling-ssl "$ZLM_SIGNALING_SSL_PORT"
    --zlm-srt "$ZLM_SRT_PORT" --zlm-onvif "$ZLM_ONVIF_PORT"
    --zlm-ice "$ZLM_ICE_PORT"
    --zlm-rtp-proxy "$ZLM_RTP_PROXY_PORT" --zlm-rtp-range "$ZLM_RTP_RANGE"
    --user-set "$user_set")

  local rc=0
  "$SETUP_BIN" "${args[@]}" || rc=$?
  [ "$rc" -eq 0 ] || fail "端口同步失败（退出码 ${rc}）—— 三处配置必须同源，不能只改一半"
}


# ============================================ 忘记登录口令 ⇒ 还原出厂口令 ====
#
# 场景：客户装完包、首次登录时把 admin 的口令改成了自己的，后来忘了。
#   平台上**没有任何自助找回入口**（登录页的「忘记密码」是注释掉的死链；
#   后端也只有 PUT /users/changeInitialPassword，前提是账号仍处于「待改密」）
#   ⇒ 唯一出路是把库里的口令写回出厂值。
#
# ⛔ 语义全在 server/cmd/uvp-setup/resetadmin.go 里，**两个平台调同一份实现**
#   （Windows 版一直如此；Linux 版 2026-10-19 起也改成调它，不再各写一套 Python）：
#     ① 口令 = 出厂口令（config.yml 的 server.demoaccount.defaultpassword）；
#     ② must_change_password 置 1 ⇒ 下次登录强制改密；
#     ③ 只动指定的那一个账号；不删库、不重建库、不改 status。
#
# ⛔ 运行时挡一道：config.yml 里那个值一旦不等于本常量就**直接拒绝执行** ——
#   宁可让客户来找我们，也不能把口令重置成一个谁也猜不到的字符串
#   （"重置完还是登不进去"是最贵的一类故障）。
#   本常量与 config.example.yml 的同源关系由 scripts/standalone-factory-password.test.mjs 锁住；
#   "这个口令 ↔ baseline 里 admin 的 bcrypt 哈希"由 Go 侧
#   resource/database/password_bootstrap_contract_test.go 用真 bcrypt 锁住。
CTL_FACTORY_PASSWORD='123456'

# read_conf_scalar 读 config.yml 里某段（可嵌套）下某个键的标量值（剥引号与行尾注释）。
# ⛔ 必须按**缩进层级**定位段落：port/host 这类键在 httpserver/redis/zlm/gormv2
#   多个段下重名，全局第一个命中往往不是你要的那个（本项目反复踩）。
read_conf_scalar() {
  local section="$1" key="$2" value=""
  # ⛔⛔ 本脚本开头是 `set -Eeuo pipefail`：`value="$(cmd)"` 在命令替换**失败时会让整个
  #   脚本当场退出**（哪怕是赋值语句，它的退出码就是命令替换的退出码）。
  #   这里要的是"读不到就回空串、由调用方决定怎么办"，所以必须 `|| value=""` 兜住 ——
  #   否则 config.yml 有任何异常都会让 start 静默死在这一行，连一句报错都没有。
  value="$("$SETUP_BIN" config-get --yml "$CONF" --section "$section" --key "$key" 2>/dev/null)" || value=""
  printf '%s' "$value"
}


# reset_admin_password 把某个账号的口令还原成出厂口令，并重新标记「首次登录必须改密」。
reset_admin_password() {
  local username="$1"
  [ -n "$username" ] || username="admin"
  # ⛔ 与 ensure_database 用**同一个表达式**取库路径（包括这个相对路径）：两个函数
  #   对"库在哪"必须永远一致，否则会出现"建库建在 A、重置改的是 B"而两边都报成功。
  local db_path="${UVP_DB_PATH:-./data/uvp.db}"

  printf '\n'
  [ -f "$db_path" ] || fail "找不到数据库 ${db_path} —— 这台机器还没执行过 start（库里还没有账号）"
  [ -f "$CONF" ]    || fail "找不到配置文件 ${CONF}"

  # ⛔ 本机 config.yml 的出厂口令一旦被改过就**拒绝执行**：重置成一个谁也猜不到的
  #   字符串，等于把"登不进去"变成"永远登不进去"（这是最贵的一类故障）。
  local configured
  configured="$(read_conf_scalar demoaccount defaultpassword)"
  if [ "$configured" != "$CTL_FACTORY_PASSWORD" ]; then
    fail "本机 config.yml 的出厂口令（server.demoaccount.defaultpassword）不是本包已知的出厂口令。
      为避免把口令重置成一个谁也猜不到的字符串，这里**拒绝执行**。
      请把该值改回 ${CTL_FACTORY_PASSWORD} 后重试；若这台机器的出厂口令确实被改过，
      请联系我们处理（或参照 README「忘记了登录口令」手工处理）。"
  fi

  log "重置账号 ${username} 的口令"
  # 语义（置 must_change_password=1、写后回读校验、清 Redis 锁定键）全在
  # uvp-setup reset-admin 里 —— 与 Windows 版**同一份实现**，不允许两边各写一套。
  local rc=0
  "$SETUP_BIN" reset-admin --root "$ROOT" --username "$username" || rc=$?
  [ "$rc" -eq 0 ] || fail "重置口令失败（uvp-setup 退出码 ${rc}）"
}


case "${1:-start}" in
  start)
    print_banner
    # ⭐ 三级缩进把"阶段 → 结论 → 明细"分开（见文件顶部"输出样式"），
    #   目的是让用户扫一眼就知道**走到哪一步了、每步成没成** ——
    #   原来所有行都是同一层级的碎句子，出了错得逐行读才知道卡在哪。
    log "阶段 1/3 · 环境检查"
    check_preconditions
    # ⛔⛔ 端口预检必须在**任何组件启动之前**：端口被别人占着而我们照常启动时，
    #   nginx 会连到别人的服务上，而 status 仍报「运行中」——
    #   客户看到的是别的系统的页面，全程却没有任何报错（今天真实踩到）。
    check_ports_free

    log "阶段 2/3 · 初始化数据与配置"
    # ⛔ 必须先建库再起 Redis/后端：后端启动时会立刻查库，
    #   库不存在就是「页面能开、登录报 no such table」。
    ensure_database
    # ⛔ 端口必须先同步进 config.yml 再起服务：Redis 读 --port 参数、
    #   后端读 config.yml，两处不同源就是「Redis 起来了但后端连不上」。
    sync_ports_into_config
    # ⛔⛔ ZLM secret 必须对齐，且必须在**后端启动之前**：
    #   不对齐时平台调 ZLM 全被拒（-100 Please login first），
    #   后端启动会「成功」但录像缓存/点播/录像查询/hook 全部装配失败，
    #   页面报的是「录像缓存服务未装配」—— 与真因完全无关。
    sync_zlm_secret
    # ⛔ ZLM 端口也要同步：它的真源是 config.ini，不是本脚本里的变量。
    sync_zlm_ports_ini
    # ⛔ TLS 证书必须在**起 ZLM 之前**备好：ZLM 的 HTTPS/WSS 要加载它。
    #   这里同时给 nginx 和 ZLM 准备同一套证书；失败不阻断（start_nginx 里
    #   还会再兜一次并给出明确报错），只是 ZLM 的 HTTPS 会用不上证书。
    ensure_tls_material || true

    log "阶段 3/3 · 启动服务"
    # 升级助手独立于业务进程，必须先启动；业务 stop 时不停止它。
    start_updater
    # ⛔ ZLM 要在**后端之前**起来：后端启动后会立即注册 hook、
    #   校验 ZLM 连通性，ZLM 没起就报连接失败。
    start_zlm
    # ⛔ nginx 在最后起：它要反代后端，后端得先在监听。
    start_nginx
    start_redis          # ⛔ 顺序不能反：后端启动时要连缓存
    start_backend

    # 收尾：访问地址（含对外网卡地址）+ 分类端口规划 + 防火墙放行提示。
    print_start_summary
    print_port_plan
    ;;
  restart-zlm)
    stop_zlm
    start_zlm
    ;;
  start-updater)
    start_updater
    ;;
  serve-updater)
    serve_updater
    ;;
  stop)
    stop_nginx
    stop_backend
    stop_zlm
    stop_redis
    # 升级助手独立运行，业务 stop/restart 不得杀掉它。
    log "已停止"
    ;;
  migrate)
    migrate_backend
    ;;
  restart)
    "$0" stop; "$0" start
    ;;
  status)
    redis_healthy    && log "Redis:  运行中（端口 ${REDIS_PORT}）"  || log "Redis:  未运行"
    zlm_healthy      && log "ZLM:    运行中（端口 ${ZLM_HTTP_PORT}）" || log "ZLM:    未运行"
    backend_healthy && log "后端:  运行中（端口 ${HTTP_PORT}）"    || log "后端:  未运行"
    if [ -x "$NGINX_BIN" ]; then
      nginx_healthy  && log "nginx:  运行中（HTTPS 开启=${NGINX_HTTPS_ENABLED}，HTTP ${NGINX_HTTP_PORT}，HTTPS ${NGINX_HTTPS_PORT}）" || log "nginx:  未运行"
    else
      log "nginx:  未安装（可直接访问 http://127.0.0.1:${HTTP_PORT}）"
    fi
    ;;
  reset-password)
    reset_admin_password "${2:-}"
    ;;
  *)
    echo "用法: $0 {start|stop|restart|status|migrate|start-updater|serve-updater|reset-password [账号名]}" >&2
    echo "  reset-password：忘记了登录口令时，把账号（默认 admin）的口令还原成出厂口令" >&2
    exit 2
    ;;
esac
