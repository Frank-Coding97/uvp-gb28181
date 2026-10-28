#!/usr/bin/env bash
# 组装 UVP 绿色安装包（Linux / x86_64 或 aarch64 / 零预装）。
#
# 出 ARM64（aarch64）包：UVP_PKG_ARCH=arm64 bash deploy/standalone/build-standalone.sh
#   ⛔ 前置的三个原生二进制（ZLM / nginx / redis）也必须是 aarch64 的，
#     脚本会在出包前逐个用 `file` 断言（见文件后半的静态校验段）。
#     构建它们的容器见本目录下的 arm64/（openEuler 20.03 LTS 底座，自带 gcc 7.3）。
#
# 产出一个 tar.gz：解压后执行 ./uvp-gb28181-ctl.sh start 即可用，
# 不需要 Docker / nginx / MySQL / Redis（Redis 与 SQLite 都自带）。
# 想开机自启再跑 ./uvp-gb28181-service-install.sh（可选，见该脚本头部说明）。
#
# 产物命名 **uvp-gb28181-<系统>-<架构>-<版本>.tar.gz**（如 uvp-gb28181-linux-amd64-1.1.0.tar.gz），
# 落在 release-output/，并附同名 .sha256 校验文件。
# 解压出来的顶层目录与包名同名（去掉 .tar.gz），即 uvp-gb28181-linux-amd64-1.1.0/。
#
# 用法：
#   deploy/standalone/build-standalone.sh [--skip-frontend] [--out DIR]
#
# 前置：
#   · server/bin/uvp-server            —— ${PKG_OS}/${PKG_ARCH} 静态二进制（由本脚本交叉编译产出）
#   · deploy/standalone/bin/redis-server、redis-cli
#   · web/dist                          —— 前端产物（--skip-frontend 时复用）
#   · server/resource/database/sqlitebaseline/baseline.sql —— SQLite 建库脚本（已生成）

# ⛔ 写这个脚本时的硬约束：**变量名后紧跟中文标点时必须写成 ${VAR}**。
#bash 5 会把全角标点当成变量名的一部分，于是报
#   "ARCHIVE（: unbound variable" —— 而 ARCHIVE 明明刚���赋值成功，
#   报错信息里的变量名还带着个怪字符，肉眼很难反应过来。

set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

SERVER_DIR="$REPO_ROOT/server"
WEB_DIR="$REPO_ROOT/web"
DEPLOY_DIR="$REPO_ROOT/deploy/standalone"

# ---- 端口默认值（必须与 uvp-gb28181-ctl.sh 的 *_PORT_DEFAULT 完全一致）----
# ⛔⛔ 为什么这里也要有一份：make-config.py 靠命令行参数把端口写进 config.yml，
#   而**首次运行时 uvp-gb28181-ctl.sh 以 config.yml 为权威**。两边不一致的话，
#   首次启动时变量会被 config.yml 里的值覆盖 —— 实测症状是
#   「脚本里明明写着 30011，Redis 却起在 6379」，毫无线索。
#   根因是这两个变量一直只靠调用方传环境变量，不传就落回 make-config.py
#   的 argparse 默认值，于是与 uvp-gb28181-ctl.sh 各说各话。
#   端口规划见 deploy/standalone/PORTS.md。
# 规划段 51000-51064 连续无空洞（详见 PORTS.md）。
HTTP_PORT="${UVP_HTTP_PORT:-51002}"
REDIS_PORT="${UVP_REDIS_PORT:-51003}"

# ---- 浏览器入口端口（nginx 明文口）----
#
# ⛔⛔ 收尾提示里「访问 http://<IP>:端口」必须写这个，**不能写 $HTTP_PORT**：
#   后端只服务 /public 与 API，**不服务 SPA** —— 照后端端口打开只会得到 404，
#   而客户会以为"装失败了"。这个坑在 README 里也踩过一次（当时写成 51002）。
# ⛔ 不在本文件另写一份字面量：端口默认值只在 uvp-gb28181-ctl.sh 里定义，
#   这里现读，避免"改了 ctl.sh 却忘了改这里"的第二份真相。
NGINX_HTTP_PORT_DEFAULT="$(sed -n 's/^NGINX_HTTP_PORT_DEFAULT=//p' "$DEPLOY_DIR/uvp-gb28181-ctl.sh" | tail -1)"
NGINX_HTTPS_PORT_DEFAULT="$(sed -n 's/^NGINX_HTTPS_PORT_DEFAULT=//p' "$DEPLOY_DIR/uvp-gb28181-ctl.sh" | tail -1)"
# ⚠️ 这里还不能调 fail()：它定义在下面（解析完参数之后）。就地报错并退出。
if [ -z "$NGINX_HTTP_PORT_DEFAULT" ]; then
  printf 'ERROR: 读不到 nginx 明文端口（ctl.sh 的 NGINX_HTTP_PORT_DEFAULT 没定义？）\n' >&2
  exit 1
fi
if [ -z "$NGINX_HTTPS_PORT_DEFAULT" ]; then
  printf 'ERROR: 读不到 nginx HTTPS 端口（ctl.sh 的 NGINX_HTTPS_PORT_DEFAULT 没定义？）\n' >&2
  exit 1
fi

# ctl_port_default 从 uvp-gb28181-ctl.sh 里取某个端口的**兜底默认值**。
#
# ⛔ 只认 `VAR="${VAR:-<数字或段>}"` 这种形态（ctl.sh 里 ZLM 那 12 个键都是这么写的）。
#   不复制字面量的理由与上面 nginx 端口完全相同：默认值只能有一个真源，
#   否则出包时写进 config.env 的值会和运行期默认值悄悄分家 ——
#   客户打开文件看到 51004，实际跑的却是别的端口。
ctl_port_default() {
  local var="$1" out
  out="$(sed -n "s/^$var=\"[\$]{$var:-\([0-9][0-9-]*\)[}]\"/\1/p" "$DEPLOY_DIR/uvp-gb28181-ctl.sh" | tail -1)"
  if [ -z "$out" ]; then
    return 1
  fi
  printf '%s' "$out"
}

# ---- SIP 端口（引导页默认值，构建期注入到前端）----
#
# ⛔⛔ SIP 端口**不走 config.env** —— 2026-07-20 起 SIP 明文配置从 YAML 移进了
#   `gb_sip_config` 表，端口由管理员首次登录的引导页录入（见 server/config/config.example.yml 注释）。
#   ⇒ 包里唯一能"默认落进规划段"的着力点，就是给前端**构建期**注入一个默认值：
#     打出来的 SPA 里 import.meta.env.VITE_DEFAULT_SIP_PORT = 51064，
#     向导一打开就预填段内端口（管理员仍可改）。
#   ⇒ 不注入的构建形态（开发、其它交付包）回落 5061，行为完全不变。
#   ⚠️ 因此 SIP 端口**改不了 config.env**：现场要改就在引导页改（存 DB）。
SIP_PORT="${UVP_SIP_PORT:-51064}"

# ---- ZLM 的两个身份：secret 与节点标识（老板定的：都用固定值）----
#
# ⛔⛔ 必须是**固定值**，不能让它们随机 —— 这是本轮最贵的两个教训：
#   · secret 对不上：平台调 ZLM 全被拒（-100 Please login first），
#     录像缓存/点播/录像查询 runtime 全部跳过装配，
#     页面报「录像缓存服务未装配」，与真因完全无关。
#   · 节点标识（mediaserverid）更隐蔽：它 seed 进 meta_node.media_server_uuid，
#     又被 apply.go 通过 setServerConfig **持久化回 ZLM 的 config.ini** ——
#     随机值一旦落盘就固化，之后改配置也不会重新对齐，
#     而 hook 归属校验（hook.go 用 MediaServerUUID 比对 body.MediaServerID）
#     就再也过不了，且**没有任何报错**。
#
# 出包时就把两者写进 config.yml，让"两处不一致"从根上不可能发生
#（此前是在启动时反推对齐，连踩两层判据不匹配的坑）。
# ⛔⛔ 绝不能用 ZLMediaKit 的官方默认 secret（035c73f7-bb6b-4889-a715-d9eb2d1925cc）。
#   二开版 main.cpp:301 有硬编码规则：secret 等于该默认**或为空**时，
#   自动 makeRandStr(32) 换成随机值并 **dumpFile 写回 config.ini**。
#   ⇒ 包里明明写死了这个值，装完启动一次就被ZLM 自己改掉，
#     而平台侧 config.yml 不知道这件事 ⇒ 双方永久失配。
#   症状极具误导性：ZLM 日志里所有成功请求都带着旧 secret（它启动时加载的是内存值），
#   而配置文件已经变了 —— 只有重启后才会暴露。
ZLM_SECRET="${UVP_ZLM_SECRET:-uvp-9f3c7a1e5d84b206c1a9e4f7b2d6c805}"
ZLM_MEDIA_SERVER_ID="${UVP_ZLM_MEDIA_SERVER_ID:-uvp-media-server-0001}"
SQLITE_DIR="$SERVER_DIR/resource/database/sqlitebaseline"
# ZLM（二开版）与它的运行时库：构建时由 deploy/standalone/fetch-zlm.sh 放到这里。
# ⛔ 不入库：MediaServer 13MB + ffmpeg 运行时库 32MB + www 16MB，且必须与目标机架构匹配。
ZLM_DIR="$DEPLOY_DIR/bin/zlm"
# nginx（前端托管 + HTTPS 终止）：构建时由 deploy/standalone/build-nginx.sh 放到这里。
# ⛔ 不入库：二进制必须与目标机的 OpenSSL 大版本对齐（见该脚本的说明）。
NGINX_DIR="$DEPLOY_DIR/bin/nginx"

# ⛔ 端口默认值只在上面定义一次（30010/30011）。
# ⛔⛔ 这里曾有过第二组 8280/6379 —— bash **后定义覆盖先定义**，
#   于是上面新加的 30010/30011 被这组旧的盖回去，端口规划等于没改。
#   与 uvp-gb28181-ctl.sh 里 nginx 端口那个坑是同一个：**同名的第二处定义**。
VERSION="${UVP_VERSION:-$(cat "$SERVER_DIR/version.json" 2>/dev/null | grep -m1 '"version"' | cut -d'"' -f4 || echo 1.0.0)}"

