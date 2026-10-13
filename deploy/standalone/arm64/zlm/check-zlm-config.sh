#!/usr/bin/env bash
# 校验 ZLM 的 cmake configure 结论：交付需要的能力开关是否**真的**启用。
#
# 用法：check-zlm-config.sh <build-dir> [<configure-log>]
# 退出码：0 = 全部启用；1 = 有关键开关被静默降级；2 = 用法/前置条件错
#
# 为什么需要它：ZLM 找不到 ffmpeg/srtp 时**只打 WARNING 就把开关关掉**，
# 编译照样成功，产出一个功能缺失的 MediaServer。"编过了"不等于"编对了"。
#
# ⛔ 判据取**构建产物**，不要抓日志文案 —— 这条是踩过坑才改的：
#    ZLM 的 ffmpeg 段落有两条查找路径，日志形态完全不同：
#      · 走 pkg-config 时 message 打的是**模块名**       → `found library: avutil`
#      · 走 find_library(HINTS) 时打的是**绝对路径**     → `found library: /opt/uvp/deps/lib/libavutil.so`
#    第一版按 `found library: .*libavutil` 断言，于是 pkg-config 命中时被判成
#    "未启用"，把一次完全正常的构建判红了。而 `ENABLE_FFMPEG defined` 这种文案
#    根本不存在（ffmpeg 成功时没有任何同名标记行），照抄会得到恒假断言。
#    真正稳定的判据是 cmake 自己写进 flags.make 的 -D 宏：
#    开关一旦生效，就一定以 -DENABLE_XXX 出现在编译命令行里。
#
# 反向验证方式（改这个脚本后必须做）：用 -DENABLE_FFMPEG=false -DENABLE_WEBRTC=false
# 重新 configure 一个空 build 目录，本脚本必须红，且点名的正是这两项。
set -Eeuo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
    printf 'usage: %s <build-dir> [<configure-log>]\n' "$0" >&2
    exit 2
fi
build_dir=$1
log=${2:-}

[[ -d "$build_dir" ]] || { printf 'ERROR: %s 不是目录\n' "$build_dir" >&2; exit 2; }

# --- 判据一（主）：flags.make 里的 -DENABLE_* -------------------------------
defs=""
while IFS= read -r f; do
    defs="$defs$(grep -ohE -- '-DENABLE_[A-Z0-9_]+' "$f" 2>/dev/null || true)"$'\n'
done < <(find "$build_dir" -name flags.make -type f 2>/dev/null)
defs=$(printf '%s' "$defs" | grep -oE '^\-DENABLE_[A-Z0-9_]+' | sort -u || true)

# --- 判据二（兜底）：CMakeCache 的 MK_COMPILE_DEFINITIONS -------------------
# configure 只跑到一半时 flags.make 可能还没生成，但 cache 已经写好了。
if [[ -z "$defs" && -f "$build_dir/CMakeCache.txt" ]]; then
    defs=$(grep -E '^MK_COMPILE_DEFINITIONS' "$build_dir/CMakeCache.txt" 2>/dev/null \
        | tr ';' '\n' | grep -oE '^ENABLE_[A-Z0-9_]+' | sed 's/^/-D/' | sort -u || true)
fi
if [[ -z "$defs" ]]; then
    printf 'ERROR: 在 %s 里找不到任何 -DENABLE_* 证据（configure 没跑完？）\n' "$build_dir" >&2
    exit 2
fi
enabled=$(printf '%s\n' "$defs" | sed 's/^-D//' | sort -u | tr '\n' ' ')
printf '已启用的开关: %s\n' "$enabled"

# --- 必需项：显式开启的四个能力开关 ----------------------------------------
# FFMPEG  → addFFmpegSource / getSnap（媒体工作台的 FFmpeg 拉流面板是平台一等功能）
# OPENSSL → https/wss/rtsps/rtmps 与 webrtc 的 DTLS
# WEBRTC  → webrtc 播放（依赖 OpenSSL + libsrtp2）
# SRT     → srt 推拉流
missing=""
for required in ENABLE_FFMPEG ENABLE_OPENSSL ENABLE_WEBRTC ENABLE_SRT; do
    # ⛔ 模式必须带 -D：defs 里的行是 "-DENABLE_FFMPEG"，写成 "-${required}"
    #    （即 "-ENABLE_FFMPEG"）是**恒假**断言 —— 每一项都会被判成未启用。
    if ! printf '%s\n' "$defs" | grep -qx -- "-D${required}"; then
        missing="$missing $required"
    fi
done
if [[ -n "$missing" ]]; then
    printf 'ERROR: 以下开关未启用：%s\n' "$missing" >&2
    if [[ -n "$log" && -f "$log" ]]; then
        grep -iE 'warning|could not|not found|未找到' "$log" | sed 's/^/      /' >&2 || true
    else
        printf '      （未提供 configure 日志，无法附上原始告警）\n' >&2
    fi
    exit 1
fi

# --- 默认开启项：被关掉不阻断交付，但要让人看见 ---------------------------
note=""
for soft in ENABLE_HLS ENABLE_MP4 ENABLE_RTPPROXY; do
    if ! printf '%s\n' "$defs" | grep -qx -- "-D${soft}"; then
        note="$note $soft"
    fi
done
if [[ -n "$note" ]]; then
    printf 'note: 以下默认开启项被关掉了（平台会少对应能力，请确认是有意为之）：%s\n' "$note" >&2
fi

printf 'OK: ENABLE_FFMPEG / ENABLE_OPENSSL / ENABLE_WEBRTC / ENABLE_SRT 均已启用\n'
