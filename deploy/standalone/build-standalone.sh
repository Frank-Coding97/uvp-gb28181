#!/usr/bin/env bash
# 组装 UVP 绿色安装包（Linux / x86_64 / 零预装）。
#
# 产出一个 tar.gz：解压后执行 ./uvp-ctl.sh start 即可用，
# 不需要 Docker / nginx / MySQL / Redis（Redis 与 SQLite 都自带）。
#
# 用法：
#   deploy/standalone/build-standalone.sh [--skip-frontend] [--out DIR]
#
# 前置：
#   · server/bin/uvp-server            —— linux/amd64 静态二进制
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

# ---- 端口默认值（必须与 uvp-ctl.sh 的 *_PORT_DEFAULT 完全一致）----
# ⛔⛔ 为什么这里也要有一份：make-config.py 靠命令行参数把端口写进 config.yml，
#   而**首次运行时 uvp-ctl.sh 以 config.yml 为权威**。两边不一致的话，
#   首次启动时变量会被 config.yml 里的值覆盖 —— 实测症状是
#   「脚本里明明写着 30011，Redis 却起在 6379」，毫无线索。
#   根因是这两个变量一直只靠调用方传环境变量，不传就落回 make-config.py
#   的 argparse 默认值，于是与 uvp-ctl.sh 各说各话。
#   端口规划见 deploy/standalone/PORTS.md。
HTTP_PORT="${UVP_HTTP_PORT:-51010}"
REDIS_PORT="${UVP_REDIS_PORT:-51011}"

# ---- 扫码接入基址里的设备可达地址 ----
#
# ⛔⛔ 为什么不能沿用「浏览器打开平台的地址」：
#   二维码是给**设备**扫的，设备要访问的是后端。
#   绿色包前端走 nginx 自签 HTTPS（SAN 只有 localhost/127.0.0.1），
#   之前前端拿 window.location.origin 当默认值 ⇒ 二维码里带自签地址 ⇒
#   手机侧 Ktor CIO 校验证书失败 ⇒ 失败被报成「连不上平台,请检查 Wi-Fi」。
#   这个值会被写进 config.yml，现场可用 gb28181.qr_provision.base_url 改。
QR_PROVISION_HOST="${UVP_QR_PROVISION_HOST:-}"

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
#   与 uvp-ctl.sh 里 nginx 端口那个坑是同一个：**同名的第二处定义**。
VERSION="${UVP_VERSION:-$(cat "$SERVER_DIR/version.json" 2>/dev/null | grep -m1 '"version"' | cut -d'"' -f4 || echo 1.0.0)}"
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

# 构建机上用的 python（生成配置要跑脚本）。目标机上由 init-database.sh 自行探测。
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
  shellcheck_out="$(shellcheck -S warning "$0" "$DEPLOY_DIR/uvp-ctl.sh" 2>&1)" || true
  if [ -n "$shellcheck_out" ]; then
    printf '%s\n' "$shellcheck_out" >&2
    fail "shellcheck 未通过（见上）。⚠️ 这类问题 bash -n 抓不到，必须过。"
  fi

  # ---- 端口变量单点定义门禁 ----
  # ⛔⛔ 今天在两个脚本里各栽一次「同名变量的第二处定义」，而症状都是
  #   「明明改了端口，运行时还是旧值」且**毫无提示**：
  #   uvp-ctl.sh 里 nginx 端口有第二组（443/80），build-standalone.sh 里
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
    "$0" "$DEPLOY_DIR/uvp-ctl.sh" 2>/dev/null | sed 's/=.*//' | sort | uniq -d || true)"
  if [ -n "$dup_ports" ]; then
    printf '端口变量存在多处字面量默认值（后者覆盖前者，改动不生效）：\n%s\n' "$dup_ports" >&2
    fail "端口默认值必须单点定义。端口规划见 deploy/standalone/PORTS.md"
  fi
else
  log "⚠️ 未安装 shellcheck，跳过脚本静态检查（apt install shellcheck 可启用）"
fi

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
  log "构建后端二进制（linux/amd64）"
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
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-mod=mod \
      "$GO_BIN" build -ldflags="-s -w" -o bin/uvp-server .
  ) || fail "后端构建失败（见上）。⚠️ 不要用旧的 bin/uvp-server 顶替——那就是本周反复踩的坑。"
  log "  后端二进制: $(du -h bin/uvp-server 2>/dev/null | cut -f1 || echo '?')"

  log "构建前端产物"
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
    if [ -x node_modules/.bin/vite ]; then
      node_modules/.bin/vite build
    else
      npx --yes vite build
    fi
  ) || fail "前端构建失败（见上）"
