#!/usr/bin/env bash
# 打 UVP-GB28181 容器版**离线升级包**
#
# 用法：
#   deploy/compose/build-images.sh --version 1.1.15 --arch amd64
#   deploy/compose/package-upgrade.sh --version 1.1.15 --arch amd64
#
# 产物（release-output/）：
#   uvp-<版本>-linux-<arch>-compose.upgrade.tar          升级包本体
#   uvp-<版本>-linux-<arch>-compose.upgrade.tar.sha256   校验值
#   uvp-<版本>-linux-<arch>-compose.images.json          manifest 的 images 片段
#
# 包结构（ComposeAdapter.PreparePackage 的硬要求，少一条就装不上）：
#   compose.yml              **必须在包根**：适配器会按注册的相对路径找它，
#                            找不到直接报 "upgrade package is missing registered compose file"
#   images/<组件>.tar        四个组件**各自一个**归档
#
# ⛔⛔ 四个镜像不能打成一个归档。签名清单里每条 images 记录是
#   `{reference, archive, config_id}`，适配器按 archive 逐个 `docker load`；
#   而 signing 脚本会拒绝「同一个 archive 出现两次」⇒ 打成一个文件就签不出合法清单。
#   （离线**安装**包正好相反：install.sh 只要一次 docker load，所以那边是一个大归档。）
#
# ⛔ 这个包不含 install.sh、也不含 config/ 与 data/：升级只换程序，
#   现场配置与数据由适配器自己保全（升级前会做备份，失败回滚）。
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

DEPLOY_DIR="$REPO_ROOT/deploy/compose"
OUT_DIR="$REPO_ROOT/release-output"
ARCH="${UVP_PKG_ARCH:-amd64}"
VERSION="${UVP_VERSION:-}"
PREFIX="${UVP_IMAGE_PREFIX:-uvp-gb28181}"

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --arch) ARCH="$2"; shift 2 ;;
    --out) OUT_DIR="$2"; shift 2 ;;
    --prefix) PREFIX="$2"; shift 2 ;;
    *) printf '未知参数: %s\n' "$1" >&2; exit 2 ;;
  esac
done

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
log()  { printf '[upgrade-pkg] %s\n' "$*"; }

if [ -z "$VERSION" ]; then
  VERSION="$(grep -m1 '"version"' "$REPO_ROOT/server/version.json" | cut -d'"' -f4 || true)"
fi
[ -n "$VERSION" ] || fail "拿不到版本号：显式传 --version"

command -v docker >/dev/null 2>&1 || fail "需要 docker（要 docker save 与 docker inspect）"

# ---- 镜像必须已经构建过 ----
COMPONENTS=(backend frontend zlm redis)
for comp in "${COMPONENTS[@]}"; do
  docker image inspect "${PREFIX}-${comp}:${VERSION}" >/dev/null 2>&1 \
    || fail "缺少镜像 ${PREFIX}-${comp}:${VERSION}
  先跑：deploy/compose/build-images.sh --version $VERSION --arch $ARCH"
done

STEM="uvp-${VERSION}-linux-${ARCH}-compose"
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/uvp-compose-upgrade.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT
PKG="$STAGE/$STEM"

log "组装 $STEM"
mkdir -p "$PKG/images"

cp "$DEPLOY_DIR/compose.yml" "$PKG/compose.yml"
chmod 0644 "$PKG/compose.yml"

# ⛔⛔ 镜像 tag 必须**在包里写死**，不能留着 ${UVP_VERSION} 模板占位符。
#
#   原因：升级时适配器（ComposeAdapter.Activate）**只替换这一个 compose.yml**，
#   完全不碰现场的 .env。而 compose 解析 ${UVP_VERSION} 是读 .env 的 ——
#   .env 里还是上次安装时那个版本号 ⇒ `docker compose up -d --pull never`
#   拉起来的还是**旧镜像**，新 tag 的镜像明明已经 docker load 进来了却没人用。
#   接着 Validate 阶段容器内跑 `-upgrade-readiness` 比对版本，
#   旧镜像报的是旧版本 ⇒ "runtime identity mismatch" ⇒ 整个升级回滚。
#   现象极具误导性：日志显示"升级失败"，但镜像确实导入了、包也确实是对的。
#
#   ⇒ 把 `${PREFIX}-<组件>:${VERSION}` 在这里固化成字面量，升级后的编排
#     自带正确的版本，与 .env 内容彻底解耦。
#   ⛔ 只固化镜像那两段；${UVP_ROOT}/${UVP_ZLM_SECRET}/端口等**必须继续走 .env** ——
#     那些是现场部署事实，写死会把客户改过的端口/路径覆盖掉。
command -v python3 >/dev/null 2>&1 || fail "需要 python3（要把编排模板里的镜像版本固化成字面量）"
python3 - "$PKG/compose.yml" "$PREFIX" "$VERSION" <<'PIN_PY'
import re
import sys

