#!/usr/bin/env bash
# 出包门禁：审计包内**预编译二进制**对「目标机的系统库」提出了什么要求。
#
# ⛔⛔ 为什么需要它（2026-10-10 客户麒麟 V10 实机踩到）：
#   包里的 nginx 是在较新的 Linux 上直接编的，ELF 里写着要
#       libssl.so.3 / libcrypto.so.3（OpenSSL 3.x） + GLIBC_2.34
#   而交付目标机（银河麒麟 V10 SP2）只有 OpenSSL 1.1.1f + glibc 2.28
#   ⇒ `error while loading shared libraries: libssl.so.3`，nginx 连 -v 都跑不起来。
#   同一个包里 ZLM 却没事 —— 因为它是用 `uvp/linux-builder:glibc217` 编的
#   （GLIBC ≤ 2.17，且零 OpenSSL 依赖）。
#   ⇒ 结论：**"包级 glibc 基线"这种笼统说法是骗人的，必须逐二进制核**。
#     本脚本就是把"拷到客户机才发现"提前到出包当天。
#
# 判据（只认两条硬规则，全是客观事实、不猜）：
#   ① 动态依赖里出现 `libssl.so*` / `libcrypto.so*` ⇒ FAIL。
#      目标国产系统（麒麟 1.1.1 / UOS 1.1.1 / openEuler 3.0）**大版本各不相同**，
#      动态链 OpenSSL 不可能同时在它们上面跑起来。唯一解法是静态链。
#   ② 要求的 `GLIBC_x.y` 高于上限（默认 2.17）⇒ FAIL。
#      glibc 是系统地基，**绝对不能随包分发**，只能把产物编到老基线上去。
#
# ⚠️ 检测手法是**扫字符串表**（`strings` 近似 DT_NEEDED），不是完整解析 ELF，
#   因为要在 macOS 出包机 / CentOS 7 容器 / 国产机上都能跑（readelf 不是哪儿都有）。
#   由此带来两条纪律：
#     ① 非 OpenSSL 的库**只作提示（[i]）、绝不判错** —— 那些名字可能只是
#        `dlopen` 的候选（实测 ZLM 里就有 `libnvcuvid.so.1`，麒麟上没装 NVIDIA 驱动
#        也照样启动成功，说明它是惰性加载）。
#     ② 静态链了 OpenSSL 的二进制里照样会有 `OPENSSL_3.0.0` 这类字符串 —— 那不是错。
#        所以 `OPENSSL_*` 也只在 [i] 里展示，不参与判定。
#
# 用法：
#   check-portable-elf.sh [--glibc-max 2.17] [--provided-dir DIR]... FILE...
#     --provided-dir  包内自带 .so 的目录（如 vendor/zlm/lib）。里面的库
#                     算"包已提供"，不再提示"需目标机自带"。
#   退出码：0 = 全部通过；1 = 有硬性不合规；2 = 用法错误。
#
# ⛔ 故意**不开 `set -e`**：`grep` 找不到就是退出码 1，那是本脚本的**正常分支**，
#   开 -e 会把"没命中"当成错误。另外要在 macOS 的 bash 3.2 与 CentOS 7 的
#   bash 4.2 下都能跑，故不用 bash 4.4+ 的空数组展开写法。
set -uo pipefail

GLIBC_MAX="2.17"
PROVIDED_DIRS=()
FILES=()

usage() {
  cat <<'EOF'
用法: check-portable-elf.sh [--glibc-max X.Y] [--provided-dir DIR]... FILE...

  --glibc-max X.Y   允许的最高 glibc 版本要求（默认 2.17，即 CentOS 7 基线）
  --provided-dir D  包内自带 .so 的目录（可重复）。其中的库不计入"需目标机自带"
  -h, --help        显示本帮助

退出码: 0 通过 / 1 有不合规 / 2 用法错误
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --glibc-max)    [ $# -ge 2 ] || { printf 'ERROR: --glibc-max 缺参数\n' >&2; exit 2; }; GLIBC_MAX="$2"; shift 2 ;;
    --provided-dir) [ $# -ge 2 ] || { printf 'ERROR: --provided-dir 缺参数\n' >&2; exit 2; }; PROVIDED_DIRS+=("$2"); shift 2 ;;
    -h|--help)      usage; exit 0 ;;
    --)             shift; while [ $# -gt 0 ]; do FILES+=("$1"); shift; done ;;
    -*)             printf 'ERROR: 未知参数 %s\n' "$1" >&2; usage >&2; exit 2 ;;
    *)              FILES+=("$1"); shift ;;
  esac
done

if [ "${#FILES[@]}" -eq 0 ]; then
  printf 'ERROR: 没给任何文件\n' >&2; usage >&2; exit 2
fi

# ---- 小工具 ---------------------------------------------------------------

# $1 > $2 ?（按点分段做数值比较，避免依赖 GNU sort -V —— macOS 的 sort 没有 -V）
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

is_elf() {
  [ -f "$1" ] || return 1
  # 判据 = ELF magic 7f 45 4c 46；用 od 而不是 file(1)，
  # 因为 file(1) 在 macOS 与 CentOS 上的措辞不同，容易写成"只在一台机器上对"。
  local magic
  magic="$(head -c 4 "$1" 2>/dev/null | od -An -tx1 2>/dev/null | tr -d ' \n')"
  [ "$magic" = "7f454c46" ]
}