fi

[ -x "$SERVER_DIR/bin/uvp-server" ] || fail "缺少后端二进制：$SERVER_DIR/bin/uvp-server"
[ -x "$DEPLOY_DIR/bin/redis-server" ] || fail "缺少 redis-server：$DEPLOY_DIR/bin/redis-server"
[ -x "$DEPLOY_DIR/bin/redis-cli" ]    || fail "缺少 redis-cli：$DEPLOY_DIR/bin/redis-cli"
[ -f "$SQLITE_DIR/baseline.sql" ]     || fail "缺少 SQLite 基线：$SQLITE_DIR/baseline.sql（先跑 generate.py）"

# ZLM（平台靠它推流/取流/录像，缺了整套功能跑不起来）
[ -x "$ZLM_DIR/MediaServer" ] || fail "缺少 ZLM：$ZLM_DIR/MediaServer（先跑 deploy/standalone/fetch-zlm.sh）"
[ -f "$ZLM_DIR/config.ini" ]   || fail "缺少 ZLM 配置：$ZLM_DIR/config.ini"
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
fi

# 静态校验：二进制必须是 linux/amd64。用 file 判，别只看文件名。
arch_of() { file "$1" | grep -oE 'x86-64|x86_64' | head -1; }
[ -n "$(arch_of "$SERVER_DIR/bin/uvp-server")" ] || fail "后端二进制不是 x86_64"
[ -n "$(arch_of "$DEPLOY_DIR/bin/redis-server")" ] || fail "redis-server 不是 x86_64"
[ -n "$(arch_of "$ZLM_DIR/MediaServer")" ]|| fail "MediaServer 不是 x86_64"

# ------------------------------------------------------------------ 组装 ----

STAGE="$(mktemp -d "${TMPDIR:-/tmp}/uvp-standalone.XXXXXX")"
cleanup() { rm -rf "$STAGE"; }
trap cleanup EXIT

PKG="$STAGE/uvp-$VERSION"
mkdir -p "$PKG"/{bin,config,data/redis,logs,run,scripts}

log "复制后端二进制"
cp "$SERVER_DIR/bin/uvp-server" "$PKG/bin/uvp-server"
chmod 0755 "$PKG/bin/uvp-server"

