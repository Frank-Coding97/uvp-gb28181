#!/usr/bin/env bash
# 构建随包分发的 nginx —— 必须**在构建机上按目标机环境编译**，不能从镜像里抠。
#
# 用法：build-nginx.sh [输出目录]
#
# ⛔⛔ 为什么不能直接 docker cp 镜像里的 nginx（这是本脚本存在的唯一理由）：
#   nginx 动态链接 OpenSSL，且**在链接期就写死了它需要的符号版本**。
#   实测：从 nginx:alpine 抠出的二进制要求 `OPENSSL_3.2.0` / `OPENSSL_3.5.0`，
#   而目标机（Ubuntu 24.04 / CentOS 8+）的系统库只提供 `OPENSSL_3.0.x`
#   ⇒ 拷过去直接报
#       nginx: /lib/x86_64-linux-gnu/libssl.so.3: version `OPENSSL_3.5.0' not found
#   而且**连 nginx -v 都跑不起来**，更别说启动 —— 属于"装上去才发现"的死结。
#
#   自己编译则只要求 OPENSSL_3.0.0（实测），且除 glibc/libssl/libcrypto/
#   libpcre2/libz 之外无依赖，这五个 Ubuntu 全自带。
#
# 编译选项的取舍：
#   --with-http_ssl_module      HTTPS 必需
#   --with-http_v2_module       HTTP/2（浏览器更省连接）
#   --with-http_realip_module   后面挂了 CDN / LB 时取真实客户端 IP要用
#   --with-http_gzip_static_module  预压缩资源（前端构建产物放进去就能省 CPU）
#   --with-threads              aio threads / 静态文件并发
#   --with-file-aio             大文件（录像下载）用异步 IO
#   不开 --with-http_rewrite_module：前端是 history 路由，nginx 侧用 try_files 就够
#   不开 --with-debug：线上不需要，省体积也少一条误配置入口
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEST="${1:-$SCRIPT_DIR/bin/nginx}"
SRC_ROOT="${UVP_NGINX_WORKDIR:-/tmp/uvp-nginx-build}"
VERSION="${UVP_NGINX_VERSION:-1.26.2}"
# ⛔ 这里曾有个死变量 PREFIX_IN_PACKAGE="bin/nginx"，注释说"conf 里会按它写 include"，
#   但它定义后再没被用过 —— conf 的路径是由 uvp-ctl.sh 用 nginx.conf.template 的
#   @ROOT@ 渲染的，不读这个变量。死变量会触发 shellcheck SC2034，而
#   build-standalone.sh 的门禁是「有输出就fail」⇒它直接把出包卡住了。

