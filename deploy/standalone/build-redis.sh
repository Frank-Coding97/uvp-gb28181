#!/usr/bin/env bash
# 构建随包分发的 redis-server / redis-cli。
#
# ⛔⛔ 背景（2026-10-10 客户银河麒麟 V10 SP2 实机踩到）：
#   包里的 redis-server / redis-cli 与 nginx 是**同一个毛病** —— 都是在新系统上编的，
#   ELF 里写着要 `libssl.so.3` / `libcrypto.so.3` + `GLIBC_2.34`；
#   而国产 OS（麒麟 V10 SP2：OpenSSL 1.1.1f + glibc 2.28）没有这些，
#   动态加载阶段直接失败。
#   ⚠️ 出问题的顺序很坑：绿色包的启动顺序是
#       ZLM → nginx → redis → 后端
#     所以**只修 nginx 的话，客户下一次重启就会在 redis 这一步撞上一模一样的报错**。
#
# ⭐ 为什么 redis 比 nginx 好修：**它根本不需要 OpenSSL**。
#   绿色包里的 redis 只服务本机后端（`--port/--bind/--dir/--appendonly`，
#   见 uvp-gb28181-ctl.sh 的 start_redis），没有任何 TLS 用法。
#   ⇒ 直接 `BUILD_TLS=no` 编出来，OpenSSL 依赖**整个消失** ——
#     不用像 nginx 那样去静态链它。
#
#   剩下唯一要管的还是 glibc：必须进 glibc ≤ 2.17 的底座编（与 nginx / ZLM 同一个镜像）。
#
# 用法：build-redis.sh [输出目录]      默认 deploy/standalone/bin
#
# 环境变量（都有默认值）：
#   UVP_LINUX_BUILDER_IMAGE  底座镜像，默认 uvp/linux-builder:glibc217
#   UVP_BUILD_PLATFORM       容器平台，默认 linux/amd64
#                            ⛔ 出 aarch64 包时设 linux/arm64，并把底座换成对应的
#                               arm64 镜像（uvp/linux-builder:glibc217-arm64）。
#   UVP_REDIS_VERSION        默认 7.4.9（对齐包内现版本；升版本要重跑门禁与冒烟）
#   UVP_REDIS_MALLOC         默认 jemalloc（Linux 上 redis 自己的默认值，与包内现版本一致）
#                            ⛔ 若老工具链编 jemalloc 5.3.0 失败，可用 MALLOC=libc 兜底：
#                               UVP_REDIS_MALLOC=libc ./build-redis.sh
#   UVP_GLIBC_MAX            默认 2.17
#   UVP_INSIDE_BUILDER       本脚本自己在容器里设，**不要手工设**
#   UVP_SRCDIR               源码预置目录（离线构建时给）
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DEST="${1:-$SCRIPT_DIR/bin}"

BUILDER_IMAGE="${UVP_LINUX_BUILDER_IMAGE:-uvp/linux-builder:glibc217}"
BUILD_PLATFORM="${UVP_BUILD_PLATFORM:-linux/amd64}"
REDIS_VERSION="${UVP_REDIS_VERSION:-7.4.9}"
REDIS_MALLOC="${UVP_REDIS_MALLOC:-jemalloc}"
GLIBC_MAX="${UVP_GLIBC_MAX:-2.17}"
SRC_ROOT="${UVP_REDIS_WORKDIR:-/tmp/uvp-redis-build}"
SRC_PROVIDED="${UVP_SRCDIR:-}"

log()  { printf '[redis-build] %s\n' "$*"; }
fail() { printf '[redis-build][ERROR] %s\n' "$*" >&2; exit 1; }

ver_gt() {
  awk -v a="$1" -v b="$2" 'BEGIN{
      n = split(a, A, "."); m = split(b, B, ".");
      k = (n > m) ? n : m;
      for (i = 1; i <= k; i++) {
        x = (i <= n) ? A[i] + 0 : 0;
        y = (i <= m) ? B[i] + 0 : 0;
        if (x > y) { print "1"; exit }
        if (x < y) { print "0"; exit }
      }
      print "0"
    }'
}

