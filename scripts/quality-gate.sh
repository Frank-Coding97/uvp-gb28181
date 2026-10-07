#!/usr/bin/env bash
#
# 仓库级质量门禁（本地版）
#
# 本脚本负责本地整体健康检查；CI 对增量 Go 代码执行同一配置的检查。
# 本地提交由「本脚本 + husky 钩子」两层承担：
#   · husky 钩子守**单次提交**（pre-commit 跑 lint-staged，commit-msg 跑 commitlint）
#   · 本脚本守**整体健康**（格式化 / 静态检查 / 单测，以及可选的完整测试）
#
# 用法:
#   scripts/quality-gate.sh           快速档（默认）
#   scripts/quality-gate.sh --full    完整档
#
# 退出码: 0=全过  1=有失败项  2=工具链缺失或参数错误
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

usage() {
  cat <<'EOF'
用法: scripts/quality-gate.sh [选项]

  快速档（默认）  gofmt / go vet / 全量 golangci-lint / ESLint / Vitest / Prettier / Stylelint
  完整档(--full)  以上全部 + vue-tsc 类型检查 + 全量 go test
  --all-lint      兼容旧参数；当前默认已是全量扫描

说明:
  · Go lint、前端格式与样式检查均扫描全量源文件。
  · 全量 go test 约 20 分钟，需要可达的开发库与 Redis，且必须 -p 1 串行
    （并发跑会互相踩真实外部依赖）。
  · 本机 go / gofmt 不在 PATH，脚本会自动回退到 /opt/homebrew/Cellar/go/*/bin。
EOF
}

MODE="fast"
for arg in "$@"; do
  case "$arg" in
    ""|--fast) MODE="fast" ;;
    --full)    MODE="full" ;;
    --all-lint) : ;;
    --help|-h) usage; exit 0 ;;
    *) echo "未知参数: $arg" >&2; usage >&2; exit 2 ;;
  esac
done

# ---------- 定位工具链 ----------
# ⚠️ 本机 go / gofmt 都不在 PATH（实测 command -v go 为空），必须显式回退，
#    否则会得到「命令不存在被当成检查通过」的假绿结论。
GO_BIN="$(command -v go 2>/dev/null || true)"
if [ -z "$GO_BIN" ]; then
  GO_BIN="$(ls -d /opt/homebrew/Cellar/go/*/bin/go 2>/dev/null | tail -1)"
fi
if [ -z "$GO_BIN" ] || [ ! -x "$GO_BIN" ]; then
  echo "✗ 找不到 go 工具链，请安装 Go 或把它的 bin 目录加进 PATH" >&2
  exit 2
fi
GOFMT_BIN="$(dirname "$GO_BIN")/gofmt"
GO_PATH_PREFIX="$(dirname "$GO_BIN")"

GOLANGCI_BIN="$(command -v golangci-lint 2>/dev/null || true)"
if [ -z "$GOLANGCI_BIN" ] && [ -x "$HOME/go/bin/golangci-lint" ]; then
  GOLANGCI_BIN="$HOME/go/bin/golangci-lint"
fi

WEB_BIN="$ROOT/web/node_modules/.bin"
export GO_BIN GOFMT_BIN GO_PATH_PREFIX GOLANGCI_BIN WEB_BIN ROOT

echo "仓库根  : $ROOT"
echo "Go      : $GO_BIN"
echo "运行模式: $MODE"

# ---------- 结果收集 ----------
NAMES=(); MARKS=(); SECS=()
FAILED=0

# run_check <显示名> <工作目录> <命令>
run_check() {
  local name="$1" dir="$2" cmd="$3" t0 t1 rc
  printf '\n──── %s ────\n' "$name"
  t0=$(date +%s)
  ( cd "$dir" && bash -c "$cmd" )
  rc=$?
  t1=$(date +%s)
  NAMES+=("$name"); SECS+=("$((t1 - t0))")
  if [ "$rc" -eq 0 ]; then
    MARKS+=("✓"); echo "✓ $name 通过"
  else
    MARKS+=("✗"); echo "✗ $name 失败 (rc=$rc)"; FAILED=1
  fi
}

# skip_check <显示名> <原因>
skip_check() {
  printf '\n──── %s ────\n跳过: %s\n' "$1" "$2"
  NAMES+=("$1"); MARKS+=("–"); SECS+=("0")
}

