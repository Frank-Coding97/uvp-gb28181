#!/usr/bin/env bash
# 构建随包分发的静态 ffmpeg（ZLM 通道抓拍 / FFmpeg 拉流源的执行器）。
#
# ⛔⛔ 背景（2026-10-20 客户现场实锤）：绿色包的 ZLM config.ini 写着
#   `bin=/usr/bin/ffmpeg`，而交付目标机（openEuler 22.03）根本没装 ffmpeg
#   ⇒ ZLM 每次抓拍 fork/exec 失败（vendor/zlm/ffmpeg/ 下留了一堆
#   `execv process failed:-2(no such file or directory)` 的 642 字节日志），
#   返回 1024x512 占位 PNG，平台侧只报「未返回 JPEG」，全程零告警——
#   从部署那天起通道抓拍就静默不可用。老板拍板：**ffmpeg 必须随包内置**，
#   不再让用户手动安装。
#
# ⭐ 产物去向：deploy/standalone/bin/ffmpeg（与 redis-server 同级），
#   build-standalone.sh 按 `$DEPLOY_DIR/bin/ffmpeg` 取件打进 vendor/zlm/bin/。
#   ⛔ 不能放 vendor/zlm/ 根下叫 `ffmpeg`——那是 ZLM 的 **ffmpeg 日志目录**
#   （[ffmpeg] log=./ffmpeg/ffmpeg.log），目录名与文件名冲突。
#
# 用法：build-ffmpeg.sh [输出目录]      默认 deploy/standalone/bin
#
# 环境变量（都有默认值）：
#   UVP_LINUX_BUILDER_IMAGE  底座镜像，默认 uvp/linux-builder:glibc217
#   UVP_BUILD_PLATFORM       容器平台，默认 linux/amd64
#                            ⛔ 出 aarch64 包时设 linux/arm64，并把底座换成
#                               uvp/linux-builder:glibc217-arm64
#   UVP_FFMPEG_VERSION       默认 7.1.1（LTS）
#   UVP_FFMPEG_MINIMAL       默认 1：只编 ZLM 抓拍/转推用到的组件（约 1/4 体积）。
#                            =0 退回完整档，体积 21MB+，**别拿去出交付包**
#                            （Gitee 发行版单附件上限 100MB，完整档会把包顶过线）。
#   UVP_FORCE_BUILDER        =1 时无视本机 glibc 强制进容器编
#   UVP_FFMPEG_DYNAMIC       =1 时编动态链（仍 glibc≤2.17；容器里没有
#                            glibc-static 时脚本也会自动走这条路）
#   UVP_INSIDE_BUILDER       本脚本自己在容器里设，**不要手工设**
#   UVP_SRCDIR               源码预置目录（离线构建时给）
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DEST="${1:-$SCRIPT_DIR/bin}"

BUILDER_IMAGE="${UVP_LINUX_BUILDER_IMAGE:-uvp/linux-builder:glibc217}"
BUILD_PLATFORM="${UVP_BUILD_PLATFORM:-linux/amd64}"
FFMPEG_VERSION="${UVP_FFMPEG_VERSION:-7.1.1}"
FORCE_BUILDER="${UVP_FORCE_BUILDER:-0}"
GLIBC_MAX="${UVP_GLIBC_MAX:-2.17}"
SRC_ROOT="${UVP_FFMPEG_WORKDIR:-/tmp/uvp-ffmpeg-build}"
SRC_PROVIDED="${UVP_SRCDIR:-}"