# ---- 产物新鲜度断言（双保险，与上面的自动构建互为兜底）----
#
# ⛔ 为什么还要这一条：自动构建可能被 SKIP_BUILD=1 跳过（离线出包/只改配置重出包），
#   也可能有人手动 cp 了一个旧二进制进 server/bin/。
#   ⇒ 无论走哪条路，这里都比一次 mtime：**产物比源码旧 = 拒绝出包**。
#
# ⭐ 判据用「产物 mtime 是否早于**最新改动的源码**」，而不是「早于脚本运行时间」——
#   后者在刚build 完时会误报（同秒/时钟精度问题），而且抓不到"构建了但漏编了某文件"。
assert_fresh() {
  local artifact="$1" label="$2" newest
  [ -e "$artifact" ] || fail "$label 不存在：$artifact"
  # 源码目录里最新的 .go / .vue / .ts / .scss 文件
  newest="$(find "$SERVER_DIR" "$WEB_DIR/src" -type f \
    \( -name '*.go' -o -name '*.vue' -o -name '*.ts' -o -name '*.scss' \) \
    -newer "$artifact" -print 2>/dev/null | head -1)"
  if [ -n "$newest" ]; then
    fail "$label 比源码旧，拒绝出包（否则会打出一个「看起来全新、跑起来是旧的」包）。
     产物: $artifact
     更新的源码: ${newest#$REPO_ROOT/}
     ⛔ 这就是本周反复踩的坑：所有出包断言都过，但包里跑的是旧代码。
     解决：删掉产物重新构建，或用 UVP_SKIP_BUILD=0 强制自动构建。"
  fi
  log "  $label 新鲜度 OK"
}
if [ "$SKIP_BUILD" != "1" ]; then
  assert_fresh "$SERVER_DIR/bin/uvp-server" "后端二进制"
  assert_fresh "$WEB_DIR/dist/index.html" "前端产物"
fi

log "复制 Redis（自带，包内跑，不连外部）"
cp "$DEPLOY_DIR/bin/redis-server" "$DEPLOY_DIR/bin/redis-cli" "$PKG/bin/"
chmod 0755 "$PKG/bin/redis-server" "$PKG/bin/redis-cli"

log "复制二开 ZLM（含 ffmpeg 运行时库）"
mkdir -p "$PKG/bin/zlm"
# ⛔ 必须连 lib/ 一起拷且保持相对位置：MediaServer 是动态链接的，
#   靠 LD_LIBRARY_PATH=$ROOT/bin/zlm/lib 找那些 .so，缺一个就起不来。
cp -a "$ZLM_DIR/MediaServer" "$PKG/bin/zlm/"
cp -a "$ZLM_DIR/lib" "$PKG/bin/zlm/lib"
cp -a "$ZLM_DIR/www" "$PKG/bin/zlm/www"
cp "$ZLM_DIR/config.ini" "$PKG/bin/zlm/config.ini"
[ -f "$ZLM_DIR/default.pem" ] && cp "$ZLM_DIR/default.pem" "$PKG/bin/zlm/default.pem"
[ -f "$ZLM_DIR/zlm-buildinfo.txt" ] && cp "$ZLM_DIR/zlm-buildinfo.txt" "$PKG/bin/zlm/"
chmod 0755 "$PKG/bin/zlm/MediaServer"

# ---- ZLM 的身份也固定（源头侧）----
# ⛔⛔ 必须改**包内这份** config.ini，而不是靠启动时对齐：
#   `mediaServerId` 的默认值是占位符 `your_server_id`，
#   而 ZLM 的 setServerConfig 会把平台下发的值**持久化回这个文件**
#   （见 server/app/gb28181/zlm/apply.go）—— 也就是它一旦落盘就固化。
#   所以这里出包时就写好，启动时ZLM 读到的第一眼就是正确值。
#   secret 同理：它必须与 config.yml 里的**逐字相同**，否则平台调 ZLM 全被拒。
log "固定 ZLM 的secret 与节点标识"
"$PY" - "$PKG/bin/zlm/config.ini" "$ZLM_SECRET" "$ZLM_MEDIA_SERVER_ID" <<'ZLM_INI_ID_PY'
import re
import sys

path, secret, msid = sys.argv[1:4]
# ⛔ 读写都用 newline="" + split("\n")：这份 ini 是 **CRLF**，
#   用 splitlines() 会吃掉 \r，写回时行尾就变了（下次判据又对不上）。
lines = open(path, encoding="utf-8", newline="").read().split("\n")

section = None
changed = []
for i, line in enumerate(lines):
    t = line.strip()
    if t.startswith("[") and t.endswith("]"):
        section = t[1:-1].strip().lower()
        continue
    m = re.match(r"^(\s*)([A-Za-z_]+)(\s*=\s*)(.*?)(\r?)$", line)
    if not m:
        continue
    indent, key, eq, old, cr = m.groups()
    # 只认 [api] 的 secret 与 [general] 的 mediaServerId，
    # 别处出现的同名键不能动（这份 ini 有 11 个段）。
    if section == "api" and key == "secret" and old.strip() != secret:
        changed.append(f"[api] secret: {old.strip()} -> (固定值)")
        lines[i] = f"{indent}{key}{eq}{secret}{cr}"
    elif section == "general" and key.lower() == "mediaserverid" and old.strip() != msid:
        changed.append(f"[general] mediaServerId: {old.strip()} -> {msid}")
        lines[i] = f"{indent}mediaServerId={msid}{cr}"

open(path, "w", encoding="utf-8", newline="").write("\n".join(lines))
print("   ZLM config.ini: " + ("; ".join(changed) if changed else "已是固定值，无需改动"))
ZLM_INI_ID_PY

log "复制 nginx（前端 + HTTPS 终止）"
mkdir -p "$PKG/bin/nginx"
cp -a "$NGINX_DIR/." "$PKG/bin/nginx/"
chmod 0755 "$PKG/bin/nginx/sbin/nginx"

log "复制前端产物"
if [ -f "$WEB_DIR/dist/index.html" ]; then
  mkdir -p "$PKG/resource/public"
  cp -a "$WEB_DIR/dist/." "$PKG/resource/public/"
fi

log "复制后端资源（SQLite 建库脚本 + 静态资源）"
cp -a "$SERVER_DIR/resource/database/sqlitebaseline" "$PKG/resource/baseline"
[ -d "$SERVER_DIR/resource/public" ] && cp -a "$SERVER_DIR/resource/public/." "$PKG/resource/public/" || true

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

cp "$SERVER_DIR/version.json" "$PKG/version.json"

# ⛔ 出包前断言：**库里有头像路径 ⇒ 包里必须有那个文件**。
#   这类"数据库说有、磁盘上没有"的组合最坑：nginx 的 SPA 回落会把
#   index.html 返回给图片请求 ⇒ HTTP 200 而内容是 HTML，
#   浏览器静默显示不出图，**没有任何一行报错**（实测踩过）。
log "校验头像资源与数据库一致"
"$PY" - "$PKG/config/config.yml" "$PKG/resource/baseline/baseline.sql" "$PKG/resource/public" <<'AVATAR_CHECK_PY'
import pathlib
import re
import sys

cfg, baseline, public = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3])

