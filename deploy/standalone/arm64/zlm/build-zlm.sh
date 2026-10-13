#!/usr/bin/env bash
# 在 glibc 2.17 构建容器里编译 UVP 二开 ZLMediaKit，产出可交付的 MediaServer。
#
# 用法：
#   build-zlm.sh <zlm-source-dir> <output-dir>
#
#   <zlm-source-dir>  ZLMediaKit 源码树（上游 pin + UVP 补丁已应用、子模块已拉取）
#                     —— 见 fetch-zlm-source.sh
#   <output-dir>      产物落地目录
#
# 环境变量：
#   UVP_BUILDER_IMAGE  构建镜像，默认 uvp/linux-builder:glibc217
#   UVP_JOBS           并行度，默认 nproc（macOS 退回 sysctl hw.ncpu）
#
# ⛔ 必须跑在 220 这类有 docker 的构建机上。产物只需要 libc/libm/libpthread
#    （全部在 CentOS 7 基础库里），但前提是**在这场容器里编** —— 在宿主
#    （Ubuntu 24.04 / glibc 2.39）直接编出来的东西跑不动 CentOS 7。
set -Eeuo pipefail

if [[ $# -ne 2 ]]; then
    printf 'usage: %s <zlm-source-dir> <output-dir>\n' "$0" >&2
    exit 2
fi

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SRC=$(cd "$1" && pwd)
OUT=$2
IMAGE=${UVP_BUILDER_IMAGE:-uvp/linux-builder:glibc217}
# ⛔ macOS 没有 nproc ⇒ 退回 sysctl；两者都缺就 4。
JOBS=${UVP_JOBS:-$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)}

# ⛔⛔ macOS 的 mktemp 默认落在 /var/folders/...（不在 $HOME 下），
#   容器挂载不进去会报 `exec format error` 或挂到空目录。仅在 macOS 上钉到 $HOME 下。
if [ "$(uname -s)" = "Darwin" ]; then
  export TMPDIR="${TMPDIR_FORCED:-$HOME/.uvp-build-tmp}"
fi
[ -n "${TMPDIR:-}" ] && mkdir -p "$TMPDIR"

[[ -f "$SRC/CMakeLists.txt" ]] || { printf 'ERROR: %s 不是 ZLMediaKit 源码树\n' "$SRC" >&2; exit 2; }

mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)

# 在副本里编：ZLM 的 cmake 会把 build/ 与 release/ 写进源码树，
# 直接编会污染那份可复用的干净源码。
work=$(mktemp -d "${TMPDIR:-/tmp}/uvp-zlm-build.XXXXXX")
trap 'rm -rf "$work"' EXIT
printf '==> 复制源码到 %s\n' "$work/src"
rsync -a --exclude='.git/' --exclude='build/' --exclude='release/' "$SRC/" "$work/src/"

# 记下源码身份，便于产物溯源
{
    printf 'ZLM_SOURCE=%s\n' "$SRC"
    if [[ -d "$SRC/.git" ]]; then
        printf 'ZLM_COMMIT=%s\n' "$(git -C "$SRC" rev-parse HEAD)"
        printf 'ZLM_COMMIT_SUBJECT=%s\n' "$(git -C "$SRC" log -1 --format=%s)"
    fi
    printf 'BUILDER_IMAGE=%s\n' "$IMAGE"
    printf 'BUILD_TIME=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%S%z)"
} > "$OUT/zlm-buildinfo.txt"

# --- 构建参数 --------------------------------------------------------------
# ENABLE_PYTHON=OFF 是相对现网开发镜像的**唯一**功能减法：那套 pymkui 是 ZLM
# 自带的管理面板，平台侧全仓零引用，留着只会平白引入 libpython 依赖。
# 其余开关刻意与开发镜像一致（WEBRTC/FFMPEG/OPENSSL/SRT/MP4/HLS/RTPPROXY 全开，
# TESTS/API 关），避免交付线与开发环境行为漂移。
#
# LDFLAGS 里的 $ORIGIN 相对 rpath 覆盖两种布局：
#   $ORIGIN/lib     — ZLM 自身的 release/linux/<mode>/lib
#   $ORIGIN/../lib  — 交付包布局 bin/ 与 lib/ 并列
#
# -ldl -lpthread：静态 libssl/libcrypto/libsrtp2 里的 dlopen/dlerror 在 CentOS 7
# （glibc 2.17）上属于 libdl，glibc 2.34 才并入 libc。CMake 的 FindOpenSSL 本会
# 带上它们，这里显式再兜一层，别让链接姿势依赖 CMake 版本的内部细节。
LDFLAGS_EXTRA='-Wl,-rpath,$ORIGIN/lib -Wl,-rpath,$ORIGIN/../lib -ldl -lpthread'