log()  { printf '[ffmpeg-build] %s\n' "$*"; }
fail() { printf '[ffmpeg-build][ERROR] %s\n' "$*" >&2; exit 1; }

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
# 与 build-redis.sh 同一条铁律：**绝不能在"新系统"上编出要带出去跑的二进制**。
# ffmpeg 与 nginx 同病：在新系统上编出来要 GLIBC_2.34，拷到国产 OS 上
# `version GLIBC_2.34 not found` 直接起不来。
if [ "${UVP_INSIDE_BUILDER:-0}" != "1" ]; then
  # ⛔ `|| true` 不能省：macOS 的 getconf 不认识 GNU_LIBC_VERSION（同 build-redis.sh）。
  host_glibc="$(getconf GNU_LIBC_VERSION 2>/dev/null | awk '{print $2}' || true)"
  need_builder=0
  if [ "$FORCE_BUILDER" = "1" ]; then
    need_builder=1
  elif [ -z "$host_glibc" ] || [ "$(ver_gt "$host_glibc" "$GLIBC_MAX")" = "1" ]; then
    need_builder=1
  fi

  if [ "$need_builder" != "1" ]; then
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
      fail "本机进不了 glibc ≤ ${GLIBC_MAX} 的构建环境。
      请在装了 docker / podman 的机器上跑本脚本（会自动进 ${BUILDER_IMAGE} 容器），
      或直接在那台 glibc ≤ ${GLIBC_MAX} 的 Linux 构建机上跑（设 UVP_INSIDE_BUILDER=1）。"
    fi

    if ! "$runtime" image inspect "$BUILDER_IMAGE" >/dev/null 2>&1; then
      fail "找不到底座镜像 ${BUILDER_IMAGE}（$runtime）。
      构建机上通常已有（ZLM / redis 就是这么编的）。"
    fi

    rel="${DEST#"$REPO_ROOT"/}"
    run_user="${UVP_DOCKER_USER:-$(id -u):$(id -g)}"
    log "进入容器 ${BUILDER_IMAGE} 编译"
    # shellcheck disable=SC2086
    exec "$runtime" run --rm -i \
      --platform "$BUILD_PLATFORM" \
      -u "$run_user" \
      -v "$REPO_ROOT":/src -w /src \
      -e UVP_INSIDE_BUILDER=1 \
      -e UVP_FFMPEG_VERSION="$FFMPEG_VERSION" \
      -e UVP_GLIBC_MAX="$GLIBC_MAX" \
      -e UVP_FFMPEG_DYNAMIC="${UVP_FFMPEG_DYNAMIC:-}" \
      -e UVP_FFMPEG_WORKDIR="$SRC_ROOT" \
      ${SRC_PROVIDED:+-e UVP_SRCDIR=/src/.uvp-src} \
      "$BUILDER_IMAGE" \
      bash /src/deploy/standalone/build-ffmpeg.sh "/src/$rel"
  fi
fi

# ---------------------------------------------------------------- 依赖 ----
for c in gcc make tar xz curl; do
  command -v "$c" >/dev/null 2>&1 || fail "构建环境缺 $c"
done

# ---------------------------------------------------------------- 源码 ----
mkdir -p "$SRC_ROOT"
cd "$SRC_ROOT"

TARBALL="ffmpeg-${FFMPEG_VERSION}.tar.xz"
SRCDIR="ffmpeg-${FFMPEG_VERSION}"
if [ -n "$SRC_PROVIDED" ]; then
  [ -d "$SRC_PROVIDED" ] || fail "UVP_SRCDIR=$SRC_PROVIDED 不是目录"
  [ -f "$SRC_PROVIDED/$TARBALL" ] || fail "预置源码目录里缺 $TARBALL"
  [ -f "$TARBALL" ] || cp "$SRC_PROVIDED/$TARBALL" .
  log "使用预置源码 $SRC_PROVIDED/$TARBALL"
elif [ -f "$TARBALL" ] && tar -tJf "$TARBALL" >/dev/null 2>&1; then
  log "已有 $TARBALL，跳过下载"
