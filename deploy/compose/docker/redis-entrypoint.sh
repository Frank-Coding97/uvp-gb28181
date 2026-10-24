#!/bin/sh
# 缓存容器入口：以命令行参数启动 Redis（不引入配置文件，避免多一份要维护的东西）
#
# ⛔ maxmemory-policy 用 noeviction：
#   这个 Redis 存的是会话、设备在线状态、进程授权锁一类**不能凭空消失**的数据。
#   一旦允许 LRU 淘汰，内存吃紧时会静默丢 key ⇒ 表现为"随机掉线/随机要重新登录"，
#   而这种问题和内存压力的因果关系极难建立。宁可让写入报错，也不要悄悄丢。
set -eu

PORT="${UVP_REDIS_PORT:-51003}"
MAXMEMORY="${UVP_REDIS_MAXMEMORY:-512mb}"

exec /usr/local/bin/redis-server \
  --bind 127.0.0.1 -::1 \
  --port "${PORT}" \
  --dir /data \
  --maxmemory "${MAXMEMORY}" \
  --maxmemory-policy noeviction \
  --appendonly no \
  --save 900 1 \
  --protected-mode yes