# ---------------------------------------------------------------- 底座 ----
# 理由与 build-nginx.sh 完全一致：**绝不能在"新系统"上编出要带出去跑的二进制**。
if [ "${UVP_INSIDE_BUILDER:-0}" != "1" ]; then
  # ⛔ `|| true` 不能省：macOS 的 getconf 不认识 GNU_LIBC_VERSION，退出码 64，
  #   在 `set -e + pipefail` 下会把整个脚本静默干掉（rc=64、零输出）。
  host_glibc="$(getconf GNU_LIBC_VERSION 2>/dev/null | awk '{print $2}' || true)"
  if [ -n "$host_glibc" ] && [ "$(ver_gt "$host_glibc" "$GLIBC_MAX")" = "0" ]; then
    log "本机 glibc ${host_glibc} ≤ ${GLIBC_MAX}，直接在本机编译（无需容器）"
  else
    case "$DEST" in
      "$REPO_ROOT"/*) : ;;
      *) fail "输出目录 $DEST 不在仓库内（$REPO_ROOT），容器里看不到它。" ;;
    esac

    runtime=""
    if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
      runtime="docker"
    elif command -v podman >/dev/null 2>&1; then
      runtime="podman"
    else
      if [ -n "$host_glibc" ]; then
        why="本机 glibc 是 ${host_glibc}，高于允许上限 ${GLIBC_MAX}"
      else
        why="本机探测不到 glibc（macOS / 非 glibc 系统），无法保证产物可移植"
      fi
      fail "${why} ⇒ 不能在本机直接编 redis。
      在新系统上编出来的 redis 要 GLIBC 2.34 + OpenSSL 3，拷到国产 OS 上必然起不来
      （2026-10-10 客户麒麟 V10 就是这么坏的，redis 是第二个倒下的）。
      请在装了 docker / podman 的机器上跑本脚本（会自动进 ${BUILDER_IMAGE} 容器），
      或直接在那台 glibc ≤ ${GLIBC_MAX} 的 Linux 构建机上跑（设 UVP_INSIDE_BUILDER=1）。"
    fi

    if ! "$runtime" image inspect "$BUILDER_IMAGE" >/dev/null 2>&1; then
      fail "找不到底座镜像 ${BUILDER_IMAGE}（$runtime）。
      构建机上通常已有（ZLM 就是这么编的）。若没有，可用本仓库的 Dockerfile 造一个
      （⛔ 用新 tag，别覆盖已有的 uvp/linux-builder:glibc217 —— 它带 devtoolset-11）：
        docker build -f deploy/standalone/Dockerfile.linux-builder \\
                     -t uvp/glibc217-builder:latest deploy/standalone/
        UVP_LINUX_BUILDER_IMAGE=uvp/glibc217-builder:latest $0"
    fi

    rel="${DEST#"$REPO_ROOT"/}"
    run_user="${UVP_DOCKER_USER:-$(id -u):$(id -g)}"
    log "本机 glibc ${host_glibc:-未知} 高于上限 ${GLIBC_MAX} ⇒ 进入容器 ${BUILDER_IMAGE} 编译"
    # shellcheck disable=SC2086
    exec "$runtime" run --rm -i \
      --platform "$BUILD_PLATFORM" \
      -u "$run_user" \
      -v "$REPO_ROOT":/src -w /src \
      -e UVP_INSIDE_BUILDER=1 \
      -e UVP_REDIS_VERSION="$REDIS_VERSION" \
      -e UVP_REDIS_MALLOC="$REDIS_MALLOC" \
      -e UVP_GLIBC_MAX="$GLIBC_MAX" \
      -e UVP_REDIS_WORKDIR="$SRC_ROOT" \
      ${SRC_PROVIDED:+-e UVP_SRCDIR=/src/.uvp-src} \
      "$BUILDER_IMAGE" \
      bash /src/deploy/standalone/build-redis.sh "/src/$rel"
  fi
fi

# ---------------------------------------------------------------- 依赖 ----
for c in gcc make tar curl; do
  command -v "$c" >/dev/null 2>&1 || fail "构建环境缺 $c"
done

# ---------------------------------------------------------------- 源码 ----
mkdir -p "$SRC_ROOT"
cd "$SRC_ROOT"

TARBALL="redis-${REDIS_VERSION}.tar.gz"
if [ -n "$SRC_PROVIDED" ]; then
  [ -d "$SRC_PROVIDED" ] || fail "UVP_SRCDIR=$SRC_PROVIDED 不是目录"
  [ -f "$SRC_PROVIDED/$TARBALL" ] || fail "预置源码目录里缺 $TARBALL"
  [ -f "$TARBALL" ] || cp "$SRC_PROVIDED/$TARBALL" .
  log "使用预置源码 $SRC_PROVIDED/$TARBALL"
elif [ -f "$TARBALL" ] && tar -tzf "$TARBALL" >/dev/null 2>&1; then
  log "已有 $TARBALL，跳过下载"
else
  ok=0
  for url in \
    "https://download.redis.io/releases/${TARBALL}" \
    "https://github.com/redis/redis/archive/refs/tags/${REDIS_VERSION}.tar.gz" \
    "https://mirrors.huaweicloud.com/redis/${TARBALL}"; do
    log "下载 $url"
    if curl -fsSL --max-time 300 -o "$TARBALL" "$url" 2>/dev/null \
       && tar -tzf "$TARBALL" >/dev/null 2>&1; then ok=1; break; fi
    rm -f "$TARBALL"
  done
  [ "$ok" = "1" ] || fail "所有源都下载失败：$TARBALL
      （离线构建时可用 UVP_SRCDIR 指一个预先放好该 tar 包的目录）"
fi

[ -d "redis-${REDIS_VERSION}" ] || tar -xzf "$TARBALL"
[ -d "redis-${REDIS_VERSION}" ] || fail "解压后没看到目录 redis-${REDIS_VERSION}"

# ---------------------------------------------------------------- 编译 ----
#
# ⛔ `BUILD_TLS=no` 是这里的关键：不编 TLS 就没有 OpenSSL 依赖，
#   动态依赖里不会出现 libssl.so / libcrypto.so —— 国产系统之间 OpenSSL 大版本
#   各不相同（麒麟/UOS 1.1.1、openEuler 3.0、Ubuntu 3.0）这件事就跟我们无关了。
#   ⛔ 别"顺手打开" BUILD_TLS：那会退回动态链系统 OpenSSL，本脚本就白修了。
#
# ⛔ MALLOC：Linux 上 redis 默认用 jemalloc（静态编进二进制，不产生 .so 依赖）。
#   包内现版本就是 jemalloc-5.3.0，这里保持一致，避免"顺手改内存分配器"带来的行为漂移。
#   老工具链若编不动 jemalloc，用 UVP_REDIS_MALLOC=libc 兜底。
cd "redis-${REDIS_VERSION}"
log "make（BUILD_TLS=no，MALLOC=${REDIS_MALLOC}）"
make -j"$(nproc 2>/dev/null || echo 2)" BUILD_TLS=no MALLOC="$REDIS_MALLOC" \
  > "$SRC_ROOT/make.log" 2>&1 || {
    tail -30 "$SRC_ROOT/make.log" >&2
    fail "编译失败（详见 $SRC_ROOT/make.log）。
      若报错发生在 deps/jemalloc 上，可改用 libc 分配器重试：
        UVP_REDIS_MALLOC=libc $0"
  }

[ -x src/redis-server ] || fail "产物 src/redis-server 不存在"
[ -x src/redis-cli ]    || fail "产物 src/redis-cli 不存在"

# ---------------------------------------------------------------- 装配 ----
mkdir -p "$DEST"
# ⛔ 产物名必须是 redis-server / redis-cli（平铺在 bin/ 下）——
#   build-standalone.sh 按 `$DEPLOY_DIR/bin/redis-server` 取件；
#   改名/挪位置会让出包在"缺少 redis-server"处失败。
#   （redis 7 里 src/redis-sentinel 是软链，这里不需要它。）
cp src/redis-server src/redis-cli "$DEST/"
chmod 0755 "$DEST/redis-server" "$DEST/redis-cli"

cat > "$DEST/redis-buildinfo.txt" <<EOF
REDIS_VERSION=$REDIS_VERSION
BUILD_TLS=no
MALLOC=$REDIS_MALLOC
BUILDER_IMAGE=${UVP_INSIDE_BUILDER:+$BUILDER_IMAGE}
BUILD_OS=$(uname -srm)
BUILD_TIME=$(date -Iseconds)
GLIBC_MAX_REQUIRED=$GLIBC_MAX
EOF

# ---------------------------------------------------------------- 自检 ----
log "自检：可移植性（glibc ≤ ${GLIBC_MAX}，且不得动态依赖 OpenSSL）"
bash "$SCRIPT_DIR/check-portable-elf.sh" --glibc-max "$GLIBC_MAX" \
  "$DEST/redis-server" "$DEST/redis-cli" \
  || fail "编出来的 redis 仍不合规 —— 要么 BUILD_TLS 没关掉，
      要么底座不是 glibc ≤ ${GLIBC_MAX} 的环境。请核对上面的逐条输出。"

log "✅ 完成：$DEST/redis-server 与 $DEST/redis-cli"
ls -la "$DEST/redis-server" "$DEST/redis-cli" | sed 's/^/    /'
