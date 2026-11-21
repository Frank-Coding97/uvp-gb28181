import { getAccessToken } from "@/utils/auth";
import { baseUrlApi } from "./utils";

/**
 * 一条待弹出的设备告警（由后端 SSE 推送，字段见
 * server/app/gb28181/alarmpopup/hub.go 的 Event）。
 *
 * `alarmId` 在「不存储报警消息」时为空 —— 那条链路本来就不落库，
 * 前端不要拿它当唯一标识（用 {@link alarmPopupKey}）。
 */
export interface AlarmPopupEvent {
  alarmId?: number;
  deviceCode: string;
  deviceName: string;
  deviceAlias?: string;
  channelCode?: string;
  channelName?: string;
  channelAlias?: string;
  sourceCode?: string;
  priority?: number;
  method?: number;
  alarmType?: number;
  alarmTypeParam?: string;
  description?: string;
  alarmTime?: string;
  receivedAt: string;
}

/**
 * 告警实时流的 SSE 地址。
 *
 * ⛔⛔ 路径**复用** `GET /api/gb28181/alarms`（靠 `?stream=1` 分流），不要改成
 * `/alarms/stream`：casbin 用 `c.Request.URL.Path` 匹配策略，新开一条路由就要同步补
 * sys_api + sys_menu_api + casbin 三处，漏一处线上立刻 403
 * （记忆见 `.workbuddy/memory/topics/api-permission-governance.md`）。
 *
 * EventSource 不能自定义请求头，JWT 只能走查询参数（后端中间件支持双通道）。
 */
export function buildAlarmStreamUrl(): string {
  const search = new URLSearchParams();
  const tokenData = getAccessToken();
  if (tokenData?.accessToken) {
    search.set("token", tokenData.accessToken);
  }
  search.set("stream", "1");
  return `${baseUrlApi("gb28181/alarms")}?${search.toString()}`;
}
