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

# ------------------------------------------------------------ 输出样式 ----
#
# ⛔⛔ 颜色只在**交互终端**下开。输出被重定向到文件、经管道、或进 systemd journal
#   时一律不加转义序列 —— 否则日志里混进 \033[..m，grep/比对/贴给客户看全是噪声。
#   想显式关掉（比如录屏、或终端配色奇怪）：NO_COLOR=1 启动即可（见 no-color.org）。
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_TAG=$'\033[36m'   # [uvp] 标签：青色
  C_OK=$'\033[32m'    # 成功标记：绿色
  C_WARN=$'\033[33m'  # 告警标记：黄色
  C_ERR=$'\033[31m'   # 错误标记：红色
  C_RST=$'\033[0m'
else
  C_TAG=''; C_OK=''; C_WARN=''; C_ERR=''; C_RST=''
fi
# 建库进度条 / 自检汇总由 python 打印，让它复用**同一套**颜色与缩进，
# 否则会出现「shell 的 ✓ 是绿的、python 的 ✓ 是白的」这种同页两种样式。
export UVP_C_TAG="$C_TAG" UVP_C_OK="$C_OK" UVP_C_WARN="$C_WARN" \
       UVP_C_ERR="$C_ERR" UVP_C_RST="$C_RST"

log()  { printf '%s[uvp]%s %s\n' "$C_TAG" "$C_RST" "$*"; }
fail() { printf '%s[uvp][ERROR]%s %s\n' "$C_ERR" "$C_RST" "$*" >&2; exit 1; }

# ⭐ 两级缩进约定（改宽度要连 python 里的 UVP_INDENT 一起改）：
#   [uvp] xxx        —— 阶段/事件行（由 log 输出）
#         ✓ xxx       —— 该阶段的**结果**行（6 格缩进 = "[uvp] " 的宽度）
#         · xxx       —— 结果之下的补充明细（不抢眼，长行让它自然折行）
#   这么分是因为原来的输出里有的行有 [uvp] 前缀、有的没有，读者无法判断
#   哪一行是"步骤"哪一行是"结论"，扫一眼全是等价的碎句子。
det()  { printf '      %s\n' "$*"; }
ok()   { printf '      %s✓%s %s\n' "$C_OK" "$C_RST" "$*"; }

