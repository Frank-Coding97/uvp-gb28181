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
SQLITE_DIR="$SERVER_DIR/resource/database/sqlitebaseline"

HTTP_PORT="${UVP_HTTP_PORT:-8280}"
REDIS_PORT="${UVP_REDIS_PORT:-6379}"
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

[ -x "$SERVER_DIR/bin/uvp-server" ] || fail "缺少后端二进制：$SERVER_DIR/bin/uvp-server（先跑 go build）"
[ -x "$DEPLOY_DIR/bin/redis-server" ] || fail "缺少 redis-server：$DEPLOY_DIR/bin/redis-server"
[ -x "$DEPLOY_DIR/bin/redis-cli" ]    || fail "缺少 redis-cli：$DEPLOY_DIR/bin/redis-cli"
[ -f "$SQLITE_DIR/baseline.sql" ]     || fail "缺少 SQLite 基线：$SQLITE_DIR/baseline.sql（先跑 generate.py）"
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