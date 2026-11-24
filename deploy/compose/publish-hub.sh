#!/usr/bin/env bash
# 把 UVP-GB28181 的四个镜像发布到 Docker Hub（或任意兼容 Registry）
#
# 用法：
#   deploy/compose/publish-hub.sh --namespace uvplatform --version 1.1.26
#   deploy/compose/publish-hub.sh --namespace uvplatform --archs amd64,arm64 --tag latest
#   deploy/compose/publish-hub.sh --namespace uvplatform --skip-build --archs amd64   # 复用已构建镜像
#   deploy/compose/publish-hub.sh --namespace uvplatform --dry-run                    # 只打印要执行的命令
#
# 前置：
#   1) docker login -u <账号>   —— 口令用 Access Token，不要用登录密码
#   2) amd64 vendor 在 deploy/standalone/bin（nginx / zlm / redis-server / redis-cli）
#      arm64 vendor 用 --vendor-arm64 指向对应目录（本机目前没有，需在 arm64 机器上出）
#
# 产物：
#   <namespace>/uvp-gb28181-{backend,frontend,zlm,redis}:<version>
#   ├─ 单架构（默认 --archs amd64）：直接就是该版本的 manifest
#   └─ 双架构：先推 <version>-amd64 / <version>-arm64，再合成多架构 manifest 指向 <version>
#      ⚠️ 多架构镜像在 Docker Hub 上**每个架构各算一次 pull**（拉取限额翻倍消耗）
#
# ⛔⛔ 为什么 compose 里必须 network_mode: host、页面上不能写 `docker run -p`：
#   ZLM 的 SDP 里写的是「平台自己看到的 IP」，bridge 网络下就是 172.x，
#   设备照着这个地址发流必然连不进来。镜像说明页只能给 compose 快速开始。
#
# ⛔ 三个密钥（OpenAPI 43 字符 base64url / SIP trace 44 字符 base64 / ZLM 节点 UUID）
#   缺一个后端直接 panic，**绝不能烤进镜像**，只能运行时注入。
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

COMPOSE_DIR="$REPO_ROOT/deploy/compose"
SERVER_DIR="$REPO_ROOT/server"
STANDALONE_BIN="$REPO_ROOT/deploy/standalone/bin"

NAMESPACE=""
VERSION=""
REGISTRY_HOST=""            # 空 = docker.io（镜像名不带 registry 前缀）
ARCHS="amd64"
VENDOR_AMD64="$STANDALONE_BIN"
VENDOR_ARM64=""
SKIP_BUILD=0
DRY_RUN=0
TAG_LATEST=""
TAG_MINOR=""

COMPONENTS=(backend frontend zlm redis)

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
log()  { printf '[publish] %s\n' "$*"; }
run()  {
  if [ "$DRY_RUN" = "1" ]; then
    printf '  DRY  %s\n' "$*"
  else
    "$@"
  fi
}

while [ $# -gt 0 ]; do
  case "$1" in
    --namespace)   NAMESPACE="$2"; shift 2 ;;
    --version)     VERSION="$2"; shift 2 ;;
    --registry)    REGISTRY_HOST="$2"; shift 2 ;;
    --archs)       ARCHS="$2"; shift 2 ;;
    --vendor-amd64) VENDOR_AMD64="$2"; shift 2 ;;
    --vendor-arm64) VENDOR_ARM64="$2"; shift 2 ;;
    --tag)         TAG_LATEST="$2"; shift 2 ;;
    --tag-minor)   TAG_MINOR="$2"; shift 2 ;;
    --skip-build)  SKIP_BUILD=1; shift ;;
    --dry-run)     DRY_RUN=1; shift ;;
    -h|--help)     sed -n '2,30p' "$0"; exit 0 ;;
    *) printf '未知参数: %s\n' "$1" >&2; exit 2 ;;
  esac
done

[ -n "$NAMESPACE" ] || fail "必须传 --namespace（Docker Hub 上的账号或组织名，例如 uvplatform）"

# 版本号真源 = server/version.json（⛔ 不是根目录 VERSION 文件）
if [ -z "$VERSION" ]; then
  VERSION="$(grep -m1 '"version"' "$SERVER_DIR/version.json" 2>/dev/null | cut -d'"' -f4 || true)"
fi
[ -n "$VERSION" ] || fail "拿不到版本号：显式传 --version，或检查 server/version.json"
case "$VERSION" in
  latest|main|master|dev) fail "版本号不能是 ${VERSION}，会把一个语义不明的 tag 推上去" ;;
esac

BASE="$NAMESPACE/uvp-gb28181"
[ -n "$REGISTRY_HOST" ] && BASE="$REGISTRY_HOST/$BASE"

log "版本 ${VERSION} / 架构 ${ARCHS} / 目标 ${BASE}-<组件>:${VERSION}"

# ---------------------------------------------------------------- 预检 ----
if [ "$DRY_RUN" = "0" ]; then
  command -v docker >/dev/null 2>&1 || fail "找不到 docker"
  # 登录态：只做提醒，不做硬拦（token 也可能放在 CI 的 credential helper 里）
  if ! grep -qs "index.docker.io\|${REGISTRY_HOST:-registry-1.docker.io}" "${DOCKER_CONFIG:-$HOME/.docker}/config.json" 2>/dev/null; then
    log "⚠️  没在 docker config 里看到目标仓库的登录凭据，请先 docker login（口令用 Access Token）"
  fi
fi

