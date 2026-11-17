#!/usr/bin/env sh

set -eu

config_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
config_file="$config_dir/nginx.conf"
edge_config_file="$config_dir/uvplatform.npm.conf"
frontend_config_file="$config_dir/uvp-frontend.nginx.conf"

content_location='location ~ ^/api/gb28181/cloud-recordings/downloads/[^/]+/content$'
location_line=$(grep -n -F "$content_location" "$config_file" | cut -d: -f1)
api_line=$(grep -n -F 'location /api/' "$config_file" | cut -d: -f1)
upgrade_import_line=$(grep -n -F 'location = /api/system/upgrade/import' "$config_file" | cut -d: -f1)
gb_line=$(grep -n -F 'location /gb28181/' "$config_file" | cut -d: -f1)
openapi_line=$(grep -n -F 'location ~ ^/openapi(?:/|$)' "$config_file" | cut -d: -f1)
share_viewer_line=$(grep -n -F 'location ^~ /api/public/video-share/viewers/' "$frontend_config_file" | cut -d: -f1)
frontend_api_line=$(grep -n -F 'location /api/' "$frontend_config_file" | cut -d: -f1)

[ -n "$location_line" ]
[ -n "$api_line" ]
[ -n "$upgrade_import_line" ]
[ -n "$gb_line" ]
[ -n "$openapi_line" ]
[ -n "$share_viewer_line" ]
[ -n "$frontend_api_line" ]
[ "$location_line" -lt "$api_line" ]
[ "$upgrade_import_line" -lt "$api_line" ]
[ "$share_viewer_line" -lt "$frontend_api_line" ]

share_viewer_block=$(sed -n "${share_viewer_line},${frontend_api_line}p" "$frontend_config_file")
for directive in 'access_log off;' 'proxy_buffering off;' 'proxy_request_buffering off;' 'gzip off;'; do
    printf '%s\n' "$share_viewer_block" | grep -F -- "$directive" >/dev/null
done

for template in \
    "$config_dir/../standalone/nginx.conf.template" \
    "$config_dir/../standalone-windows/nginx.conf.template" \
    "$config_dir/../compose/nginx/nginx.conf.template"
do
    [ "$(grep -c -F 'location ^~ /api/public/video-share/viewers/' "$template")" -eq 2 ]
    for directive in 'access_log off;' 'proxy_buffering       off;' 'proxy_request_buffering off;' 'gzip off;'; do
        grep -F -- "$directive" "$template" >/dev/null
    done
done

location_block=$(sed -n "${location_line},${api_line}p" "$config_file")
for directive in \
    'proxy_buffering off;' \
    'proxy_request_buffering off;' \
    'gzip off;' \
    'access_log off;' \
    'proxy_http_version 1.1;' \
    'proxy_read_timeout 120s;' \
    'proxy_send_timeout 120s;' \
    'proxy_set_header Host $http_host;' \
    'proxy_set_header X-Real-IP $remote_addr;' \
    'proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;' \
    'proxy_set_header X-Forwarded-Proto $scheme;' \
    'proxy_set_header X-UVP-Device-Base-URL $uvp_device_http_base;'
do
    printf '%s\n' "$location_block" | grep -F -- "$directive" >/dev/null
done

for location_line in "$upgrade_import_line" "$api_line" "$gb_line" "$openapi_line"; do
    route_block=$(sed -n "${location_line},$((location_line + 18))p" "$config_file")
    printf '%s\n' "$route_block" | grep -F -- 'proxy_set_header Host $http_host;' >/dev/null
    printf '%s\n' "$route_block" | grep -F -- 'proxy_set_header X-UVP-Device-Base-URL $uvp_device_http_base;' >/dev/null
done

upgrade_import_block=$(sed -n "${upgrade_import_line},${api_line}p" "$config_file")
for directive in \
    'client_max_body_size 9g;' \
    'client_body_timeout 30m;' \
    'proxy_send_timeout 30m;' \
    'proxy_read_timeout 30m;' \
    'proxy_request_buffering off;'
do
    printf '%s\n' "$upgrade_import_block" | grep -F -- "$directive" >/dev/null
done

grep -F -- 'proxy_cookie_flags ~^uvp_recording_download_ secure httponly samesite=strict;' "$edge_config_file" >/dev/null
