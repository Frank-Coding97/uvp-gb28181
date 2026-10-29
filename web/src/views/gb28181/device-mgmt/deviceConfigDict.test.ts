import { describe, expect, it } from "vitest";
import {
  FRAME_MIRROR_LABEL_FALLBACK,
  STREAM_NUMBER_LABEL_FALLBACK,
  applyOptionLabels,
  streamNumberLabelFrom,
  whitelistedLabel
} from "./deviceConfigDict";
import { MIRROR_OPTIONS, STREAM_NUMBER_OPTIONS } from "./deviceConfigGroups";

describe("设备配置码值字典 · 兜底值域锁定", () => {
  it("frame_mirror：1=水平、2=上下（⛔ 写反会真把画面翻错，只能靠人眼发现）", () => {
    expect(FRAME_MIRROR_LABEL_FALLBACK).toEqual({
      "0": "不启用镜像",
      "1": "水平镜像（左右翻转）",
      "2": "上下镜像（上下翻转）",
      "3": "中心镜像（旋转 180°）"
    });
  });

  it("stream_number：0=主码流，1..3=子码流", () => {
    expect(STREAM_NUMBER_LABEL_FALLBACK).toEqual({
      "0": "主码流",
      "1": "子码流 1",
      "2": "子码流 2",
      "3": "子码流 3"
    });
  });

  it("兜底常量与代码选项表逐条同源（改一处必须改另一处）", () => {
    expect(MIRROR_OPTIONS.map(o => o.value)).toEqual(["0", "1", "2", "3"]);
    expect(MIRROR_OPTIONS.map(o => o.label)).toEqual([
      FRAME_MIRROR_LABEL_FALLBACK["0"],
      FRAME_MIRROR_LABEL_FALLBACK["1"],
      FRAME_MIRROR_LABEL_FALLBACK["2"],
      FRAME_MIRROR_LABEL_FALLBACK["3"]
    ]);
    expect(STREAM_NUMBER_OPTIONS.map(o => o.value)).toEqual(["0", "1", "2", "3"]);
    expect(STREAM_NUMBER_OPTIONS.map(o => o.label)).toEqual(["主码流", "子码流 1", "子码流 2", "子码流 3"]);
  });
});

describe("applyOptionLabels · 白名单式合并", () => {
  it("字典为空查表 ⇒ 原样返回（顺序 / 值 / shortLabel 一字不改）", () => {
    const merged = applyOptionLabels(MIRROR_OPTIONS, { ...FRAME_MIRROR_LABEL_FALLBACK });
    expect(merged).toEqual(MIRROR_OPTIONS);
  });

  it("字典改名生效，但 value 与 shortLabel 不动（字典只能改名）", () => {
    const merged = applyOptionLabels(MIRROR_OPTIONS, { ...FRAME_MIRROR_LABEL_FALLBACK, "1": "左右翻转" });
    expect(merged[1]).toEqual({ value: "1", label: "左右翻转", shortLabel: "左右" });
    // 分水岭：value 一个都不许变（这几个值会原样下发设备）
    expect(merged.map(o => o.value)).toEqual(["0", "1", "2", "3"]);
  });

  it("⛔ 字典里冒出代码没声明的 value ⇒ 丢弃（不许新增一档）", () => {
    const merged = applyOptionLabels(STREAM_NUMBER_OPTIONS, {
      ...STREAM_NUMBER_LABEL_FALLBACK,
      "9": "第九路码流"
    });
    expect(merged.map(o => o.value)).toEqual(["0", "1", "2", "3"]);
    expect(merged.some(o => o.label === "第九路码流")).toBe(false);
  });

  it("只给了部分 value ⇒ 其余从代码兜底补回（改名与补底同时成立）", () => {
    const merged = applyOptionLabels(STREAM_NUMBER_OPTIONS, { "0": "主码流（高清）" });
    expect(merged.map(o => o.label)).toEqual(["主码流（高清）", "子码流 1", "子码流 2", "子码流 3"]);
  });

  it("不修改入参（返回新数组 + 新对象）", () => {
    const before = JSON.stringify(MIRROR_OPTIONS);
    const merged = applyOptionLabels(MIRROR_OPTIONS, { "1": "改了" });
    expect(JSON.stringify(MIRROR_OPTIONS)).toBe(before);
    expect(merged).not.toBe(MIRROR_OPTIONS);
    expect(merged[1]).not.toBe(MIRROR_OPTIONS[1]);
  });
});

describe("whitelistedLabel / streamNumberLabelFrom · 任意编号取名", () => {
  it("白名单外（不在兜底值域）的 key 一律不认字典名", () => {
    const labels = { "0": "主码流", "5": "字典给的第五路" };
    expect(whitelistedLabel(labels, STREAM_NUMBER_LABEL_FALLBACK, "0")).toBe("主码流");
    expect(whitelistedLabel(labels, STREAM_NUMBER_LABEL_FALLBACK, "5")).toBeUndefined();
  });

  it("0–3 走字典（改名照常生效）", () => {
    expect(streamNumberLabelFrom({ ...STREAM_NUMBER_LABEL_FALLBACK, "1": "辅码流一" }, 1)).toBe("辅码流一");
    expect(streamNumberLabelFrom(STREAM_NUMBER_LABEL_FALLBACK, 0)).toBe("主码流");
  });

  it("超出 0–3 ⇒ 过程式兜底「子码流 N」（播放控制台按上报码流数渲染，编号可能 > 3）", () => {
    expect(streamNumberLabelFrom(STREAM_NUMBER_LABEL_FALLBACK, 4)).toBe("子码流 4");
    expect(streamNumberLabelFrom(STREAM_NUMBER_LABEL_FALLBACK, 7)).toBe("子码流 7");
    // ⛔ 字典想给 "5" 起名也没用：白名单只认兜底值域，5 仍走过程式
    expect(streamNumberLabelFrom({ "5": "字典给的第五路" }, "5")).toBe("子码流 5");
  });

  it("空值不下渲染占位（与 useDictLabel 同口径）", () => {
    expect(streamNumberLabelFrom(STREAM_NUMBER_LABEL_FALLBACK, "")).toBe("");
    expect(streamNumberLabelFrom(STREAM_NUMBER_LABEL_FALLBACK, null)).toBe("");
    expect(streamNumberLabelFrom(STREAM_NUMBER_LABEL_FALLBACK, undefined)).toBe("");
  });

  it("非数字怪值 ⇒ 原样回显（排障要看得见）", () => {
    expect(streamNumberLabelFrom(STREAM_NUMBER_LABEL_FALLBACK, "abc")).toBe("abc");
  });
});