printf '==> 容器内编译（%s 核）\n' "$JOBS"
docker run --rm \
    --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -e FFMPEG_PATH_ROOT=/opt/uvp/deps \
    -e "LDFLAGS=-static-libstdc++ -static-libgcc ${LDFLAGS_EXTRA}" \
    -v "$work:/build" \
    -v "$SCRIPT_DIR:/scripts:ro" \
    -w /build/src \
    "$IMAGE" \
    bash -c '
        set -euo pipefail
        mkdir -p build && cd build
        cmake -DCMAKE_BUILD_TYPE=Release \
              -DFFMPEG_PATH_ROOT="$FFMPEG_PATH_ROOT" \
              -DENABLE_PYTHON=false \
              -DENABLE_WEBRTC=true \
              -DENABLE_FFMPEG=true \
              -DENABLE_TESTS=false \
              -DENABLE_API=false \
              .. 2>&1 | tee /build/cmake-configure.log | tail -25

        # ⛔ 关键开关被静默降级（比如 ffmpeg/srtp 没找到）会让产物功能缺失，
        #    但编译照样成功。所以"编过了"不等于"编对了"，把 configure 的结论当断言查。
        #    判据取构建产物（flags.make 的 -DENABLE_*），不抓日志文案；理由见脚本头。
        /scripts/check-zlm-config.sh /build/src/build /build/cmake-configure.log

        # ⛔⛔ 不许写成 `make ... 2>&1 | tail -15`：编译**失败**时真正有用的那几行
        #   （`error: ...`）会被尾部的 make 汇总与别的库的链接成功日志挤掉，
        #   看到的只剩 `make[2]: *** [Socket.cpp.o] Error 1` —— 完全不知道错在哪。
        #   2026-10-11 编 arm64 时就这么白瞎了一轮。
        #   全量留档；失败时把尾部与所有报错行都打出来（work 目录有 EXIT trap，别指望事后翻文件）。
        rc=0
        make -j'"$JOBS"' > /build/make.log 2>&1 || rc=$?
        if [[ ${rc:-0} -ne 0 ]]; then
            echo "=== make 失败（退出码 ${rc}），/build/make.log 尾部 80 行 ==="
            tail -80 /build/make.log || true
            echo "=== make.log 里的报错行 ==="
            grep -nE "error:|Error [0-9]" /build/make.log | head -40 || true
            exit 1
        fi
        tail -3 /build/make.log
    '

release_dir="$work/src/release/linux/Release"
[[ -x "$release_dir/MediaServer" ]] || { printf 'ERROR: 没产出 MediaServer\n' >&2; exit 1; }

printf '==> 收集产物\n'
install -m 0755 "$release_dir/MediaServer" "$OUT/MediaServer"
# www 是 ZLM 自带的管理页面（含 webrtc 播放器），平台不依赖但体积很小，随包带上
rm -rf "$OUT/www"
cp -a "$release_dir/www" "$OUT/www"

# libav* 是动态的，必须随包分发；libsrtp2 / libssl 已被静态链入，不该出现。
printf '==> 收集 MediaServer 的运行时依赖\n'
mkdir -p "$OUT/lib"
docker run --rm \
    --user "$(id -u):$(id -g)" -e HOME=/tmp \
    -e LD_LIBRARY_PATH=/opt/uvp/deps/lib \
    -v "$work:/build" \
    -v "$SCRIPT_DIR:/scripts:ro" \
    -w /build \
    "$IMAGE" \
    bash -c '/scripts/collect-runtime-libs.sh /build/src/release/linux/Release/MediaServer /build/out-libs'

if [[ -d "$work/out-libs" ]]; then
    cp -a "$work/out-libs/." "$OUT/lib/"
fi

printf '==> 产物清单\n'
# ⛔ 不用 `find -printf`（GNU 专有，macOS 的 find 没有）；改用可移植写法。
find "$OUT" -maxdepth 2 -type f -exec sh -c '
    for f; do printf "%10s  %s\n" "$(wc -c <"$f" | tr -d " ")" "$f"; done
' sh {} + | sort -k2

printf '完成：%s\n' "$OUT"
