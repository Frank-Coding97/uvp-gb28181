import { describe, expect, it } from "vitest";
import {
  DICT_CODE_MEDIA_NODE_STATE,
  MEDIA_NODE_DATA_STATUS_LABEL,
  MEDIA_NODE_STATE_LABEL_FALLBACK,
  MEDIA_NODE_STATE_OPTIONS_FALLBACK,
  MEDIA_NODE_UNKNOWN_TEXT,
  mediaNodeRuntimeKey,
  mediaNodeStateRuntimeText
} from "./mediaNodeState";

describe("media_node_state 字典（流媒体节点状态）", () => {
  it("code 与兜底值域锁死 —— active 统一为「在线」（此前四处各写 活跃/可用/在线）", () => {
    expect(DICT_CODE_MEDIA_NODE_STATE).toBe("media_node_state");
    expect(MEDIA_NODE_STATE_LABEL_FALLBACK).toEqual({
      active: "在线",
      maintenance: "维护中",
      offline: "离线"
    });
  });

  it("合成文案：生命周期状态优先于数据采集状态", () => {
    expect(mediaNodeStateRuntimeText(undefined, { state: "maintenance", status: "fresh" })).toBe("维护中");
    expect(mediaNodeStateRuntimeText(undefined, { state: "offline", status: "unavailable" })).toBe("离线");
  });

  it("合成文案：状态正常时看数据采集新鲜度", () => {
    expect(mediaNodeStateRuntimeText(undefined, { state: "active", status: "fresh" })).toBe("在线");
    expect(mediaNodeStateRuntimeText(undefined, { state: "active", status: "partial" })).toBe("部分数据");
    expect(mediaNodeStateRuntimeText(undefined, { state: "active", status: "unavailable" })).toBe("采集失败");
  });

  it("两个维度都认不出来时给「状态未知」，不是空串", () => {
    expect(mediaNodeStateRuntimeText(undefined, { state: "active" })).toBe(MEDIA_NODE_UNKNOWN_TEXT);
    expect(mediaNodeStateRuntimeText(undefined, null)).toBe(MEDIA_NODE_UNKNOWN_TEXT);
    expect(mediaNodeStateRuntimeText(undefined, {})).toBe(MEDIA_NODE_UNKNOWN_TEXT);
  });

  it("字典命中时以字典为准（现场把「维护中」改成别的说法，合成文案跟着变）", () => {
    expect(mediaNodeStateRuntimeText({ maintenance: "检修中" }, { state: "maintenance" })).toBe("检修中");
    expect(mediaNodeStateRuntimeText({ offline: "掉线" }, { state: "offline" })).toBe("掉线");
  });

  it("判定认「维度键」而不是中文串（拿文案反判逻辑会随字典改名静默失效）", () => {
    expect(mediaNodeRuntimeKey({ state: "maintenance", status: "fresh" })).toBe("maintenance");
    expect(mediaNodeRuntimeKey({ state: "offline", status: "unavailable" })).toBe("offline");
    expect(mediaNodeRuntimeKey({ state: "active", status: "unavailable" })).toBe("unavailable");
    expect(mediaNodeRuntimeKey({ state: "active", status: "partial" })).toBe("partial");
    expect(mediaNodeRuntimeKey({ state: "active", status: "fresh" })).toBe("fresh");
    expect(mediaNodeRuntimeKey({ state: "active" })).toBe("unknown");
    expect(mediaNodeRuntimeKey(null)).toBe("unknown");
  });

  it("筛选下拉兜底含「维护中」（此前只有 在线/离线，维护态筛不出来）", () => {
    expect(MEDIA_NODE_STATE_OPTIONS_FALLBACK.map(option => option.value)).toEqual(["active", "maintenance", "offline"]);
  });

  it("数据采集状态是**另一个维度**，不属于本字典值域", () => {
    // 传了字典也不该把 fresh/partial/unavailable 当成 state 去查
    expect(mediaNodeStateRuntimeText({ fresh: "不该出现" }, { state: "active", status: "fresh" })).toBe("在线");
    expect(MEDIA_NODE_DATA_STATUS_LABEL).toEqual({ fresh: "在线", partial: "部分数据", unavailable: "采集失败" });
  });
});