# _render_detail_block 渲染「首行=结论、其余=补充明细」的多行文本：
#   首行配 ✓，其余配 · 并再缩进两格。
# ⛔ 调用方必须**分两条语句**取值（先 local、再赋值），写成 `local x="$(...)"`
#   会把命令替换的退出码吞掉（local 自身恒返回 0）—— 于是 python 里
#   sys.exit("...") 换来的"中止启动"就失效了，同步失败也照常往下跑。
_render_detail_block() {
  local msg="$1"
  [ -n "$msg" ] || return 0
  local -a lines=()
  mapfile -t lines <<< "$msg"
  ok "${lines[0]}"
  local i
  for (( i = 1; i < ${#lines[@]}; i++ )); do
    if [ -n "${lines[i]}" ]; then det "  · ${lines[i]}"; fi
  done
}

# ------------------------------------------------------ 横幅 / 收尾汇总 ----

# print_banner 打印启动横幅（版本号从 version.json 取，取不到就不显示）。
# ⛔ 刻意**不画「左右都封口的方框」**：中文是双宽字符，bash 里没有 wcwidth，
#   任何"左边框 + 右边框"的框子在换字体/换终端后都会歪（右边线对不齐）。
#   用上下两条等长横线做边界，中间行不做右对齐 —— 怎么换都不会歪。
print_banner() {
  local ver="" title
  if [ -f "$ROOT/version.json" ]; then
    ver="$(grep -m1 '"version"' "$ROOT/version.json" 2>/dev/null | cut -d'"' -f4 || true)"
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
  # 国标信令端口由**引导页**录入（存 gb_sip_config 表），本脚本无从得知；
  # 这里按规划段末尾的预留位（51064）给出提示，放行命令也把它带上。
  local sip_hint=51064

  # ⛔ 段尾取 max(RTP 段尾, SIP 预留位)：SIP 默认 51064 正好在 RTP 段之后一个，
  #   只放到 RTP 段尾会把 SIP 漏掉 —— 而 SIP 恰恰是**设备接入**能不能通的
  #   关键。放行给**一整段**：规划段本来就是连续无空洞的，一条命令比逐条好记。
  fw_to="$to"
  if [ "$fw_to" -lt "$sip_hint" ]; then fw_to="$sip_hint"; fi

  printf '%s[uvp]%s 端口规划（连续段 %s-%s）\n' "$C_TAG" "$C_RST" "$NGINX_HTTPS_PORT" "$fw_to"
  ok "需要对局域网开放"
  det "  · 管理页面   ${NGINX_HTTPS_PORT}/tcp（HTTPS） · ${NGINX_HTTP_PORT}/tcp（HTTP 自动跳转）"
  det "  · 后端接口   ${HTTP_PORT}/tcp（API + 设备/App 接入地址）"
  det "  · 国标信令   ${sip_hint}/udp（默认值；实际端口以引导页录入为准）"
  det "  · 媒体服务   ${ZLM_HTTP_PORT}/tcp · ${ZLM_RTSP_PORT}/tcp · ${ZLM_RTMP_PORT}/tcp · ${ZLM_SSL_PORT}/tcp"
  det "  · WebRTC     ${ZLM_RTC_PORT}/tcp · ${ZLM_SIGNALING_PORT}/tcp · ${ZLM_SIGNALING_SSL_PORT}/tcp"
  det "  · 其它媒体   ${ZLM_SRT_PORT}/udp（SRT） · ${ZLM_ONVIF_PORT}/tcp（ONVIF） · ${ZLM_ICE_PORT}/udp（STUN）"
  det "  · 媒体收流   ${ZLM_RTP_RANGE} UDP+TCP（成对占用）"
  ok "仅本机，无需放行"
  det "  · Redis      ${REDIS_PORT}/tcp（只监听 ${REDIS_BIND}）"

  fw="$(_firewall_hint "$NGINX_HTTPS_PORT" "$fw_to")"
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
  [ -x "$NGINX_BIN" ] || scheme="http"

  if [ "$scheme" = "https" ]; then
    local_url="https://127.0.0.1:${NGINX_HTTPS_PORT}"
  else
    local_url="http://127.0.0.1:${HTTP_PORT}"
  fi

  printf '%s[uvp]%s 启动完成\n' "$C_TAG" "$C_RST"

  lan="$(detect_lan_ip)"
  if [ -n "$lan" ]; then
    if [ "$scheme" = "https" ]; then
      lan_url="https://${lan}:${NGINX_HTTPS_PORT}"
    else
      lan_url="http://${lan}:${HTTP_PORT}"
    fi
    ok "管理页面  ${lan_url}"
    det "  · 本机访问  ${local_url}"
  else
    ok "管理页面  ${local_url}"
    det "  · 未识别到对外网卡地址，其它电脑请用本机 IP 访问"
  fi
  det "  · 登录账号  admin（密码见交付说明，登录后请立即修改）"
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
BIN="$ROOT/bin"
VENDOR="$ROOT/vendor"
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
ZLM_HTTP_PORT="${UVP_ZLM_HTTP_PORT:-$(read_port_env UVP_ZLM_HTTP_PORT)}"
# ⛔ 默认值一律选**高位端口**，不用 ZLM 自带的 80/443/554。
#   1024 以下需要 CAP_NET_BIND_SERVICE，非 root 起不来（实测：
#   「Listen on :: 554 failed: permission denied」）。
#   绿色包的运行用户就是普通用户，所以默认值必须避开特权区。
ZLM_HTTP_PORT="${ZLM_HTTP_PORT:-51004}"
ZLM_SSL_PORT="${UVP_ZLM_SSL_PORT:-$(read_port_env UVP_ZLM_SSL_PORT)}"
ZLM_SSL_PORT="${ZLM_SSL_PORT:-51007}"
ZLM_RTSP_PORT="${UVP_ZLM_RTSP_PORT:-$(read_port_env UVP_ZLM_RTSP_PORT)}"
ZLM_RTSP_PORT="${ZLM_RTSP_PORT:-51005}"
# 其余对外段也统一到规划段（PORTS.md）
ZLM_RTMP_PORT="${UVP_ZLM_RTMP_PORT:-51006}"
ZLM_RTC_PORT="${UVP_ZLM_RTC_PORT:-51008}"
# ⛔ rtp_proxy 端口必须 = RTP 动态段的起点（同一个值，改一个必须改另一个）
ZLM_RTP_PROXY_PORT="${UVP_ZLM_RTP_PROXY_PORT:-51014}"
# ⛔⛔ RTP 动态端口段必须改！ZLM 默认是 49152-65535，那是 Linux 的
#   **临时端口范围**（客户端出站 connect 随机占用它）⇒ 两者抢端口，
#   表现为「偶发 bind 失败 / 偶发推流失败」，重启就好、复现极难。
# ⭐ 段长 = 默认 50 个（UDP+TCP 成对占用 ⇒ 50 路并发收流）；不够时改这个键放大，
#   但**注意别越过 51063**（51064 是 SIP 预留位，见 PORTS.md）。
ZLM_RTP_RANGE="${UVP_ZLM_RTP_RANGE:-51014-51063}"
# WebRTC 信令（键名不是 port，按"段名+port"匹配抓不到，实测漏掉导致 ZLM 起不来）
ZLM_SIGNALING_PORT="${UVP_ZLM_SIGNALING_PORT:-51009}"
ZLM_SIGNALING_SSL_PORT="${UVP_ZLM_SIGNALING_SSL_PORT:-51010}"
# SRT / onvif：容器默认 9000/3702 在目标机上极易被占，一并挪进规划段
ZLM_SRT_PORT="${UVP_ZLM_SRT_PORT:-51011}"
ZLM_ONVIF_PORT="${UVP_ZLM_ONVIF_PORT:-51012}"
# STUN/TURN（icePort / iceTcpPort）：容器默认 3478，UDP 上极易与其它服务冲突
ZLM_ICE_PORT="${UVP_ZLM_ICE_PORT:-51013}"
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
#      ① 这里改成 "1"；② `[rtc] port_range` 挪出临时端口区（如 51065-51099）；
#      ③ 防火墙上放开该段 UDP。只做 ① 等于又埋一次同样的雷。
#   ⚠️ 不给它做 config.env 开关：config.env 里的键**不会自动成为环境变量**
#      （见本文件 392 行附近的说明），写进去会让运维以为改了却没生效。
ZLM_ENABLE_TURN="0"

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
  _check_one 51000 tcp "HTTPS（浏览器访问管理页面）"     NGINX_HTTPS_PORT
  _check_one 51001 tcp "HTTP（跳 HTTPS + 保留明文 API）" NGINX_HTTP_PORT
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
  ok "端口预检通过（${NGINX_HTTPS_PORT}-${ZLM_RTP_RANGE#*-} 无冲突）"
}

# port_holder 返回占用某端口的进程描述；返回码 0=被占用，1=空闲。
#
# ⛔ 不用 lsof：绿色包不保证目标机装了它。
# ⛔⛔ ss 非 root 时**看不到别人的进程名** —— 而"被谁占的"恰恰是用户最需要的信息
#   （今天就靠它才发现 30000 已被 livesms 占用）。所以先用 ss 判"有没有人听"
#   （这一步非 root 也可靠），再尝试用 sudo -n 拿名字；拿不到就如实说
#   "需 root 才能看到名字"，**绝不编一个名字出来**。
port_holder() {
  local port="$1" proto="$2" flag="-tln"
  [ "$proto" = "udp" ] && flag="-uln"

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
    # ⛔⛔ 必须带 `|| true`：**"没找到进程"是这里的正常情况**（服务本来就没跑），
    #   而 pgrep 无匹配时退出码是 1，`var="$(cmd)"` 这条赋值语句的退出码
    #   就等于 cmd 的⇒ `set -e` 直接把整个脚本杀掉。
    #   实测症状：stop **零输出、退出码 1**，而且恰好停在"兜底什么都没找到"这条路
    #   —— 也就是说**越是该静默的情况，它越会崩**。
    #   （管道里 tr 是成功的，但赋值语句取的是整条管道的退出码，
    #     shellpipefail 又让 pgrep 的 1 冒上来。）
    found="$(pgrep -f "${exe_path}" 2>/dev/null | tr '\n' ' ' || true)"
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
  [ -f "$ROOT/version.json" ] || fail "找不到 version.json（后端启动会读它，缺了日志里全是噪音）"

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

# ---------------------------------------------------------------- 入口 ----

check_preconditions() {
  ensure_dirs
  case "$(uname -m)" in
    x86_64|amd64) : ;;
    *) fail "本包只支持 x86_64，当前架构 $(uname -m)" ;;
  esac
  [ -f "$CONF" ] || fail "缺少 $CONF —— 解压是否完整？"
  check_python
}

# check_python 校验解释器存在且 >= 3.5。
#
# ⛔⛔ 为什么必须**前置**校验版本：本脚本把 Python 代码用 heredoc 喂给解释器，
#   而 Python 是**整段一次性编译**的 —— 只要有一句是「当前解释器不认识的语法」，
#   它会直接抛 SyntaxError（报错行指向那句语法），**整段一行都不执行**。
#   于是症状变成「建库失败 / 改配置失败」这种指向完全无关位置的红字，
#   现场根本联想不到是解释器太老（真实案例：Ubuntu 16 自带 py3.5 遇到 f-string）。
#   ⇒ 与其让它以 SyntaxError 炸，不如在这里用**人话**说清楚。
# ⛔ 判据是「>= 3.5」而不是「>= 3.6」：本文件的内嵌 Python 已全部去 f-string，
#   刻意兼容 3.5（Ubuntu 16 / Debian 9 的默认 python3）。见契约测试
#   scripts/standalone-py35-compat.test.mjs。
check_python() {
  [ -n "$PY_BIN" ] || fail "找不到 python3 —— 本包需要它（Ubuntu/Debian 请先 apt install python3）"
  local ver major minor
  # ⛔ 探测代码本身必须**同时**是 py2/py3 合法语法（否则在太老的解释器上
  #   又是同一个 SyntaxError 陷阱）；只读 sys.version_info，不碰任何新语法。
  ver="$("$PY_BIN" -c 'import sys; sys.stdout.write("%d.%d" % sys.version_info[:2])' 2>/dev/null)"
  major="${ver%%.*}"
  minor="${ver##*.}"
  # 版本读不出来（非数字）按「过旧」处理，别放行。
  case "$major" in
    ''|*[!0-9]*) _python_too_old "$ver" "$PY_BIN" ;;
  esac
  case "$minor" in
    ''|*[!0-9]*) minor=0 ;;
  esac
  if [ "$major" -lt 3 ] || { [ "$major" -eq 3 ] && [ "$minor" -lt 5 ]; }; then
    _python_too_old "$ver" "$PY_BIN"
  fi
}