# ⛔⛔ 产物包名 = **产品-系统-架构-版本**（老板 2026-10-10 定）：
#     uvp-gb28181-linux-amd64-1.1.0.tar.gz
#   ⭐ 系统/架构这一对变量**同时喂给交叉编译那行**（见下面 GOOS/GOARCH），
#     故意不写两份：只改包名忘了改编译目标，会做出「包名叫 linux-amd64、
#     二进制其实是别的架构」这种包内外不一致的包，且出包与解压全程零报错。
#   ⭐ 包内解压出来的顶层目录与包名**同名**（见下面 PKG="$STAGE/${PKG_STEM}"）：
#     tar 里的根目录就是 uvp-gb28181-linux-amd64-1.1.0/，解压后目录名 = 包名去扩展名。
#     ⛔ 这两处必须共用 PKG_STEM，别再各写一份 —— 曾经就是「包名改了、解压目录没改」。
# ⛔ 系统/架构可被环境变量覆盖（出 ARM64 包：UVP_PKG_ARCH=arm64）。默认仍是 amd64，
#   老调用方（含 220 上的出包流程）行为完全不变。
PKG_OS="${UVP_PKG_OS:-linux}"
PKG_ARCH="${UVP_PKG_ARCH:-amd64}"
PKG_STEM="uvp-gb28181-${PKG_OS}-${PKG_ARCH}-${VERSION}"

SKIP_FRONTEND=0
OUT_DIR="$REPO_ROOT/release-output"

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-frontend) SKIP_FRONTEND=1; shift ;;
    --out) OUT_DIR="$2"; shift 2 ;;
    *) printf '未知参数: %s\n' "$1" >&2; exit 2 ;;
  esac
done

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
log()  { printf '[build] %s\n' "$*"; }

NODE_BIN="${NODE:-node}"
command -v "$NODE_BIN" >/dev/null 2>&1 || fail "构建机需要 node（生成发布身份文件）"

# 默认只允许把干净 Git 提交标记为发布构建。需要从本地改动出包时，调用方必须
# 显式提供一个独立的 UVP_BUILD_ID，避免把未提交代码伪装成 HEAD 对应的发布物。
BUILD_ID="${UVP_BUILD_ID:-}"
if [ -z "$BUILD_ID" ]; then
  command -v git >/dev/null 2>&1 || fail "找不到 git；请显式设置 UVP_BUILD_ID"
  [ -z "$(git -C "$REPO_ROOT" status --porcelain --untracked-files=normal)" ] || fail "源码工作区有未提交改动；请先提交，或显式设置 UVP_BUILD_ID"
  BUILD_ID="$(git -C "$REPO_ROOT" rev-parse HEAD)"
