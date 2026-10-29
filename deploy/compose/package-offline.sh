#!/usr/bin/env bash
# 打 UVP-GB28181 容器版**离线交付包**
#
# 用法：
#   deploy/compose/build-images.sh --version 1.1.4 --save /tmp/uvp-images
#   deploy/compose/package-offline.sh --version 1.1.4 --images /tmp/uvp-images
#
# 产物：release-output/uvp-gb28181-docker-<os>-<arch>-<版本>.tar.gz（附 .sha256）
#
# 包结构（客户解压后直接跑 ./install.sh）：
#   install.sh                      安装入口
#   compose.yml / .env.example      编排模板与参数样例
#   images/*.tar                    docker save 出来的镜像归档（离线模式的全部来源）
#   tools/make-config.py            配置生成器（与绿色包同一个，避免两边分叉）
#   config/config.example.yml       配置源
#   docker/                         四个 Dockerfile 与入口脚本（客户要自建镜像时用）
#
# ⛔ 这个包**不含 Docker 本身**：客户机必须预装 Docker 20.10+ / Compose v2
#    （这是容器版与绿色包最大的区别 —— 绿色包是零预装）。
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

DEPLOY_DIR="$REPO_ROOT/deploy/compose"
STANDALONE_DIR="$REPO_ROOT/deploy/standalone"
OUT_DIR="$REPO_ROOT/release-output"
ARCH="${UVP_PKG_ARCH:-amd64}"
VERSION="${UVP_VERSION:-}"
IMAGES_DIR="${UVP_IMAGES_DIR:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --arch) ARCH="$2"; shift 2 ;;
    --images) IMAGES_DIR="$2"; shift 2 ;;
    --out) OUT_DIR="$2"; shift 2 ;;
    *) printf '未知参数: %s\n' "$1" >&2; exit 2 ;;
  esac
done

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
log()  { printf '[package] %s\n' "$*"; }

if [ -z "$VERSION" ]; then
  VERSION="$(grep -m1 '"version"' "$REPO_ROOT/server/version.json" | cut -d'"' -f4 || true)"
fi
[ -n "$VERSION" ] || fail "拿不到版本号：显式传 --version"

if [ -z "$IMAGES_DIR" ]; then
  IMAGES_DIR="$OUT_DIR/docker-context/$ARCH"
fi
IMAGE_ARCHIVE="$(ls "$IMAGES_DIR"/uvp-gb28181-docker-images-${ARCH}-${VERSION}.tar 2>/dev/null | head -1 || true)"
[ -n "$IMAGE_ARCHIVE" ] || fail "找不到镜像归档：$IMAGES_DIR/uvp-gb28181-docker-images-${ARCH}-${VERSION}.tar
  先跑：deploy/compose/build-images.sh --version $VERSION --arch $ARCH --save $IMAGES_DIR"

PKG_STEM="uvp-gb28181-docker-linux-${ARCH}-${VERSION}"
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/uvp-docker-pkg.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT
PKG="$STAGE/$PKG_STEM"

log "组装离线包 $PKG_STEM"
mkdir -p "$PKG"/{images,tools,config,docker}