_python_too_old() {
  fail "本机 Python 版本过旧（$2 = ${1:-未知}），本包需要 Python 3.5 及以上。
      请安装较新的 python3 后重试，例如：
        · Ubuntu 16.04/18.04：sudo apt install python3.6  （或用 deadsnakes PPA）
        · 或把 python3 指向 3.6+ 的解释器
      若已装好新版，可用环境变量指定：
        UVP_BUILD_PY=/usr/bin/python3.6 ./uvp-gb28181-ctl.sh start"
}

# ------------------------------------------------------------ nginx ----
# nginx 只做两件事：托管前端 + HTTPS 终止，后端仍是纯 HTTP 的
# @BACKEND_PORT@（二维码里填的就是它，App 因此完全不碰 TLS）。
NGINX_DIR="$VENDOR/nginx"
NGINX_BIN="$NGINX_DIR/sbin/nginx"
NGINX_CONF="$NGINX_DIR/conf/nginx.conf"
NGINX_PID_FILE="$RUN/nginx.pid"
NGINX_LOG="$LOGS/nginx.log"
NGINX_CONF_TEMPLATE="$NGINX_DIR/conf/nginx.conf.template"
NGINX_CRT="$NGINX_DIR/conf/uvp.crt"
NGINX_KEY="$NGINX_DIR/conf/uvp.key"

# HTTPS 端口：给客户换端口时只改这里（同时也在 config.env 里）
NGINX_HTTPS_PORT="${UVP_HTTPS_PORT:-$(read_port_env UVP_HTTPS_PORT)}"
NGINX_HTTPS_PORT="${NGINX_HTTPS_PORT:-51000}"
# ⛔⛔ 变量名必须与后端的 UVP_HTTP_PORT 区分开。实测踩过：nginx 的明文端口
#   曾经也叫 UVP_HTTP_PORT，于是后端设 8390 时nginx 也去 bind 8390 ⇒
#   `bind() to 0.0.0.0:8390 failed: Address already in use`，
#   而报错完全看不出是「两个组件抢同一个端口」。
NGINX_HTTP_PORT="${UVP_NGINX_HTTP_PORT:-$(read_port_env UVP_NGINX_HTTP_PORT)}"
NGINX_HTTP_PORT="${NGINX_HTTP_PORT:-51001}"

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
# ⛔ 生成成功**不出声**（老板 2026-10-10 定：这属于"本该就有"的步骤，
#   写在启动输出里是噪音）；失败才报，且必须先试 nginx 再报。
ensure_self_signed_cert() {
  [ -f "$NGINX_CRT" ] && [ -f "$NGINX_KEY" ] && return 0
  mkdir -p "$NGINX_DIR/conf"
  if command -v openssl >/dev/null 2>&1; then
    # ⛔⛔ `-addext` 是 **OpenSSL 1.1.1+** 才有的选项，而 Ubuntu 16.04 自带的是
    #   1.0.2 ⇒ 直接带上它会让整条命令**失败**（"unknown option -addext"），
    #   而失败信息只在 nginx 日志里 ⇒ 现场表现就是"nginx 起不来"，
    #   与"证书"这个真因完全对不上（本项目已多次栽在"谎报/错报"上）。
    #   ⇒ 先带 SAN 试一次，老版本不支持就退回不带 SAN 的写法（CN 仍有效，
    #     浏览器只是多一次"名称不匹配"的告警，不影响使用）。
    if ! openssl req -x509 -nodes -newkey rsa:2048 \
          -keyout "$NGINX_KEY" -out "$NGINX_CRT" \
          -days 3650 -subj "/CN=uvp-local" \
          -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" >>"$NGINX_LOG" 2>&1; then
      rm -f "$NGINX_CRT" "$NGINX_KEY"
      openssl req -x509 -nodes -newkey rsa:2048 \
        -keyout "$NGINX_KEY" -out "$NGINX_CRT" \
        -days 3650 -subj "/CN=uvp-local" >>"$NGINX_LOG" 2>&1 || {
        log "⚠️  自签证书生成失败，nginx 不会启动（详见 ${NGINX_LOG}）"
        return 1
      }
    fi
    return 0
  fi
  log "⚠️  未找到 openssl，无法生成自签证书 —— 请自行放入 ${NGINX_CRT} 与 ${NGINX_KEY}"
  return 1
}

# ensure_tls_material 准备 TLS 证书：**nginx 与 ZLM 共用同一套**。
#
# ⛔⛔ 为什么 ZLM 也必须拿到证书：ZLM 的 HTTPS(51007) 与 WebRTC 信令 WSS(51010)
#   都要加载证书，它只从命令行 `-s/--ssl` 指定的**单个 PEM** 读（默认值
#   `exeDir()/default.pem`）—— 而绿色包里**根本没带 default.pem**
#   ⇒ 不给它，这两个端口虽然会 listen 但握手失败，WebRTC(HTTPS 场景)直接不可用。
#   ⇒ 这里把 nginx 的 crt+key 合成一份 PEM 给 ZLM，两边同一套证书（换真证书时
#     只要替换 vendor/nginx/conf/uvp.crt|key，ZLM 下次启动自动跟随）。
# ⛔ ZLM 的 `-s` 只认**单个文件**（私钥与证书在同一个 PEM 里），不能分别给
#   .crt / .key。拼接顺序照 ZLM 自带 default.pem：**先私钥、后证书**。
ensure_tls_material() {
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
  (exec 3<>"/dev/tcp/127.0.0.1/$NGINX_HTTPS_PORT") 2>/dev/null || {
    ( exec 3<&- 3>&- ) 2>/dev/null || true
    return 1
  }
  exec 3<&- 3>&-
  # 按可执行文件**绝对路径**匹配：按名字会命中同机其它 nginx
  # （这台机器上就Docker 里跑了 27 天的 nginx）。
  pgrep -f "$NGINX_BIN" >/dev/null 2>&1
}

start_nginx() {
  if [ ! -x "$NGINX_BIN" ]; then
    log "⚠️  未找到 nginx（${NGINX_BIN}），跳过。系统仍可通过 http://127.0.0.1:${HTTP_PORT} 访问"
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
      ok "nginx 启动成功（HTTPS ${NGINX_HTTPS_PORT}，前端已托管）"
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

  local zlm_secret
  zlm_secret="$("$PY_BIN" - "$ZLM_INI" <<'SECRET_PY'
import re
import sys

# 只认 [api] 段里的 secret —— config.ini 有 11 个段，别处也可能出现同名键。
section = None
for line in open(sys.argv[1], encoding="utf-8", newline="").read().split("\n"):
    t = line.strip()
    if t.startswith("[") and t.endswith("]"):
        section = t[1:-1].strip().lower()
    elif section == "api" and re.match(r"^secret\s*=", line, re.I):
        # 值可能带引号，两种都剥掉
        print(line.split("=", 1)[1].strip().strip("\"'").strip())
        break
SECRET_PY
)"
  [ -n "$zlm_secret" ] || return 0

  # config.yml 的 zlm.secret：只在它是占位符或为空时才写，
  # 绝不覆盖运维手工填过的真值。
  # python 只负责"改文件 + 输出一句**纯文本结论**"，前缀/缩进/配色由 shell 统一施加
  # （见文件顶部"输出样式"一节）。这样脚本里排版只有一个来源，不会出现
  # 「shell 的行有前缀、python 的行没有」这种半成品观感。
  local secret_msg
  secret_msg="$("$PY_BIN" - "$CONF" "$zlm_secret" <<'SECRET_SYNC_PY'
