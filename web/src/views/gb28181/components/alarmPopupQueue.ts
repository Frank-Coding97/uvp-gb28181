import type { AlarmPopupEvent } from "@/api/gb28181-alarm-stream";
import { alarmTypeOptionsForMethod } from "../alarm-management/alarmState";

/**
 * 右侧告警浮层的纯逻辑部分（便于单测，不在组件里做判断）。
 */

/** 同时最多堆几张卡片。超出后挤掉最旧的一张，避免刷屏把屏幕占满。 */
export const MAX_ALARM_POPUPS = 4;
/** 每张卡片停留时长：到点先播离场动画，再真正移除。 */
export const ALARM_POPUP_TTL_MS = 6000;
/** 离场动画时长，必须与组件里 transition 的 CSS 时长一致。 */
export const ALARM_POPUP_LEAVE_MS = 320;

export interface AlarmPopupItem {
  /** 稳定标识，用于去重与定位（见 {@link alarmPopupKey}）。 */
  key: string;
  event: AlarmPopupEvent;
  /** 已进入离场动画，此时不再响应点击。 */
  leaving: boolean;
}

/**
 * 生成卡片标识。
 *
 * ⛔ 不能只用 `alarmId`：关闭「存储报警消息」时那条链路不落库，`alarmId` 恒为空；
 * 也不能只用 `deviceCode`（同一设备连续报警会被误判成重复）。
 * `seq` 是组件内的单调序号，兜底保证同一毫秒内的两条也不会撞。
 */
export function alarmPopupKey(event: AlarmPopupEvent, seq: number): string {
  if (event.alarmId) return `alarm-${event.alarmId}`;
  return [event.deviceCode, event.sourceCode || "", event.receivedAt || "", seq].join("#");
}

/** 入队：同 key 视为重复（设备重发），直接忽略；超上限挤掉最旧的。 */
export function enqueueAlarmPopup(items: AlarmPopupItem[], item: AlarmPopupItem): AlarmPopupItem[] {
  if (items.some(existing => existing.key === item.key)) return items;
  const next = [...items, item];
  return next.length > MAX_ALARM_POPUPS ? next.slice(next.length - MAX_ALARM_POPUPS) : next;
}

export function markAlarmPopupLeaving(items: AlarmPopupItem[], key: string): AlarmPopupItem[] {
  return items.map(item => (item.key === key ? { ...item, leaving: true } : item));
}

export function removeAlarmPopup(items: AlarmPopupItem[], key: string): AlarmPopupItem[] {
  return items.filter(item => item.key !== key);
}

/**
 * 名称回退链：别名 → 名称 → 编码。
 *
 * ⛔ 必须逐级 trim 后取第一个非空（不能写 `a || b`）：设备没上报名称时后端会给
 * `alias: ""`，空串在 `||` 里会一路穿到编码上，把"别名优先"的语义弄丢。
 */
export function alarmPopupEntityName(entity: { alias?: string; name?: string; code?: string }): string {
  for (const candidate of [entity.alias, entity.name, entity.code]) {
    const trimmed = candidate?.trim();
    if (trimmed) return trimmed;
  }
  return "";
}

/** 卡片主标题：报警类型文案 > 设备描述 > 兜底文案。 */
export function alarmPopupTitle(event: AlarmPopupEvent): string {
  const label = alarmTypeOptionsForMethod(event.method).find(option => option.value === event.alarmType)?.label;
  if (label) return label;
  const description = event.description?.trim();
  if (description) return description;
  return "设备报警";
}

/** 卡片副标题：设备名 · 通道名（通道缺省时只显示设备名）。 */
export function alarmPopupSubtitle(event: AlarmPopupEvent): string {
  const device = alarmPopupEntityName({ alias: event.deviceAlias, name: event.deviceName, code: event.deviceCode });
  const channel = alarmPopupEntityName({ alias: event.channelAlias, name: event.channelName, code: event.channelCode });
  if (device && channel) return `${device} · ${channel}`;
  return device || channel;
}

/** 卡片第三行：设备描述（与主标题重复时不显示）。 */
export function alarmPopupDetail(event: AlarmPopupEvent): string {
  const description = event.description?.trim();
  if (!description || description === alarmPopupTitle(event)) return "";
  return description;
}

/** 报警级别：1 最严重，4 最轻；缺值返回 0（灰色）。 */
export function alarmPopupPriority(event: AlarmPopupEvent): number {
  const priority = event.priority;
  return priority === 1 || priority === 2 || priority === 3 || priority === 4 ? priority : 0;
}

/** 形如 14:23:05 的本地时间；解析不了就返回空串（不显示比显示 Invalid Date 强）。 */
export function formatAlarmPopupTime(receivedAt: string): string {
  const time = Date.parse(receivedAt);
  if (!Number.isFinite(time)) return "";
  const date = new Date(time);
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}
