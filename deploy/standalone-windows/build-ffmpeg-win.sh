#!/usr/bin/env bash
# 交叉编译随 Windows 包分发的 ffmpeg.exe（ZLM 通道抓拍 / FFmpeg 拉流源的执行器）。
#
# ⛔⛔ 背景（2026-10-21，1.1.12 发布被 Gitee 挡下来才做）：
#   Windows 包里的 ffmpeg.exe 之前是直接从 gyan.dev / BtbN 拷的**完整版**——
#   单个 105MB，光它就把 zip 顶到 104.8MB，正好卡在 Gitee 发行版
#   单附件上限（100MiB = 104857600 字节）边缘，任何小改动都会再越线。
#   这里用 mingw-w64 交叉编译一份**与 Linux 线同一份白名单**的精简版，
#   ⭐ 白名单在 deploy/standalone/ffmpeg-minimal-flags.conf（两条线共用一份，
#   ⛔ 不要在脚本里再维护第二份）。
#
# ⚠️ 为什么要在 Linux/macOS 上交叉编译而不是在 Windows 机上编：
#   52 那台机器只有运行时环境，没有 MSYS2/mingw 工具链；而 ffmpeg 对
#   `--target-os=mingw32 --cross-prefix=x86_64-w64-mingw32-` 的支持是成熟的，
#   产物是纯静态 PE，目标机不需要任何 DLL。
#
# 用法：build-ffmpeg-win.sh [输出文件]     默认 deploy/standalone-windows/ffmpeg.exe
#
# 环境变量：
#   UVP_MINGW_BIN    mingw 的 bin 目录（里面有 x86_64-w64-mingw32-gcc）。
#                    不给就在 PATH 里找；Homebrew 装的在
#                    /opt/homebrew/Cellar/mingw-w64/<ver>/bin
#   UVP_FFMPEG_VERSION  默认 7.1.1（与 Linux 线一致）
#   UVP_SRCDIR       预置源码目录（离线构建时给，内含 ffmpeg-<ver>.tar.xz）
#   UVP_FFMPEG_WORKDIR  默认 /tmp/uvp-ffmpeg-win-build
#   UVP_FFMPEG_MINIMAL  =0 退回完整档（体积 100MB+，**别拿去出包**）
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DEST="${1:-$SCRIPT_DIR/ffmpeg.exe}"
FFMPEG_VERSION="${UVP_FFMPEG_VERSION:-7.1.1}"
WORKDIR="${UVP_FFMPEG_WORKDIR:-/tmp/uvp-ffmpeg-win-build}"
SRC_PROVIDED="${UVP_SRCDIR:-}"
MINIMAL_FLAGS="$REPO_ROOT/deploy/standalone/ffmpeg-minimal-flags.conf"
CROSS=x86_64-w64-mingw32