# ------------------------------------------------------------ 逐架构构建 ----
IFS=',' read -r -a ARCH_LIST <<< "$ARCHS"
[ "${#ARCH_LIST[@]}" -gt 0 ] || fail "--archs 不能为空"

vendor_for_arch() {
  case "$1" in
    amd64) printf '%s' "$VENDOR_AMD64" ;;
    arm64) printf '%s' "$VENDOR_ARM64" ;;
  esac
}

# ⛔⛔ 所有架构的 vendor 必须在**开始推送之前**一次性校验完。
#   否则推完 amd64 才在 arm64 上失败，远程会留下半套 tag（Docker Hub 的 tag 删不干净，
#   只能手工一个一个删），清理成本远高于提前 1 秒失败。
if [ "$SKIP_BUILD" = "0" ]; then
  for arch in "${ARCH_LIST[@]}"; do
    case "$arch" in
      amd64|arm64) : ;;
      *) fail "不支持的架构: ${arch}，只有 amd64 / arm64" ;;
    esac
    vendor="$(vendor_for_arch "$arch")"
    if [ -z "$vendor" ]; then
      fail "架构 ${arch} 未指定 vendor 目录，请用 --vendor-${arch} 指定（arm64 需先在 arm64 机器上出产物）"
    fi
    if [ ! -d "$vendor" ]; then
      fail "架构 ${arch} 的 vendor 目录不存在: $vendor（需含 nginx/ zlm/ redis-server）"
    fi
    [ -e "$vendor/zlm/MediaServer" ] || fail "架构 ${arch} 的 vendor 缺 zlm/MediaServer: $vendor"
    [ -e "$vendor/redis-server" ]    || fail "架构 ${arch} 的 vendor 缺 redis-server: $vendor"
    [ -e "$vendor/nginx/sbin/nginx" ]|| fail "架构 ${arch} 的 vendor 缺 nginx: $vendor"
  done
fi

pushed_tags=()

for arch in "${ARCH_LIST[@]}"; do
  if [ "$SKIP_BUILD" = "0" ]; then
    vendor="$(vendor_for_arch "$arch")"
    # ⛔ 变量后面紧跟中文时一律写 ${var}：macOS 自带的 bash 3.2 会把 UTF-8 字节
    #   并进变量名，报 "vendor? : unbound variable"，且行号指到下一行，极难归因。
    log "构建 ${arch}, vendor=${vendor}"
    run env UVP_VENDOR_DIR="$vendor" \
      "$COMPOSE_DIR/build-images.sh" --arch "$arch" --prefix "$BASE" --version "$VERSION"
  fi

  # 单架构时 build-images.sh 的 tag 就是最终 tag；双架构时先推带架构后缀的
  for comp in "${COMPONENTS[@]}"; do
    local_image="$BASE-$comp:$VERSION"
    [ "${#ARCH_LIST[@]}" -eq 1 ] && push_tag="$local_image" || push_tag="$local_image-$arch"
    if [ "${#ARCH_LIST[@]}" -gt 1 ]; then
      run docker tag "$local_image" "$push_tag"
    fi
    log "推送 $push_tag"
    run docker push "$push_tag"
    pushed_tags+=("$push_tag")
  done
done

# --------------------------------------------------- 多架构 manifest 合成 ----
if [ "${#ARCH_LIST[@]}" -gt 1 ]; then
  for comp in "${COMPONENTS[@]}"; do
    target="$BASE-$comp:$VERSION"
    log "合成多架构 manifest → $target"
    run docker manifest rm "$target" || true
    arch_tags=()
    for arch in "${ARCH_LIST[@]}"; do
      arch_tags+=(--amend "$BASE-$comp:$VERSION-$arch")
    done
    run docker manifest create "$target" "${arch_tags[@]}"
    run docker manifest push "$target"
  done
fi

# ------------------------------------------------------------ 别名 tag ----
# 别名用 imagetools 直接复制**远程** manifest，不重新构建、不重传层。
# ⛔ 别用 `docker tag && docker push`：那会把本机最后一次构建的**单架构**镜像
#   推成别名，多架构语义当场丢失。
make_alias() {
  local alias="$1" comp src dst
  for comp in "${COMPONENTS[@]}"; do
    src="$BASE-$comp:$VERSION"
    dst="$BASE-$comp:$alias"
    log "别名 $dst → $src"
    if docker buildx version >/dev/null 2>&1; then
      run docker buildx imagetools create -t "$dst" "$src"
    else
      run docker manifest create "$dst" "$src"
      run docker manifest push "$dst"
    fi
  done
}

[ -n "$TAG_MINOR" ] && make_alias "$TAG_MINOR"
[ -n "$TAG_LATEST" ] && make_alias "$TAG_LATEST"

# ---------------------------------------------------------------- 收尾 ----
echo
log "已推送："
for t in "${pushed_tags[@]}"; do printf '  %s\n' "$t"; done

if [ "$DRY_RUN" = "0" ]; then
  echo
  log "核对远程 manifest："
  for comp in "${COMPONENTS[@]}"; do
    printf '  %-52s ' "$BASE-$comp:$VERSION"
    if docker manifest inspect "$BASE-$comp:$VERSION" >/dev/null 2>&1; then
      echo "OK"
    else
      echo "⚠️  inspect 失败（私有仓库未登录时也会这样）"
    fi
  done
  echo
  log "客户侧在线安装："
  printf '  sudo ./install.sh --online --registry %s --version %s\n' "${REGISTRY_HOST:+$REGISTRY_HOST/}$NAMESPACE" "$VERSION"
fi
