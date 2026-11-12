# UVP-GB28181 前端镜像 —— 托管 SPA + HTTPS 终止 + 反向代理后端
#
# ⛔ 基础镜像必须是 **debian:12(bookworm)-slim**，不能用 alpine：
#   随包分发的 nginx 是**静态链 OpenSSL/PCRE2/zlib、但动态链 glibc** 的二进制
#   （见 deploy/standalone/build-nginx.sh），alpine 是 musl ⇒ 直接 "not found"。
#   ⚠️ 必须锁 12（bookworm）不能写 `debian:slim`：Debian 13(trixie) 起
#   移除了 libcrypt.so.1，而我们的 nginx 动态依赖它 ⇒ 换 13 会起不来。
#
# ⛔ 为什么不用 nginx 官方镜像：它的模块集与我们的二进制不一致，
#   而 nginx.conf 里用到了 http2/ssl/realip 等指令；用同一份二进制才能保证
#   「绿色包能跑的配置，容器里也能跑」。
FROM debian:12-slim

# curl 是给容器健康检查用的（debian:12-slim 默认既没 curl 也没 wget，
# 而 healthcheck 要在容器内自己发起 HTTP 探测）。
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata openssl curl \
    && rm -rf /var/lib/apt/lists/*

ENV TZ=Asia/Shanghai

# nginx 的安装根与编译期 --prefix 保持一致（/opt/uvp-nginx），
# 这样 nginx 找不到相对路径时的回退行为与绿色包完全一致。
# ⛔ 整个 nginx/ 目录一起拷，而不是逐个文件拷：
#   逐个拷时只要少一个文件（比如某次构建没产出 mime.types）就会在 build 阶段炸，
#   而报的是 "file not found"，看不出到底缺了什么。整目录拷的话缺什么
#   会在**运行时**才暴露，但至少构建上下文的组成是"所见即所得"。
COPY nginx/ /opt/uvp-nginx/
COPY nginx.conf.template /opt/uvp-nginx/conf/nginx.conf.template
COPY entrypoint.sh /usr/local/bin/uvp-nginx-entrypoint.sh

# 前端构建产物。⛔ 独立目录、不与 nginx 混放：升级时只换这一个目录的内容。
COPY dist /opt/uvp/web

RUN chmod +x /opt/uvp-nginx/sbin/nginx /usr/local/bin/uvp-nginx-entrypoint.sh \
    && mkdir -p /opt/uvp/run /opt/uvp/logs /opt/uvp/tls

EXPOSE 51000/tcp 51001/tcp

ENTRYPOINT ["/usr/local/bin/uvp-nginx-entrypoint.sh"]