cp "$DEPLOY_DIR/install.sh"        "$PKG/install.sh"
cp "$DEPLOY_DIR/compose.yml"       "$PKG/compose.yml"
cp "$DEPLOY_DIR/.env.example"      "$PKG/.env.example"
cp "$STANDALONE_DIR/make-config.py" "$PKG/tools/make-config.py"
cp "$REPO_ROOT/server/config/config.example.yml" "$PKG/config/config.example.yml"
cp "$DEPLOY_DIR/docker"/*.Dockerfile "$DEPLOY_DIR/docker"/*.sh "$PKG/docker/"
cp "$DEPLOY_DIR/nginx/nginx.conf.template" "$PKG/docker/nginx.conf.template"
# ⛔ 包内留档的 version.json 必须与镜像里那份**同一份**（带 build_id）。
#   源码根目录那份只有 name+version，拷它会让"现场版本清单"与
#   "二进制真实身份"对不上，排查升级问题时看到的 build_id 是空的。
if [ -f "$REPO_ROOT/server/bin/version.json" ]; then
  cp "$REPO_ROOT/server/bin/version.json" "$PKG/version.json"
else
  cp "$REPO_ROOT/server/version.json" "$PKG/version.json"
fi

# ⛔⛔ 升级助手（uvp-updater）必须进包，且**不放镜像里**：
#   页面内升级要把容器 stop/recreate，干这件事的进程不能活在被停的容器里，
#   所以它由 install.sh 装到宿主机 $ROOT/updater/ 并注册成 systemd 服务。
#   缺它 ⇒「服务升级」页只会说"升级助手未运行"，任何升级都点不动。
#   这也是四件补齐工作里的第一件（原先容器版根本没这个二进制）。
[ -x "$REPO_ROOT/server/bin/uvp-updater" ] \
  || fail "缺少升级助手二进制：server/bin/uvp-updater
  先跑：deploy/compose/build-images.sh --version $VERSION --arch $ARCH（它会一并编译 updater）"
mkdir -p "$PKG/updater"
cp "$REPO_ROOT/server/bin/uvp-updater" "$PKG/updater/uvp-updater"
# 注册脚本：装服务、写 registration、校验 gate 权限，install.sh 启动前调用它。
cp "$DEPLOY_DIR/uvp-compose-upgrade-install.sh" "$PKG/tools/uvp-compose-upgrade-install.sh"

# ⛔⛔ SQLite 建库脚本必须进包，且路径与绿色包**完全一致**（resource/baseline/baseline.sql）：
#   install.sh 靠它在首次安装时建库，缺了直接 fail「找不到 baseline.sql」。
#   这条曾经漏过 —— 包能打出来、能解压、能生成配置，一路走到建库才炸，
#   而报错只提"找不到"，看不出是打包脚本根本没拷。
mkdir -p "$PKG/resource/baseline"
cp "$REPO_ROOT/server/resource/database/sqlitebaseline/baseline.sql" "$PKG/resource/baseline/baseline.sql"

# 镜像身份：升级时要用它比对 config id（离线 load 后没有 RepoDigest 可比对）
[ -f "$IMAGES_DIR/image-identity.json" ] && cp "$IMAGES_DIR/image-identity.json" "$PKG/image-identity.json"

log "  复制镜像归档 $(du -h "$IMAGE_ARCHIVE" | cut -f1)"
cp "$IMAGE_ARCHIVE" "$PKG/images/"

chmod 0755 "$PKG/install.sh"
chmod 0755 "$PKG/updater/uvp-updater" "$PKG/tools/uvp-compose-upgrade-install.sh"
chmod 0755 "$PKG"/tools/make-config.py
chmod 0644 "$PKG"/compose.yml "$PKG"/.env.example "$PKG"/config/config.example.yml
chmod 0755 "$PKG"/docker/*.sh

# ⛔ 包内不能有软链接与绝对路径条目：解压到客户机上时它们会指向不存在的东西，
#    而安装脚本是按"包内自包含"来写的。
if find "$PKG" -type l | grep -q .; then
  fail "包内出现软链接（离线包必须自包含）：$(find "$PKG" -type l | head -5)"
fi

mkdir -p "$OUT_DIR"
ARCHIVE="$OUT_DIR/${PKG_STEM}.tar.gz"
log "打包 $ARCHIVE"
# ⛔ macOS 自带的是 bsdtar，默认会把每个文件的 `com.apple.provenance` 等 xattr
#   写成 LIBARCHIVE 扩展头；Linux 的 GNU tar 解包时会对每个条目刷一行
#      `tar: Ignoring unknown extended header keyword 'LIBARCHIVE.xattr...'`
#    客户看到一屏警告会以为包坏了。加 --no-mac-metadata 后两个平台产物一致。
TAR_FLAGS=()
if tar --version 2>/dev/null | grep -qi bsdtar; then
  TAR_FLAGS+=(--no-mac-metadata)
fi
# ⚠️ 必须写成 ${TAR_FLAGS[@]+"${TAR_FLAGS[@]}"}：macOS 自带 bash 3.2 在 set -u 下
#   展开空数组会直接 "unbound variable" 退出。
tar ${TAR_FLAGS[@]+"${TAR_FLAGS[@]}"} -czf "$ARCHIVE" -C "$STAGE" "$PKG_STEM"
( cd "$OUT_DIR" && sha256sum "$(basename "$ARCHIVE")" > "$(basename "$ARCHIVE").sha256" )

log "完成："
log "  $(du -h "$ARCHIVE" | cut -f1)  $ARCHIVE"
log "  $(cat "$ARCHIVE.sha256")"