log()  { printf '[ffmpeg-win] %s\n' "$*"; }
fail() { printf '[ffmpeg-win][ERROR] %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------- 工具链 ----
if [ -n "${UVP_MINGW_BIN:-}" ]; then
  export PATH="$UVP_MINGW_BIN:$PATH"
fi
command -v "$CROSS-gcc" >/dev/null 2>&1 || fail "找不到 $CROSS-gcc。
      macOS: brew install mingw-w64（若 Homebrew 沙箱被拒，可手工解 bottle 后设 UVP_MINGW_BIN）
      Debian/Ubuntu: apt install mingw-w64"
log "交叉编译器：$(command -v "$CROSS-gcc") → $("$CROSS-gcc" -dumpversion)"

for c in make tar xz curl; do
  command -v "$c" >/dev/null 2>&1 || fail "构建环境缺 $c"
done

# ---------------------------------------------------------------- 源码 ----
mkdir -p "$WORKDIR"
cd "$WORKDIR"
TARBALL="ffmpeg-${FFMPEG_VERSION}.tar.xz"
SRCDIR="ffmpeg-${FFMPEG_VERSION}"
if [ -n "$SRC_PROVIDED" ]; then
  [ -f "$SRC_PROVIDED/$TARBALL" ] || fail "预置源码目录里缺 $TARBALL"
  cp -f "$SRC_PROVIDED/$TARBALL" .
elif [ -f "$TARBALL" ] && tar -tJf "$TARBALL" >/dev/null 2>&1; then
  log "已有 $TARBALL，跳过下载"
else
  ok=0
  for url in "https://ffmpeg.org/releases/$TARBALL" "https://www.ffmpeg.org/releases/$TARBALL"; do
    log "下载 $url"
    if curl -fsSL --max-time 600 -o "$TARBALL" "$url" 2>/dev/null && tar -tJf "$TARBALL" >/dev/null 2>&1; then
      ok=1; break
    fi
    rm -f "$TARBALL"
  done
  [ "$ok" = "1" ] || fail "源码下载失败：$TARBALL（离线时用 UVP_SRCDIR 指预置目录）"
fi
[ -d "$SRCDIR" ] || tar -xJf "$TARBALL"
[ -d "$SRCDIR" ] || fail "解压后没看到目录 $SRCDIR"

# ---------------------------------------------------------------- 配置 ----
# ⭐ --disable-autodetect 是静态化的关键，与 Linux 线同一条理由：
#   不关掉它，configure 会去链宿主机（macOS/Debian）上的 zlib / sdl2 之类，
#   产物就退化成「半静态」，拷到客户机上照样起不来。
CONFIGURE_FLAGS=(
  --disable-debug --disable-doc
  --disable-htmlpages --disable-manpages --disable-podpages --disable-txtpages
  --disable-autodetect
  --enable-static --disable-shared
  --cross-prefix="$CROSS-"
  --target-os=mingw32
  --arch=x86_64
  --extra-ldflags=-static
)

if [ "${UVP_FFMPEG_MINIMAL:-1}" = "1" ]; then
  [ -f "$MINIMAL_FLAGS" ] || fail "找不到精简白名单 $MINIMAL_FLAGS"
  log "精简档：与 Linux 线共用 $(basename "$MINIMAL_FLAGS")"
  while IFS= read -r line; do
    line="${line%%#*}"
    line="$(printf '%s' "$line" | tr -d '[:space:]')"
    [ -n "$line" ] || continue
    CONFIGURE_FLAGS+=("$line")
  done < "$MINIMAL_FLAGS"
else
  log "完整档（UVP_FFMPEG_MINIMAL=0）：产物 100MB+，别拿去出包"
fi

cd "$SRCDIR"
log "configure"
./configure "${CONFIGURE_FLAGS[@]}" > "$WORKDIR/configure.log" 2>&1 || {
  tail -30 "$WORKDIR/configure.log" >&2
  fail "configure 失败（详见 $WORKDIR/configure.log）"
}

log "make -j$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)"
make -j"$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)" > "$WORKDIR/make.log" 2>&1 || {
  tail -30 "$WORKDIR/make.log" >&2
  fail "编译失败（详见 $WORKDIR/make.log）"
}

[ -x ./ffmpeg.exe ] || fail "产物 ./ffmpeg.exe 不存在"

# ---------------------------------------------------------------- 装配 ----
mkdir -p "$(dirname "$DEST")"
cp -f ./ffmpeg.exe "$DEST"
chmod 0755 "$DEST"

BYTES=$(wc -c < "$DEST" | tr -d ' ')
log "产物：$DEST（$((BYTES / 1024 / 1024)) MB）"

# 终检 1：必须是 PE32+ (console) x86-64，不能是别的架构
file "$DEST" | grep -q 'PE32+ executable (console) x86-64' \
  || fail "产物不是 x86-64 PE32+ 控制台程序：$(file -b "$DEST")"

# 终检 2：⛔ 不能依赖工具链自带的运行时 DLL——客户机上没有 libgcc_s_seh-1.dll
#   / libwinpthread-1.dll 这些，缺了就是「ffmpeg.exe 一闪而过、ZLM 日志里只有
#   execv failed」，正是当初抓拍静默不可用的形态。只允许系统自带的 DLL。
BAD_DLLS="$("$CROSS-objdump" -p "$DEST" 2>/dev/null \
  | awk '/DLL Name/{print $3}' \
  | grep -viE '^(KERNEL32|USER32|ADVAPI32|WS2_32|SHELL32|OLE32|GDI32|MSVCRT|bcrypt|secur32|SHLWAPI|COMDLG32|WINMM|PSAPI|IPHLPAPI|DNSAPI|CRYPT32|NTDLL|UCRTBASE|api-ms-.*)\.dll$' || true)"
if [ -n "$BAD_DLLS" ]; then
  fail "产物依赖了非系统 DLL（客户机不会有）：$BAD_DLLS"
fi
log "动态依赖检查通过：只有系统 DLL"

# 终检 3：⛔ 白名单漏组件不会在编译期报错，只会在现场静默失败
#   （ZLM 把 execv 失败吞成一行 642 字节日志，通道抓拍从此不返回 JPEG）。
#   这里按 configure 生成的宏**逐个点名核对**，缺一个就中止。
#   ⚠️ 不能在本机跑 ffmpeg.exe（Windows 二进制）来 `-encoders` 自检，
#      所以只能查 config_components.h / config.h 里的 CONFIG_* 宏。
REQUIRED_MACROS=(
  CONFIG_RTSP_PROTOCOL CONFIG_RTP_PROTOCOL CONFIG_TCP_PROTOCOL
  CONFIG_RTSP_DEMUXER CONFIG_RTP_DEMUXER CONFIG_SDP_DEMUXER
  CONFIG_H264_DEMUXER CONFIG_HEVC_DEMUXER CONFIG_FLV_DEMUXER
  CONFIG_MOV_DEMUXER CONFIG_MPEGTS_DEMUXER CONFIG_IMAGE2_DEMUXER
  CONFIG_H264_DECODER CONFIG_HEVC_DECODER CONFIG_MJPEG_DECODER CONFIG_AAC_DECODER
  CONFIG_H264_PARSER CONFIG_HEVC_PARSER CONFIG_AAC_PARSER
  CONFIG_MJPEG_ENCODER CONFIG_PNG_ENCODER CONFIG_AAC_ENCODER
  CONFIG_MJPEG_MUXER CONFIG_MP4_MUXER CONFIG_FLV_MUXER CONFIG_MPEGTS_MUXER CONFIG_SEGMENT_MUXER
  CONFIG_SCALE_FILTER CONFIG_FORMAT_FILTER CONFIG_ARESAMPLE_FILTER
  CONFIG_H264_MP4TOANNEXB_BSF CONFIG_HEVC_MP4TOANNEXB_BSF CONFIG_AAC_ADTSTOASC_BSF
)
MISSING=()
for macro in "${REQUIRED_MACROS[@]}"; do
  grep -rqE "^#define ${macro} 1" "$SRCDIR/config_components.h" "$SRCDIR/config.h" 2>/dev/null \
    || MISSING+=("$macro")
done
if [ ${#MISSING[@]} -gt 0 ]; then
  fail "精简白名单缺组件（宏未启用）：${MISSING[*]}
      改 deploy/standalone/ffmpeg-minimal-flags.conf 后重编，别硬发。"
fi
log "组件核对通过：抓拍/转推所需 ${#REQUIRED_MACROS[@]} 项组件齐全"

cat > "$(dirname "$DEST")/ffmpeg-buildinfo.txt" <<EOF
FFMPEG_VERSION=$FFMPEG_VERSION
TARGET_OS=mingw32/x86_64
LINK_MODE=static
CONFIGURE_FLAGS=${CONFIGURE_FLAGS[*]}
CROSS_GCC=$("$CROSS-gcc" -dumpversion)
BUILD_OS=$(uname -srm)
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%S%z)
EOF

log "完成"