fi
[[ "$BUILD_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] || fail "UVP_BUILD_ID 只能包含字母、数字、点、下划线或连字符"

# 构建机上用的 python（生成配置要跑脚本）。目标机上由 uvp-gb28181-ctl.sh 自行探测。
PY="${UVP_BUILD_PY:-python3}"
command -v "$PY" >/dev/null 2>&1 || fail "构建机需要 python3（用来生成 config.yml）"

# ---------------------------------------------------------------- 前置检查 ----

# ⛔⛔ shellcheck 门禁（不是可选的质量检查，是**必须过**的）：
#   `bash -n` 抓不到"命令名后面紧跟 # 注释"这类错 —— 因为它语法合法，
#   bash 会把 start_zlm# 当成一个叫「start_zlm#」的命令，报command not found。
#   ⭐ 这个错我连犯两次（ensure_database#、start_zlm#），都是靠实跑才发现，
#   所以必须用工具兜住而不是靠眼睛。SC2288 就是专抓这个的。
if command -v shellcheck >/dev/null 2>&1; then
  log "shellcheck 检查脚本"
  shellcheck_out="$(shellcheck -S warning "$0" "$DEPLOY_DIR/uvp-gb28181-ctl.sh" 2>&1)" || true
  if [ -n "$shellcheck_out" ]; then
    printf '%s\n' "$shellcheck_out" >&2
    fail "shellcheck 未通过（见上）。⚠️ 这类问题 bash -n 抓不到，必须过。"
  fi

  # ---- 端口变量单点定义门禁 ----
  # ⛔⛔ 今天在两个脚本里各栽一次「同名变量的第二处定义」，而症状都是
  #   「明明改了端口，运行时还是旧值」且**毫无提示**：
  #   uvp-gb28181-ctl.sh 里 nginx 端口有第二组（443/80），build-standalone.sh 里
  #   HTTP_PORT/REDIS_PORT 有第二组（8280/6379）。
  #   bash 的**后定义覆盖先定义**，且shellcheck 不报（语法合法、变量名正确）。
  #   ⇒ 这里显式拦：端口变量每台机器只允许出现一次赋值。
  log "检查端口变量是否单点定义"
  # ⛔⛔ 判据要挑对，否则门禁自己变噪音。
  #   我第一版按"变量名出现过几次"来数 ⇒ **误报**：端口变量的正常写法本来就是
  #   两行（先读环境变量、再套默认值）：
  #       HTTP_PORT="${UVP_HTTP_PORT:-$(read_port_env UVP_HTTP_PORT)}"
  #       HTTP_PORT="${HTTP_PORT:-$HTTP_PORT_DEFAULT}"
  #   这样每个变量必然出现两次。
  #   ⭐ 真正要抓的是「**两处独立的默认值**」—— 也就是 `X="${...:-默认值}"`
  #   这种**自带字面量**的赋值在同一变量上出现多次（后者会覆盖前者）。
  #   判据：只看自带字面量的那一类。
  dup_ports="$(grep -hE '^(HTTP_PORT|REDIS_PORT|NGINX_HTTPS_PORT|NGINX_HTTP_PORT|ZLM_HTTP_PORT|ZLM_RTSP_PORT)="\$\{[A-Za-z_]+:-[0-9]+' \
    "$0" "$DEPLOY_DIR/uvp-gb28181-ctl.sh" 2>/dev/null | sed 's/=.*//' | sort | uniq -d || true)"
  if [ -n "$dup_ports" ]; then
    printf '端口变量存在多处字面量默认值（后者覆盖前者，改动不生效）：\n%s\n' "$dup_ports" >&2
    fail "端口默认值必须单点定义。端口规划见 deploy/standalone/PORTS.md"
  fi
else
  log "⚠️ 未安装 shellcheck，跳过脚本静态检查（apt install shellcheck 可启用）"
fi

# ---- nginx 两个端口默认值：都存在、且**互不相同** ----
#
# ⛔⛔ 为什么单独加这一条（2026-10-19 端口互换时发现的门禁缺口）：
#   这两个默认值在 ctl.sh 里是**成对**使用的（一个给 TLS 段、一个给明文段）。
#   只改其中一个（或抄漏一行）时，最坏的结果不是"端口错了"，而是**两个 listen
#   抢同一端口**：httpsconfig.Render 会报「HTTPS 端口不能与 HTTP 端口相同」，
#   客户看到的是"装不上"，而真因在出包脚本里一个数字。
#   上面那段 dup_ports 只抓"同一变量被定义两次"，抓不到"两个变量撞成同一个值"。
# ⛔ 判据用取值比对，不用"出现过几次"（两行式赋值必然出现两次，数次数会误报）。
nginx_https_default="$(sed -n 's/^NGINX_HTTPS_PORT_DEFAULT=//p' "$DEPLOY_DIR/uvp-gb28181-ctl.sh" | tail -1)"
nginx_http_default="$(sed -n 's/^NGINX_HTTP_PORT_DEFAULT=//p' "$DEPLOY_DIR/uvp-gb28181-ctl.sh" | tail -1)"
[ -n "$nginx_https_default" ] && [ -n "$nginx_http_default" ] \
  || fail "uvp-gb28181-ctl.sh 里找不到 NGINX_HTTPS_PORT_DEFAULT / NGINX_HTTP_PORT_DEFAULT（默认值换写法了？）"
[ "$nginx_https_default" != "$nginx_http_default" ] \
  || fail "nginx 的 HTTPS 与明文端口默认值相同（${nginx_https_default}）—— 两个 listen 会抢同一端口"
log "nginx 端口默认值：明文 ${nginx_http_default} / HTTPS ${nginx_https_default}"

# ------------------------------------------------------------ 自动构建 ----
#
# ⛔⛔⛔ 为什么这里必须自己构建，而不是「复制预编译产物」（老板 2026-10-09 定）
#
# 这个脚本原本只有两行`cp`：
#     cp "$SERVER_DIR/bin/uvp-server" "$PKG/bin/uvp-server"
#     cp -a "$WEB_DIR/dist/." "$PKG/resource/public/"
#   ⇒ 它**从不自己编译**，只是把上次构建的产物搬进包里。
#
#   而所有出包断言（ZLM 身份、扫码基址、SQLite 基线校验）验的都是**配置**，
#   **产物压根不在断言范围内** ⇒ 打包一个陈旧产物时，**全部检查照样通过**。
#
#   本周因此踩了 4 次（2026-10-07/ 08 / 09 ×2），症状极具误导性：
#     「源码里修复在、config.yml 里配置对、包里密钥也在，但运行行为就是旧的」
#   ⇒ 第一反应永远是怀疑「配置没读到 / 代码没生效 / 环境变量没传进去」，
#      每次都要绕一大圈才想到「二进制比代码旧」。
#
#   ⇒ 改成「先构建再打包」，并保留 SKIP_BUILD 逃生口
#     （离线出包/只改配置重出包时可用，但会在日志里显式标注）。
#
# ⚠️ 构建**故意放在前置检查之后、复制之前**：
#   前置检查（shellcheck / 端口单点定义）要先过，否则白等一次几分钟的构建。
SKIP_BUILD="${UVP_SKIP_BUILD:-0}"
if [ "$SKIP_BUILD" = "1" ]; then
  log "⚠️ 已跳过构建（UVP_SKIP_BUILD=1）—— **包里的产物可能比源码旧**，请自行确认"
else
  log "构建后端与升级助手二进制（${PKG_OS}/${PKG_ARCH}）"
  # CGO_ENABLED=0：SQLite 引擎是纯 Go（modernc.org/sqlite），不需要 libcgo。
  #   交叉编译必须关 CGO —— 它会去找本机（macOS）的 C 工具链，产物就废了。
  # -s -w：去符号表与调试信息，22MB vs 66MB，对交付体积差别很大。
  (
    cd "$SERVER_DIR" || exit 1
    # 优先用go.mod 里声明的 go 版本，避免本机 go 版本过低报"requires go >= x"
    GO_BIN="${GO:-go}"
    command -v "$GO_BIN" >/dev/null 2>&1 || fail "找不到 go 工具链（apt install golang-go，或用 UVP_SKIP_BUILD=1 跳过）"
    # ⛔ GOFLAGS=-mod=mod：默认 -mod=readonly 会因 vendor/ 或 go.sum 不一致而拒绝编译，
    #   而这类失败在打包脚本里极难定位。
    # ⛔ GOOS/GOARCH 取自 PKG_OS/PKG_ARCH（包名那段），保证「包名里的架构」
    #   与「真正编出来的架构」永远是同一个来源，不会被改漏。
    CGO_ENABLED=0 GOOS="$PKG_OS" GOARCH="$PKG_ARCH" GOFLAGS=-mod=mod \
      "$GO_BIN" build -ldflags="-s -w -X uvplatform.com/uvp-gb28181/app/global/app.BuildID=$BUILD_ID" -o bin/uvp-server .
    CGO_ENABLED=0 GOOS="$PKG_OS" GOARCH="$PKG_ARCH" GOFLAGS=-mod=mod \
      "$GO_BIN" build -ldflags="-s -w" -o bin/uvp-updater ./cmd/uvp-updater
    CGO_ENABLED=0 GOOS="$PKG_OS" GOARCH="$PKG_ARCH" GOFLAGS=-mod=mod \
      "$GO_BIN" build -ldflags="-s -w" -o bin/uvp-setup ./cmd/uvp-setup
    # 主机防火墙代理：安全页「内核级封禁」的执行者（nftables，需 root 运行）。
    # ⛔ 必须随包出：绿色包原来不带它，导致「国标接入安全」切保护模式直接报
    #   `dial unix .../firewall-agent.sock: no such file or directory`。
    CGO_ENABLED=0 GOOS="$PKG_OS" GOARCH="$PKG_ARCH" GOFLAGS=-mod=mod \
      "$GO_BIN" build -ldflags="-s -w" -o bin/uvp-firewall-agent ./cmd/uvp-firewall-agent
  ) || fail "后端构建失败（见上）。⚠️ 不要用旧的 bin/uvp-server 顶替——那就是本周反复踩的坑。"
  log "  后端二进制: $(du -h bin/uvp-server 2>/dev/null | cut -f1 || echo '?')"
  log "  升级助手二进制: $(du -h bin/uvp-updater 2>/dev/null | cut -f1 || echo '?')"

  # ⛔⛔ `--skip-frontend` 必须真的跳过前端构建。
  #   原实现只在后面的"前端产物存在性检查"（见 `if [ "$SKIP_FRONTEND" -eq 0 ]`）
  #   用了这个开关，**构建这一段根本没看它** ⇒ 传了 --skip-frontend 照样 `rm -rf dist`
  #   再 vite build 一遍，白等几分钟（2026-10-11 出 arm64 包时实测踩到，
  #   而且当时 dest 删除被安全护栏拦住，前端产物被删掉一半才失败，很费解）。
  # ⛔ 这里刻意**不重排块内缩进**：这个块有 40 行、含 SIP 端口注入的读回断言，
  #   为了对齐缩进动大段代码得不偿失，bash 也不在乎。
  if [ "$SKIP_FRONTEND" -eq 1 ]; then
  log "跳过前端构建（--skip-frontend）：复用已有 $WEB_DIR/dist"
  else
  log "构建前端产物（SIP 默认端口 ${SIP_PORT} 构建期注入）"
  (
    cd "$WEB_DIR" || exit 1
    # ⛔ rm -rf dist：vite 会**按内容哈希命名 chunk**，
    #   残留旧 hash 文件会让页面引用到旧产物 ⇒ 看起来像"改了没生效"。
    rm -rf dist
    # npm ci（有 lock 就用它保证版本一致）否则 npm install；
    # node_modules 缺失时先装—— 出包机可能是干净环境。
    if [ ! -d node_modules ]; then
      log "  node_modules 缺失，先安装依赖（首次较慢）"
      if [ -f package-lock.json ]; then npm ci --no-audit --no-fund
      else npm install --no-audit --no-fund; fi
    fi
    # ⛔⛔ 构建期注入 SIP 默认端口（原因见文件头 SIP_PORT 注释）。
    #   用 `.env.production.local`：Vite 只在 `.env.[mode]` 与 `.env.[mode].local`
    #   里保证读得到（`vite build` 的 mode 就是 production），而 `.local` 已被
    #   web/.gitignore 的 `*.local` 忽略 ⇒ 不会污染工作区。
    ENV_LOCAL="$WEB_DIR/.env.production.local"
    printf 'VITE_DEFAULT_SIP_PORT=%s\nVITE_BUILD_ID=%s\n' "$SIP_PORT" "$BUILD_ID" > "$ENV_LOCAL"
    # 构建失败/断言失败/正常结束一律清掉，别把这个临时文件留在工作区。
    trap 'rm -f "$ENV_LOCAL"' EXIT
    if [ -x node_modules/.bin/vite ]; then
      node_modules/.bin/vite build
    else
      npx --yes vite build
    fi
    # ⛔⛔ 读回断言：注入没生效必须**当场报错**。
    #   否则包照常出、向导预填的还是 5061，而"SIP 在段内"只存在于文档里，
    #   现场根本发现不了 —— 正是本项目反复踩的「静默不生效」那类坑。
    if ! grep -rq "${SIP_PORT}" dist 2>/dev/null; then
      echo "错误：前端产物里找不到注入的 SIP 默认端口 ${SIP_PORT} —— VITE_DEFAULT_SIP_PORT 未生效（变量名/vite 读 env 的方式有变）。" >&2
      exit 1
    fi
    # ⛔ 变量必须带花括号：写成 `$SIP_PORT（` 时 bash 会把全角括号的字节吞进变量名，
    #   报 `SIP_PORT（: unbound variable`（2026-10-10 实测：前端 3 分钟构建**成功**后
    #   才在最后一行日志炸掉，看起来像"前端构建失败"，其实构建是好的）。
    log "  已注入 SIP 默认端口 ${SIP_PORT}（产物内读回校验通过）"
  ) || fail "前端构建失败（见上）"
  fi   # ← 对应上面的 `if [ "$SKIP_FRONTEND" -eq 1 ]`
fi

[ -x "$SERVER_DIR/bin/uvp-server" ] || fail "缺少后端二进制：$SERVER_DIR/bin/uvp-server"
[ -x "$SERVER_DIR/bin/uvp-setup" ] || fail "缺少安装辅助程序：$SERVER_DIR/bin/uvp-setup"
[ -x "$SERVER_DIR/bin/uvp-updater" ] || fail "缺少升级助手二进制：$SERVER_DIR/bin/uvp-updater"
[ -x "$SERVER_DIR/bin/uvp-firewall-agent" ] || fail "缺少主机防火墙代理：$SERVER_DIR/bin/uvp-firewall-agent"
[ -x "$DEPLOY_DIR/bin/redis-server" ] || fail "缺少 redis-server：$DEPLOY_DIR/bin/redis-server（先跑 deploy/standalone/build-redis.sh）"
[ -x "$DEPLOY_DIR/bin/redis-cli" ]    || fail "缺少 redis-cli：$DEPLOY_DIR/bin/redis-cli（先跑 deploy/standalone/build-redis.sh）"
[ -f "$SQLITE_DIR/baseline.sql" ]     || fail "缺少 SQLite 基线：$SQLITE_DIR/baseline.sql（先跑 generate.py）"

# ZLM（平台靠它推流/取流/录像，缺了整套功能跑不起来）
[ -x "$ZLM_DIR/MediaServer" ] || fail "缺少 ZLM：$ZLM_DIR/MediaServer（先跑 deploy/standalone/fetch-zlm.sh）"
[ -f "$ZLM_DIR/config.ini" ]   || fail "缺少 ZLM 配置：$ZLM_DIR/config.ini"
# ffmpeg（ZLM 通道抓拍的执行器）——2026-10-20 客户现场实锤后强制内置：
# 包里不带它、config.ini 指着 /usr/bin/ffmpeg 时，目标机没装 ⇒ 抓拍静默不可用。
[ -x "$DEPLOY_DIR/bin/ffmpeg" ] || fail "缺少内置 ffmpeg：$DEPLOY_DIR/bin/ffmpeg（先跑 deploy/standalone/build-ffmpeg.sh）"
# ⛔ 必须校验依赖闭包完整：MediaServer 缺库时报的是
#   `error while loading shared libraries: libavfilter.so.9`，
#   而那是**启动瞬间**的事 —— 装完的客户机上表现为「服务起了但推流全失败」。
[ -d "$ZLM_DIR/lib" ]         || fail "缺少 ZLM 运行时库目录：$ZLM_DIR/lib（见 deploy/standalone/README）"

# nginx（前端 + HTTPS）。⛔ 它的 OpenSSL 符号版本必须匹配目标机 ——
#   从镜像里直接抠出来的 nginx 要 OPENSSL_3.2/3.5，而目标机只有 3.0，
#   拷过去会报 "version `OPENSSL_3.5.0' not found" 且**nginx 连启动都做不到**。
#   ⇒ 必须按目标机环境自行编译（build-nginx.sh），不引入新的运行时依赖。
[ -x "$NGINX_DIR/sbin/nginx" ] || fail "缺少 nginx：$NGINX_DIR/sbin/nginx（先跑 deploy/standalone/build-nginx.sh）"
[ -f "$SERVER_DIR/version.json" ]     || fail "缺少 version.json"
# ⛔ 源配置必须用 config.example.yml：config.yml 因含数据库凭据被 .gitignore 排除，
#   任何从 git clone 下来的构建机上都不存在它（实测首次出包就撞到这个）。
#   用 example 版还有一层好处 —— 打包结果不依赖某个开发者的本地配置，
#   仓库里的 example 版是唯一真源。
[ -f "$SERVER_DIR/config/config.example.yml" ] || fail "缺少 config.example.yml"

if [ "$SKIP_FRONTEND" -eq 0 ]; then
  [ -f "$WEB_DIR/dist/index.html" ] || fail "缺少前端产物：$WEB_DIR/dist/index.html（先跑 vite build）"
else
  # ⛔ --skip-frontend 复用已有 dist ⇒ 无法保证 SIP 默认端口已注入。
  #   这不会让包坏掉（引导页仍可手工填端口），但会让"SIP 在 51064"只停留在文档上 ——
  #   提示一声，别让现场以为向导预填的就是规划值。
  if ! grep -rq "$SIP_PORT" "$WEB_DIR/dist" 2>/dev/null; then
    log "⚠️ 复用已有前端产物（--skip-frontend）：产物里看不到 SIP 默认端口 ${SIP_PORT}，"
    log "   引导页会预填 5061（仍可手工改成 ${SIP_PORT}）；要预填就在不带 --skip-frontend 时构建。"
  fi
fi

"$NODE_BIN" "$REPO_ROOT/scripts/version.mjs" check-frontend-build-id "$BUILD_ID" "$WEB_DIR/dist"
log "  前端构建身份 ${BUILD_ID} 读回校验通过"

# 静态校验：二进制必须是**目标架构**（PKG_ARCH，默认 linux/amd64）。用 file 判，别只看文件名。
# ⛔ 判据从 PKG_ARCH 反推，与上面那行交叉编译同源 —— 否则会做出
#   「包名写着 arm64、里面装的还是 x86_64」这种包内外不一致、且全程零报错的包。
case "$PKG_ARCH" in
  amd64) ELF_ARCH_RE='x86-64|x86_64' ;;
  arm64) ELF_ARCH_RE='aarch64|ARM aarch64' ;;
  *) fail "不支持的架构 PKG_ARCH=$PKG_ARCH（目前只支持 amd64 / arm64）" ;;
