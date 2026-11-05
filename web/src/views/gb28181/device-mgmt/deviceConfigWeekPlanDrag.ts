/**
 * 录像计划时间轴的**拖拽几何**（纯函数，无 Vue / DOM 依赖）。
 *
 * 从 `DeviceConfigWeekPlan.vue` 里抽出来的唯一理由是"能单测"：拖拽的坑全在算术上
 * （吸附、边界、最短时长、跨天末端），而这些一旦写错，界面表现是"拖到一半卡住"
 * 或"悄悄把隔壁那段盖掉"，靠肉眼看很难复现。组件只负责把 clientX 换算成
 * `deltaMinutes` 再调这里。
 *
 * ## 三条口径
 *
 * 1. ⛔ **吸附到 15 分钟**：拖拽是粗调。协议里秒是必选整数，所以被拖过的那一段
 *    秒会归零（`08:00:30` → `08:00:00`）—— 这是刻意的，想保留秒就回去用输入框改。
 *    未被拖动的段一律原样返回，**绝不重排或重算**（重算等于静默改别人排的计划）。
 * 2. ⛔ **不许盖住邻居**：上界取"左边最近的结束"、下界取"右边最近的开始"，
 *    而不是按数组下标取邻居 —— 用户手输时间完全可能让数组顺序与时间顺序不一致。
 * 3. ⛔ **末端必须落 23:59:59**：协议的时间范围到 `23:59:59`，`24:00:00` 是非法的。
 *    所以 `formatMinutes(1440)` 给的是 `23:59:59` 而不是 `24:00:00`。
 */

/** 一天的分钟数。拖到最右就是 1440，格式化时收成 23:59:59。 */
export const DAY_MINUTES = 24 * 60;

/** 拖拽吸附粒度。 */
export const SNAP_MINUTES = 15;

/** 一段最短 15 分钟 —— 再短就没有实际录像意义，而且窄段在轴上根本抓不住。 */
export const MIN_SEGMENT_MINUTES = 15;

export type WeekPlanSegment = { start: string; stop: string };
export type DragMode = "move" | "start" | "stop";

const TIME_PATTERN = /^(\d{1,2}):(\d{1,2})(?::(\d{1,2}))?$/;

/** `HH:MM[:SS]` → 从 00:00 起的分钟数（可带小数秒）。非法值返回 `null`。 */
export function parseMinutes(value: string | undefined): number | null {
  const match = TIME_PATTERN.exec((value ?? "").trim());
  if (!match) return null;
  const hours = Number(match[1]);
  const minutes = Number(match[2]);
  const seconds = Number(match[3] ?? 0);
  if (hours > 24 || minutes > 59 || seconds > 59) return null;
  const total = hours * 60 + minutes + seconds / 60;
  return total > DAY_MINUTES ? null : total;
}

/** 分钟数 → `HH:MM:SS`。右端 1440 收成 `23:59:59`（标准里没有 24:00:00）。 */
export function formatMinutes(value: number): string {
  const totalSeconds = Math.min(DAY_MINUTES * 60 - 1, Math.max(0, Math.round(value * 60)));
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${pad(Math.floor(totalSeconds / 3600))}:${pad(Math.floor((totalSeconds % 3600) / 60))}:${pad(totalSeconds % 60)}`;
}

/** 吸附到最近的一格（15 分钟）。 */
export function snapMinutes(value: number): number {
  return Math.round(value / SNAP_MINUTES) * SNAP_MINUTES;
}

/** 分钟 → 轴上百分比（0~100）。 */
export function percentOfMinutes(value: number): number {
  return Math.min(100, Math.max(0, (value / DAY_MINUTES) * 100));
}

/** 轴上百分比 → 分钟。 */
export function minutesAtPercent(value: number): number {
  return (Math.min(100, Math.max(0, value)) / 100) * DAY_MINUTES;
}

function clamp(value: number, lower: number, upper: number): number {
  // ⛔ 先 `max(lower, upper)`：原段短于最短时长时（手输 5 分钟）会出现 upper < lower，
  //    直接 clamp 会把值压到 upper，反而越过了下界。
  const ceiling = Math.max(lower, upper);
  return Math.min(ceiling, Math.max(lower, value));
}

/**
 * 本段能活动的上下界（分钟）。
 *
 * 取"左边最近的结束 / 右边最近的开始"，不看数组下标：手输时间可能让顺序与时间不一致。
 */
export function neighborBounds(segments: WeekPlanSegment[], index: number): { lower: number; upper: number } {
  const self = segments[index];
  const selfStart = parseMinutes(self?.start);
  const selfStop = parseMinutes(self?.stop);
  let lower = 0;
  let upper = DAY_MINUTES;
  if (selfStart === null || selfStop === null) return { lower, upper };

  segments.forEach((segment, position) => {
    if (position === index) return;
    const start = parseMinutes(segment.start);
    const stop = parseMinutes(segment.stop);
    if (start === null || stop === null) return;
    if (stop <= selfStart) lower = Math.max(lower, stop);
    if (start >= selfStop) upper = Math.min(upper, start);
  });
  return { lower, upper };
}

/**
 * 把一次拖拽应用到某一天的计划上，返回**新的**段数组（不改入参）。
 *
 * @param segments     这一天的段（拖拽开始时的快照，不是实时值 —— 否则每帧叠加会漂移）
 * @param index        被拖的段
 * @param mode         `move` 平移 / `start` 拖左端 / `stop` 拖右端
 * @param deltaMinutes 相对按下位置的分钟位移（可负）
 */
export function applyDrag(segments: WeekPlanSegment[], index: number, mode: DragMode, deltaMinutes: number): WeekPlanSegment[] {
  const self = segments[index];
  if (!self || !Number.isFinite(deltaMinutes)) return segments;
  const start = parseMinutes(self.start);
  const stop = parseMinutes(self.stop);
  if (start === null || stop === null) return segments;

  const { lower, upper } = neighborBounds(segments, index);
  let nextStart = start;
  let nextStop = stop;

  if (mode === "move") {
    // 长度取原长度与最短时长的较大者：原段比 15 分钟还短时，拖动不该把它"放大"，
    // 但也不能缩成 0 —— 保底 15 分钟，剩下的交给用户回输入框改。
    // ⛔ 取整到分钟：`08:00:30~18:07:45` 这种带秒的原段，平移后若把 600.25 分钟原样加给
    //    吸附过的起点，秒会跑到 stop 上（18:15:15），看起来像"拖一下凭空多出 15 秒"。
    const length = Math.max(Math.round(stop - start), MIN_SEGMENT_MINUTES);
    nextStart = clamp(snapMinutes(start + deltaMinutes), lower, upper - length);
    nextStop = Math.min(nextStart + length, upper);
  } else if (mode === "start") {
    nextStart = clamp(snapMinutes(start + deltaMinutes), lower, stop - MIN_SEGMENT_MINUTES);
  } else {
    nextStop = clamp(snapMinutes(stop + deltaMinutes), start + MIN_SEGMENT_MINUTES, upper);
  }

  return segments.map((segment, position) =>
    position === index ? { start: formatMinutes(nextStart), stop: formatMinutes(nextStop) } : segment
  );
}