log() { printf '[nginx-build] %s\n' "$*"; }
fail() { printf '[nginx-build][ERROR] %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------- 依赖 ----
for c in gcc make tar curl; do
  command -v "$c" >/dev/null 2>&1 || fail "构建机缺 $c"
done

# 头文件存在即可用；不存在则尝试用包管理器装（Debian/Ubuntu 与 RHEL 系分支）
missing=()
for h in /usr/include/openssl/ssl.h /usr/include/zlib.h /usr/include/pcre2.h; do
  [ -f "$h" ] || missing+=("$h")
done
if [ "${#missing[@]}" -gt 0 ]; then
  log "缺少开发头文件：${missing[*]}"
  if command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update -qq && sudo apt-get install -y --no-install-recommends \
      libssl-dev zlib1g-dev libpcre2-dev
  elif command -v yum >/dev/null 2>&1; then
    sudo yum install -y openssl-devel zlib-devel pcre2-devel
  else
    fail "请先安装：openssl-devel / zlib-devel / pcre2-devel（Debian 系：libssl-dev zlib1g-dev libpcre2-dev）"
  fi
fi

# ---------------------------------------------------------------- 源码 ----
mkdir -p "$SRC_ROOT"
cd "$SRC_ROOT"
TARBALL="nginx-${VERSION}.tar.gz"
if [ ! -d "nginx-${VERSION}" ]; then
  # 国内优先清华镜像；官方源作为回退（实测清华不通、nginx.org 通）
  for url in \
    "https://mirrors.tuna.tsinghua.edu.cn/nginx/${TARBALL}" \
    "https://mirrors.aliyun.com/nginx/${TARBALL}" \
    "https://nginx.org/download/${TARBALL}"; do
    log "下载 $url"
    if curl -fsSL --max-time 120 -o "$TARBALL" "$url" 2>/dev/null \
       && tar -tzf "$TARBALL" >/dev/null 2>&1; then
      break
    fi
    rm -f "$TARBALL"
  done
  [ -f "$TARBALL" ] || fail "所有源都下载失败（最后尝试：$url）"
  tar -xzf "$TARBALL"
fi

# ---------------------------------------------------------------- 编译 ----
cd "nginx-${VERSION}"
log "configure"
./configure \
  --prefix=/opt/uvp-nginx \
  --with-http_ssl_module \
  --with-http_v2_module \
  --with-http_realip_module \
  --with-http_gzip_static_module \
  --with-http_stub_status_module \
  --with-threads \
  --with-file-aio \
  --with-pcre \
  --with-cc-opt='-O2 -static-libgcc' \
  > "$SRC_ROOT/configure.log" 2>&1 || {
    tail -20 "$SRC_ROOT/configure.log" >&2
    fail "configure 失败（详见 $SRC_ROOT/configure.log）"
  }

log "make -j$(nproc 2>/dev/null || echo 2)"
make -j"$(nproc 2>/dev/null || echo 2)" > "$SRC_ROOT/make.log" 2>&1 || {
  tail -20 "$SRC_ROOT/make.log" >&2
  fail "编译失败（详见 $SRC_ROOT/make.log）"
}
[ -x objs/nginx ] || fail "产物 objs/nginx 不存在"

# ⛔ 不跑 make install：它要写 /opt（需要 root），而我们只要那个二进制 + 配置模板。
#   直接从 objs/ 取，改 prefix 后自建目录树。

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

# 预压缩的静态资源目录：把前端构建产物的 .gz 放这里，
# 配合 --with-http_gzip_static_module 可省实时压缩 CPU。
# 目录必须存在（nginx 缺 gzip_static 目录会在启动时报错）
mkdir -p "$DEST/conf/gzip_static"

cat > "$DEST/conf/uvp-nginx-buildinfo.txt" <<EOF
NGINX_VERSION=$VERSION
BUILD_OS=$(uname -srm)
BUILD_DIR=$SRC_ROOT
BUILD_TIME=$(date -Iseconds)
BUILD_PY=uvp-green-package/deploy/standalone/build-nginx.sh
# OpenSSL 要求（拷到别的机器前必须核对这一行）
OPENSSL_VERSIONS_REQUIRED=$(strings objs/nginx | grep -oE 'OPENSSL_[0-9.]+' | sort -u | tr '\n' ' ')
EOF

# ---------------------------------------------------------------- 自检 ----
log "自检：依赖是否齐"
missing_libs="$(ldd "$DEST/sbin/nginx" 2>/dev/null | grep 'not found' || true)"
[ -z "$missing_libs" ] || fail "产物缺动态库：$missing_libs"

required_openssl="$(strings objs/nginx | grep -oE 'OPENSSL_[0-9.]+' | sort -u | tr '\n' ' ')"
log "OpenSSL 符号要求：$required_openssl"
log "本机 libssl 提供：$(strings /lib/x86_64-linux-gnu/libssl.so.3 2>/dev/null | grep -oE 'OPENSSL_[0-9.]+' | sort -u | tr '\n' ' ')"

# ⛔ 这一条是本脚本的核心价值：把「拷过去才发现版本不匹配」提前到构建时。
#   nginx 要的每个 OPENSSL_x 都必须被本机 libssl 提供，否则打出来的包
#   在目标机上必然起不来。
system_openssl="$(strings /lib/x86_64-linux-gnu/libssl.so.3 2>/dev/null | grep -oE 'OPENSSL_[0-9.]+' | sort -u || true)"
for v in $required_openssl; do
  if ! printf '%s\n' $system_openssl | grep -qx "$v"; then
    fail "本机 libssl 不提供 $v —— 这样的 nginx 拷到与本机相同的系统上才能跑；
     若目标机系统不同（glibc / OpenSSL 大版本不一致），请在**目标机同款环境**里编译。"
  fi
done

log "✅ 完成：$DEST"
"$DEST/sbin/nginx" -v 2>&1 | sed 's/^/    /'
du -sh "$DEST" | awk '{print "    体积：" $1}'
