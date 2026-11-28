import { describe, expect, it } from "vitest";
import {
  applyDrag,
  formatMinutes,
  neighborBounds,
  parseMinutes,
  percentOfMinutes,
  snapMinutes,
  type WeekPlanSegment
} from "./deviceConfigWeekPlanDrag";

/** 两条不重叠的段：08:00~10:00 与 12:00~14:00。 */
const TWO_SEGMENTS: WeekPlanSegment[] = [
  { start: "08:00:00", stop: "10:00:00" },
  { start: "12:00:00", stop: "14:00:00" }
];

describe("录像计划拖拽几何", () => {
  it("时间解析：24:00:00 合法（= 一天结束），越界与乱写返回 null", () => {
    expect(parseMinutes("08:30:30")).toBeCloseTo(510.5);
    expect(parseMinutes("24:00:00")).toBe(1440);
    expect(parseMinutes("8:05")).toBe(485);
    expect(parseMinutes("25:00:00")).toBeNull();
    expect(parseMinutes("08:60:00")).toBeNull();
    expect(parseMinutes("")).toBeNull();
    expect(parseMinutes(undefined)).toBeNull();
    expect(parseMinutes("早上八点")).toBeNull();
  });

  it("格式化：右端收成 23:59:59，标准里没有 24:00:00", () => {
    expect(formatMinutes(0)).toBe("00:00:00");
    expect(formatMinutes(1440)).toBe("23:59:59");
    // 超界的值也不能溢出成 25:00:00
    expect(formatMinutes(1500)).toBe("23:59:59");
    expect(formatMinutes(-5)).toBe("00:00:00");
    expect(formatMinutes(510.5)).toBe("08:30:30");
  });

  it("吸附到 15 分钟", () => {
    expect(snapMinutes(7)).toBe(0);
    expect(snapMinutes(8)).toBe(15);
    expect(snapMinutes(97)).toBe(90);
    expect(snapMinutes(-20)).toBe(-15);
  });

  it("邻居边界按时间算，不按数组下标：乱序数组也不会盖住别人", () => {
    const unordered: WeekPlanSegment[] = [
      { start: "12:00:00", stop: "14:00:00" },
      { start: "08:00:00", stop: "10:00:00" }
    ];
    // 下标 1 是 08:00~10:00，左边无、右边是 12:00
    expect(neighborBounds(unordered, 1)).toEqual({ lower: 0, upper: 720 });
    expect(neighborBounds(unordered, 0)).toEqual({ lower: 600, upper: 1440 });
  });

  it("平移：吸附 15 分钟，被邻居挡住时贴边停，不越界", () => {
    const moved = applyDrag(TWO_SEGMENTS, 0, "move", 60);
    expect(moved[0]).toEqual({ start: "09:00:00", stop: "11:00:00" });
    // 后一段必须原样带回来，不许被重算
    expect(moved[1]).toEqual(TWO_SEGMENTS[1]);

    // 往后拖 5 小时：长度 2h ⇒ 最晚只能贴到后一段的开始 12:00
    expect(applyDrag(TWO_SEGMENTS, 0, "move", 300)[0]).toEqual({ start: "10:00:00", stop: "12:00:00" });
    // 往前拖 10 小时：贴到 00:00
    expect(applyDrag(TWO_SEGMENTS, 0, "move", -600)[0]).toEqual({ start: "00:00:00", stop: "02:00:00" });
  });

  it("拖左端：不短于 15 分钟、不越过左邻的结束", () => {
    expect(applyDrag(TWO_SEGMENTS, 1, "start", -60)[0]).toEqual(TWO_SEGMENTS[0]);
    // 12:00 往左拖 10 分钟 → 11:45
    expect(applyDrag(TWO_SEGMENTS, 1, "start", -10)[1]).toEqual({ start: "11:45:00", stop: "14:00:00" });
    // 往右拖 5 小时：最多到 13:45（保 15 分钟）
    expect(applyDrag(TWO_SEGMENTS, 1, "start", 300)[1]).toEqual({ start: "13:45:00", stop: "14:00:00" });
    // 往左拖到前一天：被前一段 10:00 挡住
    expect(applyDrag(TWO_SEGMENTS, 1, "start", -300)[1]).toEqual({ start: "10:00:00", stop: "14:00:00" });
  });

  it("拖右端：不越过右邻的开始、不短于 15 分钟", () => {
    // 08:00~10:00 的 stop 往右拖 5 小时 → 被 12:00 挡住
    expect(applyDrag(TWO_SEGMENTS, 0, "stop", 300)[0]).toEqual({ start: "08:00:00", stop: "12:00:00" });
    // 往左拖到 05:00 → 保底 08:15
    expect(applyDrag(TWO_SEGMENTS, 0, "stop", -300)[0]).toEqual({ start: "08:00:00", stop: "08:15:00" });
  });

  it("拖右端到最右给 23:59:59，不是 24:00:00", () => {
    const segments: WeekPlanSegment[] = [{ start: "20:00:00", stop: "21:00:00" }];
    expect(applyDrag(segments, 0, "stop", 600)[0]).toEqual({ start: "20:00:00", stop: "23:59:59" });
    // 往左拖到 00:00：起点被 0 卡住，长度不变
    expect(applyDrag(segments, 0, "start", -2000)[0]).toEqual({ start: "00:00:00", stop: "21:00:00" });
  });

  it("只抹掉被拖那一段的秒，没被拖的段一律原样", () => {
    const segments: WeekPlanSegment[] = [
      { start: "08:00:30", stop: "10:00:45" },
      { start: "12:00:15", stop: "14:00:00" }
    ];
    const moved = applyDrag(segments, 0, "move", 15);
    expect(moved[0]).toEqual({ start: "08:15:00", stop: "10:15:00" });
    // 后一段的秒必须还在
    expect(moved[1]).toEqual({ start: "12:00:15", stop: "14:00:00" });

    // 只拖右端时，起点那 30 秒不能被顺手抹掉
    expect(applyDrag(segments, 0, "stop", 15)[0]).toEqual({ start: "08:00:30", stop: "10:15:00" });
  });

  it("原段短于 15 分钟时不会被拖动放大成别处", () => {
    const tiny: WeekPlanSegment[] = [{ start: "09:00:00", stop: "09:05:00" }];
    const moved = applyDrag(tiny, 0, "move", 0);
    expect(moved[0]).toEqual({ start: "09:00:00", stop: "09:15:00" });
  });

  it("非法段与非法位移原样返回，不抛错", () => {
    const broken: WeekPlanSegment[] = [{ start: "乱写", stop: "10:00:00" }];
    expect(applyDrag(broken, 0, "move", 60)).toBe(broken);
    expect(applyDrag(TWO_SEGMENTS, 0, "move", Number.NaN)).toBe(TWO_SEGMENTS);
    expect(applyDrag(TWO_SEGMENTS, 9, "move", 60)).toBe(TWO_SEGMENTS);
  });

  it("百分比换算与一天长度对齐", () => {
    expect(percentOfMinutes(0)).toBe(0);
    expect(percentOfMinutes(720)).toBe(50);
    expect(percentOfMinutes(1440)).toBe(100);
  });
});
