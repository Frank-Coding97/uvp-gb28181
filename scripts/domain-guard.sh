#!/usr/bin/env bash
# 域名残留检查：确认全仓不存在 uvplatform.cn。
#
# 为什么需要它：域名散落在 1100+ 个 Go 文件的 import 路径、代码生成器模板、
# Nginx 部署配置与前端扫码页里，靠人眼不可能盯全。改完必须有一条机械判据。
#
# 口径：只扫 git 跟踪的文件。未跟踪的（web/dist 构建产物、.workbuddy 记忆、
# tmp 临时目录）不参与判定 —— 那些要么是构建生成的、要么不是仓库事实。
#
# 用法：
#   scripts/domain-guard.sh            # 有残留则打印并以1 退出
#   scripts/domain-guard.sh --allow    # 只报告不失败（迁移中途用）

set -eu

OLD='uvplatform\.cn'
NEW_DOMAIN='uvplatform.com'

cd "$(dirname "$0")/.."

# -z 空字节分隔：文件路径含空格时也安全（仓库里有 web/src/... 这类带空格的路径风险）
files=$(git ls-files -z | xargs -0 grep -lIE "$OLD" 2>/dev/null || true)

if [ -z "$files" ]; then
    printf '✅ 无 %s 残留（判据：git 跟踪文件全量扫描）\n' "$NEW_DOMAIN"
    exit 0
fi

count=$(printf '%s\n' "$files" | wc -l | tr -d ' ')
hits=$(git ls-files -z | xargs -0 grep -cIE "$OLD" 2>/dev/null | awk -F: '{s+=$NF} END {print s+0}')

printf '⛔ 仍有 %s 残留：%s 个文件 / %s 处\n' "$NEW_DOMAIN" "$count" "$hits"
printf '%s\n' "$files" | sed 's/^/   /'

if [ "${1:-}" = "--allow" ]; then
    printf '\n(--allow 模式：不判失败)\n'
    exit 0
fi

cat <<'EOF'

修复路径提示：
  · Go import / go.mod      → 直接替换后 `cd server && go build ./...` 验证
  · 代码生成器模板          → server/gen/templates/*.tpl 必须一起改，
                             否则以后 `go run main.go -c` 生成的代码 import 旧路径直接编译不过
  · Nginx / 前端对外网址    → 改了还要配套 DNS 解析与 TLS 证书，否则线上不可达
EOF
exit 1