else
  ok=0
  for url in \
    "https://ffmpeg.org/releases/${TARBALL}" \
    "https://github.com/FFmpeg/FFmpeg/archive/refs/tags/n${FFMPEG_VERSION}.tar.gz" \
    "https://www.ffmpeg.org/releases/${TARBALL}"; do
    log "下载 $url"
    if curl -fsSL --max-time 600 -o "$TARBALL" "$url" 2>/dev/null \
       && tar -tJf "$TARBALL" >/dev/null 2>&1; then ok=1; break; fi
    # GitHub tag 包是 .tar.gz，单独兜一次
    case "$url" in
      *.tar.gz)
        if tar -tzf "$TARBALL" >/dev/null 2>&1; then
          rm -rf "FFmpeg-n${FFMPEG_VERSION}"
          tar -xzf "$TARBALL"
          mv "FFmpeg-n${FFMPEG_VERSION}" "$SRCDIR"
          tar -cJf "$TARBALL" --transform "s,^${SRCDIR},${SRCDIR}," "$SRCDIR" 2>/dev/null \
            || { log "GitHub 源重打包失败，换下一个源"; rm -rf "$SRCDIR" "$TARBALL"; continue; }
          ok=1; break
        fi
        ;;
    esac
    rm -f "$TARBALL"
  done
  [ "$ok" = "1" ] || fail "所有源都下载失败：$TARBALL
      （离线构建时可用 UVP_SRCDIR 指一个预先放好该 tar 包的目录）"
fi

[ -d "$SRCDIR" ] || tar -xJf "$TARBALL"
[ -d "$SRCDIR" ] || fail "解压后没看到目录 $SRCDIR"

# ---------------------------------------------------------------- 配置 ----
# ⭐ --disable-autodetect 是静态化的关键：默认 configure 会自动探测系统里的
#   zlib / libxcb / vaapi / vdpau 等——那些库在 CentOS 7 底座上只有动态 .so，
#   一旦链进去，"静态 ffmpeg"就退化成"半静态"，拷到目标机上照样缺库起不来。
#   关掉自动探测 = 只用 ffmpeg 自带的原生编解码器/协议（h264/hevc 解码、
#   mjpeg 编码、rtp/rtsp/tcp/udp/http 协议等全在内），ZLM 的 getSnap
#   （-i rtsp://... -f mjpeg -frames:v 1）与常规转推模板都够用。
#   ⚠️ 代价：不带 libx264/x265 等外编编码器（H.264 **编码**只有原生 encoder，
#   质量一般）。要补的话得在容器里先编 x264 静态库再 --enable-libx264 --enable-gpl。
LINK_MODE="static"
if [ "${UVP_FFMPEG_DYNAMIC:-0}" = "1" ]; then
  LINK_MODE="dynamic"
elif [ ! -e /usr/lib64/libc.a ] && [ ! -e /usr/lib/libc.a ]; then
  log "容器里没有 glibc-static（libc.a），退化为动态链（仍 glibc ≤ ${GLIBC_MAX}）"
  LINK_MODE="dynamic"
fi

X86ASM_FLAG=""
if ! command -v nasm >/dev/null 2>&1 && ! command -v yasm >/dev/null 2>&1; then
  # 无汇编器只影响 x86 上的解码速度，不影响功能；抓拍场景无所谓。
  X86ASM_FLAG="--disable-x86asm"
  log "无 nasm/yasm，加 ${X86ASM_FLAG}（仅损失解码速度，不影响功能）"
fi

cd "$SRCDIR"
CONFIGURE_FLAGS=(
  --disable-debug --disable-doc
  --disable-htmlpages --disable-manpages --disable-podpages --disable-txtpages
  --disable-autodetect
  $X86ASM_FLAG
)
if [ "$LINK_MODE" = "static" ]; then
  CONFIGURE_FLAGS+=(--disable-shared --enable-static)
fi

# ------------------------------------------------------------ 精简白名单 ----
# ⛔⛔ 为什么要有精简档（2026-10-21，1.1.12 发布被 Gitee 挡下来才做）：
#   Gitee 发行版**单个附件上限 100MiB（104857600 字节）**，而 1.1.12 三个包全越线
#   （110.4 / 109.0 / 104.8MB）。体积不是代码涨的，是 `9e23be98` 起把**完整版 ffmpeg**
#   塞进包里——Windows 那个 ffmpeg.exe 单个就 105MB，Linux 版 20.9/32.8MB。
#   精简后：amd64 20.9→6.5MB、arm64 32.8→5.5MB，功能经真 RTSP 抓拍验证不变。
#
# ⭐ 白名单内容与「为什么是这些组件」全部写在 ffmpeg-minimal-flags.conf 里，
#   Windows 交叉编译（deploy/standalone-windows/build-ffmpeg-win.sh）读的是**同一份**，
#   ⛔ 不要在脚本里再维护第二份清单。
#   UVP_FFMPEG_MINIMAL=0 可退回完整档（排障用，别拿去出包）。
MINIMAL_FLAGS="$SCRIPT_DIR/ffmpeg-minimal-flags.conf"
if [ "${UVP_FFMPEG_MINIMAL:-1}" = "1" ]; then
  [ -f "$MINIMAL_FLAGS" ] || fail "找不到精简白名单 $MINIMAL_FLAGS"
  log "精简档：只保留 ZLM 抓拍/转推用到的组件（UVP_FFMPEG_MINIMAL=0 可退回完整档）"
  while IFS= read -r line; do
    line="${line%%#*}"                     # 去行尾注释
    line="$(printf '%s' "$line" | tr -d '[:space:]')"
    [ -n "$line" ] || continue
    CONFIGURE_FLAGS+=("$line")
  done < "$MINIMAL_FLAGS"
