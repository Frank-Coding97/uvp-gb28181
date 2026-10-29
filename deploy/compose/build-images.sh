#!/usr/bin/env bash
# 构建 UVP-GB28181 的四个容器镜像（后端 / 前端 / 流媒体 / 缓存）
#
# 用法：
#   deploy/compose/build-images.sh [--arch amd64|arm64] [--version X.Y.Z] [--out DIR] [--save DIR]
#
# 产物：
#   · 四个镜像：uvp-gb28181-{backend,frontend,zlm,redis}:<version>
#   · --save DIR 时额外导出 docker save 归档（打离线包用）
#   · 镜像身份摘要（digest / config id）写入 <out>/image-identity.json，供升级校验用
#
# ⛔ 三个与绿色包**共享**的二进制（ZLM / nginx / redis）不在这里编译，
#   直接取 deploy/standalone/bin 下的产物（由 fetch-zlm.sh / build-nginx.sh / build-redis.sh 产出）。
#   理由：容器与绿色包用**同一份二进制**，出问题能直接对齐归因；
#   重编一次 ZLM 几十分钟，且编译参数差异会让两边行为分叉。
#   ARM64 用 UVP_VENDOR_DIR 指向 deploy/standalone 下对应的 arm64 产物目录。
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

DEPLOY_DIR="$REPO_ROOT/deploy/compose"
DOCKER_DIR="$DEPLOY_DIR/docker"
SERVER_DIR="$REPO_ROOT/server"
WEB_DIR="$REPO_ROOT/web"
STANDALONE_DIR="$REPO_ROOT/deploy/standalone"

ARCH="${UVP_PKG_ARCH:-amd64}"
VERSION="${UVP_VERSION:-}"
OUT_DIR="${UVP_IMAGE_OUT:-$REPO_ROOT/release-output/docker-context}"
SAVE_DIR=""
SKIP_BUILD="${UVP_SKIP_BUILD:-0}"
CONTEXT_ONLY=0
VENDOR_DIR="${UVP_VENDOR_DIR:-$STANDALONE_DIR/bin}"

REGISTRY_PREFIX="${UVP_IMAGE_PREFIX:-uvp-gb28181}"

while [ $# -gt 0 ]; do
  case "$1" in
    --arch) ARCH="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --out) OUT_DIR="$2"; shift 2 ;;
    --save) SAVE_DIR="$2"; shift 2 ;;
    --skip-build) SKIP_BUILD=1; shift ;;
    --context-only) CONTEXT_ONLY=1; shift ;;
    --prefix) REGISTRY_PREFIX="$2"; shift 2 ;;
    *) printf '未知参数: %s\n' "$1" >&2; exit 2 ;;
  esac
done

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
log()  { printf '[images] %s\n' "$*"; }

# ⛔ docker 检查放在 --context-only 之后：本机（macOS）常常没有 daemon，
#    但照样要能"先把构建上下文组装出来"，再搬到真正的构建机上跑 docker build。
if [ "$CONTEXT_ONLY" = "0" ]; then
  command -v docker >/dev/null 2>&1 || fail "需要 docker（本机没有 daemon 时用 --context-only 组装上下文，再搬到构建机上构建）"
fi

# ---------------------------------------------------------------- 版本 ----
if [ -z "$VERSION" ]; then
  VERSION="$(grep -m1 '"version"' "$SERVER_DIR/version.json" 2>/dev/null | cut -d'"' -f4 || true)"
fi
[ -n "$VERSION" ] || fail "拿不到版本号：显式传 --version（或检查 server/version.json）"
log "版本 ${VERSION} / 架构 ${ARCH}"

BUILD_ID="${UVP_BUILD_ID:-}"
if [ -z "$BUILD_ID" ]; then
  command -v git >/dev/null 2>&1 || fail "找不到 git；请显式设置 UVP_BUILD_ID"
  [ -z "$(git -C "$REPO_ROOT" status --porcelain --untracked-files=normal)" ] || fail "工作区有未提交改动；请先提交，或显式设置 UVP_BUILD_ID"
  BUILD_ID="$(git -C "$REPO_ROOT" rev-parse HEAD)"
fi

GOARCH="$ARCH"
case "$ARCH" in
  amd64) GOARCH=amd64 ;;
  arm64) GOARCH=arm64 ;;
  *) fail "不支持的架构：$ARCH（目前 amd64 / arm64）" ;;
esac

