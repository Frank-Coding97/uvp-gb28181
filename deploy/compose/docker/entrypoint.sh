#!/bin/sh
# 前端容器入口：渲染 nginx.conf → 兜底生成自签证书 → 拉起 nginx
#
# ⛔ 这个脚本只做「启动时装配」，不做安装：
#   配置与证书都由宿主机的安装脚本生成好并挂进来（重建容器必须保持同一份证书，
#   否则浏览器又要重新信任一次）。这里的证书生成只是**兜底**——
#   卷没挂、或首次跑时目录为空的情况下不至于开天窗。
set -eu

HTTPS_PORT="${UVP_HTTPS_PORT:-51000}"
PLAIN_HTTP_PORT="${UVP_PLAIN_HTTP_PORT:-51001}"
BACKEND_HOST="${UVP_BACKEND_HOST:-127.0.0.1}"
BACKEND_PORT="${UVP_BACKEND_PORT:-51002}"

WEB_ROOT="${UVP_WEB_ROOT:-/opt/uvp/web}"
UPLOADS_ROOT="${UVP_UPLOADS_ROOT:-/opt/uvp/uploads}"
RUN_ROOT="${UVP_RUN_ROOT:-/opt/uvp/run}"
LOG_ROOT="${UVP_LOG_ROOT:-/opt/uvp/logs}"
TLS_DIR="${UVP_TLS_DIR:-/opt/uvp/tls}"
TLS_CERT="${UVP_TLS_CERT:-$TLS_DIR/uvp.crt}"
TLS_KEY="${UVP_TLS_KEY:-$TLS_DIR/uvp.key}"

CONF_TEMPLATE=/opt/uvp-nginx/conf/nginx.conf.template
CONF=/opt/uvp-nginx/conf/nginx.conf

mkdir -p "$RUN_ROOT" "$LOG_ROOT" "$(dirname "$TLS_CERT")" "$(dirname "$TLS_KEY")"

# ---- 工作进程身份 ----
# ⛔⛔ 必须显式写出「用户 + 主组」，且主组名随发行版而变（Debian 系是 nogroup、
#   RPM 系是 nobody）。绿色包踩过：只写 `user nobody;` 时 nginx 会去查同名组，
#   Debian 上没有 nobody 组 ⇒ `getgrnam("nobody") failed`，nginx 直接起不来，
#   而其它组件（ZLM/Redis/后端）全都正常，症状极具误导性（见绿色包模板注释）。
# ⛔ 容器若以非 root 运行（没有 setuid 权限）就**不写** user 指令：
#   写了会被 nginx 忽略并打印警告，worker 仍以当前用户跑，行为反而更清晰。
if [ "$(id -u)" = "0" ]; then
  NGINX_WORKER_DIRECTIVE="user www-data www-data;"
else
  NGINX_WORKER_DIRECTIVE="# 非 root 运行：不写 user 指令，worker 继承容器用户"
fi

# ---- 渲染配置 ----
# ⛔ 用 `|` 做分隔符：路径里会出现 `/`，用 `/` 分隔就得转义一大片。
sed -e "s|@NGINX_WORKER_DIRECTIVE@|${NGINX_WORKER_DIRECTIVE}|g" \
    -e "s|@HTTPS_PORT@|${HTTPS_PORT}|g" \
    -e "s|@PLAIN_HTTP_PORT@|${PLAIN_HTTP_PORT}|g" \
    -e "s|@BACKEND_HOST@|${BACKEND_HOST}|g" \
    -e "s|@BACKEND_PORT@|${BACKEND_PORT}|g" \
    -e "s|@WEB_ROOT@|${WEB_ROOT}|g" \
    -e "s|@UPLOADS_ROOT@|${UPLOADS_ROOT}|g" \
    -e "s|@RUN_ROOT@|${RUN_ROOT}|g" \
    -e "s|@LOG_ROOT@|${LOG_ROOT}|g" \
    -e "s|@TLS_CERT@|${TLS_CERT}|g" \
    -e "s|@TLS_KEY@|${TLS_KEY}|g" \
    "$CONF_TEMPLATE" > "$CONF"

# ---- 自签证书（仅在缺失时兜底）----
# ⛔ 正式证书由安装脚本按客户实际访问域名/IP 生成（SAN 里带 IP，否则浏览器
#   用 IP 访问会 ERR_CERT_COMMON_NAME_INVALID）。这里只保证"能起来"。
if [ ! -s "$TLS_CERT" ] || [ ! -s "$TLS_KEY" ]; then
  echo "[uvp-nginx] 未发现证书，生成自签兜底证书（仅能用于本机验收，勿用于生产）" >&2
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
    -subj "/CN=localhost" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
    -keyout "$TLS_KEY" -out "$TLS_CERT" >/dev/null 2>&1
  chmod 0644 "$TLS_CERT"
  chmod 0600 "$TLS_KEY"
fi

# ⛔ nginx 起不来时不要静默退出：先做一次配置自检并打印，
#   否则容器反复重启而日志里只有 "exited"，现场完全没线索。
if ! /opt/uvp-nginx/sbin/nginx -t -c "$CONF" 2>&1; then
  echo "[uvp-nginx] nginx 配置自检失败，配置如下（已渲染）：" >&2
  cat "$CONF" >&2
  exit 1
fi

exec /opt/uvp-nginx/sbin/nginx -c "$CONF" -g "daemon off;"
