#!/usr/bin/env bash
# 校验 glibc 2.17 构建镜像的两条保证。
#
# 背景：交付目标要求最保守的 glibc 基线。原生二进制能引用的最高 GLIBC_ 符号
# 版本完全由构建机决定 —— 在 Ubuntu 24.04（glibc 2.39）上编出来的东西一定
# 跑不动 CentOS 7。所以这里必须把「基线」钉死成可执行的断言，而不是靠记忆。
#
# 分两种模式，因为二者的保证强度不同：
#
#   模式 A（配方校验）：按镜像声明的 $CXX/$CXXFLAGS/$LDFLAGS 编译，也就是
#     CMake / autoconf 实际会用的形态。断言 glibc <= 2.17 **且** 不动态依赖
#     libstdc++/libgcc_s。后者是 devtoolset-11 的必要处置：它的
#     libstdc++.so.6 带 GLIBCXX_3.4.29，而 CentOS 7 系统版只到 GLIBCXX_3.4.19。
#
#   模式 B（裸调用校验）：只用裸 `g++`（不带任何 LDFLAGS）编译。此时只断言
#     glibc <= 2.17 —— 裸调用本来就拿不到 $LDFLAGS，要求它静态链接 libstdc++
#     是错的断言。这一条守的是「基础镜像没被换成更高 glibc 的发行版」。
#
# ⛔ 常见误判：只跑模式 A 会以为「镜像强制了静态 libstdc++」。并没有 ——
#    任何绕过 $LDFLAGS 的构建（裸 g++、硬编码编译器路径、自己拼链接命令）
#    都不受保证。所以真实产物必须再单独查一遍：见 deploy/linux/verify-artifacts.sh。
set -Eeuo pipefail

# ⛔ CXXFLAGS / LDFLAGS 一律用 `${VAR:-}` 取默认值，不许裸写 —— 本脚本在 `set -u`
#   下运行，而"镜像没声明这两个变量"是完全正常的情形（x86 线就是由调用方
#   `docker run -e LDFLAGS=...` 传进来的）。裸写会让自检**自己崩掉**并报
#   `CXXFLAGS: unbound variable`，看着像编译环境有问题，其实是脚本的问题
#   —— 2026-10-10 arm64 镜像构建第 21 步就是这么红的。
BASELINE="GLIBC_${UVP_GLIBC_BASELINE:-2.17}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/probe.cpp" <<'CPP'
// 刻意用满各类 C++11 运行时设施，尽量逼出 libstdc++/libgcc 的符号需求
#include <atomic>
#include <chrono>
#include <functional>
#include <iostream>
#include <memory>
#include <mutex>
#include <random>
#include <regex>
#include <stdexcept>
#include <string>
#include <thread>
#include <unordered_map>
#include <vector>

static std::mutex g_mu;
static std::atomic<int> g_count{0};

static void worker(int n) {
    std::lock_guard<std::mutex> lk(g_mu);
    g_count.fetch_add(n, std::memory_order_relaxed);
}

int main() {
    std::vector<std::thread> ts;
    for (int i = 0; i < 4; ++i) ts.emplace_back(worker, i);
    for (auto &t : ts) t.join();

    std::unordered_map<std::string, int> m{{"a", 1}, {"b", 2}};
    std::regex re("[a-z]+");
    std::mt19937 rng(42);
    std::uniform_int_distribution<int> dist(0, 9);

    auto up = std::make_shared<std::string>("probe");
    try {
        throw std::runtime_error("probe");
    } catch (const std::exception &e) {
        up->assign(e.what());
    }

    auto ms = std::chrono::duration_cast<std::chrono::milliseconds>(
                  std::chrono::steady_clock::now().time_since_epoch())
                  .count();

    std::cout << *up << " " << m.size() << " " << std::regex_match("abc", re)
              << " " << dist(rng) << " " << g_count.load() << " " << ms << "\n";
    return 0;
}
CPP

