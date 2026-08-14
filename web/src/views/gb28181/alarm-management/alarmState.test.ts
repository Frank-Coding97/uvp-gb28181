import { describe, expect, it } from "vitest";

import {
  alarmPriorityTagColor,
  alarmTypeOptionsForMethod,
  displayAlarmEntityName,
  mayDeleteAlarms,
  mayViewAlarms,
  normalizeAlarmQuery,
  pageAfterAlarmDeletion
} from "./alarmState";

describe("alarm type labels", () => {
  it("uses method-specific GB/T 28181 alarm type meanings", () => {
    expect(alarmTypeOptionsForMethod(2)).toContainEqual({ value: 2, label: "设备防拆报警" });
    expect(alarmTypeOptionsForMethod(5)).toContainEqual({ value: 2, label: "运动目标检测报警" });
    expect(alarmTypeOptionsForMethod(6)).toContainEqual({ value: 2, label: "存储设备风扇故障报警" });
    expect(alarmTypeOptionsForMethod(1)).toEqual([]);
    expect(alarmTypeOptionsForMethod(undefined)).toEqual([]);
  });
});

describe("alarm permissions", () => {
  it("keeps view and physical-delete permissions separate", () => {
    expect(mayViewAlarms(["*:*:*"])).toBe(true);
    expect(mayDeleteAlarms(["*:*:*"])).toBe(true);
    expect(mayViewAlarms(["gb28181:alarm:view"])).toBe(true);
    expect(mayDeleteAlarms(["gb28181:alarm:view"])).toBe(false);
    expect(mayDeleteAlarms(["gb28181:alarm:delete"])).toBe(true);
    expect(mayViewAlarms(["unrelated"])).toBe(false);
  });
});

describe("alarm query state", () => {
  it("trims optional filters and drops empty values", () => {
    expect(
      normalizeAlarmQuery({
        page: 1,
        pageSize: 20,
        sourceCode: " 3701 ",
        keyword: "   ",
        alarmFrom: "2026-08-04T10:00:00+08:00",
        alarmTo: "2026-08-04T11:00:00+08:00"
      })
    ).toEqual({
      page: 1,
      pageSize: 20,
      sourceCode: "3701",
      alarmFrom: "2026-08-04T10:00:00+08:00",
      alarmTo: "2026-08-04T11:00:00+08:00"
    });
  });

  it("rejects partial or reversed time ranges before the request", () => {
    expect(() => normalizeAlarmQuery({ page: 1, pageSize: 20, alarmFrom: "2026-08-04T10:00:00+08:00" })).toThrow();
    expect(() =>
      normalizeAlarmQuery({
        page: 1,
        pageSize: 20,
        alarmFrom: "2026-08-04T12:00:00+08:00",
        alarmTo: "2026-08-04T11:00:00+08:00"
      })
    ).toThrow();
  });
});

describe("alarm deletion state", () => {
  it("moves to the nearest valid page after deletion", () => {
    expect(pageAfterAlarmDeletion(3, 20, 41, 1)).toBe(2);
    expect(pageAfterAlarmDeletion(2, 20, 50, 1)).toBe(2);
    expect(pageAfterAlarmDeletion(1, 20, 1, 1)).toBe(1);
  });

});

describe("alarm display fallback", () => {
  it("prefers alias, name and code in that order", () => {
    expect(displayAlarmEntityName({ alias: "东门", name: "通道一", code: "C1" })).toBe("东门");
    expect(displayAlarmEntityName({ alias: "", name: "通道一", code: "C1" })).toBe("通道一");
    expect(displayAlarmEntityName({ alias: "", name: "", code: "C1" })).toBe("C1");
    expect(displayAlarmEntityName(null, "未知来源")).toBe("未知来源");
  });
});

describe("alarm priority colors", () => {
  it("maps the four alarm levels to distinct colors", () => {
    expect(alarmPriorityTagColor({ value: 1 })).toBe("red");
    expect(alarmPriorityTagColor({ value: 2 })).toBe("orange");
    expect(alarmPriorityTagColor({ value: 3 })).toBe("gold");
    expect(alarmPriorityTagColor({ value: 4 })).toBe("arcoblue");
  });

  it("falls back to gray for unknown or missing levels", () => {
    expect(alarmPriorityTagColor({ value: null })).toBe("gray");
    expect(alarmPriorityTagColor({ value: 99 })).toBe("gray");
    expect(alarmPriorityTagColor(null)).toBe("gray");
    expect(alarmPriorityTagColor(undefined)).toBe("gray");
  });
});
