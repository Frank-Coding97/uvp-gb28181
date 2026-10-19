#!/bin/sh
# 流媒体容器入口：填充持久化目录 → 按部署参数改写 config.ini → 拉起 MediaServer
set -eu

cd /opt/zlm

# ⛔ 为什么必须"种子复制"而不是直接挂空卷：
#   www 目录同时承担两件事 —— ① ZLM 自带的 Web 资源 ② 录像/截图/HLS 的落盘位置。
#   宿主机卷一挂上来就是空的，ZLM 的静态资源会全部消失（表现为 ZLM 的 HTTP 口
#   能连上但什么都不返回）。所以第一次启动要把镜像里的那份铺进去。
# ⛔ 只在**空目录**时铺：已经跑过一次、里面有录像了就绝不能再覆盖。
if [ -z "$(ls -A /opt/zlm/www 2>/dev/null)" ]; then
  echo "[uvp-zlm] www 为空，从镜像种子初始化" >&2
  cp -a /opt/zlm/www.default/. /opt/zlm/www/
fi

mkdir -p /opt/zlm/ffmpeg /opt/zlm/log

# ⛔⛔ 配置用**副本**（/opt/zlm/run/config.ini），不动镜像里的 /opt/zlm/config.ini：
#   后者属主是 root，非 root 容器既不能原地改、也建不了同目录的临时文件
#   （实测报 `cannot create config.ini.tmp: Permission denied`，而且这是
#    set -e 下的一行 stderr —— 端口改写直接失效，ZLM 仍起在 51004）。
#   run 目录在 Dockerfile 里已 0777。
#
# ⛔ 相对路径兜底：config.ini 里写的是 ./www、./ffmpeg、./log。
#   我们不确定 ZLM 是拿 **cwd** 还是 **配置文件所在目录** 当基准，
#   所以在 run 里放三个软链，两种基准都指到同一个真实目录 ——
#   否则录像会落到 run/www 下，看起来像"录像功能坏了"。
RUN_DIR=/opt/zlm/run
[ -f "$RUN_DIR/config.ini" ] || cp /opt/zlm/config.ini "$RUN_DIR/config.ini"
ln -sfn /opt/zlm/www    "$RUN_DIR/www"
ln -sfn /opt/zlm/ffmpeg "$RUN_DIR/ffmpeg"
ln -sfn /opt/zlm/log    "$RUN_DIR/log"

# ⛔⛔ 先把 CRLF 统一成 LF，再改写。
#   随包的 config.ini 是 **Windows 行尾（CRLF）**：段名行实际是 "[http]\r"。
#   不去掉 \r 的话，下面 awk 里 `sec=="http"` 永远不成立 ⇒
#   **端口一行都改不到，但脚本不报错**（awk 只是照原样 print），
#   表现是日志写着"端口基址 53004"、ZLM 却仍 Listen on 51005，
#   然后 `address already in use` 无限重启 —— 完全指不到行尾。
#   实测踩过：hook 那步用 sed 能改成功（sed 不依赖行尾），
#   只有按段名匹配的 awk 全军覆没，所以"一半改了、一半没改"。
tr -d '\r' < "$RUN_DIR/config.ini" > "$RUN_DIR/config.ini.lf"
mv "$RUN_DIR/config.ini.lf" "$RUN_DIR/config.ini"

# ============================================================ 端口改写 ====
# ⛔⛔ 为什么必须在这里改，而不是做完镜像就固定死：
#   ZLM 的端口是写在 config.ini 里的（不像后端能只改一个环境变量），
#   而客户机上「51000 段被别的系统占了」是常见情况 —— 绿色包靠 ctl.sh 统一
#   规划端口解决，容器版没有 ctl.sh，只能由 entrypoint 在启动时改写。
#
# ⛔⛔ 这里写的是**绝对目标值**，不是「在当前值基础上加偏移」：
#   ZLM 收到 setServerConfig 会把配置**写回自己的 config.ini**
#   （后端下发的 general.mediaServerId / hook.* 都这样落盘）。
#   若按「当前值 + delta」算，容器重启第二次就会在已改写过的值上再加一次，
#   端口一路漂走；写成绝对目标值才是幂等的。
#
# ⛔ 端口布局不是连续的，必须逐段按 ZLM 的语义映射（相对 51004 的偏移）：
#     http.port=base+0      rtsp.port=base+1    rtmp.port=base+2   http.sslport=base+3
#     rtc.port=base+4       rtc.signalingPort=base+5   rtc.signalingSslPort=base+6
#     srt.port=base+7       onvif.port=base+8   rtc.icePort=base+9
#     rtp_proxy.port=base+10   rtp_proxy.port_range=base+10..base+59
#   ⛔ [rtc] 段里还有一个 `port_range=49152-65535`（ICE 临时端口），
#      不能跟着平移 —— 它不在 51000 规划内，动它会破坏 RTC。
ZLM_BASE="${UVP_ZLM_PORT_BASE:-51004}"
BACKEND_PORT="${UVP_BACKEND_PORT:-51002}"

