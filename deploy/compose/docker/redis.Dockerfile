# UVP-GB28181 缓存镜像 —— Redis（平台内部用，不对外）
#
# ⛔ 用随包分发那份自编的 redis-server（deploy/standalone/build-redis.sh），
#   与绿色包同一份二进制 ⇒ 同一套行为与 glibc 兼容性。它是 glibc 动态链接的，
#   所以基础镜像必须是 debian:12-slim，不能是 alpine。
FROM debian:12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

ENV TZ=Asia/Shanghai

COPY redis-server /usr/local/bin/redis-server
COPY redis-cli /usr/local/bin/redis-cli
COPY redis-entrypoint.sh /usr/local/bin/uvp-redis-entrypoint.sh

RUN chmod +x /usr/local/bin/redis-server /usr/local/bin/redis-cli /usr/local/bin/uvp-redis-entrypoint.sh \
    && mkdir -p /data

EXPOSE 51003/tcp

ENTRYPOINT ["/usr/local/bin/uvp-redis-entrypoint.sh"]
