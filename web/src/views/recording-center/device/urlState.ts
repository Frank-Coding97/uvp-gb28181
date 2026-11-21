import dayjs from "dayjs";

/**
 * 设备录像页的日期 / 时间参数处理。
 *
 * 这一页的核心心智是「通道 + 哪一天」，所以 URL 里只保留两个值：
 *   `?date=2026-11-20&t=14:23:07`
 * 日期决定查询范围，`t` 决定进来之后播放头停在哪、以及是否自动开播。
 *
 * ⛔ 时间一律按**后端时区**理解（查询接口的 `timezone` 字段），
 *    与 `createDefaultRecordQueryForm` 保持同一口径：格式 `YYYY-MM-DDTHH:mm:ss`，
 *    不带时区后缀，由后端按 timezone 解释。前端如果偷偷换成 UTC 或浏览器本地时区，
 *    会出现「查询范围和用户看到的日期差一天」这种最难查的错。
 */

export const DAY_FORMAT = "YYYY-MM-DD";
export const DATE_TIME_FORMAT = "YYYY-MM-DDTHH:mm:ss";

export interface DayRange {
  startTime: string;
  endTime: string;
}

/** 读取 URL 里的日期参数；不合法就回退。 */
export function parseDateParam(value: unknown, fallback: string): string {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return fallback;
  return dayjs(value).isValid() ? value : fallback;
}

/**
 * 读取 URL 里的播放定位参数。
 *
 * 允许两种写法：`14:23:07`（当天的时间点，最常见）和完整的
 * `2026-11-20T14:23:07`（跨天分享时更精确）。解析不出来一律当成「没有指定」，
 * 不能让一个手写错的链接把页面搞崩。
 */
export function parseTimeParam(value: unknown): string | null {
  if (typeof value !== "string" || !value) return null;
  const raw = value.trim();
  const timeOnly = /^(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(raw);
  if (timeOnly) {
    const [, hour, minute, second = "00"] = timeOnly;
    if (Number(hour) > 23 || Number(minute) > 59 || Number(second) > 59) return null;
    return `${hour}:${minute}:${second}`;
  }
  const parsed = dayjs(raw);
  return parsed.isValid() ? parsed.format(DATE_TIME_FORMAT) : null;
}

/**
 * 生成某一天 00:00:00 → 23:59:59 的查询区间。
 *
 * `ceiling` 用于「今天」：不查未来时间段（设备侧对未发生的区间没有意义，
 * 部分设备还会因此返回错误）。传后端返回的 `serverNow` 最准确。
 */
export function dayRange(date: string, ceiling?: string | null): DayRange {
  const start = dayjs(date).startOf("day");
  const end = start.add(1, "day").subtract(1, "second");
  const capped = ceiling && dayjs(ceiling).isValid() && dayjs(ceiling).isBefore(end) ? dayjs(ceiling) : end;
  return {
    startTime: start.format(DATE_TIME_FORMAT),
    endTime: capped.format(DATE_TIME_FORMAT)
  };
}

/** 日期加减，用于 ‹ › 和「今天」按钮。 */
export function shiftDate(date: string, days: number): string {
  return dayjs(date).add(days, "day").format(DAY_FORMAT);
}

/** 该日期是否就是 `reference` 所在的那一天（用来判断「今天」并限制不查未来）。 */
export function isSameDay(date: string, reference: string | null | undefined): boolean {
  if (!reference) return false;
  const target = dayjs(reference);
  return target.isValid() && target.format(DAY_FORMAT) === date;
}

/** 后端给的时间带时区（`+08:00` / `Z`），展示时去掉后缀，只留本地可读形态。 */
export function formatDateTime(value: string | null | undefined): string {
  if (!value) return "--";
  return value.replace("T", " ").replace(/([+-]\d{2}:\d{2}|Z)$/, "");
}

export function formatClock(value: string | null | undefined): string {
  return value ? formatDateTime(value).slice(11, 19) : "--:--:--";
}

/** 单段时长，按 `HH:mm:ss` 展示。 */
export function durationText(startTime: string | null | undefined, endTime: string | null | undefined): string {
  const seconds = Math.max(0, Math.floor((Date.parse(endTime || "") - Date.parse(startTime || "")) / 1000));
  if (!Number.isFinite(seconds)) return "--";
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remain = seconds % 60;
  return [hours, minutes, remain].map(value => String(value).padStart(2, "0")).join(":");
}

/**
 * 把 URL 里的 `HH:mm:ss` 落到某一天上，得到完整时间。
 * 解析不出来就返回当天起点，保证调用方拿到的永远是一个可用时间。
 */
export function resolveTimeOnDate(date: string, time: string | null): string {
  const start = dayjs(date).startOf("day");
  if (!time) return start.format(DATE_TIME_FORMAT);
  const clock = /^(\d{2}):(\d{2}):(\d{2})$/.exec(time);
  if (clock) {
    return start
      .add(Number(clock[1]), "hour")
      .add(Number(clock[2]), "minute")
      .add(Number(clock[3]), "second")
      .format(DATE_TIME_FORMAT);
  }
  const parsed = dayjs(time);
  return parsed.isValid() ? parsed.format(DATE_TIME_FORMAT) : start.format(DATE_TIME_FORMAT);
}