echo "[uvp-zlm] 端口基址 ${ZLM_BASE}，hook 回调端口 ${BACKEND_PORT}" >&2

awk -v base="$ZLM_BASE" '
function p(off) { return base + off }
/^\[/ { sec=$0; gsub(/[][ \t]/, "", sec); print; next }
sec=="http"      && /^port=/             { print "port=" p(0);           next }
sec=="http"      && /^sslport=/          { print "sslport=" p(3);        next }
sec=="rtsp"      && /^port=/             { print "port=" p(1);           next }
sec=="rtmp"      && /^port=/             { print "port=" p(2);           next }
sec=="srt"       && /^port=/             { print "port=" p(7);           next }
sec=="onvif"     && /^port=/             { print "port=" p(8);           next }
sec=="rtc"       && /^signalingPort=/    { print "signalingPort=" p(5);  next }
sec=="rtc"       && /^signalingSslPort=/ { print "signalingSslPort=" p(6); next }
sec=="rtc"       && /^iceTcpPort=/       { print "iceTcpPort=" p(9);     next }
sec=="rtc"       && /^icePort=/          { print "icePort=" p(9);        next }
sec=="rtc"       && /^tcpPort=/          { print "tcpPort=" p(4);        next }
sec=="rtc"       && /^port=/             { print "port=" p(4);           next }
sec=="rtp_proxy" && /^port_range=/       { print "port_range=" p(10) "-" p(59); next }
sec=="rtp_proxy" && /^port=/             { print "port=" p(10);          next }
{ print }
' "$RUN_DIR/config.ini" > "$RUN_DIR/config.ini.tmp"
mv "$RUN_DIR/config.ini.tmp" "$RUN_DIR/config.ini"

# ---- hook 回调地址里的后端端口 ----
# ⛔ 只改「本平台的 managed hook 端点」（/index/hook/ 这个路径前缀）：
#   cap 与 node 参数**一个都不能动** —— cap 是由 zlm.secret + mediaServerId
#   确定性派生的（playauth.HookCapability），跟端口无关；
#   但整体重写 URL 时很容易顺手把它们弄丢，丢了就是"死 hook"：
#   平台静默拒收全部回调，日志里只有 signal_phase='' / broadcast_sn=0 这种间接痕迹。
sed -i -E "s#127\.0\.0\.1:[0-9]+/index/hook/#127.0.0.1:${BACKEND_PORT}/index/hook/#g" "$RUN_DIR/config.ini"

# ---- 节点身份 ----
# ⛔ 必须与 config.yml 的 gb28181.zlm.mediaserverid 一致：
#   它同时决定 hook URL 里的 node 参数与 cap 的派生输入。
#   两者不一致 ⇒ ZLM 上报的 hook 认不出属于哪个节点（hook.server_started_node_unresolved）。
if [ -n "${UVP_ZLM_MEDIA_SERVER_ID:-}" ]; then
  awk -v want="$UVP_ZLM_MEDIA_SERVER_ID" '
    /^\[/ { sec=$0; gsub(/[][ \t]/, "", sec); print; next }
    sec=="general" && /^mediaServerId=/ { print "mediaServerId=" want; next }
    { print }
  ' "$RUN_DIR/config.ini" > "$RUN_DIR/config.ini.tmp"
  mv "$RUN_DIR/config.ini.tmp" "$RUN_DIR/config.ini"
fi

exec /opt/zlm/MediaServer -c "$RUN_DIR/config.ini"
