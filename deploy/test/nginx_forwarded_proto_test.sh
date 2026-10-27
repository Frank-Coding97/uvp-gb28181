#!/usr/bin/env sh
# 交付形态的内置 nginx 不能把上游的 X-Forwarded-Proto 覆盖成本跳 $scheme。
#
# 2026-10-27 实测（62 上级 / 236 下级 级联对讲）：
#   浏览器 https → 外层 NPM(443, TLS 终止) → 内置 nginx(51000, http) → 后端(51002)
#   外层已给 `X-Forwarded-Proto: https`，内置 nginx 写死 `$scheme` 把它改回 http，
#   后端拼出的 WHIP 上行地址是 http://… ⇒ 浏览器按混合内容拦掉
#   ⇒ 前端只看到 `Failed to fetch`，对讲永远起不来（点播不走这条，所以只有对讲中招）。
#
# 正确写法：map $http_x_forwarded_proto $uvp_forwarded_proto { default …; "" $scheme; }
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)

for tpl in \
    "$root/deploy/standalone/nginx.conf.template" \
    "$root/deploy/compose/nginx/nginx.conf.template" \
    "$root/deploy/standalone-windows/nginx.conf.template"
do
    [ -f "$tpl" ] || { echo "缺少模板：$tpl" >&2; exit 1; }

    grep -q 'map \$http_x_forwarded_proto \$uvp_forwarded_proto' "$tpl" \
        || { echo "缺少 X-Forwarded-Proto 的归一 map：$tpl" >&2; exit 1; }

    if grep -q 'proxy_set_header X-Forwarded-Proto \$scheme;' "$tpl"; then
        echo "仍然写死 \$scheme（会覆盖上游 https）：$tpl" >&2
        exit 1
    fi

    grep -q 'proxy_set_header X-Forwarded-Proto \$uvp_forwarded_proto;' "$tpl" \
        || { echo "反代没有使用归一变量：$tpl" >&2; exit 1; }
done

echo "ok: 三个交付形态的 nginx 模板都沿用上游 X-Forwarded-Proto"
