#!/usr/bin/env bash
# 构建随包分发的 nginx。
#
# ⛔⛔ 为什么必须**在 glibc ≤ 2.17 的底座里编 + 把 OpenSSL 静态编进去**
#   （2026-10-10 客户银河麒麟 V10 SP2 实机踩到 —— 这是本脚本存在的唯一理由）：
#
#   【事故现场】包内 nginx 启动即报
#       nginx: error while loading shared libraries: libssl.so.3
#   因为那份 nginx 是在较新的 Linux 上直接编的，ELF 里写着要：
#       libssl.so.3 / libcrypto.so.3（OpenSSL 3.x） + GLIBC_2.34
#   而目标机（银河麒麟 V10 SP2）只有 **OpenSSL 1.1.1f + glibc 2.28**。
#   ⛔ 补库救不了：即便把 libssl.so.3 拷过去，下一关是 `version 'GLIBC_2.34' not found`，
#      而 glibc 是系统地基、绝不能随包分发。
#
#   【对照】同一个包里 ZLM 反而起得来 —— 它是 `uvp/linux-builder:glibc217` 编的
#   （GLIBC ≤ 2.17、零 OpenSSL 依赖）。⇒ nginx 必须照同一条路走。
#
#   ⛔ **别踩第二脚**：那个底座里自带的是 OpenSSL **1.0.2**（CentOS 7）。
#      若只是"换个地方编、仍然动态链系统 OpenSSL"，产物会改要 `libssl.so.10` ——
#      麒麟（1.1.1）、UOS（1.1.1）、Ubuntu 24.04（3.0）**一个都没有**，等于换个姿势再栽。
#      ⇒ 唯一解法：**OpenSSL 从源码静态编（no-shared）**，并顺手把 PCRE2 / zlib 也静态进去，
#        让 nginx 只剩 libc/libcrypt 两个动态依赖。这样一份产物在麒麟 / UOS /
#        openEuler / Ubuntu / CentOS 7 上通吃。
#
#   ⚠️ 历史教训（仍然有效，别退回去）：曾经从 `nginx:alpine` 镜像里直接抠二进制，
#      它要求 `OPENSSL_3.2.0` / `OPENSSL_3.5.0` 符号，拷到只有 3.0.x 的机器上就报
#      `version 'OPENSSL_3.5.0' not found`，连 `nginx -v` 都跑不起来 —— 同一个病。
#      ⇒ 结论不变：**必须自己编，且不能让产物动态依赖 OpenSSL。**
#
#   本脚本编译完会**立刻用出包门禁自检**（check-portable-elf.sh），不合格就地失败。
#
# 用法：build-nginx.sh [输出目录]      默认 deploy/standalone/bin/nginx
#
# 环境变量（都有默认值，一般不用改）：
#   UVP_LINUX_BUILDER_IMAGE  底座镜像，默认 uvp/linux-builder:glibc217
#   UVP_BUILD_PLATFORM       容器平台，默认 linux/amd64
#                            ⛔ 出 aarch64 包时设 linux/arm64，并把底座换成对应的
#                               arm64 镜像（uvp/linux-builder:glibc217-arm64）——
#                               镜像架构和 --platform 不一致会直接报 exec format error。
#   UVP_NGINX_VERSION        默认 1.26.2
#   UVP_OPENSSL_VERSION      默认 3.0.15（静态编入；刷新包时跟上 3.0.x 最新 LTS 小版本）
#   UVP_PCRE2_VERSION        默认 10.44
#   UVP_ZLIB_VERSION         默认 1.3.1
#   UVP_GLIBC_MAX            允许的最高 glibc 要求，默认 2.17
#   UVP_INSIDE_BUILDER       由本脚本自己在容器里设，**不要手工设**
#   UVP_SRCDIR               源码预置目录（离线构建时给；给了就不联网下载）
#
# 编译选项的取舍：
#   --with-http_ssl_module         HTTPS 必需
#   --with-http_v2_module          HTTP/2（浏览器更省连接）
#   --with-http_realip_module      后面挂 CDN / LB 时取真实客户端 IP 要用
#   --with-http_gzip_static_module 预压缩资源（省实时压缩 CPU）
#   --with-http_stub_status_module 状态页
#   --with-threads / --with-file-aio  静态文件并发、大文件（录像下载）异步 IO
#   不开 --with-http_rewrite_module：前端是 history 路由，nginx 侧 try_files 就够
#   不开 --with-debug：线上不需要，省体积也少一条误配置入口
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DEST="${1:-$SCRIPT_DIR/bin/nginx}"