esac
arch_of() { file "$1" | grep -oE "$ELF_ARCH_RE" | head -1; }
[ -n "$(arch_of "$SERVER_DIR/bin/uvp-server")" ] || fail "后端二进制不是 $PKG_ARCH"
[ -n "$(arch_of "$SERVER_DIR/bin/uvp-setup")" ] || fail "安装辅助程序不是 $PKG_ARCH"
[ -n "$(arch_of "$SERVER_DIR/bin/uvp-updater")" ] || fail "升级助手二进制不是 $PKG_ARCH"
[ -n "$(arch_of "$SERVER_DIR/bin/uvp-firewall-agent")" ] || fail "主机防火墙代理不是 $PKG_ARCH"
[ -n "$(arch_of "$DEPLOY_DIR/bin/redis-server")" ] || fail "redis-server 不是 $PKG_ARCH"
[ -n "$(arch_of "$ZLM_DIR/MediaServer")" ]|| fail "MediaServer 不是 $PKG_ARCH"
[ -n "$(arch_of "$DEPLOY_DIR/bin/ffmpeg")" ]|| fail "内置 ffmpeg 不是 $PKG_ARCH"
# nginx 也必须一起查：它是最容易"忘了换"的那个（ZLM/redis 有明显的 lib 目录提示，
# nginx 只是 bin/nginx 里一个文件，装到目标机才会以 `Exec format error` 暴露）。
[ -n "$(arch_of "$NGINX_DIR/sbin/nginx")" ]|| fail "nginx 不是 $PKG_ARCH"

# ------------------------------------------------------------------ 组装 ----

STAGE="$(mktemp -d "${TMPDIR:-/tmp}/uvp-standalone.XXXXXX")"
cleanup() { rm -rf "$STAGE"; }
trap cleanup EXIT

PKG="$STAGE/${PKG_STEM}"
RELEASE_METADATA_DIR="$STAGE/release-metadata"
"$NODE_BIN" "$REPO_ROOT/scripts/version.mjs" release-metadata "$BUILD_ID" "$RELEASE_METADATA_DIR"
# ⛔ 包内布局（2026-10-10 整理，与 uvp-gb28181-ctl.sh 顶部注释必须一致）：
#   bin/    只放当前版本的后端程序（uvp-server）
#   updater/ 独立升级助手，固定在安装根，不能随 current 切换被替换
#   vendor/ 第三方运行时，每个组件自成一体（自带 conf/lib/www）
#   config/ data/ logs/ run/ resource/
#   ⛔ 顶层只允许 4 个文件：uvp-gb28181-ctl.sh（唯一入口）、
#      uvp-gb28181-service-install.sh / -uninstall.sh（可选的服务注册）、version.json。
#      再往顶层塞东西先问一句「它是不是应该进 config/ 或 data/」。
# ⛔ 不再建空的 scripts/ —— 历史上它从没被填过任何东西。
mkdir -p "$PKG"/{bin,updater,config,data/redis,logs,run}
mkdir -p "$PKG"/vendor/{redis,nginx,zlm}

log "复制后端二进制"
cp "$SERVER_DIR/bin/uvp-server" "$PKG/bin/uvp-server"
cp "$SERVER_DIR/bin/uvp-setup" "$PKG/bin/uvp-setup"
# 主机防火墙代理：特权进程，只由 ctl.sh（root）/ systemd 启动，不随后端进程树走。
cp "$SERVER_DIR/bin/uvp-firewall-agent" "$PKG/bin/uvp-firewall-agent"
chmod 0755 "$PKG/bin/uvp-setup" "$PKG/bin/uvp-firewall-agent"
cp "$SERVER_DIR/bin/uvp-updater" "$PKG/updater/uvp-updater"
chmod 0755 "$PKG/bin/uvp-server" "$PKG/updater/uvp-updater"