# 从基线 SQL 里取 sys_users 的 avatar 值（不连库，出包阶段还没有库）
sql = baseline.read_text(encoding="utf-8", errors="replace")
m = re.search(r"INSERT INTO [\"`]?sys_users[\"`]?\s*\([^)]*\)\s*VALUES(.*?);", sql, re.S)
urls = re.findall(r"'(/public/uploads/[^']+)'", m.group(1)) if m else []

problems = []
for url in urls:
    rel = url.lstrip("/").split("public/", 1)[-1]      # /public/uploads/x → uploads/x
    if not (public / rel).is_file():
        problems.append(f"库里有 {url}，但包内 resource/public/{rel} 不存在")
if problems:
    sys.exit("❌ 头像资源与数据库不一致：\n  - " + "\n  - ".join(problems)
             + "\n   这个组合的现场症状是「HTTP 200 但图片出不来」——"
               "nginx 把 index.html 返回给了图片请求，全程无报错。")
print(f"✅ 头像资源与数据库一致（{len(urls)} 个引用）")
AVATAR_CHECK_PY

log "生成生产配置（SQLite + 本机 Redis）"
$PY \
  "$DEPLOY_DIR/make-config.py" \
  --source "$SERVER_DIR/config/config.example.yml" \
  --target "$PKG/config/config.yml" \
  --http-port "$HTTP_PORT" \
  --redis-port "$REDIS_PORT" \
  --db-path "./data/uvp.db" \
  --zlm-secret "$ZLM_SECRET" \
  --zlm-media-server-id "$ZLM_MEDIA_SERVER_ID" \
  --qr-provision-host "$QR_PROVISION_HOST"

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
    problems.append(f"secret={zlm.get('secret')!r}（应为 {want_secret!r}）")
if zlm.get("mediaserverid") != want_uuid:
    problems.append(f"mediaserverid={zlm.get('mediaserverid')!r}（应为 {want_uuid!r}）")
if problems:
    sys.exit("❌ ZLM 身份配置未写进 config.yml：" + "；".join(problems)
             + "。\n   出了这个包，ZLM 侧与平台侧就对不上——现场表现为"
               "「录像缓存服务未装配」等各功能报未装配，且 ZLM 进程本身看起来完全正常。")
print(f"✅ ZLM 身份配置已固定（secret {len(want_secret)} 字符 / 节点标识 {len(want_uuid)} 字符）")
ZLM_ID_CHECK_PY

# ⛔ 出包前断言「扫码接入基址」真的写进去了。
#   这一条的价值不在于「值对不对」，而在于**杜绝二维码指向错误地址**：
#   地址不对时二维码照样出得来、扫得动，只是设备换不到接入信息，
#   而失败现场在手机上，现场根本查不到是包的问题（2026-10-08 就是这样坏的）。
log "校验扫码接入基址"
"$PY" - "$PKG/config/config.yml" "$HTTP_PORT" <<'QR_BASE_CHECK_PY'
import re
import sys

import yaml

path, http_port = sys.argv[1:3]
text = open(path, encoding="utf-8").read()
qr = (yaml.safe_load(text) or {}).get("gb28181", {}).get("qr_provision", {})

problems = []
# ① 段只能出现一次 —— 重复键 YAML 不报错，只是后者静默覆盖前者，
#   但文件里两份配置会让现场运维改错地方。
hits = sum(1 for line in text.split("\n") if line.strip() == "qr_provision:")
if hits != 1:
    problems.append(f"qr_provision 段出现 {hits} 次（应为 1 次）")