# ---------- 后端检查 ----------
# gofmt 用自定义片段：要排除 third_party（sipgo fork），且要能给出文件清单。
run_check "gofmt（Go 格式）" "$ROOT/server" '
  bad="$("$GOFMT_BIN" -l . 2>/dev/null | grep -v "^third_party/" || true)"
  if [ -n "$bad" ]; then
    printf "%s\n" "$bad" | head -20
    echo "共 $(printf "%s\n" "$bad" | wc -l | tr -d " ") 个文件不合规，可用 gofmt -w 修复"
    exit 1
  fi
  echo "全部合规"'

run_check "go vet（后端）" "$ROOT/server" '"$GO_BIN" vet ./...'

# 域名残留：散落在 1100+ 个 Go 文件 import、10 个代码生成器模板、Nginx 部署配置
# 与前端扫码页里，靠人眼盯不完整。口径与 scripts/domain-guard.sh 一致。
# ⛔ 本注释刻意不写出完整旧域名 —— 那是护栏的判据本身，写出来会把自己判违规。
run_check "域名残留（uvplatform.com）" "$ROOT" './scripts/domain-guard.sh'

if [ -n "$GOLANGCI_BIN" ] && [ -x "$GOLANGCI_BIN" ]; then
  # golangci-lint 内部要调 go，而 go 不在 PATH，所以给它补上。
  #
  # ⛔ 不要用「把改动文件列表传给 golangci-lint」的做法：它要求所有文件名同目录，
  #    跨目录会以 "named files must all be in one directory" 直接失败。
  #    正确姿势是 --new：按 git diff 判定，只报本次改动引入的问题。
  run_check "golangci-lint（全量）" "$ROOT/server" \
    'PATH="$GO_PATH_PREFIX:$PATH" "$GOLANGCI_BIN" run --timeout 10m'
else
  skip_check "golangci-lint（静态检查）" \
    "未安装。安装: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"
fi

# ---------- 前端检查 ----------
if [ -x "$WEB_BIN/eslint" ]; then
  run_check "ESLint（前端）" "$ROOT/web" './node_modules/.bin/eslint src'
  run_check "Vue 表单规则" "$ROOT/web" 'node scripts/check-form-rules.mjs && node --test scripts/check-form-rules.test.mjs'
  run_check "Vitest（前端单测）" "$ROOT/web" './node_modules/.bin/vitest run'
  run_check "Prettier（前端全量）" "$ROOT/web" './node_modules/.bin/prettier --check "src/**/*.{js,ts,json,tsx,css,less,scss,vue,html,md}"'
  run_check "Stylelint（前端全量）" "$ROOT/web" './node_modules/.bin/stylelint "src/**/*.{vue,scss,css}"'
else
  skip_check "ESLint（前端）" "web/node_modules 未安装（先在 web/ 跑 pnpm install）"
  skip_check "Vitest（前端单测）" "web/node_modules 未安装（先在 web/ 跑 pnpm install）"
fi

# ---------- 完整档额外检查 ----------
if [ "$MODE" = "full" ]; then
  if [ -x "$WEB_BIN/vue-tsc" ]; then
    run_check "vue-tsc（前端类型检查）" "$ROOT/web" './node_modules/.bin/vue-tsc --noEmit'
  else
    skip_check "vue-tsc（前端类型检查）" "web/node_modules 未安装"
  fi
  # ⚠️ 必须 -p 1：并发跑会互相踩真实外部依赖（开发库 / Redis），结论不可信。
  run_check "go test（全量，串行）" "$ROOT/server" '
    pkgs="$("$GO_BIN" list ./... 2>/dev/null | grep -v "/third_party/" || true)"
    [ -z "$pkgs" ] && { echo "没列出任何包"; exit 1; }
    "$GO_BIN" test -p 1 -count=1 $pkgs'
fi

# ---------- 汇总 ----------
printf '\n════════ 汇总 ════════\n'
i=0
while [ "$i" -lt "${#NAMES[@]}" ]; do
  printf '  %s  %-30s %4ss\n' "${MARKS[$i]}" "${NAMES[$i]}" "${SECS[$i]}"
  i=$((i + 1))
done

if [ "$FAILED" -eq 0 ]; then
  printf '\n✓ 门禁通过\n'
  exit 0
fi
printf '\n✗ 门禁未通过，见上方失败项\n'
exit 1
