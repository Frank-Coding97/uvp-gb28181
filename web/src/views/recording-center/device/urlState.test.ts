import { describe, expect, it } from "vitest";
import {
  dayRange,
  durationText,
  formatClock,
  isSameDay,
  parseDateParam,
  parseTimeParam,
  resolveTimeOnDate,
  shiftDate
} from "./urlState";

describe("设备录像页的日期参数", () => {
  it("只接受 YYYY-MM-DD，其余一律回退到默认值", () => {
    expect(parseDateParam("2026-11-20", "2026-01-01")).toBe("2026-11-20");
    expect(parseDateParam("2026/11/20", "2026-01-01")).toBe("2026-01-01");
    expect(parseDateParam("", "2026-01-01")).toBe("2026-01-01");
    expect(parseDateParam(undefined, "2026-01-01")).toBe("2026-01-01");
    expect(parseDateParam(20261120, "2026-01-01")).toBe("2026-01-01");
  });

  it("时间点支持 HH:mm:ss 和完整时间两种写法，非法值当作没指定", () => {
    expect(parseTimeParam("14:23:07")).toBe("14:23:07");
    expect(parseTimeParam("14:23")).toBe("14:23:00");
    expect(parseTimeParam("2026-11-20T14:23:07")).toBe("2026-11-20T14:23:07");
    expect(parseTimeParam("24:00:00")).toBeNull();
    expect(parseTimeParam("12:99:00")).toBeNull();
    expect(parseTimeParam("不是时间")).toBeNull();
    expect(parseTimeParam(undefined)).toBeNull();
  });
});

describe("按天查询区间", () => {
  it("生成 00:00:00 → 23:59:59 的整天范围", () => {
    expect(dayRange("2026-11-20")).toEqual({
      startTime: "2026-11-20T00:00:00",
      endTime: "2026-11-20T23:59:59"
    });
  });

  it("传了上限（今天的 serverNow）就截到那一刻，不查未来", () => {
    expect(dayRange("2026-11-20", "2026-11-20T14:05:00")).toEqual({
      startTime: "2026-11-20T00:00:00",
      endTime: "2026-11-20T14:05:00"
    });
  });

  it("上限落在别的日期时不影响整天范围", () => {
    // 翻看历史日期时 serverNow 一定在更晚的一天，不能因此把区间截断。
    expect(dayRange("2026-11-18", "2026-11-20T14:05:00").endTime).toBe("2026-11-18T23:59:59");
  });

  it("天数加减能跨月", () => {
    expect(shiftDate("2026-11-20", -1)).toBe("2026-11-19");
    expect(shiftDate("2026-12-01", -1)).toBe("2026-11-30");
    expect(shiftDate("2026-11-30", 1)).toBe("2026-12-01");
  });

  it("判断是不是同一天用日期前缀，不受时区后缀影响", () => {
    expect(isSameDay("2026-11-20", "2026-11-20T14:05:00")).toBe(true);
    expect(isSameDay("2026-11-20", "2026-11-20T00:05:00+08:00")).toBe(true);
    expect(isSameDay("2026-11-20", "2026-11-21T00:05:00")).toBe(false);
    expect(isSameDay("2026-11-20", null)).toBe(false);
  });

  it("把 URL 里的时间点落到具体某一天", () => {
    expect(resolveTimeOnDate("2026-11-20", "14:23:07")).toBe("2026-11-20T14:23:07");
    // 没带时间点就从当天零点开始，调用方拿到的永远是可用的时间串
    expect(resolveTimeOnDate("2026-11-20", null)).toBe("2026-11-20T00:00:00");
  });
});

describe("展示格式", () => {
  it("去掉时间串的时区后缀，只留本地可读形态", () => {
    expect(formatClock("2026-11-20T14:23:07+08:00")).toBe("14:23:07");
    expect(formatClock("2026-11-20T14:23:07Z")).toBe("14:23:07");
    expect(formatClock(null)).toBe("--:--:--");
  });

  it("单段时长按 HH:mm:ss 展示", () => {
    expect(durationText("2026-11-20T14:00:00", "2026-11-20T15:30:05")).toBe("01:30:05");
    expect(durationText(null, null)).toBe("--");
  });
});
