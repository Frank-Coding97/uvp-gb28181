#!/usr/bin/env bash
# 一键产出随包的三个 aarch64 原生二进制：redis / nginx / ZLM。
#
# ⛔ 为什么需要它（2026-10-10）：
#   AMD64 绿包的构建路径（build-nginx.sh / build-redis.sh + build-zlm.sh）本来就在，
#   但它们默认目标是 linux/amd64，出 ARM 包时必须**三件事同时换**，少一件就出废包：
#     ① 底座镜像换成 aarch64 版（见同目录 Dockerfile.linux-builder-arm64）；
#     ② 容器平台参数与镜像架构一致（不一致的表现是 `exec format error`）；
#     ③ 可移植性门禁的 glibc 上限从 2.17 抬到 2.28（openEuler 20.03 底座就是 2.28）。
#   ③ 最容易漏，而且漏了的症状是自检报「要 GLIBC_2.28，上限 2.17」——看着像编错了，
#   其实是门禁的上限没跟着底座改。
#
# 用法：
#   deploy/standalone/arm64/build-native-arm64.sh <repo-root> <zlm-source-dir>
#
# 环境变量：
#   UVP_ARM64_IMAGE   aarch64 底座镜像，默认 uvp/linux-builder:openeuler2003-arm64
#   UVP_GLIBC_MAX     可移植性门禁的 glibc 上限，默认 2.28
#
# ⛔ 必须在能跑 arm64 容器的机器上执行：aarch64 主机是原生速度；
#   x86_64 构建机要先用 `docker run --privileged --rm tonistiigi/binfmt --install arm64`
#   注册 binfmt（qemu 用户态模拟，慢 5~10 倍，但结论一致）。
set -Eeuo pipefail

REPO="${1:?用法: $0 <repo-root> <zlm-source-dir>}"
ZLM_SRC="${2:?用法: $0 <repo-root> <zlm-source-dir>}"
IMG="${UVP_ARM64_IMAGE:-uvp/linux-builder:openeuler2003-arm64}"
GLIBC_MAX="${UVP_GLIBC_MAX:-2.28}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$REPO" && pwd)"
ZLM_SRC="$(cd "$ZLM_SRC" && pwd)"

log()  { printf '[arm64-native] %s\n' "$*"; }
fail() { printf '[arm64-native][ERROR] %s\n' "$*" >&2; exit 1; }

# 并行度：Linux 用 nproc，macOS 没有 nproc 改问 sysctl（两者都缺就退回 4）。
jobs_count() {
  nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4
}

# ⛔ macOS 的 mktemp 默认落在 /var/folders/...（不在用户目录下），容器挂不进去；
#   仅在 macOS 上把临时目录钉到 $HOME 下，Linux 构建机保持系统默认。
if [ "$(uname -s)" = "Darwin" ]; then
  export TMPDIR="${TMPDIR_FORCED:-$HOME/.uvp-build-tmp}"
fi
[ -n "${TMPDIR:-}" ] && mkdir -p "$TMPDIR"

command -v docker >/dev/null 2>&1 || fail "缺 docker（原生二进制必须在容器里编，理由见 build-nginx.sh 头部）"
docker image inspect "$IMG" >/dev/null 2>&1 \
  || fail "找不到底座镜像 $IMG —— 先构建它：
      docker build --platform linux/arm64 \\
        -f deploy/standalone/arm64/Dockerfile.linux-builder-arm64 \\
        -t $IMG deploy/standalone/arm64/"

img_arch="$(docker image inspect "$IMG" --format '{{.Architecture}}')"
[ "$img_arch" = "arm64" ] || fail "底座镜像 $IMG 的架构是 $img_arch，不是 arm64（镜像名与内容对不上）"

export UVP_BUILD_PLATFORM=linux/arm64
export UVP_LINUX_BUILDER_IMAGE="$IMG"
export UVP_GLIBC_MAX="$GLIBC_MAX"

log "① redis（BUILD_TLS=no，无 OpenSSL 依赖）"
bash "$REPO/deploy/standalone/build-redis.sh" "$REPO/deploy/standalone/bin"

log "② nginx（静态链 OpenSSL/pcre2/zlib）"
bash "$REPO/deploy/standalone/build-nginx.sh" "$REPO/deploy/standalone/bin/nginx"

log "③ ZLM（二开版，含 libav* 运行时闭包）"
out="$(mktemp -d)"
UVP_BUILDER_IMAGE="$IMG" UVP_JOBS="${UVP_JOBS:-$(jobs_count)}" \
  bash "$SCRIPT_DIR/zlm/build-zlm.sh" "$ZLM_SRC" "$out"