BUILDER_IMAGE="${UVP_LINUX_BUILDER_IMAGE:-uvp/linux-builder:glibc217}"
BUILD_PLATFORM="${UVP_BUILD_PLATFORM:-linux/amd64}"
NGINX_VERSION="${UVP_NGINX_VERSION:-1.26.2}"
OPENSSL_VERSION="${UVP_OPENSSL_VERSION:-3.0.15}"
PCRE2_VERSION="${UVP_PCRE2_VERSION:-10.44}"
ZLIB_VERSION="${UVP_ZLIB_VERSION:-1.3.1}"
GLIBC_MAX="${UVP_GLIBC_MAX:-2.17}"
SRC_ROOT="${UVP_NGINX_WORKDIR:-/tmp/uvp-nginx-build}"
SRC_PROVIDED="${UVP_SRCDIR:-}"

log()  { printf '[nginx-build] %s\n' "$*"; }
fail() { printf '[nginx-build][ERROR] %s\n' "$*" >&2; exit 1; }

# $1 > $2 ?（点分段数值比较；不用 `sort -V`，macOS 的 sort 没有它）
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

# ---------------------------------------------------------------- 底座 ----
#
# ⛔ 这一段的全部意义：**不让本脚本在"新系统"上把 nginx 编出来**。
#   实测过的坏法：在 Ubuntu 24/26 上直接 ./configure && make，
#   产物要 libssl.so.3 + GLIBC_2.34 ⇒ 客户麒麟上连 `nginx -v` 都跑不起来。
#   ⇒ 要么已经在合规底座里（UVP_INSIDE_BUILDER=1，或本机 glibc 本来就 ≤ 上限），
#     要么把自己丢进容器里重跑一遍。
if [ "${UVP_INSIDE_BUILDER:-0}" != "1" ]; then
  # ⛔⛔ `|| true` 不能省：macOS 的 getconf **不认识** GNU_LIBC_VERSION，
  #   会打印 "no such configuration parameter" 并以**退出码 64** 收场。
  #   在本脚本的 `set -Eeuo pipefail` 下，这个"探测失败"会被当成致命错误
  #   ⇒ **整个脚本当场退出，一行输出都没有**（实测 rc=64）。
  #   症状极具误导性：看起来像"脚本不存在/没跑起来"，其实是探测命令的锅。
  host_glibc="$(getconf GNU_LIBC_VERSION 2>/dev/null | awk '{print $2}' || true)"
  if [ -n "$host_glibc" ] && [ "$(ver_gt "$host_glibc" "$GLIBC_MAX")" = "0" ]; then
    log "本机 glibc ${host_glibc} ≤ ${GLIBC_MAX}，直接在本机编译（无需容器）"
  else
    case "$DEST" in
      "$REPO_ROOT"/*) : ;;
      *) fail "输出目录 $DEST 不在仓库内（$REPO_ROOT），容器里看不到它。
      请把 DEST 放在仓库目录下，或先 cd 到仓库里再跑本脚本。" ;;
    esac

    runtime=""
    if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
      runtime="docker"
    elif command -v podman >/dev/null 2>&1; then
      runtime="podman"
    else
      # 说清楚"为什么本机不能编"，别让下一个人以为是环境坏了
      if [ -n "$host_glibc" ]; then
        why="本机 glibc 是 ${host_glibc}，高于允许上限 ${GLIBC_MAX}"
      else
        why="本机探测不到 glibc（macOS / 非 glibc 系统），无法保证产物可移植"
      fi
      fail "${why} ⇒ 不能在本机直接编 nginx。
      在新系统上编出来的 nginx 要 OpenSSL 3 + 新 glibc，拷到国产 OS 上必然起不来
      （2026-10-10 客户麒麟 V10 就是这么坏的）。
      请在装了 docker / podman 的机器上跑本脚本（会自动进 ${BUILDER_IMAGE} 容器），
      或直接在那台 glibc ≤ ${GLIBC_MAX} 的 Linux 构建机上跑（设 UVP_INSIDE_BUILDER=1）。"
    fi

    if ! "$runtime" image inspect "$BUILDER_IMAGE" >/dev/null 2>&1; then
      fail "找不到底座镜像 ${BUILDER_IMAGE}（$runtime）。
      若构建机上已有它（ZLM 就是这么编的），不用做任何事 —— 换个能访问该镜像的机器跑即可。
      若本机没有，可用本仓库的 Dockerfile 造一个（⛔ 用**新 tag**，别覆盖上面那个：
      已有的 uvp/linux-builder:glibc217 还带 devtoolset-11，ZLM 的构建依赖它）：
        docker build -f deploy/standalone/Dockerfile.linux-builder \\
                     -t uvp/glibc217-builder:latest deploy/standalone/
        UVP_LINUX_BUILDER_IMAGE=uvp/glibc217-builder:latest $0
      也可以用 UVP_LINUX_BUILDER_IMAGE 指定任何合规镜像（要求 glibc ≤ ${GLIBC_MAX}）。"
    fi

    rel="${DEST#"$REPO_ROOT"/}"
    # ⛔ 用宿主 uid/gid 跑容器：否则容器以 root 写的产物会变成 **root 所有**，
    #   回到宿主机后 git 与编辑器都改不动它（得 sudo chown 才能收拾）。
    #   不想要这个行为就设 UVP_DOCKER_USER=root。
    run_user="${UVP_DOCKER_USER:-$(id -u):$(id -g)}"
    log "本机 glibc ${host_glibc:-未知} 高于上限 ${GLIBC_MAX} ⇒ 进入容器 ${BUILDER_IMAGE} 编译"
    # ${VAR:+...} 是"有值才加这个参数"，用来可选地挂源码目录进去
    # shellcheck disable=SC2086
    exec "$runtime" run --rm -i \
      --platform "$BUILD_PLATFORM" \
      -u "$run_user" \
      -v "$REPO_ROOT":/src -w /src \
      -e UVP_INSIDE_BUILDER=1 \
      -e UVP_NGINX_VERSION="$NGINX_VERSION" \
      -e UVP_OPENSSL_VERSION="$OPENSSL_VERSION" \
      -e UVP_PCRE2_VERSION="$PCRE2_VERSION" \
      -e UVP_ZLIB_VERSION="$ZLIB_VERSION" \
      -e UVP_GLIBC_MAX="$GLIBC_MAX" \
      -e UVP_NGINX_WORKDIR="$SRC_ROOT" \
      ${SRC_PROVIDED:+-e UVP_SRCDIR=/src/.uvp-src} \
      "$BUILDER_IMAGE" \
      bash /src/deploy/standalone/build-nginx.sh "/src/$rel"
  fi
fi

# ---------------------------------------------------------------- 依赖 ----
for c in gcc make tar curl perl; do
  command -v "$c" >/dev/null 2>&1 || fail "构建环境缺 $c"
done
# ⛔ 这里**不再**检查 openssl-devel / pcre2-devel / zlib-devel：
#   三个库全部从源码静态编译，不依赖底座里的 -devel 包 —— 那正是"编出
#   libssl.so.10 依赖"的根源。少一个系统包就少一处版本耦合。

# ---------------------------------------------------------------- 源码 ----
mkdir -p "$SRC_ROOT"
cd "$SRC_ROOT"

# 下载 + 校验。⛔ 校验"解出来有顶层目录"而不是只看文件非空：
#   否则一个半截的 HTML 错误页也会被当成下载成功，直到 tar 解压才炸。
fetch() { # fetch <tarball> <url>...
  local tarball="$1"; shift
  if [ -f "$tarball" ] && tar -tzf "$tarball" >/dev/null 2>&1; then
    log "已有 $tarball，跳过下载"
    return 0
  fi
  local url
  for url in "$@"; do
    log "下载 $url"
    if curl -fsSL --max-time 300 -o "$tarball" "$url" 2>/dev/null \
       && tar -tzf "$tarball" >/dev/null 2>&1; then
      return 0
    fi
    rm -f "$tarball"
  done
  fail "所有源都下载失败：$tarball
      （离线构建时可用 UVP_SRCDIR 指一个预先放好这些 tar 包的目录）"
}

if [ -n "$SRC_PROVIDED" ]; then
  [ -d "$SRC_PROVIDED" ] || fail "UVP_SRCDIR=$SRC_PROVIDED 不是目录"
  log "使用预置源码目录 $SRC_PROVIDED"
  for f in "nginx-${NGINX_VERSION}.tar.gz" "openssl-${OPENSSL_VERSION}.tar.gz" \
           "pcre2-${PCRE2_VERSION}.tar.gz" "zlib-${ZLIB_VERSION}.tar.gz"; do
    [ -f "$SRC_PROVIDED/$f" ] || fail "预置源码目录里缺 $f"
    [ -f "$f" ] || cp "$SRC_PROVIDED/$f" .
  done
else
  fetch "nginx-${NGINX_VERSION}.tar.gz" \
    "https://mirrors.tuna.tsinghua.edu.cn/nginx/nginx-${NGINX_VERSION}.tar.gz" \
    "https://mirrors.aliyun.com/nginx/nginx-${NGINX_VERSION}.tar.gz" \
    "https://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz"
  fetch "openssl-${OPENSSL_VERSION}.tar.gz" \
    "https://www.openssl.org/source/openssl-${OPENSSL_VERSION}.tar.gz" \
    "https://github.com/openssl/openssl/releases/download/openssl-${OPENSSL_VERSION}/openssl-${OPENSSL_VERSION}.tar.gz" \
    "https://www.openssl.org/source/old/3.0/openssl-${OPENSSL_VERSION}.tar.gz"
  fetch "pcre2-${PCRE2_VERSION}.tar.gz" \
    "https://github.com/PCRE2Project/pcre2/releases/download/pcre2-${PCRE2_VERSION}/pcre2-${PCRE2_VERSION}.tar.gz" \
    "https://sourceforge.net/projects/pcre/files/pcre2/${PCRE2_VERSION}/pcre2-${PCRE2_VERSION}.tar.gz/download"
  fetch "zlib-${ZLIB_VERSION}.tar.gz" \
    "https://zlib.net/fossils/zlib-${ZLIB_VERSION}.tar.gz" \
    "https://github.com/madler/zlib/releases/download/v${ZLIB_VERSION}/zlib-${ZLIB_VERSION}.tar.gz"
fi

for d in "nginx-${NGINX_VERSION}" "openssl-${OPENSSL_VERSION}" \
         "pcre2-${PCRE2_VERSION}" "zlib-${ZLIB_VERSION}"; do
  [ -d "$d" ] || tar -xzf "${d}.tar.gz"
  [ -d "$d" ] || fail "解压 ${d}.tar.gz 后没看到目录 $d"
done

# ---------------------------------------------------------------- 编译 ----
#
# ⛔ 关于 --with-openssl / --with-pcre / --with-zlib：这三个选项让 nginx
#   **在它自己的构建过程里**把对应库编成静态库再链进 nginx 二进制，不去碰底座的动态库。
#   这正是"产物不再需要 libssl.so.*"的机制。查过 nginx 1.26.2 的 auto/lib/*/make，
#   实际行为是（已核对源码，不是推测）：
#     · OpenSSL：`cd $OPENSSL && ./config --prefix=... no-shared no-threads $OPENSSL_OPT
#                 && make && make install_sw`，然后链 `$OPENSSL/.openssl/lib/libssl.a`
#       ⇒ `no-shared` 其实 nginx 自己就会传；这里仍写上，是为了不依赖它的实现细节。
#       ⇒ `no-tests` 是合法的 OpenSSL 3.0 Configure 选项（`tests` 在 `@disablables` 里），
#          写上能省掉 OpenSSL 自测套件的编译时间。
#     · PCRE2 ：`cd $PCRE && ./configure --disable-shared $PCRE_CONF_OPT && make libpcre2-8.la`
#       ⇒ `--disable-shared` 也是 nginx 自己加的。
#       ⛔⛔ **千万别用 `--with-pcre-opt='--disable-shared'`**：nginx 把
#          `--with-pcre-opt` 当 **CFLAGS** 用（`CFLAGS="$PCRE_OPT"`），
#          传进去会被当成编译器选项，直接编不过 —— 这是个只看文档绝对看不出来的坑
#          （2026-10-10 写脚本时按直觉写错了，核对 nginx 源码才发现）。
#     · zlib  ：`cd $ZLIB && ./configure && make libz.a` —— zlib 本来就只出 .a。
#   ⛔ 别改成"先 yum 装 openssl-devel 再让 nginx 链系统库"：底座里的 OpenSSL 是 **1.0.2**，
#      那会立刻退回"产物要 libssl.so.10"的老坑。
cd "nginx-${NGINX_VERSION}"
log "configure（openssl ${OPENSSL_VERSION} / pcre2 ${PCRE2_VERSION} / zlib ${ZLIB_VERSION} 全部静态链）"
./configure \
  --prefix=/opt/uvp-nginx \
  --with-http_ssl_module \
  --with-http_v2_module \
  --with-http_realip_module \
  --with-http_gzip_static_module \
  --with-http_stub_status_module \
  --with-threads \
  --with-file-aio \
  --with-openssl="$SRC_ROOT/openssl-${OPENSSL_VERSION}" \
  --with-openssl-opt='no-shared no-tests' \
  --with-pcre="$SRC_ROOT/pcre2-${PCRE2_VERSION}" \
  --with-zlib="$SRC_ROOT/zlib-${ZLIB_VERSION}" \
  --with-cc-opt='-O2 -static-libgcc' \
  > "$SRC_ROOT/configure.log" 2>&1 || {
    tail -30 "$SRC_ROOT/configure.log" >&2
    fail "configure 失败（详见 $SRC_ROOT/configure.log）"
  }