import re
import sys

path, secret = sys.argv[1], sys.argv[2]
lines = open(path, encoding="utf-8").read().split("\n")

# ⛔⛔ 必须按**缩进层级**定位 zlm 段，不能只认「顶格 + 结尾冒号」。
#   实测踩过：config.yml 里 zlm 是**嵌套**段（gb28181: → zlm:，2 空格缩进），
#   顶级段判据永远不成立⇒ 静默不替换 ⇒ 后端仍拿 CHANGE_ME 去连 ZLM。
#   这是"配置文件是静默的"又一例：改不成功也不报错，只是功能不工作。
zlm_indent = None
changed = False
for i, line in enumerate(lines):
    t = line.strip()
    if not t or t.startswith("#"):
        continue
    m = re.match(r"^(\s*)zlm\s*:\s*(#.*)?$", line)
    if m and zlm_indent is None:
        zlm_indent = len(m.group(1))
        continue
    if zlm_indent is None:
        continue
    # 遇到同级或更浅的非注释行 ⇒ 已走出 zlm 段，停。
    # ⛔ 不加这一句就会顺着文件往下扫，把 media/ 等其它段的 secret 也改掉。
    cur = len(line) - len(line.lstrip())
    if cur <= zlm_indent:
        break
    sm = re.match(r"^(\s*)secret(\s*:\s*)(.*?)(\r?)$", line)
    if not sm:
        continue
    indent, sep, raw, cr = sm.groups()
    # ⛔⛔ 必须先剥掉**行尾注释**再判值。实测踩过：
    #   config.yml 里这行长这样——
    #       secret: "CHANGE_ME"           # ZLM API secret(部署时填 ZLM 实际 secret)
    #   正则的 `(.*?)$` 会把注释一起吞进值里，判读成
    #   `CHANGE_ME"  # ZLM API secret(...)` ⇒ 不等于占位符 ⇒ **静默跳过**。
    #   而启动脚本照常打印「ZLM secret 与 config.ini 一致，无需改动」——
    #   **谎报成功**，后端仍然拿 CHANGE_ME 去连 ZLM。
    #   这类"配置文件是静默的 + 脚本还报成功"是最坏的组合：骗过了所有人。
    tail = ""
    vm = re.match(r"^\s*(.*?)\s*(#.*)?$", raw)
    if vm:
        raw, tail = vm.group(1), (vm.group(2) or "")
    old = raw.strip().strip("\"'").strip()
    # 只替换占位符/空值；已有真值说明运维填过，尊重它（幂等的关键）
    if old and old.upper() not in ("CHANGE_ME", "CHANGE-ME", "TODO", "YOUR_SECRET"):
        continue
    # 保留行尾注释：那是给现场运维看的说明，不能被我们抹掉
    # ⛔⛔ 不用 f-string：目标机可能是 Ubuntu 16（自带 python3 = 3.5），
    #   而 f-string 是 **3.6+** 语法 ⇒ 整个 heredoc 在编译期就 SyntaxError，
    #   报错行还指向这里（"建库/改配置失败"），与真因（解释器版本）完全无关。
    #   本文件所有内嵌 Python 一律只用 `%` 或字符串拼接。见契约测试
    #   scripts/standalone-py35-compat.test.mjs（会拦住回退）。
    lines[i] = ('%ssecret%s"%s"%s' % (indent, sep, secret, cr)) + ("    " + tail if tail else "")
    changed = True
    break

if changed:
    open(path, "w", encoding="utf-8", newline="").write("\n".join(lines))
    print("ZLM secret 已按 config.ini 对齐写入 config.yml（%d 字符）" % len(secret))
else:
    print("ZLM secret 与 config.ini 一致，无需改动")
SECRET_SYNC_PY
)"
  _render_detail_block "$secret_msg"
}