path, prefix, version = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path, encoding="utf-8").read()

# image: ${UVP_IMAGE_PREFIX:-uvp-gb28181}-backend:${UVP_VERSION:?...}
pattern = re.compile(
    r"\$\{UVP_IMAGE_PREFIX:-[^}]*\}-([A-Za-z0-9._-]+):\$\{UVP_VERSION:[^}]*\}"
)
text, count = pattern.subn(lambda m: "%s-%s:%s" % (prefix, m.group(1), version), text)
if count == 0:
    sys.exit("编排模板里没有找到 ${UVP_IMAGE_PREFIX}/${UVP_VERSION} 形式的镜像引用，"
             "模板可能已被改坏（拒绝打一个升级后会跑旧镜像的包）")

leftover = re.findall(r"\$\{UVP_VERSION[^}]*\}", text)
if leftover:
    sys.exit("编排模板里还有未固化的版本占位符：%s" % ", ".join(sorted(set(leftover))))

open(path, "w", encoding="utf-8").write(text)
print("[upgrade-pkg]   固化镜像版本 %d 处 → %s-*:%s" % (count, prefix, version))
PIN_PY

# ⛔ compose.yml 里不能出现 :latest —— 适配器会直接判非法。
if grep -E '^[[:space:]]*image:.*(:latest[[:space:]]*$|:[[:space:]]*latest)' "$PKG/compose.yml" >/dev/null 2>&1; then
  fail "compose.yml 里有 :latest 引用，升级包会被拒绝"
fi

IMAGES_JSON="$OUT_DIR/${STEM}.images.json"
mkdir -p "$OUT_DIR"
printf '[\n' > "$IMAGES_JSON"
first=1
for comp in "${COMPONENTS[@]}"; do
  ref="${PREFIX}-${comp}:${VERSION}"
  archive="images/${comp}.tar"
  log "  docker save ${ref} → ${archive}"
  docker save --output "$PKG/$archive" "$ref"
  config_id="$(docker image inspect --format '{{.Id}}' "$ref")"
  [ -n "$config_id" ] || fail "拿不到 ${ref} 的 config id"
  [ $first -eq 1 ] || printf ',\n' >> "$IMAGES_JSON"
  printf '  {"reference": "%s", "archive": "%s", "config_id": "%s"}' \
    "$ref" "$archive" "$config_id" >> "$IMAGES_JSON"
  first=0
done
printf '\n]\n' >> "$IMAGES_JSON"

ARCHIVE="$OUT_DIR/${STEM}.upgrade.tar"
log "打包 $ARCHIVE"
# ⛔ 用固定属主与权限打包：升级包会被签名摘要，同一份内容在别的机器上重打
#    必须得到同样的字节，否则「重打一次校验值就变了」会让人怀疑文件被篡改。
# ⛔⛔ mode 里必须用**大写 X**，不能写 u=rw,go=r：
#   小写 rw/r 会**同样作用在目录上**，打出来的包解压后目录是 drw-r--r--
#   （没有 x 位）⇒ 连 `cd` 进去都 Permission denied，Go 里 os.ReadFile
#   也会在路径中间目录上失败。大写 X 只给「目录」和「本来就有执行位的文件」
#   补 x，文件仍然是 0644、目录是 0755。
tar --format=ustar --owner=0 --group=0 --numeric-owner \
    --mode='u=rwX,go=rX' \
    -cf "$ARCHIVE" -C "$STAGE" "$STEM"
( cd "$OUT_DIR" && sha256sum "$(basename "$ARCHIVE")" > "$(basename "$ARCHIVE").sha256" )

log "完成："
log "  $(du -h "$ARCHIVE" | cut -f1)  $ARCHIVE"
log "  $(cat "$ARCHIVE.sha256")"
log "  images 片段：$IMAGES_JSON"