# ---- 产物新鲜度断言（双保险，与上面的自动构建互为兜底）----
#
# ⛔ 为什么还要这一条：自动构建可能被 SKIP_BUILD=1 跳过（离线出包/只改配置重出包），
#   也可能有人手动 cp 了一个旧二进制进 server/bin/。
#   ⇒ 无论走哪条路，这里都比一次 mtime：**产物比源码旧 = 拒绝出包**。
#
# ⛔⛔ 第一版把它包在 `if SKIP_BUILD != 1` 里，**逻辑写反了**：
#   SKIP_BUILD=1 恰恰是「产物可能没重建」的场景，却正好跳过了检查 ——
#   实测「touch 成10-01 + SKIP_BUILD=1」照样出包成功。
#   ⇒ **断言必须无条件执行**；跳过构建时它就是唯一的守门人。
#
# ⭐ 判据用「产物 mtime 是否早于**最新改动的源码**」，而不是「早于脚本运行时间」——
#   后者在刚 build 完时会误报（同秒/时钟精度问题），而且抓不到"构建了但漏编了某文件"。
assert_fresh() {
  local artifact="$1" label="$2" newest
  [ -e "$artifact" ] || fail "$label 不存在：$artifact"
  # 源码目录里最新的 .go / .vue / .ts / .scss 文件。
  #
  # ⛔⛔ 必须排除**构建时自动生成的声明文件**（实测踩过）：
  #   web/src/auto-import.d.ts 由 vite 插件在**每次构建时重写**，
  #   而后端二进制是在前端构建**之前**编译的（时间戳必然更早）
  #   ⇒ 拿它当判据 ⇒ **每次出包都误报「二进制比源码旧」**，直接卡死流程。
  #   ⚠️ 它确实被 git 跟踪（所以不能用「未跟踪」筛），只能按文件名排除。
  #
  # 判据要挑「人写的」文件：.d.ts 是类型声明、由工具生成；.vue/.go/.scss 才是。
  #
  # ⛔⛔ `_test.go` 也必须排除：测试文件**不编进二进制**（go build 忽略它们），
  #   但 git 操作（lint-staged / checkout / stash pop）会刷新它们的 mtime。
  #   ⇒ 只要动过任何测试文件，二进制就"比源码旧"，而重编一次并不能让它更新
  #   （build 不看 .go_test），于是**出包流程被永久卡死**。
  #   实测踩过：提交后 lint-staged 刷新了 config_service_test.go 的 mtime，
  #   后续出包一律报"比源码旧"，重编也无效。
  newest="$(find "$SERVER_DIR" "$WEB_DIR/src" -type f \
    \( -name '*.go' -o -name '*.vue' -o -name '*.ts' -o -name '*.scss' \) \
    ! -name '*.d.ts' \
    ! -name '*_test.go' \
    ! -name '*.test.ts' \
    ! -name '*.spec.ts' \
    -newer "$artifact" -print 2>/dev/null | head -1)"
  if [ -n "$newest" ]; then
    fail "$label 比源码旧，拒绝出包（否则会打出一个「看起来全新、跑起来是旧的」包）。
     产物: $artifact
     更新的源码: ${newest#$REPO_ROOT/}
     ⛔ 这就是本周反复踩的坑：所有出包断言都过，但包里跑的是旧代码。
     解决：删掉产物重新构建（或去掉 UVP_SKIP_BUILD 让脚本自动构建）。"
  fi
  log "  $label 新鲜度 OK"
}
assert_fresh "$SERVER_DIR/bin/uvp-server" "后端二进制"
assert_fresh "$SERVER_DIR/bin/uvp-setup" "安装辅助程序"
assert_fresh "$SERVER_DIR/bin/uvp-updater" "升级助手二进制"
assert_fresh "$SERVER_DIR/bin/uvp-firewall-agent" "主机防火墙代理"
if [ -f "$WEB_DIR/dist/index.html" ]; then
  assert_fresh "$WEB_DIR/dist/index.html" "前端产物"
else
  # 没产物时上面的自动构建应该已经建好了；真没有就报清楚
  fail "前端产物不存在：$WEB_DIR/dist/index.html"
fi

log "复制 Redis（自带，包内跑，不连外部）"
cp "$DEPLOY_DIR/bin/redis-server" "$DEPLOY_DIR/bin/redis-cli" "$PKG/vendor/redis/"
chmod 0755 "$PKG/vendor/redis/redis-server" "$PKG/vendor/redis/redis-cli"

log "复制二开 ZLM（含 ffmpeg 运行时库）"
mkdir -p "$PKG/vendor/zlm"
# ⛔ 必须连 lib/ 一起拷且保持相对位置：MediaServer 是动态链接的，
#   靠 LD_LIBRARY_PATH=$ROOT/vendor/zlm/lib 找那些 .so，缺一个就起不来。
cp -a "$ZLM_DIR/MediaServer" "$PKG/vendor/zlm/"
cp -a "$ZLM_DIR/lib" "$PKG/vendor/zlm/lib"
cp -a "$ZLM_DIR/www" "$PKG/vendor/zlm/www"
cp "$ZLM_DIR/config.ini" "$PKG/vendor/zlm/config.ini"
[ -f "$ZLM_DIR/default.pem" ] && cp "$ZLM_DIR/default.pem" "$PKG/vendor/zlm/default.pem"
[ -f "$ZLM_DIR/zlm-buildinfo.txt" ] && cp "$ZLM_DIR/zlm-buildinfo.txt" "$PKG/vendor/zlm/"
chmod 0755 "$PKG/vendor/zlm/MediaServer"

# ⛔ 不打包 swagger 的 sourcemap（www 下 5 份 .map 共 5.9MB，纯调试元数据）。
#   2026-10-22 起 amd64 包被它们顶到 101.6MiB，越过 Gitee 单附件 100MiB（104857600 字节）上限
#   ⇒ 只删 .map，swagger 页面本体（js/css/openapi.json）照常保留。
find "$PKG/vendor/zlm/www" -type f -name '*.map' -delete 2>/dev/null || true

# ---- ffmpeg 随包内置（2026-10-20 起，老板拍板：不再让用户手动安装）----
# ⛔⛔ 客户现场实锤（openEuler 22.03）：包里不带 ffmpeg、config.ini 指着
#   /usr/bin/ffmpeg 时，ZLM 的 getSnap 每次都 fork/exec 失败（execv -2）、
#   返回 1024x512 占位 PNG，平台侧只报「未返回 JPEG」——通道抓拍从部署
#   当天起静默不可用，零告警。
# ⛔ 放 vendor/zlm/bin/ 而不是 vendor/zlm/ 根下：根下已有 ZLM 的 ffmpeg
#   日志目录（[ffmpeg] log=./ffmpeg/ffmpeg.log），同名文件与目录无法共存。
log "内置 ffmpeg（ZLM 通道抓拍依赖）"
mkdir -p "$PKG/vendor/zlm/bin"
cp "$DEPLOY_DIR/bin/ffmpeg" "$PKG/vendor/zlm/bin/ffmpeg"
chmod 0755 "$PKG/vendor/zlm/bin/ffmpeg"

# ---- ZLM 的身份也固定（源头侧）----
# ⛔⛔ 必须改**包内这份** config.ini，而不是靠启动时对齐：
#   `mediaServerId` 的默认值是占位符 `your_server_id`，
#   而 ZLM 的 setServerConfig 会把平台下发的值**持久化回这个文件**
#   （见 server/app/gb28181/zlm/apply.go）—— 也就是它一旦落盘就固化。
#   所以这里出包时就写好，启动时ZLM 读到的第一眼就是正确值。
#   secret 同理：它必须与 config.yml 里的**逐字相同**，否则平台调 ZLM 全被拒。
log "固定 ZLM 的secret 与节点标识，并关闭目录浏览"
"$PY" - "$PKG/vendor/zlm/config.ini" "$ZLM_SECRET" "$ZLM_MEDIA_SERVER_ID" <<'ZLM_INI_ID_PY'
import re
import sys

path, secret, msid = sys.argv[1:4]
# ⛔ 读写都用 newline="" + split("\n")：这份 ini 是 **CRLF**，
#   用 splitlines() 会吃掉 \r，写回时行尾就变了（下次判据又对不上）。
lines = open(path, encoding="utf-8", newline="").read().split("\n")

section = None
changed = []
http_header = -1
has_dirmenu = False
for i, line in enumerate(lines):
    t = line.strip()
    if t.startswith("[") and t.endswith("]"):
        section = t[1:-1].strip().lower()
        if section == "http":
            http_header = i
        continue
    m = re.match(r"^(\s*)([A-Za-z_]+)(\s*=\s*)(.*?)(\r?)$", line)
    if not m:
        continue
    indent, key, eq, old, cr = m.groups()
    # 只认三处：[api] 的 secret、[general] 的 mediaServerId、[ffmpeg] 的 bin，
    # 别处出现的同名键不能动（这份 ini 有 11 个段）。
    if section == "api" and key == "secret" and old.strip() != secret:
        changed.append("[api] secret: " + old.strip() + " -> (固定值)")
        lines[i] = indent + key + eq + secret + cr
    elif section == "general" and key.lower() == "mediaserverid" and old.strip() != msid:
        changed.append("[general] mediaServerId: " + old.strip() + " -> " + msid)
        lines[i] = indent + "mediaServerId=" + msid + cr
    # 2026-10-20 起 ffmpeg 随包内置：bin 必须指向包内 vendor/zlm/bin/ffmpeg。
    # 相对路径 ./bin/ffmpeg 由 ZLM 按 exeDir（= vendor/zlm）解析，
    # ctl.sh 启动时也会 cd 到同一目录，两种解析口径落到同一个文件。
    elif section == "ffmpeg" and key == "bin" and old.strip() != "./bin/ffmpeg":
        changed.append("[ffmpeg] bin: " + old.strip() + " -> ./bin/ffmpeg（包内内置）")
        lines[i] = indent + key + eq + "./bin/ffmpeg" + cr
    # ⛔⛔ [http] dirMenu 必须出厂为 0：开着时 ZLM 的 HTTP 口把 www 目录整个列成
    #   网页，而 www 正是录像/截图/HLS 的落盘目录 ⇒ 同网段任何人不用登录平台
    #   就能把全部历史录像翻一遍（allow_ip_range 出厂就放行 10/172/192）。
    #   ⛔ 这条同时也由 uvp-setup ports-sync 在每次启动时兜一次（存量包升级后靠它），
    #     这里做的是「**随包发出去的这份文件**本身就是对的」——两者缺一不可：
    #     只靠启动期，客户手里那份包仍是 1；只靠出包期，老现场永远不修。
    elif section == "http" and key == "dirMenu":
        has_dirmenu = True
        if old.strip() != "0":
            changed.append("[http] dirMenu: " + old.strip() + " -> 0（关闭目录浏览）")
            lines[i] = indent + key + eq + "0" + cr

# 键不存在时补一行（ZLM 缺键本身即关闭，但显式写出来现场才看得懂、才能核对）。
if not has_dirmenu and http_header >= 0:
    lines.insert(http_header + 1, "dirMenu=0")
    changed.append("[http] dirMenu: (缺) -> 0（关闭目录浏览）")

open(path, "w", encoding="utf-8", newline="").write("\n".join(lines))
print("   ZLM config.ini: " + ("; ".join(changed) if changed else "已是固定值，无需改动"))
ZLM_INI_ID_PY
# ⛔ 对**结果**再断言一次：上面那段 python 一旦段名判据没匹配上就静默跳过
#   （写回的是原串，照样打印"无需改动"）。而漏洞只在客户机上暴露 ——
#   www 目录（录像/截图/HLS）会被 ZLM 的 HTTP 口按目录列给同网段任何人。
if ! grep -q '^dirMenu=0' "$PKG/vendor/zlm/config.ini"; then
  fail "包内 ZLM config.ini 的 [http] dirMenu 不是 0 —— 目录浏览会把全部录像暴露出去"
fi
log "  [http] dirMenu=0（关闭目录浏览：www 里有录像，不能按目录列出去）"

log "复制 nginx（前端 + HTTPS 终止）"
mkdir -p "$PKG/vendor/nginx"
cp -a "$NGINX_DIR/." "$PKG/vendor/nginx/"
chmod 0755 "$PKG/vendor/nginx/sbin/nginx"
# nginx.conf is a rendered runtime file and may contain the build machine's
# absolute install path. Ship only the template so each installation renders
# paths for its own root on first start.
rm -f "$PKG/vendor/nginx/conf/nginx.conf"
rm -f "$PKG/vendor/nginx/conf/uvp.crt" "$PKG/vendor/nginx/conf/uvp.key"

# ---- nginx 配置模板：以仓库版本为唯一真源 ----
#
# ⛔⛔ 历史坑（2026-10-10 实测踩到，直接导致升级后 nginx 起不来）：
#   `deploy/standalone/bin/nginx/conf/` 下还有一份**同名模板副本**，而
#   `bin/` 整体被 `deploy/standalone/.gitignore` 忽略 ⇒ 那份副本
#   **不入版本管理、没人维护**：改了仓库里的模板，它纹丝不动，
#   而打进包的恰恰是它。实测症状：升级后 nginx 启动失败，报
#     `open() ".../bin/nginx/conf/mime.types" failed`
#   ——旧副本里 include 的仍是 bin/nginx 路径，而旧布局迁移已把 bin/nginx 删了。
#   ⇒ ① 出包时强制用仓库版本覆盖；② 断言包内模板不再出现 bin/nginx。
cp "$DEPLOY_DIR/nginx.conf.template" "$PKG/vendor/nginx/conf/nginx.conf.template"
if [ -e "$PKG/vendor/nginx/conf/nginx.conf" ]; then
  fail "包内不能包含构建机路径的 nginx.conf；目标机必须从 nginx.conf.template 渲染"
fi
if grep -q '@ROOT@/bin/nginx' "$PKG/vendor/nginx/conf/nginx.conf.template"; then
  fail "nginx 配置模板里仍是旧路径（@ROOT@/bin/nginx）——仓库模板没覆盖进去"
fi
log "  nginx 模板已用仓库版本覆盖（唯一真源：deploy/standalone/nginx.conf.template）"

log "复制前端产物"
if [ -f "$WEB_DIR/dist/index.html" ]; then
  mkdir -p "$PKG/resource/public"
  cp -a "$WEB_DIR/dist/." "$PKG/resource/public/"
fi

log "复制后端资源（SQLite 建库脚本 + 静态资源）"
cp -a "$SERVER_DIR/resource/database/sqlitebaseline" "$PKG/resource/baseline"
[ -d "$SERVER_DIR/resource/public" ] && cp -a "$SERVER_DIR/resource/public/." "$PKG/resource/public/" || true

# ⛔⛔ 包内不再保留 EasyPlayer 的**明文副本**：vite 插件（web/build/easyplayer-assets.ts）
#   会把同一份资源再按内容哈希放一份到 easyplayer/<16hex>/，且把 index.html 的 <script>
#   改写成哈希路径。明文那 4 个文件（pro.js / pro.wasm / decode.js / lib.js，共 6.3MB）
#   在包里是**逐字节重复**。2026-10-22 因它们把 Windows 包顶到 106.0MB、越过 Gitee
#   单附件 100MiB（104857600 字节）上限而剔除。
#   ⛔ 只删 easyplayer/ 下 maxdepth 1 的明文副本，哈希目录必须留着；
#   ⛔ 改写没生效时**一个字都不删**（宁可包大，也不能让播放器 404）。
if grep -qE 'src="/easyplayer/[0-9a-f]{16}/EasyPlayer-pro\.js"' "$PKG/resource/public/index.html" 2>/dev/null; then
  find "$PKG/resource/public/easyplayer" -maxdepth 1 -type f -delete 2>/dev/null || true
  log "  已剔除 EasyPlayer 明文副本（index.html 走哈希路径，实测省 6.3MB）"
else
  log "  注意：index.html 未指向 EasyPlayer 哈希目录，保留明文副本（包会更大，播放不受影响）"
fi

# ---- 内置自签证书（安装时不再现场生成）----
# ⛔⛔ 为什么要随包带：客户机（CentOS 7 等）不保证有 openssl，而"安装时生成"
#   等于在交付链路上多加一个会失败、且失败了只写在日志里的步骤。
# ⛔ 放在 resource/cert/ 而**不是** vendor/nginx/conf/：升级是原地 tar 覆盖，
#   而 vendor/nginx/conf 会被整目录替换 —— 客户上传的真证书会被内置证书盖掉。
#   ctl 只在目标文件缺失时才拷贝（见 ensure_self_signed_cert）。
# ⛔ 有效期见 assets/cert 的生成说明：起点刻意写在过去，现场机器时间不准也不会被判失效。
CERT_SRC="$DEPLOY_DIR/assets/cert"
[ -f "$CERT_SRC/uvp.crt" ] && [ -f "$CERT_SRC/uvp.key" ] \
  || fail "缺少内置证书 ${CERT_SRC}/uvp.{crt,key} —— 出包资产不全"
mkdir -p "$PKG/resource/cert"
install -m 0644 "$CERT_SRC/uvp.crt" "$PKG/resource/cert/uvp.crt"
install -m 0600 "$CERT_SRC/uvp.key" "$PKG/resource/cert/uvp.key"
log "  内置证书已随包（resource/cert/，安装时直接拷贝，不调用 openssl）"

# ---- IP 归属地数据库（可选资源，缺失时运行时自动走远程或显示未知）----
if [ -d "$SERVER_DIR/resource/ipdb" ]; then
  mkdir -p "$PKG/resource/ipdb"
  cp -a "$SERVER_DIR/resource/ipdb/." "$PKG/resource/ipdb/"
  log "  ip2region 数据库已随包（resource/ipdb/）"
fi

# ---- admin 默认头像（种子资源）----
# ⛔⛔ 为什么不能直接用 server/resource/public/uploads：那个目录被 gitignore
#   （server/.gitignore 的 `/resource/public/uploads`）——它是**运行时产物**。
#   构建机从 git 拉代码 ⇒ 那里是空的 ⇒ 头像文件压根进不了包。
#   实测症状：URL 存在但文件不存在，nginx 的 SPA 回落把 index.html 返回给了
#   图片请求 ⇒ HTTP 200 而内容是 HTML，浏览器显示不出图。
#
#⇒ 走server/resource/seed-assets/（已加 gitignore 例外、随源码入库），
#   出包时落到 resource/public/uploads/seed/ —— **不带日期**，
#   这样路径稳定、不会被用户上传的头像覆盖，也不与 uploads/<日期>/ 混。
AVATAR_SEED_SRC="$SERVER_DIR/resource/seed-assets/avatar"
# ⛔ 这里曾有个死变量 AVATAR_SEED_URL —— 定义后从未使用（URL 是写在
#   sys_users 种子里的，两边靠"约定的同一个路径"对齐，不是靠这个变量传递）。
#   它会触发 shellcheck SC2034，而出包门禁是「有输出就fail」⇒ 直接卡住出包。
if [ -f "$AVATAR_SEED_SRC/admin.png" ]; then
  mkdir -p "$PKG/resource/public/uploads/seed"
  cp "$AVATAR_SEED_SRC/admin.png" "$PKG/resource/public/uploads/seed/admin.png"
  log "  admin 头像已随包（$(basename "$AVATAR_SEED_SRC/admin.png") → resource/public/uploads/seed/）"
else
  log "  ⚠️ 未找到 $AVATAR_SEED_SRC/admin.png，admin 将无默认头像（不阻断出包）"
fi

install -m 0644 "$RELEASE_METADATA_DIR/server/version.json" "$PKG/version.json"

# ⛔ 出包前断言：**库里有头像路径 ⇒ 包里必须有那个文件**。
#   这类"数据库说有、磁盘上没有"的组合最坑：nginx 的 SPA 回落会把
#   index.html 返回给图片请求 ⇒ HTTP 200 而内容是 HTML，
#   浏览器静默显示不出图，**没有任何一行报错**（实测踩过）。
log "校验头像资源与数据库一致"
"$PY" - "$PKG/config/config.yml" "$PKG/resource/baseline/baseline.sql" "$PKG/resource/public" <<'AVATAR_CHECK_PY'
import os
import re
import sys

# ⛔ 不用 pathlib：内嵌 Python 要在 python3.5 上也能跑（目标机 Ubuntu 16 自带 3.5），
#   而 3.5 的 C 层函数（sqlite3.connect / os.remove / 内置 open…）**不认 Path 对象**
#   —— 传进去就是 `TypeError: argument 1 must be str, not PosixPath`（path-like 是 3.6 才有）。
#   契约锁在 scripts/standalone-py35-compat.test.mjs（"内嵌 Python 不得用 pathlib"）。
cfg, baseline, public = sys.argv[1], sys.argv[2], sys.argv[3]

# 从基线 SQL 里取 sys_users 的 avatar 值（不连库，出包阶段还没有库）
sql = open(baseline, encoding="utf-8", errors="replace").read()
m = re.search(r"INSERT INTO [\"`]?sys_users[\"`]?\s*\([^)]*\)\s*VALUES(.*?);", sql, re.S)
urls = re.findall(r"'(/public/uploads/[^']+)'", m.group(1)) if m else []

problems = []
for url in urls:
    rel = url.lstrip("/").split("public/", 1)[-1]      # /public/uploads/x → uploads/x
    if not os.path.isfile(os.path.join(public, rel)):
        problems.append("库里有 " + url + "，但包内 resource/public/" + rel + " 不存在")
if problems:
    sys.exit("❌ 头像资源与数据库不一致：\n  - " + "\n  - ".join(problems)
             + "\n   这个组合的现场症状是「HTTP 200 但图片出不来」——"
               "nginx 把 index.html 返回给了图片请求，全程无报错。")
print("✅ 头像资源与数据库一致（%d 个引用）" % len(urls))
AVATAR_CHECK_PY

log "生成生产配置（SQLite + 本机 Redis）"
$PY \
  "$DEPLOY_DIR/make-config.py" \
  --source "$SERVER_DIR/config/config.example.yml" \
  --target "$PKG/config/config.yml" \
  --http-port "$HTTP_PORT" \
  --redis-port "$REDIS_PORT" \
  --db-path "./data/uvp.db" \
  --log-path "./logs/app/uvp-gb28181.log" \
  --zlm-secret "$ZLM_SECRET" \
  --zlm-media-server-id "$ZLM_MEDIA_SERVER_ID" \
  --openapi-enabled "${UVP_OPENAPI_ENABLED:-true}" \
  --openapi-master-key-id "${UVP_OPENAPI_MASTER_KEY_ID:-uvp-openapi-k1}" \
  --upgrade-trusted-keys "${UVP_UPGRADE_TRUSTED_KEYS:-}"
# ⛔⛔ 这里**不要**引用后面才定义的 $OPENAPI_MASTER_KEY_ENV / $SIP_TRACE_KEY_ENV：
#   本函数（生成 config.yml）在它们定义之前就调用了 ⇒ 展开成空串 ⇒
#   make-config.py 收到空值且**不报错**。（本项目已栽过一次同型坑：
#   quality-gate.sh 引用了从没定义过的 $PY_BIN。）

# ⛔ 出包前**解析后断言**这两个固定值真的进去了。
#   理由：make-config.py 用的是"段落内替换"，判据（段名/键名/缩进/行尾注释）
#   任一不匹配都会**静默不替换**，而它仍照常打印"ZLM secret / 节点标识已固定"。
#   ⇒ 查"字符串出现过"没意义，必须 yaml.safe_load 之后断言取值。
log "校验 ZLM 身份配置"
"$PY" - "$PKG/config/config.yml" "$ZLM_SECRET" "$ZLM_MEDIA_SERVER_ID" <<'ZLM_ID_CHECK_PY'
import sys

import yaml

path, want_secret, want_uuid = sys.argv[1:4]
zlm = (yaml.safe_load(open(path, encoding="utf-8")) or {}).get("gb28181", {}).get("zlm", {})
problems = []
if zlm.get("secret") != want_secret:
    problems.append("secret=%r（应为 %r）" % (zlm.get("secret"), want_secret))
if zlm.get("mediaserverid") != want_uuid:
    problems.append("mediaserverid=%r（应为 %r）" % (zlm.get("mediaserverid"), want_uuid))
if problems:
    sys.exit("❌ ZLM 身份配置未写进 config.yml：" + "；".join(problems)
             + "。\n   出了这个包，ZLM 侧与平台侧就对不上——现场表现为"
               "「录像缓存服务未装配」等各功能报未装配，且 ZLM 进程本身看起来完全正常。")
print("✅ ZLM 身份配置已固定（secret %d 字符 / 节点标识 %d 字符）"
      % (len(want_secret), len(want_uuid)))
ZLM_ID_CHECK_PY

log "复制启停脚本"
cp "$DEPLOY_DIR/uvp-gb28181-ctl.sh" "$PKG/uvp-gb28181-ctl.sh"
chmod 0755 "$PKG/uvp-gb28181-ctl.sh"

# ---- systemd 服务注册脚本（可选：把本包装成开机自启的系统服务）----
# ⛔ 它们是**可选**工具：绿色包的核心契约仍是「解压 + ./uvp-gb28181-ctl.sh start」，
#   不装服务也能完整使用 ⇒ 不进 ctl、不参与迁移逻辑。
# ⛔ 必须随包分发：漏了之后现场跑 ./uvp-gb28181-service-install.sh 会报「找不到文件」，
#   用户只会当成包坏了 —— 「脚本没进包」这类坑本项目已经踩过一次（nginx 模板那份副本）。
for svc_script in uvp-gb28181-service-install.sh uvp-gb28181-service-uninstall.sh; do
  [ -f "$DEPLOY_DIR/${svc_script}" ] || { printf '[build][ERROR] 缺少 %s\n' "${svc_script}" >&2; exit 1; }
  cp "$DEPLOY_DIR/${svc_script}" "$PKG/${svc_script}"
  chmod 0755 "$PKG/${svc_script}"
done
log "已打包服务注册脚本（install / uninstall）"
# ⛔ 不再分发独立的 init-database.sh —— 建库已内置进 `uvp-gb28181-ctl.sh start`。
#   绿色包的契约是「解压即用」：用户装完只做"执行 start"这一件事。
#   多一条"记得先建库"的步骤就会有一批人漏掉，而漏掉的症状极具误导性
#   （页面能开、登录页报 no such table），用户联想不到是因为少跑了一个脚本。
[ -f "$DEPLOY_DIR/README.md" ] && cp "$DEPLOY_DIR/README.md" "$PKG/README.md"

# ---- SIP 报文诊断加密密钥（每份包一份随机）----
#
# ⛔⛔⛔ 为什么必须在**出包时**生成、而不是让客户自己填（老板 2026-10-09 定）：
#   报文诊断的加密密钥缺失时，后端**fail-closed 不落库**（安全，但功能彻底不可用），
#   而health 只报 `invalid SIP trace encryption key`，**不说该怎么修**
#   ⇒ 现场看到的是一个「菜单能进、列表永远空」的页面，
#   没有任何线索指向「你得去 config.env 里加一行」。
#   实测2026-10-09 就是这样：功能默认不可用，老板直接看到的。
#
#   安全取舍：密钥明文随包分发（与初始口令同级）。
#   但报文里的密码字段本就依赖它加密落库 —— 能读到包的人本来就能读到初始口令，
#   所以「不自动生成」换来的安全收益很有限，而「装完不可用」的代价是确定的。
#   ⇒ 客户想更严可以自行换成自己的 32 字节值（见下面注释里的生成命令）。
#
# ⚠️ 换密钥后**历史密文解不开**，需要能接受丢历史再换。
# ⚠️ 用 secrets 而不是 openssl rand：后者在本机/目标机的可用性不确定，
#    而这里本来就依赖 python3（脚本里已在用它生成配置）。
SIP_TRACE_KEY_ENV="UVP_SIP_TRACE_ENCRYPTION_KEY"
SIP_TRACE_KEY="$("$PY" -c 'import base64,secrets;print(base64.b64encode(secrets.token_bytes(32)).decode())')"
if [ -z "$SIP_TRACE_KEY" ]; then
  fail "生成 SIP 报文诊断密钥失败（python3 不可用？）"
fi
# ---- OpenAPI 主密钥（同样只走环境变量，不进配置文件）----
#
# ⛔ 后端装配 OpenAPI 有五道前置检查（app/openapi/routes/runtime.go），
#   任何一道不过就统一返回 ErrUnavailable ⇒ 页面报 503 且**不说是哪一道**。
#   其中一道就是主密钥：`base64.RawURLEncoding.Strict().DecodeString(os.Getenv(...))`
#   要求 **32 字节的无填充 base64url** ⇒ 用标准 base64 会因 `+` `/` `=` 被拒。
#   ⛔ `secrets.token_urlsafe(32)` 产出的正是无填充 base64url，天然合规；
#      而 `base64.b64encode`（SIP 密钥用的那个）**带填充**，这里不能用它。
OPENAPI_MASTER_KEY_ENV="UVP_OPENAPI_MASTER_KEY"
OPENAPI_MASTER_KEY="$("$PY" -c 'import secrets;print(secrets.token_urlsafe(32))')"
if [ -z "$OPENAPI_MASTER_KEY" ]; then
  fail "生成 OpenAPI 主密钥失败（python3 不可用？）"
fi
# ---- config.env 的「端口区」----
#
# ⭐ 2026-10-20（老板要求"所有端口都能改，尤其 ZLM 那些"）：把**全部 16 个端口**
#   连同默认值直接写进 config.env，客户解包后打开这个文件就能改 ——
#   不必先 start 一次，也不必去翻 PORTS.md 找键名。
#
# ⛔ 值一律**现读 ctl.sh**（见 ctl_port_default）：只要这里复制一份字面量，
#   就会在某次改默认值后与运行期悄悄分家 —— 客户按文件里的值改完，
#   实际跑的还是另一套端口，而页面上完全看不出来。
# ⛔ 出包时仍可用同名环境变量临时覆盖（如 UVP_ZLM_HTTP_PORT=52004）。
pkg_port_or_die() {
  local env_key="$1" ctl_var="$2" val
  val="$(printenv "$env_key" 2>/dev/null || true)"
  if [ -z "$val" ]; then
    val="$(ctl_port_default "$ctl_var")" || {
      printf 'ERROR: 读不到 %s 的默认端口（ctl.sh 里找不到 %s="${%s:-<数字>}"？）\n' \
        "$env_key" "$ctl_var" "$ctl_var" >&2
      return 1
    }
  fi
  printf '%s' "$val"
}
PORT_NGINX_HTTP="${UVP_NGINX_HTTP_PORT:-$NGINX_HTTP_PORT_DEFAULT}"
PORT_HTTPS="${UVP_HTTPS_PORT:-$NGINX_HTTPS_PORT_DEFAULT}"
PORT_BACKEND="$HTTP_PORT"
PORT_REDIS="$REDIS_PORT"
PORT_ZLM_HTTP="$(pkg_port_or_die UVP_ZLM_HTTP_PORT ZLM_HTTP_PORT)" || exit 1
PORT_ZLM_SSL="$(pkg_port_or_die UVP_ZLM_SSL_PORT ZLM_SSL_PORT)" || exit 1
PORT_ZLM_RTSP="$(pkg_port_or_die UVP_ZLM_RTSP_PORT ZLM_RTSP_PORT)" || exit 1
PORT_ZLM_RTMP="$(pkg_port_or_die UVP_ZLM_RTMP_PORT ZLM_RTMP_PORT)" || exit 1
PORT_ZLM_RTC="$(pkg_port_or_die UVP_ZLM_RTC_PORT ZLM_RTC_PORT)" || exit 1
PORT_ZLM_SIGNALING="$(pkg_port_or_die UVP_ZLM_SIGNALING_PORT ZLM_SIGNALING_PORT)" || exit 1
PORT_ZLM_SIGNALING_SSL="$(pkg_port_or_die UVP_ZLM_SIGNALING_SSL_PORT ZLM_SIGNALING_SSL_PORT)" || exit 1
PORT_ZLM_SRT="$(pkg_port_or_die UVP_ZLM_SRT_PORT ZLM_SRT_PORT)" || exit 1
PORT_ZLM_ONVIF="$(pkg_port_or_die UVP_ZLM_ONVIF_PORT ZLM_ONVIF_PORT)" || exit 1
PORT_ZLM_ICE="$(pkg_port_or_die UVP_ZLM_ICE_PORT ZLM_ICE_PORT)" || exit 1
PORT_ZLM_RTP_PROXY="$(pkg_port_or_die UVP_ZLM_RTP_PROXY_PORT ZLM_RTP_PROXY_PORT)" || exit 1
PORT_ZLM_RTP_RANGE="$(pkg_port_or_die UVP_ZLM_RTP_RANGE ZLM_RTP_RANGE)" || exit 1

{
  printf '# [uvp-ports] ============================================================\n'
  printf '# [uvp-ports] 端口（**全部可改**：改下面的值 → ./uvp-gb28181-ctl.sh restart）\n'
  printf '# [uvp-ports]\n'
  printf '# [uvp-ports] · 不用手写 UVP_USER_SET_ 标记：程序会对比上次落盘值，\n'
  printf '# [uvp-ports]   发现你改过就自动记住，并在文件顶部补上标记行。\n'
  printf '# [uvp-ports] · UVP_ZLM_RTP_RANGE 与 UVP_ZLM_RTP_PROXY_PORT 必须同一个起点；\n'
  printf '# [uvp-ports]   只改其中一个，程序会按你改的那个自动拉齐另一个。\n'
  printf '# [uvp-ports] · SIP 端口**不在本文件**（登录后引导页录入、存数据库），\n'
  printf '# [uvp-ports]   默认值 = RTP 段尾 + 1；改了它，存量设备必须逐台改成同一端口。\n'
  printf '# [uvp-ports] · 1024 以下是特权端口，本包以普通用户运行，绑不上。\n'
  printf '# [uvp-ports] ============================================================\n'
  printf '# [uvp-ports] ---- 管理页 / 后端 / Redis ----\n'
  printf 'UVP_NGINX_HTTP_PORT=%s\n' "$PORT_NGINX_HTTP"
  printf 'UVP_HTTPS_PORT=%s\n' "$PORT_HTTPS"
  printf 'UVP_HTTP_PORT=%s\n' "$PORT_BACKEND"
  printf 'UVP_REDIS_PORT=%s\n' "$PORT_REDIS"
  printf '# [uvp-ports] ---- 流媒体（ZLMediaKit）----\n'
  printf 'UVP_ZLM_HTTP_PORT=%s\n' "$PORT_ZLM_HTTP"
  printf 'UVP_ZLM_SSL_PORT=%s\n' "$PORT_ZLM_SSL"
  printf 'UVP_ZLM_RTSP_PORT=%s\n' "$PORT_ZLM_RTSP"
  printf 'UVP_ZLM_RTMP_PORT=%s\n' "$PORT_ZLM_RTMP"
  printf 'UVP_ZLM_RTC_PORT=%s\n' "$PORT_ZLM_RTC"
  printf 'UVP_ZLM_RTP_PROXY_PORT=%s\n' "$PORT_ZLM_RTP_PROXY"
  printf 'UVP_ZLM_RTP_RANGE=%s\n' "$PORT_ZLM_RTP_RANGE"
  printf 'UVP_ZLM_SIGNALING_PORT=%s\n' "$PORT_ZLM_SIGNALING"
  printf 'UVP_ZLM_SIGNALING_SSL_PORT=%s\n' "$PORT_ZLM_SIGNALING_SSL"
  printf 'UVP_ZLM_SRT_PORT=%s\n' "$PORT_ZLM_SRT"
  printf 'UVP_ZLM_ONVIF_PORT=%s\n' "$PORT_ZLM_ONVIF"
  printf 'UVP_ZLM_ICE_PORT=%s\n' "$PORT_ZLM_ICE"
  printf '\n'
  printf '# ---- SIP 报文诊断加密密钥（打包时生成，每份包不同）----\n'
  printf '# 报文里的密码字段用它加密后落库。缺失或无效则**整条报文不落库**\n'
  printf '# （安全优先，但功能不可用且health 只报 invalid key，不提示怎么修）。\n'
  printf '# 要换成自己的值：python3 -c "import secrets,base64;print(base64.b64encode(secrets.token_bytes(32)).decode())"\n'
  printf '# ⚠️ 换密钥后历史密文解不开，需能接受丢历史再换。\n'
  printf '%s=%s\n' "$SIP_TRACE_KEY_ENV" "$SIP_TRACE_KEY"
  printf '\n'
  printf '# ---- OpenAPI 主密钥（打包时生成，每份包不同）----\n'
  printf '# 格式必须是 **32 字节的无填充 base64url**（后端用 Strict 解码，\n'
  printf '# 标准 base64 的 + / = 会直接被拒 ⇒ 表现为 OpenAPI 客户端页 503）。\n'
  printf '# 生成：python3 -c "import secrets;print(secrets.token_urlsafe(32))"\n'
  printf '# ⚠️ 换主密钥后**已签发的客户端凭据全部失效**，需重新下发。\n'
  printf '# ⚠️ 本文件明文随包分发 —— 它与 openapi.master_key_id 配套使用。\n'
  printf '%s=%s\n' "$OPENAPI_MASTER_KEY_ENV" "$OPENAPI_MASTER_KEY"
} > "$PKG/config/config.env"
log "写入 ${SIP_TRACE_KEY_ENV}（32 字节随机，base64）"
log "写入 ${OPENAPI_MASTER_KEY_ENV}（32 字节随机，base64url）"

# ------------------------------------------------- 可移植性门禁（打包前）----
#
# ⛔⛔ 为什么要在**打包前**、对着**装好的包**再查一遍（2026-10-10 客户麒麟 V10 实机踩到）：
#   包里的 `vendor/nginx/sbin/nginx` 是在较新的 Linux 上直接编的，ELF 里写着要
#       `libssl.so.3` / `libcrypto.so.3`（OpenSSL 3.x） + `GLIBC_2.34`
#   而交付目标机（银河麒麟 V10 SP2）只有 **OpenSSL 1.1.1f + glibc 2.28**
#   ⇒ 启动即 `error while loading shared libraries: libssl.so.3`，nginx 连 `-v` 都跑不起来。
#   ⛔ 同一个包里 ZLM 反而没事 —— 它走的是 `uvp/linux-builder:glibc217`
#   （GLIBC ≤ 2.17、零 OpenSSL 依赖）。**差别只在"在哪编的"，在开发机上完全看不出来。**
#   实测同批五个可执行文件：nginx / redis-server / redis-cli **三个坏**，
#   ZLM 与 Go 后端**两个好** ⇒ 判据不能停在"包级 glibc 基线"这种笼统说法，
#   必须逐个二进制核（判据与理由见 check-portable-elf.sh 头部注释）。
log "审计可移植性（国产 OS / 老 glibc 目标机）"
# ⛔ 通配符必须是 `*.so*` 而不是 `*.so` —— 库名是 `libavcodec.so.60` 这种带版本号的，
#   `*.so` 一个都匹配不到（本地实测踩过：glob 不展开，白跑一趟）。
bash "$DEPLOY_DIR/check-portable-elf.sh" \
  --glibc-max "${UVP_GLIBC_MAX:-2.17}" \
  --provided-dir "$PKG/vendor/zlm/lib" \
  "$PKG/bin/uvp-server" \
  "$PKG/bin/uvp-setup" \
  "$PKG/bin/uvp-firewall-agent" \
  "$PKG/updater/uvp-updater" \
  "$PKG/vendor/nginx/sbin/nginx" \
  "$PKG/vendor/redis/redis-server" \
  "$PKG/vendor/redis/redis-cli" \
  "$PKG/vendor/zlm/MediaServer" \
  "$PKG/vendor/zlm/bin/ffmpeg" \
  "$PKG"/vendor/zlm/lib/*.so* \
  || fail "包内二进制对目标系统库的要求过高（拷到国产 OS 上必然起不来，逐条见上）。"

# --------------------------------------------------------------- 打包 ----

mkdir -p "$OUT_DIR"
# 包名 = uvp-gb28181-<系统>-<架构>-<版本>.tar.gz（见文件顶部 PKG_* 注释）
ARCHIVE="$OUT_DIR/${PKG_STEM}.tar.gz"

# ⛔ mktemp -d 建的是 0700，tar 会把这个模式存进归档项；
#   用 root 解压后目录变成 0700，非 root 的服务进程就**穿不过去** ⇒ 页面全 404，
#   而且没有任何报错。必须显式 chmod。
chmod 0755 "$STAGE" "$PKG"
# 注册文件与安装密钥必须位于非组/全局可写目录，且密钥文件不能被其他用户读取。
# 构建机 umask 可能放宽目录和文件默认权限，不能把它带进交付包。
chmod 0700 "$PKG/config"
chmod 0600 "$PKG/config/config.yml" "$PKG/config/config.env"

# Static vendor trees can contain nested repository metadata. Never include
# .git directories in a customer release archive.
find "$PKG" -name .git -exec rm -rf {} +

log "打包 → ${ARCHIVE}"
# ⛔⛔ 必须保证归档里**没有 Apple 扩展属性**（xattr / provenance / ACL）。
#   macOS 的 bsdtar 默认把它们打进归档，Linux 的 GNU tar 每解一个文件就警告一次
#   「Ignoring unknown extended header keyword 'LIBARCHIVE.xattr.com.apple.provenance'」
#   —— 几百行警告刷屏，客户以为包坏了。
#   ⛔ 这类"只在目标机暴露"的交付问题只能靠实机解压发现 —— 本地 macOS 解压不会有任何提示。
#
# ⛔⛔ 为什么不能写死 `tar --format=gnu`：那是 GNU tar 独有的选项，
#   macOS 自带的 bsdtar 会直接报 `Can't use format gnu: No such format 'gnu'`
#   —— 而这个脚本本身就是 macOS 出包机在跑（它大量使用 macOS 专有的
#   mktemp/du/shasum 组合）。原写法等于"在 macOS 上跑不了自己的出包脚本"，
#   只是此前出包机装了 brew 的 gnu-tar 才碰巧能过。
#
# 现在的做法：按可用性择优。GNU tar 用 --format=gnu；
# bsdtar 用 --format=ustar +显式关掉各类扩展属性（语义等价）。
TAR_BIN="tar"
if ! tar --format=gnu --version >/dev/null 2>&1; then
  if command -v gtar >/dev/null 2>&1; then
    TAR_BIN="gtar"
  else
    TAR_BIN="tar"   # bsdtar 路径
  fi
fi

# ⛔⛔ 归档里每个条目的属主必须钉成 root:root（2026-10-20 客户机 CentOS 7 现场）：
#   在 220 上以 superadmin 出包 ⇒ 823 个条目全记成 1000/1000；客户用 root
#   `tar -xzf` 解压时**默认 --same-owner**，安装目录属主就变成 1000（普通用户）。
#   而升级助手以 root 运行时只认 root 属主的路径（它要以 root 身份替换程序
#   文件），于是装到「阶段 3/3」才炸：
#     open trusted updater registration: updater registration parent
#     "/opt/uvp/uvp-gb28181-linux-amd64-1.1.11" owner UID 1000 is not trusted
#   ⛔ 这比"组可写"更隐蔽：目录是 755、权限位检查全过，只有**属主**不对；
#      而 owner 能 chmod 自己拥有的目录 ⇒ 等于能换掉整棵树。
#   打包时写死 0:0，解压到哪台机器、谁解压都不会错位。
TAR_OWNER_FLAGS=""
# ⛔ 别用 `--help | grep` 探测：bsdtar 3.5.3（macOS 自带）**支持** --owner/--uid，
#   但 --help 里一个字都不列 ⇒ 会误判成"不支持"。直接试跑一次才准。
if "$TAR_BIN" --owner=0 --group=0 --version >/dev/null 2>&1; then
  TAR_OWNER_FLAGS="--owner=0 --group=0"                 # GNU tar 与新版 bsdtar
elif "$TAR_BIN" --uid 0 --gid 0 --version >/dev/null 2>&1; then
  TAR_OWNER_FLAGS="--uid 0 --gid 0"                     # 老版 bsdtar
else
  # 极老的打包器：退化为"不改写属主"，但要让出包人知道后果。
  log "  ⚠ 打包器不支持改写归档属主 ⇒ 客户机若以 root 解压，安装目录属主会是出包机 UID"
fi

if [ "$TAR_BIN" = "tar" ]; then
  # bsdtar：ustar 格式本身不带扩展头，再显式关掉 xattr/acl 双保险。
  # --no-xattrs 在旧版 bsdtar 上不存在，故先探测再传。
  TAR_XATTR_FLAG=""
  if tar --no-xattrs --version >/dev/null 2>&1; then
    TAR_XATTR_FLAG="--no-xattrs"
  fi
  log "  打包器: bsdtar（${TAR_XATTR_FLAG:-无扩展属性开关}，${TAR_OWNER_FLAGS:-无属主改写}）"
  "$TAR_BIN" --format=ustar ${TAR_XATTR_FLAG} ${TAR_OWNER_FLAGS} -czf "${ARCHIVE}" -C "${STAGE}" "${PKG_STEM}"
else
  log "  打包器: ${TAR_BIN}（GNU tar，--format=gnu，${TAR_OWNER_FLAGS:-无属主改写}）"
  "$TAR_BIN" --format=gnu ${TAR_OWNER_FLAGS} -czf "${ARCHIVE}" -C "${STAGE}" "${PKG_STEM}"
fi
test -s "$ARCHIVE" || fail "产物为空"

# 抽一行给现场看：属主列必须是 0/0（GNU tar）或 0 0（bsdtar）。这一行能一眼
# 看出"包是不是又被出包机的普通用户身份染上了"。
# ⛔ 不要写成 `tar tvzf ... | head -1`：本脚本开了 pipefail，tar 被 SIGPIPE 打断
#   会返回 141 ⇒ 整行失败、脚本直接退出（而它只是打印日志）。
if [ -n "$TAR_OWNER_FLAGS" ]; then
  tar_listing="$("$TAR_BIN" --numeric-owner -tvzf "${ARCHIVE}" 2>/dev/null || true)"
  log "  归档属主抽查: ${tar_listing%%$'\n'*}"
fi

SIZE="$(du -h "$ARCHIVE" | cut -f1)"
SHA="$(shasum -a 256 "$ARCHIVE" | cut -d' ' -f1)"

cat > "$OUT_DIR/${PKG_STEM}.sha256" <<EOF
${SHA}  ${PKG_STEM}.tar.gz
EOF

log "完成：${ARCHIVE}（${SIZE}）"
log "校验：${SHA}"
printf '\n下一步：\n  1. 上传到目标机并解压：tar -xzf %s\n  2. 进目录启动（首次会自动建库，无需单独跑建库脚本）：cd %s && ./uvp-gb28181-ctl.sh start\n  3. 访问：http://<目标机IP>:%s\n' \
  "${PKG_STEM}.tar.gz" "${PKG_STEM}" "$NGINX_HTTP_PORT_DEFAULT"