log "make -j$(nproc 2>/dev/null || echo 2)"
make -j"$(nproc 2>/dev/null || echo 2)" > "$SRC_ROOT/make.log" 2>&1 || {
  tail -30 "$SRC_ROOT/make.log" >&2
  fail "编译失败（详见 $SRC_ROOT/make.log）"
}
[ -x objs/nginx ] || fail "产物 objs/nginx 不存在"

# ⛔ 不跑 make install：它要写 /opt（需要 root），而我们只要那个二进制 + 配置模板。
#   直接从 objs/ 取，prefix 由启动命令覆盖，自建目录树。
# ⛔ 也不 strip：留符号便于现场用 strings/gdb 定位。门禁只看**动态依赖**，
#   不受符号影响 —— 静态 OpenSSL 二进制里出现 OPENSSL_* 符号串是正常现象。

# ---------------------------------------------------------------- 装配 ----
log "装配到 $DEST"
# ⛔⛔ 每次装配都从零重建，`bin/nginx/` 里不保留任何旧文件。
#   实测踩过：改了仓库里的 nginx.conf.template（pid 占位符补空格）并提交，
#   但**没有重跑本脚本** —— `bin/nginx/` 是构建产物目录（gitignore），
#   里面还是上一次拷进去的旧模板，于是出包后 nginx 依旧报
#   `unknown directive "pid/home/..."`。看起来像"改了没用"，其实是产物没更新。
#   ⇒ 推论：**改完模板/脚本必须重跑 build-nginx.sh**，否则包里的还是旧的。
rm -rf "$DEST"
mkdir -p "$DEST/sbin" "$DEST/conf" "$DEST/logs" "$DEST/client_body_temp"

