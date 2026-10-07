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

[ -x "$SERVER_DIR/bin/uvp-server" ] || fail "缺少后端二进制：$SERVER_DIR/bin/uvp-server（先跑 go build）"
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

cp "$SERVER_DIR/version.json" "$PKG/version.json"

log "生成生产配置（SQLite + 本机 Redis）"
$PY \
  "$DEPLOY_DIR/make-config.py" \
  --source "$SERVER_DIR/config/config.example.yml" \
  --target "$PKG/config/config.yml" \
  --http-port "$HTTP_PORT" \
  --redis-port "$REDIS_PORT" \
  --db-path "./data/uvp.db"

log "复制启停脚本"
cp "$DEPLOY_DIR/uvp-ctl.sh" "$PKG/uvp-ctl.sh"
chmod 0755 "$PKG/uvp-ctl.sh"
# ⛔ 不再分发独立的 init-database.sh —— 建库已内置进 `uvp-ctl.sh start`。
#   绿色包的契约是「解压即用」：用户装完只做"执行 start"这一件事。
#   多一条"记得先建库"的步骤就会有一批人漏掉，而漏掉的症状极具误导性
#   （页面能开、登录页报 no such table），用户联想不到是因为少跑了一个脚本。
[ -f "$DEPLOY_DIR/README.md" ] && cp "$DEPLOY_DIR/README.md" "$PKG/README.md"

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