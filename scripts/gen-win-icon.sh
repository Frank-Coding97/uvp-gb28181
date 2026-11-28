#!/usr/bin/env bash
# 重新生成 Windows 托盘启动器的图标资源（PE 里那个「资源管理器/快捷方式」图标）。
#
# 为什么需要单独的 .syso
# ─────────────────────
# Windows 上「exe 文件本身的图标」是 PE 资源（RT_GROUP_ICON + 7 张 RT_ICON），
# 跟托盘里画的那个图标（`//go:embed app.ico` + systray.SetIcon）**是两套东西**：
#   · 前者由资源管理器读，Go 源码里怎么画都影响不到它；
#   · 不嵌资源时，包根的 UVP-GB28181.exe 就是一张白纸图标（默认程序图标）。
# 做法：用 rsrc 把 app.ico 编成 `rsrc_windows_amd64.syso`，Go 在 GOOS=windows
# GOARCH=amd64 构建时会自动链接同目录下这个后缀的 syso。
#
# ⛔ 这个 .syso 是**提交进仓库的产物**（33KB），不是每次构建现生成：
#   构建机是 Windows（52），不该为了出个包先去 go install 一个工具；
#   而且资源里不含版本号/路径，一次生成长期有效，换图标才需要重跑本脚本。
# ⛔ 只嵌图标，**不嵌 manifest**：托盘程序用 `-H=windowsgui` 构建，manifest 由
#   Go 链接器自己处理，两边都塞会打架。
#
# 用法：scripts/gen-win-icon.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TRAY_DIR="$ROOT/server/cmd/uvp-tray"
ICO="$TRAY_DIR/app.ico"
OUT="$TRAY_DIR/rsrc_windows_amd64.syso"

[ -f "$ICO" ] || { echo "找不到源图标：$ICO" >&2; exit 1; }

if ! command -v go >/dev/null 2>&1; then
  echo "需要 go 工具链（rsrc 用 go install 装）" >&2
  exit 1
fi

RSRC="$(go env GOPATH)/bin/rsrc"
if [ ! -x "$RSRC" ]; then
  echo "首次运行，安装 rsrc…"
  go install github.com/akavel/rsrc@latest
fi

"$RSRC" -ico "$ICO" -o "$OUT" -arch amd64
echo "已生成 $OUT（$(wc -c < "$OUT" | tr -d ' ') 字节）"
echo "校验方式：(cd server && GOOS=windows GOARCH=amd64 go build -o /tmp/t.exe ./cmd/uvp-tray)"
echo "          解析 PE 资源应看到 7 个 ICON + 1 个 GROUP_ICON。"