cp objs/nginx "$DEST/sbin/nginx"
chmod 0755 "$DEST/sbin/nginx"

# mime.types 是必需 include（否则 JS/CSS 全按 octet-stream 下，浏览器拒绝执行）
cp conf/mime.types "$DEST/conf/mime.types"
cp "$SCRIPT_DIR/nginx.conf.template" "$DEST/conf/nginx.conf.template"

# 预压缩的静态资源目录：配合 --with-http_gzip_static_module 节省实时压缩 CPU。
# 目录必须存在（nginx 缺 gzip_static 目录会在启动时报错）
mkdir -p "$DEST/conf/gzip_static"

# ⛔ 构建信息是给"下一个怀疑包有问题的人"看的。判"这份 nginx 能跑在哪些系统上"
#   只需一条命令：
#     strings -a vendor/nginx/sbin/nginx | grep -E '^lib.*\.so|GLIBC_'
#   静态 OpenSSL 之后这里应当**看不到 libssl.so / libcrypto.so**，且 GLIBC 只有 ≤ 2.17。
cat > "$DEST/conf/uvp-nginx-buildinfo.txt" <<EOF
NGINX_VERSION=$NGINX_VERSION
BUILT_IN_STATIC=openssl-${OPENSSL_VERSION},pcre2-${PCRE2_VERSION},zlib-${ZLIB_VERSION}
BUILDER_IMAGE=${UVP_INSIDE_BUILDER:+$BUILDER_IMAGE}
BUILDER_IMAGE=${BUILDER_IMAGE:-（本机直接编译，未用容器）}
BUILD_OS=$(uname -srm)
BUILD_DIR=$SRC_ROOT
BUILD_TIME=$(date -Iseconds)
GLIBC_MAX_REQUIRED=$GLIBC_MAX
EOF

# ---------------------------------------------------------------- 自检 ----
#
# ⛔ 这一条是本脚本的核心价值：把「拷过去才发现版本对不上」提前到构建时。
#   用的是**出包门禁同一套判据**（check-portable-elf.sh），
#   所以不存在"构建时一套标准、出包时另一套标准"的漂移。
log "自检：可移植性（glibc ≤ ${GLIBC_MAX}，且不得动态依赖 OpenSSL）"
bash "$SCRIPT_DIR/check-portable-elf.sh" --glibc-max "$GLIBC_MAX" "$DEST/sbin/nginx" \
  || fail "编出来的 nginx 仍不合规 —— 要么它没真正静态链上 OpenSSL/PCRE2/zlib，
      要么底座本身不是 glibc ≤ ${GLIBC_MAX} 的环境。请核对上面的逐条输出。"

log "✅ 完成：$DEST"
"$DEST/sbin/nginx" -v 2>&1 | sed 's/^/    /'
du -sh "$DEST" | awk '{print "    体积：" $1}'