sync_zlm_ports_ini() {
  [ -f "$ZLM_INI" ] || return 0      # 没有 ini 就用 ZLM 默认值，不阻塞启动

  # ⛔ 取值必须分成两条语句（先声明、再赋值）：写成 `local msg="$(...)"`
  #   会把命令替换的退出码吞掉 —— 下面 python 里那句 sys.exit("❌ ...")
  #   本来是要**中止启动**的（端口没同步成功 = ZLM 会跑到规划段外），
  #   被吞掉之后就会带着错误端口继续启动。
  #
  # python 只输出纯文本（首行结论 + 其后明细），排版由 _render_detail_block 施加。
  local port_msg
  port_msg="$("$PY_BIN" - "$ZLM_INI" "$ZLM_HTTP_PORT" "$ZLM_SSL_PORT" "$ZLM_RTSP_PORT" \
                    "$ZLM_RTMP_PORT" "$ZLM_RTC_PORT" "$ZLM_RTP_PROXY_PORT" "$ZLM_RTP_RANGE" \
"$ZLM_SIGNALING_PORT" "$ZLM_SIGNALING_SSL_PORT" "$ZLM_SRT_PORT" "$ZLM_ONVIF_PORT" "$ZLM_ICE_PORT" \
"$ZLM_ENABLE_TURN" <<'ZLM_INI_PY'
import re
import sys

(path, http_port, ssl_port, rtsp_port, rtmp_port, rtc_port, rtp_proxy_port, rtp_range,
 signaling_port, signaling_ssl_port, srt_port, onvif_port, ice_port, enable_turn) = sys.argv[1:15]

# 段名（小写） → {键: 新值}。端口规划见 deploy/standalone/PORTS.md。
# ⛔ **只列真正要对外的段**：onvif/shell 这类管理端口默认关掉（port=0），
#   改它们没意义；而且它们在本机回环上，不属于防火墙要开的范围。
targets = {
    "http":   {"port": http_port, "sslport": ssl_port},
    "rtsp":   {"port": rtsp_port, "sslport": "0"},      # sslport=0 = 不启用
    "rtmp":   {"port": rtmp_port, "sslport": "0"},
    "rtc":    {"port": rtc_port,
               # ⛔⛔ WebRTC 的信令端口**键名不叫 port**（叫 signalingPort），
               #   所以按"段名 + 键名 port"去匹配是抓不到它们的 ——
               #   实测漏掉后 ZLM 报 `Listen on :: 3001 failed: address already in use`：
               #   3001 是容器默认值、这台机器已被占用，ZLM 起不来。
               #   而 status 只看 http 端口(30100) ⇒ 报「运行中」但推流/WebRTC 全废。
               "signalingPort": signaling_port,
               "signalingSslPort": signaling_ssl_port,
               # STUN/TURN：容器默认 3478 同样在规划段外，且 3478/3479 常被别的服务占
               "icePort": ice_port,
               "iceTcpPort": ice_port,
               # ⛔ TURN 默认关（=0）。它**不是端口**，所以不参与端口预检；
               #   但必须写进 ini —— 原因见文件上方 ZLM_ENABLE_TURN 处的长注释
               #   （平台只下发 STUN、从不下发 turn:，这个中继是死功能，
               #    而它的 [rtc] port_range 占着 Linux 临时端口区）。
               "enableTurn": enable_turn},
    # ⛔⛔ RTP 动态段的**真源是 [rtp_proxy] port_range**，不是 [rtp] ——
    #   ZLM 源码 src/Common/config.cpp: RtpProxy::kPortRange = "rtp_proxy.port_range"
    #   （默认 "30000-35000"）。
    #   曾把 port_range 写到 [rtp] 段，而该段只有 audioMtuSize/videoMtuSize/
    #   rtpMaxSize/lowLatency/h264_stap_a —— **根本没有 port_range 这个键** ⇒
    #   行扫描匹配不到 ⇒ 静默不生效，ZLM 实际仍用 30000-35000（5001 个端口）。
    #   症状极具误导性：PORTS.md 写着"RTP 段 = 51200-51299"、端口预检也照这段查，
    #   而 ZLM 收流实际开在 30000-35000 —— 规划与运行完全对不上，且**零报错**。
    "rtp_proxy": {"port": rtp_proxy_port, "port_range": rtp_range},
    # SRT 与 onvif 也不改就会被容器默认值(9000/3702)拖住，
    # 而这两个在目标机上很可能已被别的服务占用。
    "srt":    {"port": srt_port},
    "onvif":  {"port": onvif_port},
}

section = None
changed = []
out = []
# ⛔ 不能用 splitlines()：它会**吃掉** \r\n 的行尾信息，写回时行尾就变了
#   （ini 里混行尾虽然多数能解析，但会让 diff 变脏、且下次再匹配又对不上）。
for line in open(path, encoding="utf-8", newline="").read().split("\n"):
    stripped = line.strip()
    if stripped.startswith("[") and stripped.endswith("]"):
        section = stripped[1:-1].strip().lower()
        out.append(line)
        continue
    # ⛔⛔ 正则必须容忍 **CRLF**：ZLM 的 config.ini 是 Windows 行尾，
    #   \`(.*)$\` 里的 \`.\` 会吃掉 \r，于是键名后跟 \r 匹配不上
    #   \`([a-z_]+)(\s*=)\` ⇒ **signalingPort 这类驼峰键一个都改不到**。
    #   实测症状：日志仍显示 `Listen on :: 3001 failed: address already in use`
    #   而同步脚本报告"已改"[rtc] port —— 看着改了、实际没改。
    m = re.match(r"^(\s*)([A-Za-z_]+)(\s*=\s*)(.*?)(\r?)$", line)
    if m and section in targets and m.group(2) in targets[section]:
        indent, key, eq, old_val, cr = m.groups()
        new_val = targets[section][key]
        if old_val.strip() != new_val:
            changed.append("[%s] %s %s→%s" % (section, key, old_val.strip(), new_val))
        out.append("%s%s%s%s%s" % (indent, key, eq, new_val, cr))
        continue
    out.append(line)


# 只输出**纯文本**结论：前缀/缩进/配色由 shell 统一施加（见 ctl 顶部"输出样式"）。
# ⛔ 不再逐条打印"哪个键从多少改成多少"：12 个映射挤一大坨，而**有效端口**
#   在收尾的「端口规划」里已经分类列全了，重复输出只会让输出更乱
#   （老板 2026-10-10 定）。这里只报"改了几处"。
if changed:
    open(path, "w", encoding="utf-8", newline="").write("\n".join(out))
    print("ZLM 端口已同步到规划段（%d 处）" % len(changed))
else:
    print("ZLM 端口已与规划一致，无需改动")

# ⛔⛔ 读回校验 —— 上面打印 changed 只能证明"改到了某个同名键"，
#   不能证明**改到了 ZLM 真正读的那个键**。段名/键名任一不匹配时它静默不改，
#   日志照样显示"无需改动"（本项目已多次栽在"谎报成功"上，见 PORTS.md 附注）。
#   ⇒ 这里按 ZLM 的键路径回读一次，不对就**中止启动**（配合脚本顶部的 set -e）。
def _read_key(section_want, key):
    sec = None
    for line in open(path, encoding="utf-8", newline="").read().split("\n"):
        s = line.strip()
        if s.startswith("[") and s.endswith("]"):
            sec = s[1:-1].strip().lower()
        elif sec == section_want:
            m = re.match(r"^\s*" + re.escape(key) + r"\s*=\s*(.*?)\s*\r?$", line)
            if m:
                return m.group(1)
    return None

_got = _read_key("rtp_proxy", "port_range")
if _got != rtp_range:
    sys.exit("❌ [rtp_proxy] port_range 未生效（期望 %s，实际 %s）"
             " —— ZLM 收流端口会跑出规划段，端口预检与实际监听对不上" % (rtp_range, _got))

# ⛔ TURN 开关同样回读，但**策略刻意与上面不同**：
#   · 键在、却写不进去 ⇒ 是**我们自己的同步逻辑坏了**（段名/键名/行尾不匹配 ——
#     历史上 signalingPort 就栽在这上面：报告"已改"、实际一个都没改到）⇒ 必须中止启动。
#   · 键**根本不存在** ⇒ 是未知配置形态（旧版 ini / 客户手改过）。
#     为一个"死开关"拦住整个平台上线不划算 ⇒ 大声告警 + 给出补救命令，继续启动。
#     判据为什么不统一：port_range 影响**实际监听端口与收流功能**，写不进去会直接坏功能；
#     enableTurn 只是关掉一个没人会用的中继，没写进去不改变任何可用性。
_got_turn = _read_key("rtc", "enableTurn")
if _got_turn is None:
    print("   ⚠️ [rtc] enableTurn 键不存在，TURN 状态未知 —— 请在该段手工补一行 "
          "`enableTurn=0`（TURN 是死功能：平台只下发 STUN，从不下发 turn:；"
          "而它的 port_range 占着 49152-65535 临时端口区）", file=sys.stderr)
elif _got_turn != enable_turn:
    sys.exit("❌ [rtc] enableTurn 未生效（期望 %s，实际 %s）"
             " —— 同步逻辑写不动这个键，ZLM 会继续开着中继并占用临时端口区"
             % (enable_turn, _got_turn))
ZLM_INI_PY
)"
  _render_detail_block "$port_msg"
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
  #   只会命中本包启动的。
  pgrep -f "$ZLM_BIN" >/dev/null 2>&1
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
  # ⛔ 证书：与 nginx 共用同一套（ensure_tls_material 把 crt+key 合成 PEM）。
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
        log "ZLM 进程已退出，最后 20 行日志："
        tail -20 "$ZLM_LOG" >&2 || true
        rm -f "$ZLM_PID_FILE"
        # ⛔ 区分两种退出：端口被别人占着 vs 自身起不来。
        #   这两种的处置完全不同（前者要换端口，后者要查库），混为一谈会误导排障。
        if port_in_use_by_other "$ZLM_HTTP_PORT"; then
          fail "ZLM 端口 ${ZLM_HTTP_PORT} 已被**其他进程**占用（多半是另一套 ZLM），
   本包的 ZLM 无法bind 而退出。处理：停掉占用者，或用 UVP_ZLM_HTTP_PORT 换一个端口。"
        fi
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

  [ -f "$baseline" ] || fail "缺少建库脚本 ${baseline}（包不完整？请重新解压）"

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
      ok "数据库已就绪（${db_path}），跳过初始化"
      return 0
    fi
    log "检测到 $db_path 存在但库不完整（可能是上次建库中断），将重建"
  fi

  log "首次运行：正在初始化数据库…"
  # ⛔⛔ 下面这段 python 有三条硬约束，任一条破了都出过事故（行内注释写了原因）：
  #
  #   ① **切分规则必须与基线生成器同源**。baseline.sql 里既有 MySQL 转义换行（\n）、
  #      又有菜单标题/回调 URL 里的分号 ⇒ 想当然的切法会切坏语句，
  #      而且报的错与真实原因毫无关系（三种错法都踩过，见 verify_baseline.py 顶部）。
  #      ⇒ 内嵌的 split_sql() 与 server/resource/database/sqlitebaseline/generate.py
  #        里的同名函数**逐字一致**；verify_baseline.py 就是拿它验收基线的，
  #        所以"包里跑的切分"与"验收过的切分"是同一条规则。
  #      ⛔ 改这边必须同步改那边 —— 契约锁在 scripts/standalone-baseline-loader.test.mjs。
  #
  #   ② **整库放在一个事务里**。executescript 是"每条语句各自提交"——
  #      467 次 fsync 实测 2.5s；单事务 0.4s（6 倍），而且**原子**：中途失败回滚，
  #      不会留下"表建了一半"的库（那种库下次启动只能靠"sys_users 在不在"去猜，
  #      猜错的后果就是"页面能开、登录报 no such table"）。
  #
  #   ③ **进度条只在交互终端画**。输出被重定向或进 systemd journal 时改打阶段行，
  #      否则 \r 会把日志刷成一堆半截行。
  #
  # ⛔ PYTHONIOENCODING=utf-8 是**必须的**：目标机（客户现场是 Ubuntu 16）LANG 常是
  #   C/POSIX，此时 python 的 stdout 编码是 ASCII，一 print 中文/█/✓ 就
  #   UnicodeEncodeError。而 bash 直接写字节反而是好的 ⇒ 症状是"shell 那几行正常、
  #   python 这几行报错退出"，很难联想到是 locale。用环境变量而不是
  #   sys.stdout.reconfigure()（那是 3.7+ 的 API，py3.5 上没有）。
  PYTHONIOENCODING=utf-8 "$py" - "$baseline" "$db_path" <<'DB_INIT_PY'