fail=0
note() { printf '      %s\n' "$*" >&2; }

# ---------------------------------------------------------------------------
# 断言 1：不得引用高于 GLIBC_2.17 的符号版本
# ---------------------------------------------------------------------------
check_glibc_ceiling() {
    local bin="$1" label="$2"
    local requested
    requested=$(readelf --dyn-syms -W "$bin" \
        | grep -oE 'GLIBC_[0-9]+(\.[0-9]+)+' | sort -uV || true)

    if [[ -z "$requested" ]]; then
        echo "FAIL [$label]: 解析不出任何 GLIBC_ 符号 —— readelf 口径失效，断言不可信" >&2
        return 1
    fi

    printf '      [%s] GLIBC requested: %s\n' "$label" "$(echo "$requested" | tr '\n' ' ')"

    # 把基线塞进排序集合，基线之后若还有条目即为越界
    local over
    over=$(printf '%s\n%s\n' "$requested" "$BASELINE" \
        | sort -uV \
        | awk -v base="$BASELINE" 'seen && $0 != base { print } $0 == base { seen = 1 }')

    if [[ -n "$over" ]]; then
        echo "FAIL [$label]: 引用了高于 ${BASELINE} 的符号版本：" >&2
        echo "$over" | sed 's/^/        /' >&2
        note "→ 构建镜像的 glibc 基线被抬高的发行版污染，产物跑不动 CentOS 7"
        return 1
    fi
    return 0
}

# ---------------------------------------------------------------------------
# 断言 2：不得对 libstdc++ / libgcc_s 保持动态依赖
# ---------------------------------------------------------------------------
check_no_dynamic_cxx_runtime() {
    local bin="$1" label="$2"

    if readelf --dyn-syms -W "$bin" | grep -qE 'GLIBCXX_[0-9]'; then
        echo "FAIL [$label]: 动态引用了 libstdc++ 符号（GLIBCXX_*）" >&2
        note "→ -static-libstdc++ 未生效"
        return 1
    fi

    local hits
    hits=$(ldd "$bin" 2>/dev/null | grep -E 'libstdc\+\+|libgcc_s' || true)
    if [[ -n "$hits" ]]; then
        echo "FAIL [$label]: 仍动态链接 libstdc++/libgcc_s：" >&2
        echo "$hits" | sed 's/^/        /' >&2
        note "→ 该产物在 CentOS 7 上会报 version GLIBCXX_3.4.29 not found"
        return 1
    fi
    return 0
}

echo "==> 模式 A：按镜像声明的 \$CXX/\$LDFLAGS 编译（CMake/autoconf 的实际形态）"
echo "    CXX=${CXX:-<unset>}"
echo "    LDFLAGS=${LDFLAGS:-<unset>}"
"${CXX:-g++}" -std=c++11 -O2 -pthread ${CXXFLAGS:-} "$tmp/probe.cpp" -o "$tmp/probeA" ${LDFLAGS:-}
check_glibc_ceiling "$tmp/probeA" "A/glibc" || fail=1
check_no_dynamic_cxx_runtime "$tmp/probeA" "A/libstdc++" || fail=1

echo "==> 模式 B：裸 g++ 编译（只守基础镜像的 glibc 基线）"
g++ -std=c++11 -O2 -pthread "$tmp/probe.cpp" -o "$tmp/probeB"
check_glibc_ceiling "$tmp/probeB" "B/glibc" || fail=1

if [[ "$fail" -ne 0 ]]; then
    echo "glibc ${BASELINE} 基线校验失败" >&2
    exit 1
fi

echo "    gcc       : $(gcc -dumpversion)"
echo "    glibc     : $(ldd --version | head -1 | awk '{print $NF}')"
echo "    cmake     : $(cmake --version | head -1 | awk '{print $3}')"
echo "    arch      : $(uname -m)   # arm64 镜像里没有 nasm，这一行不查汇编器"
echo "OK: glibc ${BASELINE} baseline verified"