# ② 必须是非空基址，且指向后端明文端口
base_url = str(qr.get("base_url") or "")
if not base_url:
    problems.append("base_url 为空（没传 --qr-provision-host？）——"
                    "后端会拒绝出码，扫码页面永远转圈")
elif f":{http_port}" not in base_url:
    problems.append(f"base_url={base_url!r} 未指向后端端口 {http_port}")
elif base_url.startswith("https://"):
    # 不是必然错，但绿色包默认不该这样 —— nginx 是自签证书，设备侧必然验不过
    print(f"   ⚠️ base_url 用了 https（{base_url}）——设备侧必须信任该证书才能扫通")
if problems:
    sys.exit("❌ 扫码接入基址未正确写进 config.yml：" + "；".join(problems)
             + "。\n   出了这个包，页面能出二维码，但设备扫码后换不到接入信息，"
               "而失败现场在手机上、看不出是包的问题。")
print(f"✅ 扫码接入基址: {base_url}")
QR_BASE_CHECK_PY

log "复制启停脚本"
cp "$DEPLOY_DIR/uvp-ctl.sh" "$PKG/uvp-ctl.sh"
chmod 0755 "$PKG/uvp-ctl.sh"
# ⛔ 不再分发独立的 init-database.sh —— 建库已内置进 `uvp-ctl.sh start`。
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
{
  printf '\n'
  printf '# ---- SIP 报文诊断加密密钥（打包时生成，每份包不同）----\n'
  printf '# 报文里的密码字段用它加密后落库。缺失或无效则**整条报文不落库**\n'
  printf '# （安全优先，但功能不可用且health 只报 invalid key，不提示怎么修）。\n'
  printf '# 要换成自己的值：python3 -c "import secrets,base64;print(base64.b64encode(secrets.token_bytes(32)).decode())"\n'
  printf '# ⚠️ 换密钥后历史密文解不开，需能接受丢历史再换。\n'
  printf '%s=%s\n' "$SIP_TRACE_KEY_ENV" "$SIP_TRACE_KEY"
} > "$PKG/config.env"
log "写入 $SIP_TRACE_KEY_ENV（32 字节随机，base64）"

# --------------------------------------------------------------- 打包 ----

mkdir -p "$OUT_DIR"
ARCHIVE="$OUT_DIR/uvp-$VERSION-standalone-linux-amd64.tar.gz"

# ⛔ mktemp -d 建的是 0700，tar 会把这个模式存进归档项；
#   用 root 解压后目录变成 0700，非 root 的服务进程就**穿不过去** ⇒ 页面全 404，
#   而且没有任何报错。必须显式 chmod。
chmod 0755 "$STAGE" "$PKG"

log "打包 → ${ARCHIVE}"
# ⛔ 必须加 --format=gnu（等价于 --format=ustar + 不带扩展头）。
#   macOS 的 bsdtar 默认把 Apple 扩展属性（xattr / provenance / ACL）打进归档，
#   Linux 的 GNU tar 每解一个文件就警告一次
#   「Ignoring unknown extended header keyword 'LIBARCHIVE.xattr.com.apple.provenance'」——
#   几百行警告刷屏，客户以为包坏了。
#   ⛔ 这类"只在目标机暴露"的交付问题只能靠实机解压发现 —— 本地 macOS 解压不会有任何提示。
tar --format=gnu -czf "${ARCHIVE}" -C "${STAGE}" "uvp-${VERSION}"
test -s "$ARCHIVE" || fail "产物为空"

SIZE="$(du -h "$ARCHIVE" | cut -f1)"
SHA="$(shasum -a 256 "$ARCHIVE" | cut -d' ' -f1)"

cat > "$OUT_DIR/uvp-$VERSION-standalone-linux-amd64.sha256" <<EOF
${SHA}  uvp-${VERSION}-standalone-linux-amd64.tar.gz
EOF

log "完成：${ARCHIVE}（${SIZE}）"
log "校验：${SHA}"
printf '\n下一步：\n  1. 上传到目标机并解压：tar -xzf %s\n  2. 首次使用先建库：./init-database.sh\n  3. 启动：./uvp-ctl.sh start\n  4. 访问：http://<目标机IP>:%s\n' \
  "uvp-${VERSION}-standalone-linux-amd64.tar.gz" "$HTTP_PORT"