# ------------------------------------------------------------ 编译产物 ----
if [ "$SKIP_BUILD" = "1" ]; then
  log "⚠️ 已跳过前后端构建（--skip-build）—— 镜像里的产物可能比源码旧"
else
  log "交叉编译后端（linux/${GOARCH}）"
  (
    cd "$SERVER_DIR"
    GO_BIN="${GO:-go}"
    command -v "$GO_BIN" >/dev/null 2>&1 || fail "找不到 go 工具链"
    # CGO_ENABLED=0 是硬要求：SQLite 引擎是纯 Go 的，开 CGO 会去找构建机的 C 工具链，
    # 产物就不再是静态链接 ⇒ alpine 镜像里跑不起来。
    CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" GOFLAGS=-mod=mod \
      "$GO_BIN" build -ldflags="-s -w -X uvplatform.com/uvp-gb28181/app/global/app.BuildID=$BUILD_ID" -o bin/uvp-server .
    # ⛔⛔ 升级助手必须随包出，而且它**不进镜像**：容器版做页面内升级时，
    #   升级流程要把这些容器 stop/recreate，干这件事的进程不能活在被停掉的容器里。
    #   ⇒ 它跑在宿主机 systemd（uvp-updater.service），由 install.sh 装进 $ROOT/updater/。
    #   缺了它，「服务升级」页只会显示"升级助手未运行"，任何升级都点不动。
    CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" GOFLAGS=-mod=mod \
      "$GO_BIN" build -ldflags="-s -w" -o bin/uvp-updater ./cmd/uvp-updater
  ) || fail "后端构建失败"

  log "构建前端产物"
  (
    cd "$WEB_DIR"
    rm -rf dist
    [ -d node_modules ] || npm ci --no-audit --no-fund
    # 与绿色包一致：SIP 默认端口在构建期注入，引导页才会预填段内端口。
    ENV_LOCAL="$WEB_DIR/.env.production.local"
    printf 'VITE_DEFAULT_SIP_PORT=%s\nVITE_BUILD_ID=%s\n' "${UVP_SIP_PORT:-51064}" "$BUILD_ID" > "$ENV_LOCAL"
    trap 'rm -f "$ENV_LOCAL"' EXIT
    node_modules/.bin/vite build
  ) || fail "前端构建失败"
fi

[ -x "$SERVER_DIR/bin/uvp-server" ] || fail "缺少后端二进制"
[ -f "$WEB_DIR/dist/index.html" ]   || fail "缺少前端产物"

# ---- 随包第三方二进制 ----
[ -x "$VENDOR_DIR/redis-server" ]        || fail "缺少 redis-server：$VENDOR_DIR/redis-server"
[ -x "$VENDOR_DIR/redis-cli" ]           || fail "缺少 redis-cli"
[ -x "$VENDOR_DIR/zlm/MediaServer" ]     || fail "缺少 ZLM：$VENDOR_DIR/zlm/MediaServer"
[ -f "$VENDOR_DIR/zlm/config.ini" ]      || fail "缺少 ZLM 配置"
[ -x "$VENDOR_DIR/nginx/sbin/nginx" ]    || fail "缺少 nginx：$VENDOR_DIR/nginx/sbin/nginx"
[ -f "$VENDOR_DIR/nginx/conf/mime.types" ] || fail "缺少 nginx mime.types"

# -------------------------------------------------------- 组装上下文 ----
CTX="$OUT_DIR/$ARCH"
rm -rf "$CTX"
mkdir -p "$CTX"/{backend,frontend,zlm,redis}

log "组装构建上下文 → $CTX"
# ⛔ 先把所有目标目录 mkdir 出来：`cp` 到不存在的父目录时 GNU cp 与 BSD cp
#   行为不一致（一个直接报错、一个可能把目标做成文件），别赌。
mkdir -p "$CTX"/backend/resource/public \
         "$CTX"/frontend/dist "$CTX"/frontend/nginx/conf \
         "$CTX"/zlm/zlm "$CTX"/redis