import hashlib
import os
import re
import sqlite3
import sys
import time
from pathlib import Path

# ⛔ 输出编码：主要靠调用方设的 PYTHONIOENCODING=utf-8（见 shell 里的调用行，
#   那是**版本无关**的做法 —— py3.5 上根本没有 reconfigure）。这里再兜一层，
#   方便有人把这个 heredoc 抠出来单独跑（比如契约测试）时不至于炸编码。
try:
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
except Exception:
    pass

# shell 顶部把同一套颜色导进来了（非终端 / NO_COLOR 时为空串）。
C_OK = os.environ.get("UVP_C_OK", "")
C_WARN = os.environ.get("UVP_C_WARN", "")
C_RST = os.environ.get("UVP_C_RST", "")
IND = "      "        # 与 shell 的 det()/ok() 同宽（"[uvp] " 恰好 6 个字符）

baseline_path, db_path = Path(sys.argv[1]), Path(sys.argv[2])


def split_sql(text):
    """按分号切分语句，跳过字符串字面量与注释里的分号。

    ⛔ 不能直接 text.split(';')：菜单标题、回调 URL 里都可能带分号（带查询串的
       回调地址就有），切出来的片段既不完整也不报错，表现为"少了几条 INSERT"。
    ⛔ 也不能用 sqlite3.complete_statement 逐行累积：前一句的尾随注释会被算进
       缓冲区导致误判，报 no such table —— 而表明明就在前面几行。

    ⛔⛔ 本函数与 server/resource/database/sqlitebaseline/generate.py 的
       split_sql() 必须**逐字一致**（那边是基线生成器、也是 verify_baseline.py
       验收基线时用的切分器）。契约锁在 scripts/standalone-baseline-loader.test.mjs。
    """
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
                if i + 1 < n and text[i + 1] == quote:   # '' 转义
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


sql = baseline_path.read_text(encoding="utf-8")
stmts = split_sql(sql)
if not stmts:
    raise SystemExit("建库失败：从 baseline.sql 里切不出任何语句（文件被截断？）")

# 仅用于对账：只打印切分指纹就退出，不碰数据库。
# 契约测试拿它跟 generate.split_sql() 的结果比 —— 两边一旦漂移，指纹立刻对不上。
if os.environ.get("UVP_DB_SPLIT_DIGEST") == "1":
    digest = hashlib.sha256("\n\x1e\n".join(stmts).encode("utf-8")).hexdigest()
    print("stmts=%d sha256=%s" % (len(stmts), digest))
    sys.exit(0)


def phase_of(statement):
    head = statement[:40].upper()
    if head.startswith("CREATE TABLE"):
        return "table"
    if head.startswith("CREATE INDEX") or head.startswith("CREATE UNIQUE INDEX"):
        return "index"
    if head.startswith("INSERT"):
        return "seed"
    if head.startswith("PRAGMA"):
        return "pragma"
    return "other"


PHASE_LABEL = {"table": "建表", "index": "建索引", "seed": "灌种子数据",
               "pragma": "设置", "other": "其它"}

phases = [phase_of(s) for s in stmts]

# ⛔⛔ 进度分母只算**真正会建出对象**的语句。
#   原因：SQLite 里**表名和索引名都是库级唯一**，而 MySQL 的索引名只在**表内**唯一
#   ⇒ 基线从 MySQL 转过来后，同一个索引名会在多张表上各出现一次（如 idx_deleted_at
#   在 gb_device / gb_channel 等 5 张表上都有），这些重复的
#   `CREATE INDEX IF NOT EXISTS` 会被 SQLite **静默跳过**（实测 9 条）。
#   若按语句条数计进度，进度条跑到 "316/316" 而结尾对账只有 307 个索引 ——
#   读者的第一反应是"对账写错了"，而不是"有 9 条压根没生效"。
#   ⇒ 分母改用"去重后真正会建出来的对象数"，并把被跳过的那几条**显式报出来**。
CREATE_OBJ_RE = re.compile(
    r'(?is)^CREATE\s+(?:UNIQUE\s+)?(TABLE|INDEX)\s+(?:IF\s+NOT\s+EXISTS\s+)?"?([^"\s(]+)"?')

counted = [True] * len(stmts)      # counted[i]=False ⇒ 该语句不会建出新对象，不计入进度
obj_names = set()
skipped_dup = []
for _i, _stmt in enumerate(stmts):
    if phases[_i] not in ("table", "index"):
        continue
    _m = CREATE_OBJ_RE.match(_stmt)
    if not _m:
        continue
    _key = (_m.group(1).upper(), _m.group(2))
    if _key in obj_names:
        counted[_i] = False
        skipped_dup.append(_m.group(2))
    else:
        obj_names.add(_key)

total = sum(counted)
counts = {name: sum(1 for c, p in zip(counted, phases) if c and p == name)
          for name in PHASE_LABEL}
seen = dict.fromkeys(PHASE_LABEL, 0)   # 逐条累加，避免每条都 phases[:i].count()（O(n²)）
_done = [0]

