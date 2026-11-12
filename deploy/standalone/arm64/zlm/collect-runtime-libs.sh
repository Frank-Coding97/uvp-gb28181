#!/usr/bin/env bash
# 在构建容器**内部**运行：把某个原生产物的传递依赖里「不属于目标机基础库」
# 的那些共享库复制到指定目录，让交付包自带它们。
#
# 用法：collect-runtime-libs.sh <binary> <dest-dir>
#
# 为什么需要：ffmpeg 的 libav* 是动态链接的（见 builder/Dockerfile 里的取舍说明），
# 而 CentOS 7 基础库里没有 ffmpeg —— 不把这几个 .so 随包带过去，目标机上
# MediaServer 会直接 `error while loading shared libraries: libavcodec.so.60`。
#
# 与之配套的验收在 deploy/linux/verify-artifacts.sh：它拿 centos:7 的 ldconfig
# 清单当白名单，逐条核对依赖闭包，正是为了兜住这里漏掉的情况。
set -Eeuo pipefail

if [[ $# -ne 2 ]]; then
    printf 'usage: %s <binary> <dest-dir>\n' "$0" >&2
    exit 2
fi

BIN=$1
DEST=$2

[[ -f "$BIN" ]] || { printf 'ERROR: %s 不存在\n' "$BIN" >&2; exit 2; }
mkdir -p "$DEST"
DEST=$(cd "$DEST" && pwd)

# 目标机（CentOS 7）基础库里有的，不随包分发。
# 带 libstdc++/libgcc_s 是刻意的：本线一律 -static-libstdc++ -static-libgcc，
# 如果这里出现了它们，说明静态链接没生效 —— 让它漏到 verify-artifacts.sh
# 去报错，比在这里静默放行好。
is_base() {
    case "$1" in
        libc.so.*|libm.so.*|libpthread.so.*|libdl.so.*|librt.so.*|libz.so.*|ld-linux*) return 0 ;;
        *) return 1 ;;
    esac
}

needed_of() {
    LC_ALL=C readelf -d "$1" 2>/dev/null | sed -n 's/.*(NEEDED).*\[\(.*\)\].*/\1/p'
}

resolved_path_of() {
    ldd "$1" 2>/dev/null | awk -v s="$2" '$1 == s { print $3; exit }'
}

copied=0
collect() {
    local bin=$1 need path base
    while IFS= read -r need; do
        [[ -n "$need" ]] || continue
        is_base "$need" && continue
        path=$(resolved_path_of "$bin" "$need")
        if [[ -z "$path" || ! -f "$path" ]]; then
            printf 'WARN: %s 需要 %s，但 ldd 没解出实路径（可能 LD_LIBRARY_PATH 不全）\n' \
                "$(basename "$bin")" "$need" >&2
            continue
        fi
        base=$(basename "$path")
        if [[ ! -e "$DEST/$base" ]]; then
            cp -L "$path" "$DEST/"
            printf '      + %s\n' "$base"
            copied=$((copied + 1))
            collect "$path"
        fi
    done < <(needed_of "$bin")
}

printf '==> 收集 %s 的运行时依赖 → %s\n' "$(basename "$BIN")" "$DEST"
collect "$BIN"

if [[ "$copied" -eq 0 ]]; then
    printf '     （没有额外依赖：产物只用到目标机基础库）\n'
fi
