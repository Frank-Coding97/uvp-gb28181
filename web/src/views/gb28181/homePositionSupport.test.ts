import { describe, expect, it } from "vitest";
import { HOME_POSITION_SUPPORT_LABEL_FALLBACK, homePositionSupportLabel } from "./homePositionSupport";

describe("homePositionSupportLabel", () => {
  it("兜底表按后端值域写死 —— 只有支持 / 不支持 / 尚未确认三档", () => {
    expect(Object.keys(HOME_POSITION_SUPPORT_LABEL_FALLBACK).sort()).toEqual(["supported", "unknown", "unsupported"]);
    expect(HOME_POSITION_SUPPORT_LABEL_FALLBACK).toEqual({
      supported: "支持",
      unsupported: "不支持",
      unknown: "尚未确认"
    });
  });

  it("三态各自取名（不传表时用兜底）", () => {
    expect(homePositionSupportLabel("supported")).toBe("支持");
    expect(homePositionSupportLabel("unsupported")).toBe("不支持");
    expect(homePositionSupportLabel("unknown")).toBe("尚未确认");
  });

  it("字典改名覆盖兜底", () => {
    const labels = { ...HOME_POSITION_SUPPORT_LABEL_FALLBACK, unsupported: "设备不支持" };

    expect(homePositionSupportLabel("unsupported", labels)).toBe("设备不支持");
    // 没改的档位仍走兜底
    expect(homePositionSupportLabel("supported", labels)).toBe("支持");
  });

  it("⛔ 空值按「尚未确认」处理 —— 诊断提示里不许出现「控制能力：」这种半句", () => {
    expect(homePositionSupportLabel(null)).toBe("尚未确认");
    expect(homePositionSupportLabel(undefined)).toBe("尚未确认");
    expect(homePositionSupportLabel("")).toBe("尚未确认");
    expect(homePositionSupportLabel("   ")).toBe("尚未确认");
  });

  it("码值大小写/空格容错", () => {
    expect(homePositionSupportLabel(" unknown ")).toBe("尚未确认");
  });

  it("后端加了第四态时原样回显 —— 排障要看得见它到底叫什么", () => {
    expect(homePositionSupportLabel("partial")).toBe("partial");
  });
});
