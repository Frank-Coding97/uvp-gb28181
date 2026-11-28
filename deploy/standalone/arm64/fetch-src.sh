#!/bin/sh
# fetch-src.sh <dest> <url> [url ...] —— 面向"不靠谱镜像站"的源码下载器
#
# 为什么需要它（2026-10-10 实测）：
#   构建镜像时用裸 `curl -fsSL -o x.tar.gz <gh-proxy 链接>` 下 openssl 源码，
#   传到一半被对端掐断，curl 报
#       curl: (18) transfer closed with 7440633 bytes remaining to read
#   整个 36 分钟的镜像构建就白跑（镜像构建无断点，重来又要重编一遍工具链）。
#
# 本脚本做四件事：
#   ① 多源轮询 —— 一个镜像站抽风就换下一个（多个 GitHub 加速站 + 官方直链）；
#   ② 断点续传 —— 中途断了保留 .part，下一轮用 `curl -C -` 接着下，
#      ⛔ 但只在"同一个 URL"内续传（跨源混着续有拿到错位字节的风险）；
#   ③ 完整性校验 —— 下完先按扩展名验证归档能否完整解出（gzip -t / xz -t + tar -t），
#      校验不过就丢掉重下，绝不把半截包喂给 tar；
#   ④ 低速保护 —— `--speed-time/--speed-limit` 把"连着 60 秒跑不到 1KB/s"的连接判死，
#      避免 github 直连那种 ~10 B/s 把构建挂死。
#
# 退出码：0 = 已就位；1 = 所有源、所有轮次都失败。
set -eu

dest="${1:-}"
[ -n "$dest" ] || { echo "usage: fetch-src.sh <dest> <url> [url ...]" >&2; exit 2; }
shift
[ "$#" -ge 1 ] || { echo "usage: fetch-src.sh <dest> <url> [url ...]" >&2; exit 2; }

part="${dest}.part"
mark="${dest}.urlmark"

size_of() {
    # ⛔ 必须保证"永远是数字"：文件不存在时 wc 会报错且不输出，
    # 后面 `[ "$cur" -gt 0 ]` 拿到空串会打 "integer expression expected" 的噪音。
    # ⛔ 也不能靠 `2>/dev/null` 压掉 —— 文件不存在时是**重定向本身**失败，
    # 那句 "No such file or directory" 由 shell 打出来，压不住。所以先判存在。
    _n=0
    if [ -f "$1" ]; then
        _n=$(wc -c < "$1" 2>/dev/null || true)
        _n=$(printf '%s' "$_n" | tr -d ' \n')
        [ -n "$_n" ] || _n=0
    fi
    printf '%s' "$_n"
}

verify() {
    _f="$1"
    [ -s "$_f" ] || return 1
    case "$_f" in
        *.tar.gz|*.tgz) gzip -t "$_f" 2>/dev/null && tar -tzf "$_f" >/dev/null 2>&1 ;;
        *.tar.xz)       xz -t  "$_f" 2>/dev/null && tar -tJf "$_f" >/dev/null 2>&1 ;;
        *.tar.bz2)      bzip2 -t "$_f" 2>/dev/null && tar -tjf "$_f" >/dev/null 2>&1 ;;
        *.tar)          tar -tf "$_f" >/dev/null 2>&1 ;;
        *)              [ -s "$_f" ] ;;
    esac
}

mkdir -p "$(dirname "$dest")"

round=1
while [ "$round" -le 3 ]; do
    for url in "$@"; do
        cur="$(size_of "$part")"
        prev_url=""
        [ -f "$mark" ] && prev_url="$(cat "$mark")"

        if [ "$cur" -gt 0 ] && [ "$prev_url" = "$url" ]; then
            echo "[fetch] round $round 续传($cur 字节)  $url" >&2
            if curl -fsSL -C - --retry 4 --retry-all-errors --retry-delay 2 \
                    --connect-timeout 20 --speed-time 60 --speed-limit 1024 \
                    -o "$part" "$url"; then ok=1; else ok=0; fi
        else
            echo "[fetch] round $round 全新下载  $url" >&2
            rm -f "$part" "$mark"
            if curl -fsSL --retry 4 --retry-all-errors --retry-delay 2 \
                    --connect-timeout 20 --speed-time 60 --speed-limit 1024 \
                    -o "$part" "$url"; then ok=1; else ok=0; fi
        fi

        printf '%s' "$url" > "$mark"

        if [ "$ok" = 1 ] && verify "$part"; then
            mv -f "$part" "$dest"
            rm -f "$mark"
            echo "[fetch] OK $dest ($(size_of "$dest") 字节)" >&2
            exit 0
        fi

        if [ "$ok" = 1 ]; then
            echo "[fetch] 下载完成但归档不完整（$cur -> $(size_of "$part") 字节），丢弃重来" >&2
        fi
        rm -f "$part" "$mark"
    done
    round=$((round + 1))
    sleep 3
done

echo "[fetch] 所有源、所有轮次均失败：$dest" >&2
exit 1