# ⛔⛔ 用"覆盖拷贝 + 只删陈旧条目"，**不要 `rm -rf "$ZLM_DIR"` 整目录重建**：
#   ① bin/zlm 里那份 config.ini（ZLM 真实运行配置：端口/secret/hook 全在里面）
#      **是本地手工维护的，build-zlm.sh 的产物里没有它**。整目录删掉再拷入，
#      一旦中途出岔子就把配置丢了；而且丢了的症状是出包在
#      `[ -f "$ZLM_DIR/config.ini" ] || fail "缺少 ZLM 配置"` 处失败，离现场很远。
#   ② "整目录删除"属于**批量删除**（本目录 55 个文件），会被批量删除保护直接拦下——
#      2026-10-11 实测报 `[SAFE_DELETE_BULK_CONFIRM_REQUIRED] count=55 threshold=50`
#      并让脚本当场中断，编译成果白白停在临时目录里。
#   ③ 覆盖拷贝是幂等的：同路径文件直接盖掉，语义上等价于"先清空再拷入"，
#      差别只是不会误删产物里没有的手工文件。
ZLM_DIR="$REPO/deploy/standalone/bin/zlm"
mkdir -p "$ZLM_DIR"
cp -a "$out/." "$ZLM_DIR/"

# 陈旧条目 = 目标里有、新产物里没有的（例如上游自带的悬空 `www/webassist/.git` 指针文件）。
# ⛔ 用"按行遍历代替数组"：macOS 自带 bash 3.2，`"${arr[@]}"` 在空数组下配 `set -u` 会报错。
# ⛔ 不走 `find -delete`：要先把最深层的列出来（`sort -r`），否则父目录先没了子项就删不掉。
stale="$(cd "$ZLM_DIR" && find . -mindepth 1 | while IFS= read -r rel; do
           [ "$rel" = "./config.ini" ] && continue
           [ -e "$out/$rel" ] || printf '%s\n' "$rel"
         done | sort -r)"
if [ -n "$stale" ]; then
  printf '%s\n' "$stale" | while IFS= read -r rel; do
    [ -n "$rel" ] || continue
    log "清理陈旧条目 ${rel#./}"
    rm -rf "${ZLM_DIR:?}/${rel#./}"
  done
fi
[ -f "$ZLM_DIR/config.ini" ] && log "config.ini 原样保留（未参与本次覆盖）" \
  || log "⚠️ 目录里没有 config.ini —— 出包前必须补上，否则会失败"

# ⛔ 临时目录在 macOS 上落在 /var/folders/...（不一定在 TMPDIR 下），
#   这里只是尽力清理，删不掉也不该让构建判失败。
rm -rf "$out" 2>/dev/null || true

log "产物架构核对："
for f in "$REPO/deploy/standalone/bin/zlm/MediaServer" \
         "$REPO/deploy/standalone/bin/redis-server" \
         "$REPO/deploy/standalone/bin/nginx/sbin/nginx"; do
  printf '  %-62s %s\n' "$(file -b "$f" | cut -c1-62)" "${f#"$REPO"/}"
done

# ⛔⛔ 出包命令必须把 UVP_GLIBC_MAX 一起传：本线的底座是 openEuler 20.03（glibc **2.28**），
#   而 build-standalone.sh 的可移植性门禁默认按 x86 线的 **2.17** 判。
#   漏传的症状是包已经组装好了、最后一步门禁突然报一串
#   「要求 glibc 版本：GLIBC_2.27 GLIBC_2.28（上限 2.17）」——
#   看着像"编错了"，其实只是判据用的基线没跟着底座抬（2026-10-11 实测踩到）。
#   ⛔ 门禁上限只影响"判定"，不会改变产物里的 glibc 要求，所以补上传参即可，不用重编。
log "✅ 三个原生二进制已完成（aarch64）。接着出包："
log "     UVP_PKG_ARCH=arm64 UVP_GLIBC_MAX=2.28 \\"
log "       bash deploy/standalone/build-standalone.sh [--skip-frontend]"
log "   ⛔ UVP_GLIBC_MAX=2.28 不能省（本线底座 glibc 2.28，门禁默认 2.17）。"
log "   ⛔ --skip-frontend 只在 web/dist 已经构建好时用；脚本自己会做 SIP 端口读回校验。"
log "   ⛔ 脚本还要一个带 PyYAML 的 python3，缺了会崩在 ZLM 身份配置校验那步："
log "      用 UVP_BUILD_PY=<python> 指定（例如某个装了 pyyaml 的 venv）。"