cp "$SERVER_DIR/bin/uvp-server"        "$CTX/backend/uvp-server"
cp "$SERVER_DIR/version.json"          "$CTX/backend/version.json"
# ⛔ resource/database 必须进镜像：首次启动靠它建库（SQLite 基线 / 迁移脚本）。
cp -a "$SERVER_DIR/resource/database"  "$CTX/backend/resource/database"
cp -a "$WEB_DIR/dist/."                "$CTX/frontend/dist/"
cp -a "$VENDOR_DIR/nginx/sbin"         "$CTX/frontend/nginx/sbin"
cp "$VENDOR_DIR/nginx/conf/mime.types" "$CTX/frontend/nginx/conf/mime.types"
cp "$DEPLOY_DIR/nginx/nginx.conf.template" "$CTX/frontend/nginx.conf.template"
cp "$DOCKER_DIR/entrypoint.sh"         "$CTX/frontend/entrypoint.sh"
cp -a "$VENDOR_DIR/zlm/."              "$CTX/zlm/zlm/"
cp "$DOCKER_DIR/zlm-entrypoint.sh"     "$CTX/zlm/zlm-entrypoint.sh"
cp "$VENDOR_DIR/redis-server" "$VENDOR_DIR/redis-cli" "$CTX/redis/"
cp "$DOCKER_DIR/redis-entrypoint.sh"   "$CTX/redis/redis-entrypoint.sh"

if [ "$CONTEXT_ONLY" = "1" ]; then
  log "仅组装上下文（--context-only）：$CTX"
  log "把这个目录连同 docker/ 一起搬到构建机，再跑 build-images.sh --skip-build 构建镜像。"
  exit 0
fi

# ------------------------------------------------------------ 构建镜像 ----
# ⛔ 构建与"采集身份"必须分成两步：config id 只有在镜像**存在之后**才拿得到。
build_one() {
  local comp="$1" ctx="$2" dockerfile="$3"
  local image="${REGISTRY_PREFIX}-${comp}:${VERSION}"
  log "构建 ${image}"
  docker build --platform "linux/${ARCH}" -f "$DOCKER_DIR/${dockerfile}" -t "$image" "$CTX/${ctx}"
}

build_one backend  backend  backend.Dockerfile
build_one frontend frontend frontend.Dockerfile
build_one zlm      zlm      zlm.Dockerfile
build_one redis    redis    redis.Dockerfile

# ---- 镜像身份摘要 ----
# ⛔ 记录的是**本机镜像的 config id**，不是 Registry 的 RepoDigest：
#   docker save/load 不保留 RepoDigest，离线升级只能拿 config id 比对身份
#   （与服务升级方案 U11-2 的判定契约一致）。
IMAGE_IDENTITY="$OUT_DIR/image-identity.json"
log "采集镜像身份 → $IMAGE_IDENTITY"
UVP_ARCH="$ARCH" node -e '
const { execFileSync } = require("child_process");
const fs = require("fs");
const [out, prefix, version, buildId] = process.argv.slice(1);
const components = ["backend", "frontend", "zlm", "redis"];
const images = {};
for (const comp of components) {
  const image = `${prefix}-${comp}:${version}`;
  const configId = execFileSync("docker", ["inspect", image, "--format", "{{.Id}}"], { encoding: "utf8" }).trim();
  const size = execFileSync("docker", ["inspect", image, "--format", "{{.Size}}"], { encoding: "utf8" }).trim();
  images[comp] = { image, config_id: configId, size_bytes: Number(size) };
}
fs.writeFileSync(out, JSON.stringify({
  schema: "uvp-image-identity/v1",
  version, build_id: buildId, arch: process.env.UVP_ARCH || "amd64", prefix, images,
}, null, 2) + "\n");
' "$IMAGE_IDENTITY" "$REGISTRY_PREFIX" "$VERSION" "$BUILD_ID"

cat "$IMAGE_IDENTITY"

if [ -n "$SAVE_DIR" ]; then
  mkdir -p "$SAVE_DIR"
  ARCHIVE="$SAVE_DIR/uvp-gb28181-docker-images-${ARCH}-${VERSION}.tar"
  log "导出镜像归档 → $ARCHIVE"
  docker save \
    "${REGISTRY_PREFIX}-backend:${VERSION}" \
    "${REGISTRY_PREFIX}-frontend:${VERSION}" \
    "${REGISTRY_PREFIX}-zlm:${VERSION}" \
    "${REGISTRY_PREFIX}-redis:${VERSION}" \
    -o "$ARCHIVE"
  ( cd "$SAVE_DIR" && sha256sum "$(basename "$ARCHIVE")" > "$(basename "$ARCHIVE").sha256" )
  log "  归档大小: $(du -h "$ARCHIVE" | cut -f1)"
fi

log "完成。镜像列表："
docker images --format '{{.Repository}}:{{.Tag}} {{.Size}}' | grep "^${REGISTRY_PREFIX}-" || true
