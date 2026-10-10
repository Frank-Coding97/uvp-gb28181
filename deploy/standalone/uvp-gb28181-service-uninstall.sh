#!/usr/bin/env bash
# UVP-GB28181 绿色安装包 —— 卸载 systemd 服务注册
#
# 用法（在**包根目录**执行，与 uvp-gb28181-ctl.sh 同级）：
#   ./uvp-gb28181-service-uninstall.sh
#
# 非 root 运行会自动用 sudo 重新执行。
#
# ⛔⛔ 只摘掉「服务注册」这一件事，**绝不动业务数据**：
#   data/（SQLite 库 + Redis 数据 + 密钥）、config/、logs/ 全部原样保留。
#   要连数据一起清，自己删安装目录 —— 这一步刻意不自动化：
#   误删客户数据库的代价，远大于手工多敲一条 rm 的麻烦。

set -Eeuo pipefail

UNIT_NAME="uvp-gb28181.service"
UNIT_PATH="/etc/systemd/system/${UNIT_NAME}"

log()  { printf '[uvp-service] %s\n' "$*"; }
fail() { printf '[uvp-service][ERROR] %s\n' "$*" >&2; exit 1; }

SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
ROOT="$(dirname "$SELF")"

if [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 \
    || fail "需要 root 权限，而本机没有 sudo —— 请直接用 root 账号执行"
  log "需要 root 权限，正在通过 sudo 重新执行…"
  exec sudo -- "$SELF" "$@"
fi

command -v systemctl >/dev/null 2>&1 \
  || fail "本机没有 systemctl —— 这里没有可卸载的服务注册"

if [ ! -f "$UNIT_PATH" ]; then
  # ⛔ 幂等：没装过也要正常退出 0（下次仍是「没装过」）。顺手清掉可能的 failed 残留记录。
  systemctl reset-failed "$UNIT_NAME" >/dev/null 2>&1 || true
  log "未发现服务单元 ${UNIT_PATH} —— 本包没有注册过服务，无需卸载"
  exit 0
fi

log "停止服务并取消开机自启…"
# ⛔ disable --now 中的 stop 会执行 unit 里的 ExecStop（= ctl stop），systemd 会**按 unit 的
#   User= 降权**执行它 ⇒ 不会像「用 root 直接跑 ctl」那样把 logs/app 之类建出 root 属主目录。
# ⛔ 失败不当致命错误：服务可能本来就处于 failed/dead，仍要继续把注册摘干净。
systemctl disable --now "$UNIT_NAME" 2>&1 | sed 's/^/    /' || true

rm -f "$UNIT_PATH"
systemctl daemon-reload
systemctl reset-failed "$UNIT_NAME" >/dev/null 2>&1 || true
log "已移除服务单元 ${UNIT_PATH}"

# ------------------------------------------------------------ 残留进程检查 ----
# ⛔ 实测 ctl stop 有漏杀 ZLM 的先例（PID 文件缺失时按可执行路径兜底也没盖全）。
#   服务被摘掉了、进程却还活着 ⇒ 下次手工 start 会撞端口，而现象是「端口莫名其妙被占」。
leftover="$(pgrep -af "$ROOT" 2>/dev/null | grep -vE 'uvp-gb28181-service-(un)?install' || true)"
if [ -n "$leftover" ]; then
  log "⚠️ 安装目录下仍有进程在跑（可能是 ctl stop 漏掉的）："
  printf '%s\n' "$leftover" | sed 's/^/    /'
  printf '[uvp-service] 需要的话手工结束：pkill -f %s\n' "$ROOT"
fi

cat <<DONE

服务注册已移除，**数据与配置都还在**（未被删除）：
  ${ROOT}/data/      SQLite 数据库、Redis 数据、运行期密钥
  ${ROOT}/config/    config.yml + config.env
  ${ROOT}/logs/      全部日志

重新启用：./uvp-gb28181-service-install.sh
临时手工启停：./uvp-gb28181-ctl.sh start|stop|status

DONE