# ---- 进度渲染 ------------------------------------------------------------
# ⛔ 进度写 stdout 而不是 stderr：本脚本其余输出都在 stdout，两个流混着走
#   会出现"进度条与日志互相盖"的乱序（stdout 行缓冲、stderr 无缓冲）。
BAR = 26
show_bar = sys.stdout.isatty() and os.environ.get("UVP_NO_PROGRESS") != "1"
_drawn = [0.0]
_reported = set()


def _bar(done, note):
    filled = BAR * done // total
    return "[%s%s] %3d%%  %s" % ("█" * filled, "░" * (BAR - filled),
                                 done * 100 // total, note)


def advance(idx, phase):
    """idx: 本语句在 stmts 里的 1-based 序号。"""
    if counted[idx - 1]:
        _done[0] += 1
        seen[phase] += 1
    note = "%s %d/%d" % (PHASE_LABEL[phase], seen[phase], counts[phase])
    if not show_bar:
        # 非交互：只在每个主要阶段**跑完**时打一行 —— 467 行会把 journal 刷爆，
        # 但一行都不打又让日志里"这段时间在干什么"变成空白。
        if (phase in ("table", "index", "seed") and phase not in _reported
                and seen[phase] == counts[phase]):
            _reported.add(phase)
            print(IND + note)
        return
    now = time.monotonic()
    # 限流：逐条重绘在快机器上会闪成一片（467 条 ≤0.4s ⇒ 每秒上千帧）
    if now - _drawn[0] < 0.05 and _done[0] < total:
        return
    _drawn[0] = now
    sys.stdout.write("\r" + IND + _bar(_done[0], note))
    sys.stdout.flush()


# ---- 执行 ----------------------------------------------------------------
# ⛔ 外键必须显式打开：SQLite 的 foreign_keys PRAGMA **默认为 OFF**，
#   建表语句里那些 REFERENCES 在默认连接下全是摆设，表现为"引用完整性悄悄没了"
#   —— 开发库验过、客户机上数据却能写成孤儿。
# ⛔⛔ 而且必须在**事务外**执行：SQLite 里 `PRAGMA foreign_keys` 在事务内是
#   **静默空操作**。基线文件开头那两句 PRAGMA 若不搬出事务，就等于没开。
db = sqlite3.connect(db_path)
db.execute("PRAGMA foreign_keys = ON")
db.execute("PRAGMA busy_timeout = 5000")

processed = 0
try:
    db.execute("BEGIN")
    for idx, statement in enumerate(stmts, start=1):
        if phases[idx - 1] == "pragma":
            if db.in_transaction:
                db.execute("COMMIT")
            db.execute(statement)          # PRAGMA 要事务外才生效
            db.execute("BEGIN")
        else:
            db.execute(statement)
        processed = idx
        advance(idx, phases[idx - 1])
    db.execute("COMMIT")
except Exception as exc:
    try:
        db.execute("ROLLBACK")
    except sqlite3.Error:
        pass
    db.close()
    try:
        os.remove(db_path)      # 别留半成品：下次启动会看到"文件在、库是空的"
    except OSError:
        pass
    bad = stmts[processed][:200].replace("\n", " ") if processed < len(stmts) else ""
    raise SystemExit(
        "建库失败，已回滚（未留下半成品库）\n"
        "  %s: %s\n"
        "  第 %d 条语句: %s" % (type(exc).__name__, exc, processed + 1, bad)
    )

if show_bar:
    # 把 100% 那一帧固定下来（补换行），免得它被后面的输出用 \r 擦掉
    sys.stdout.write("\r" + IND + _bar(total, "完成") + "\n")
    sys.stdout.flush()

# ---- 建库后自检 ----------------------------------------------------------
# ⛔ 判据不是"没报错"，而是逐项对账（与 verify_baseline.py 同口径）：
#   少建表/漏灌种子都不会报错，只有对账才看得出来。
def one(query):
    return db.execute(query).fetchone()[0]


tables = one("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
indexes = one("SELECT count(*) FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%'")
seeded = 0
for (name,) in db.execute(
        "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"):
    seeded += one('SELECT count(*) FROM "%s"' % name)
admins = one("SELECT count(*) FROM sys_users WHERE username='admin'")
fk_on = one("PRAGMA foreign_keys")
integrity = one("PRAGMA integrity_check")
dangling = db.execute("PRAGMA foreign_key_check").fetchall()
db.close()

if integrity != "ok":
    raise SystemExit("建库后自检失败: integrity_check=%s" % integrity)
if fk_on != 1:
    raise SystemExit("建库后自检失败: 外键未开启（PRAGMA foreign_keys=%s）" % fk_on)
if dangling:
    raise SystemExit("建库后自检失败: 外键悬挂 %s" % dangling[:5])
if admins != 1:
    raise SystemExit("建库后自检失败: admin 账号数=%s（应为 1）" % admins)

print(IND + C_OK + "✓" + C_RST + " %d 张表 · %d 个索引 · %d 行种子数据 · 管理员账号已就位"
      % (tables, indexes, seeded))

# ⛔ 被 SQLite 静默跳过的那几条必须报出来，不能只在进度分母里悄悄扣掉：
#   少了索引不会报错，只会让软删表（gb_device 等）的查询在数据量上来后变慢，
#   到时候没人能想起"装库时就少建了 9 个索引"。
#   ⭐ 根治在 generate.py：索引名要带上表名（项目里新模型已经这么做了，
#     如 idx_openapi_capability_group_deleted），而不是沿用 MySQL 的表内短名。
if skipped_dup:
    names = sorted(set(skipped_dup))
    shown = " / ".join(names[:4]) + (" / …" if len(names) > 4 else "")
    print(IND + C_WARN + "!" + C_RST + " %d 条 CREATE INDEX 未生效：SQLite 的索引名是库级唯一"
          "（MySQL 只在表内唯一），同名只建第一个" % len(skipped_dup))
    print(IND + "  · 重名索引：%s" % shown)
DB_INIT_PY
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
  #
  # ⛔⛔ 但「nginx 端口」有个例外：它**不写进 config.yml**（nginx 读的是自己的
  #   conf），所以这里必须拿它和 config.env 里的值对照：
  #   若 config.env 记录的是**上一版包的默认值**（本版已改），就该更新。
  #   ⇒ 判据：值等于「非当前默认值」时，无法区分是"用户主动设的"还是
  #   "老包留下的"。这个歧义必须消掉，否则升级包后客户会莫名停在旧端口。
  #   ⇒ 做法：给 config.env 加一个"版本标记"，值变了就整体重写。
  if [ ! -f "$ENV_FILE" ]; then
    local yml_http yml_redis
    yml_http="$(sed -n 's/^[[:space:]]*port:[[:space:]]*:\([0-9]\+\).*/\1/p' "$CONF" | head -1)"
    yml_redis="$(awk '/^redis:/{f=1;next} f&&/^[^[:space:]]/{exit} f&&/^[[:space:]]+port:/{print $2;exit}' "$CONF")"
    HTTP_PORT="${yml_http:-$HTTP_PORT}"
    REDIS_PORT="${yml_redis:-$REDIS_PORT}"
  fi

  # ⛔ 写入版本标记：包升级后若规划变了（PORT_PLAN_REV），旧的 config.env 整体作废，
  #   重新按新默认值起。⛔ 但**用户显式设过的端口要保留** ——
  #   见下方 preserve_env_if_user_set()：只有"值等于旧默认值"才跟着升级，
  #   用户手动改过的一律不动。
  # ⛔⛔⛔ 这里**不能硬编码「上一版的默认值」**来判别"是不是用户设的"。
  #   实测踩了两次：
  #   ① 我把默认值改成 30010，却把"旧默认"仍写成 8280 —— 而8280 恰好是
  #      上一版的真实默认值，判据本身没错，但我改代码时漏改这里就等于
  #      **判据会随版本漂移**；下次改默认值时又错一次。
  #   ② 调试残留（UVP_NGINX_HTTP_PORT=8280）被误判成"用户主动设的"，
  #      于是 nginx 去 bind 后端的端口。
  #
  # ⭐ 正确判据：**config.env 里显式记录"这些值是默认还是用户设的"**。
  #   写入时对每个键加一个 UVP_USER_SET_<键> 标记；只有带标记的才当用户设置。
  #   缺标记的（老包写的）一律按新默认值处理 —— 老包本来就没有标记，
  #   这正好与"跟着升级"的诉求一致。
  #   ⚠️ 若用户手工编辑过 config.env，标记还在 ⇒ 依然尊重其设置。
  # 端口值在文件头已由 read_port_env 决定（只认带 UVP_USER_SET_ 标记的键），
  # 这里只负责把最终生效值落盘。
  {
    printf 'UVP_HTTP_PORT=%s\nUVP_REDIS_PORT=%s\n' "${HTTP_PORT}" "${REDIS_PORT}"
    printf 'UVP_HTTPS_PORT=%s\nUVP_NGINX_HTTP_PORT=%s\n' "${NGINX_HTTPS_PORT}" "${NGINX_HTTP_PORT}"
    # 记录"这四个值不是用户设的" ⇒ 下次读到它们时按默认值处理。
    # ⛔ 环境变量显式传入时（${UVP_*}非空）才算用户设置，要写 1。
    [ -n "${UVP_HTTP_PORT:-}" ]        && printf 'UVP_USER_SET_UVP_HTTP_PORT=1\n'
    [ -n "${UVP_REDIS_PORT:-}" ]       && printf 'UVP_USER_SET_UVP_REDIS_PORT=1\n'
    [ -n "${UVP_HTTPS_PORT:-}" ]       && printf 'UVP_USER_SET_UVP_HTTPS_PORT=1\n'
    [ -n "${UVP_NGINX_HTTP_PORT:-}" ]  && printf 'UVP_USER_SET_UVP_NGINX_HTTP_PORT=1\n'
  } > "$ENV_FILE.new"
  # ⛔⛔⛔ 必须把**非本函数托管的键原样搬回去**，否则用户手工加的键会被无声清掉。
  #
  #   实测踩过：`sync_ports_into_config` 用 `> "$ENV_FILE"` 全量覆盖、只写 4 个端口键，
  #   而上一版的注释正教用户「在 config.env 里加一行
  #   UVP_SIP_TRACE_ENCRYPTION_KEY=...  重启即生效」——
  #   ⇒ 用户照做、重启、密钥消失、SIP 报文诊断永久degraded，
  #   **全程零报错**（fail-closed 不落库，连日志都不一定明显）。
  #
  #   ⛔ 不能改成「只在文件不存在时才写」：那会让端口变更不再持久化。
  #   ⛔ 也不能靠「追加」：`>` 是这里的既有语义（要重置 USER_SET 标记）。
  #   ⇒ 做法：先备份旧文件里**所有非托管键**（含注释行），写完端口后再追加回去。
  #
  #   托管键清单要显式列出：只有这4 个端口键 + 对应的 UVP_USER_SET_ 标记会被接管。
  local preserved
  preserved="$(mktemp "$ROOT/config/.config.env.preserve.XXXXXX")"
  if [ -f "$ENV_FILE" ]; then
    grep -vE '^(UVP_HTTP_PORT|UVP_REDIS_PORT|UVP_HTTPS_PORT|UVP_NGINX_HTTP_PORT|UVP_USER_SET_UVP_HTTP_PORT|UVP_USER_SET_UVP_REDIS_PORT|UVP_USER_SET_UVP_HTTPS_PORT|UVP_USER_SET_UVP_NGINX_HTTP_PORT)=' "$ENV_FILE" > "$preserved" || true
  else
    : > "$preserved"
  fi
  cat "$ENV_FILE.new" > "$ENV_FILE"
  # 追加时先补一个空行，避免用户第一行注释紧贴端口行、可读性差
  if [ -s "$preserved" ]; then
    printf '\n' >> "$ENV_FILE"
    cat "$preserved" >> "$ENV_FILE"
  fi
  rm -f "$ENV_FILE.new" "$preserved"

  # nginx 端口也持久化，否则 stop/status 阶段读到的是默认值

  # config.yml 里同步：后端是通过这两个键读端口的
  if [ -f "$CONF" ]; then
    "$PY_BIN" - "$CONF" "$HTTP_PORT" "$REDIS_PORT" "$ZLM_HTTP_PORT" "$ZLM_RTP_PROXY_PORT" <<'SYNC_PY'
import re, sys
path, http_port, redis_port, zlm_http, zlm_rtp_proxy = sys.argv[1:6]
lines = open(path, encoding="utf-8").read().split("\n")

def replace_in_section(section, key, value):
    """只在指定段落内替换末级键 —— host/port 这类键在 gormv2 各段下重名，
    全局替换会把 mysql 的也改掉。找不到段落或键就返回 False，不猜。

    ⛔⛔ 本段与文件里其余内嵌 Python 一样，**禁用 f-string**：目标机可能是
    Ubuntu 16（自带 python3 = 3.5），f-string 是 3.6+ 语法，会让整个 heredoc
    在编译期 SyntaxError（报错行指向用到 f-string 的那句，与真因无关）。"""
    start = None
    for i, line in enumerate(lines):
        if line.strip() == section + ":":
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
    pat = re.compile(r"^(?P<ind>\s*)" + key + r"\s*:\s*.*?(?P<trail>\s*)$")
    for i in range(start + 1, end):
        m = pat.match(lines[i])
        if m:
            lines[i] = "%s%s: %s" % (m.group("ind"), key, value)
            return True
    return False

# 只在值真的变了才写盘，避免无谓地改 mtime（运维会误以为配置被动过）
dirty = False
if replace_in_section("httpserver", "port", ":" + http_port):
    dirty = True
if replace_in_section("redis", "port", redis_port):
    dirty = True
# ---- ZLM 相关端口（见 deploy/standalone/PORTS.md）----
# ⛔ 这三处必须跟着 ZLM 实际监听的端口走，否则会出现
#   「平台按 18080 调 ZLM、ZLM 却听 30100」⇒ 所有推流/取流静默失败。
for _sec, _pairs in (
    ("zlm", {"httpport": zlm_http, "rtpport": zlm_rtp_proxy}),
    ("media", {"hookport": http_port}),      # ZLM hook 回调指向本机后端
):
    for _k, _v in _pairs.items():
        if replace_in_section(_sec, _k, str(_v)):
            dirty = True
if dirty:
    open(path, "w", encoding="utf-8").write("\n".join(lines))
SYNC_PY
  fi
  ok "端口配置已写入 config.env / config.yml"
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
    if [ -z "$PY_BIN" ]; then
      fail "找不到 python3 —— 改端口需要它（Ubuntu/Debian 请先 apt install python3）"
    fi
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