else
  log "完整档（UVP_FFMPEG_MINIMAL=0）：体积会到 21MB+，别用这个出交付包"
fi

log "configure（${LINK_MODE}）"
./configure "${CONFIGURE_FLAGS[@]}" > "$SRC_ROOT/configure.log" 2>&1 || {
  tail -30 "$SRC_ROOT/configure.log" >&2
  fail "configure 失败（详见 $SRC_ROOT/configure.log）"
}

# ---------------------------------------------------------------- 编译 ----
log "make -j$(nproc 2>/dev/null || echo 2)（约 5-10 分钟）"
make -j"$(nproc 2>/dev/null || echo 2)" > "$SRC_ROOT/make.log" 2>&1 || {
  tail -30 "$SRC_ROOT/make.log" >&2
  fail "编译失败（详见 $SRC_ROOT/make.log）"
}

[ -x ./ffmpeg ] || fail "产物 ./ffmpeg 不存在"

# ---------------------------------------------------------------- 装配 ----
mkdir -p "$DEST"
cp ./ffmpeg "$DEST/ffmpeg"
chmod 0755 "$DEST/ffmpeg"

# 终检：真跑一次 -version（比 file 更硬——链接残缺当场暴露），并记录形态。
OUT="$(./ffmpeg -version 2>&1 | head -1)" || fail "ffmpeg -version 都跑不起来，产物不可用"
log "产物自检：$OUT"
if [ "$LINK_MODE" = "static" ] && file "$DEST/ffmpeg" | grep -q 'statically linked'; then
  log "形态：静态链接"
else
  # ⚠️ 两种情况都会走到这里，**都不能算失败**：
  #   ① 明确选了 dynamic（容器没有 glibc-static）；
  #   ② 选了 static（有 libc.a）但 ffmpeg 的 configure 并不会替我们传 `-static`，
  #      编出来仍是动态 —— aarch64 的 openeuler 底座就是这样（2026-10-21 实测）。
  #      包内 nginx / redis 也是动态＋glibc≤2.28，目标机自带 libc 即可，形态一致。
  # ⇒ 判据统一落到「可移植性门禁」：无动态 OpenSSL + GLIBC 不超过上限。
  [ "$LINK_MODE" = "static" ] && log "静态未成立（configure 不带 -static），改按动态形态过可移植性门禁"
  bash "$SCRIPT_DIR/check-portable-elf.sh" \
    --glibc-max "$GLIBC_MAX" \
    "$DEST/ffmpeg" \
    || fail "ffmpeg 没过可移植性门禁（不能随包分发）"
  LINK_MODE="dynamic"
fi

cat > "$DEST/ffmpeg-buildinfo.txt" <<EOF
FFMPEG_VERSION=$FFMPEG_VERSION
LINK_MODE=$LINK_MODE
CONFIGURE_FLAGS=${CONFIGURE_FLAGS[*]}
BUILDER_IMAGE=${UVP_INSIDE_BUILDER:+$BUILDER_IMAGE}
BUILD_OS=$(uname -srm)
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%S%z)
GLIBC_MAX_REQUIRED=$GLIBC_MAX
EOF

log "完成：$DEST/ffmpeg（$(du -h "$DEST/ffmpeg" | cut -f1)）"