# 包内自带的库文件名集合（用于把 libavcodec.so.60 这类"包里已经带了"的排除掉）
provided_names() {
  local d n
  for d in ${PROVIDED_DIRS[@]+"${PROVIDED_DIRS[@]}"}; do
    [ -d "$d" ] || continue
    for n in "$d"/*; do [ -e "$n" ] && basename "$n"; done
  done | sort -u
}

# ---- 主流程 ---------------------------------------------------------------

PROVIDED="$(provided_names || true)"
PROVIDED_FLAT="$(printf '%s\n' "$PROVIDED" | tr '\n' ' ')"

hard_fail=0
need_system_all=""
checked=0

for f in ${FILES[@]+"${FILES[@]}"}; do
  if [ ! -e "$f" ]; then
    printf '[FAIL] %s\n       文件不存在（是把路径写错了，还是构建产物没生成？）\n' "$f"
    hard_fail=1
    continue
  fi
  if ! is_elf "$f"; then
    printf '[skip] %s  非 ELF 文件\n' "$f"
    continue
  fi
  checked=$((checked + 1))

  syms="$(strings -a "$f" 2>/dev/null || true)"

  # DT_NEEDED 里的 soname。用 ^...$ 锚定，避免把二进制里普通字符串当成库名。
  needed="$(printf '%s\n' "$syms" | grep -E '^lib[][A-Za-z0-9_.+-]*\.so(\.[0-9]+)*$' | sort -u || true)"
  glibc_vers="$(printf '%s\n' "$syms" | grep -E '^GLIBC_[0-9]+\.[0-9]+$' | sed 's/^GLIBC_//' | sort -u || true)"
  openssl_vers="$(printf '%s\n' "$syms" | grep -E '^OPENSSL_[0-9]+\.[0-9]+' | sort -u || true)"

  # ---- 硬规则 ①：动态依赖 OpenSSL ----
  bad_ssl="$(printf '%s\n' "$needed" | grep -E '^lib(ssl|crypto)\.so' || true)"

  # ---- 硬规则 ②：glibc 要求过高 ----
  too_new=""
  for v in $glibc_vers; do
    [ -n "$v" ] || continue
    if [ "$(ver_gt "$v" "$GLIBC_MAX")" = "1" ]; then
      too_new="$too_new $v"
    fi
  done

  # ---- 其余动态库：列出来让人工核目标机有没有 ----
  others=""
  for n in $needed; do
    case "$n" in
      libssl.so*|libcrypto.so*) continue ;;                 # 已在硬规则里报
      libc.so.*|ld-linux*|libdl.so.*|libm.so.*|libpthread.so.*|librt.so.*|libgcc_s.so.*) continue ;;
    esac
    if printf '%s\n' "$PROVIDED" | grep -qxF -- "$n"; then continue; fi   # 包内自带
    others="$others $n"
  done

  maxglibc=""
  for v in $glibc_vers; do
    [ -n "$v" ] || continue
    if [ -z "$maxglibc" ] || [ "$(ver_gt "$v" "$maxglibc")" = "1" ]; then maxglibc="$v"; fi
  done

  if [ -n "$bad_ssl" ] || [ -n "$too_new" ]; then
    hard_fail=1
    printf '[FAIL] %s\n' "$f"
    if [ -n "$bad_ssl" ]; then
      printf '       需要动态 OpenSSL：%s\n' "$(printf '%s' "$bad_ssl" | tr '\n' ' ')"
      printf '       ⇒ 国产系统大版本各不相同（麒麟 1.1.1 / UOS 1.1.1 / openEuler 3.0），\n'
      printf '         动态链不可能通吃。必须把 OpenSSL 静态编进去（no-shared）。\n'
    fi
    if [ -n "$too_new" ]; then
      # ⛔ 打印时补回 GLIBC_ 前缀：让人能直接拿这行去和客户现场那句
      #   `version 'GLIBC_2.34' not found` 对上号（比较用的是裸版本号）。
      printf '       要求 glibc 版本：%s（上限 %s）\n' \
        "$(printf '%s' "$too_new" | tr ' ' '\n' | grep . | sort -u | sed 's/^/GLIBC_/' | tr '\n' ' ')" \
        "$GLIBC_MAX"
      printf '       ⇒ glibc 是系统地基、不能随包分发。请在老基线环境里重编，\n'
      printf '         例如 uvp/linux-builder:glibc217（CentOS 7 底座）。\n'
    fi
  else
    printf '[ok]   %s   glibc max=%s   无动态 OpenSSL\n' "$f" "${maxglibc:-无}"
  fi

  if [ -n "$openssl_vers" ]; then
    printf '       [i] 二进制内含 OpenSSL 符号：%s（静态链的正常现象，不判错）\n' "$(printf '%s' "$openssl_vers" | tr '\n' ' ')"
  fi
  if [ -n "$others" ]; then
    printf '       [i] 需目标机自带的库：%s\n' "$(printf '%s' "$others" | tr ' ' '\n' | grep . | sort -u | tr '\n' ' ')"
    need_system_all="$need_system_all $others"
  fi
done

if [ -n "$need_system_all" ]; then
  printf '\n提示：以下库需目标系统自带，请确认国产系统（麒麟/UOS/openEuler）里都有：\n  %s\n' \
    "$(printf '%s' "$need_system_all" | tr ' ' '\n' | grep . | sort -u | tr '\n' ' ')"
fi

if [ "$hard_fail" -ne 0 ]; then
  printf '\n✗ 可移植性门禁未通过（审计 %d 个 ELF）——\n' "$checked"
  printf '  这样的包只能在"和构建机同款"的系统上跑，拷到国产 OS / 老系统上必然起不来。\n'
  exit 1
fi

printf '\n✓ 可移植性门禁通过（审计 %d 个 ELF，glibc 上限 %s）\n' "$checked" "$GLIBC_MAX"
exit 0
