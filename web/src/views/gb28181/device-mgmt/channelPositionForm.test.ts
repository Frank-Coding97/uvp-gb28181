import { describe, expect, it } from "vitest";

import { coordPatchAfterSave, positionSourceText, resolveChannelCoordPayload } from "./channelPositionForm";

/**
 * 通道「人工录入坐标」入参归一。
 *
 * 这三个分支在界面上长得一样（都是两个输入框），只能靠用例把契约钉住：
 *   留空 = 不动坐标 / 成对数字 = 设置 / 成对 0 = 清除。
 * 契约错一格的后果是静默的 —— 要么坐标被莫名其妙清掉，要么用户填了没生效。
 */
describe("resolveChannelCoordPayload", () => {
  it("两个框都留空 = 本次不改坐标，请求体里不带这两个字段", () => {
    expect(resolveChannelCoordPayload("", "")).toEqual({ payload: {} });
    // 全是空白也算留空（a-input 清空后可能残留空格）
    expect(resolveChannelCoordPayload("   ", "  ")).toEqual({ payload: {} });
  });

  it("成对数字 = 设置坐标", () => {
    expect(resolveChannelCoordPayload("116.99505", "36.66237")).toEqual({
      payload: { longitude: 116.99505, latitude: 36.66237 }
    });
    // 负值（西经 / 南纬）与省略整数位的写法都要能过
    expect(resolveChannelCoordPayload("-122.4194", ".5")).toEqual({
      payload: { longitude: -122.4194, latitude: 0.5 }
    });
    // 前后空格不该影响结果
    expect(resolveChannelCoordPayload(" 116.99505 ", " 36.66237 ")).toEqual({
      payload: { longitude: 116.99505, latitude: 36.66237 }
    });
  });

  it("成对 0 = 清除坐标（0 在本仓全链路表示无坐标）", () => {
    expect(resolveChannelCoordPayload("0", "0")).toEqual({ payload: { longitude: 0, latitude: 0 } });
    expect(resolveChannelCoordPayload("0.0", "0.000")).toEqual({ payload: { longitude: 0, latitude: 0 } });
  });

  it("只填一个 = 非法，前端先拦（后端也会 400）", () => {
    for (const [lng, lat] of [
      ["116.99505", ""],
      ["", "36.66237"]
    ]) {
      const r = resolveChannelCoordPayload(lng, lat);
      expect("error" in r && r.error).toContain("成对填写");
    }
  });

  it("恰好一个为 0 = 非法，避免库里出现新经度配旧纬度的半对状态", () => {
    // 这是最容易写错的一格：单独看 "0" 是合法数字，但它跟另一端配不成坐标。
    // 中国境内不存在经纬度任一分量为 0 的合法点位，所以 0 只能作为"清除"信号成对出现。
    expect(resolveChannelCoordPayload("0", "36.66237")).toEqual({
      error: "经度与纬度都不能为 0（0 表示未设置坐标；如需清除请将两者都填 0）"
    });
    expect(resolveChannelCoordPayload("116.99505", "0")).toEqual({
      error: "经度与纬度都不能为 0（0 表示未设置坐标；如需清除请将两者都填 0）"
    });
  });

  it("非十进制数字被拒（不用裸 Number，`0x10` / `1e5` 也挡掉）", () => {
    for (const bad of ["abc", "0x10", "1e5", "Infinity", "NaN", "116.99a", "1,5"]) {
      const r = resolveChannelCoordPayload(bad, "36.66237");
      expect("error" in r && r.error).toBeTruthy();
    }
  });

  it("越界坐标被拒，边界值本身放行", () => {
    expect("error" in resolveChannelCoordPayload("180.0001", "36")).toBe(true);
    expect("error" in resolveChannelCoordPayload("-180.0001", "36")).toBe(true);
    expect("error" in resolveChannelCoordPayload("116", "90.0001")).toBe(true);
    expect("error" in resolveChannelCoordPayload("116", "-90.0001")).toBe(true);
    expect(resolveChannelCoordPayload("180", "90")).toEqual({ payload: { longitude: 180, latitude: 90 } });
    expect(resolveChannelCoordPayload("-180", "-90")).toEqual({ payload: { longitude: -180, latitude: -90 } });
  });
});

describe("coordPatchAfterSave", () => {
  it("没碰坐标时返回 null —— 不要把未修改误报成已更新", () => {
    expect(coordPatchAfterSave({})).toBeNull();
    // 只有一个分量也算没碰（这种入参本来就会被上一步拦掉，这里只是兜底）
    expect(coordPatchAfterSave({ longitude: 116.99505 })).toBeNull();
  });

  it("设置坐标后来源标成人工，并带更新时间", () => {
    const patch = coordPatchAfterSave({ longitude: 116.99505, latitude: 36.66237 });
    expect(patch).not.toBeNull();
    expect(patch!.longitude).toBe(116.99505);
    expect(patch!.latitude).toBe(36.66237);
    expect(patch!.positionSource).toBe("manual");
    expect(Number.isNaN(Date.parse(patch!.positionUpdatedAt as string))).toBe(false);
  });

  it("清除坐标后来源清空、更新时间置 null —— 与后端写的 NULL 对齐", () => {
    // ⛔ 这里不能填 new Date()：库里 position_updated_at 是 NULL，
    //   界面若显示"刚刚更新"就是凭空多出来的状态。
    const patch = coordPatchAfterSave({ longitude: 0, latitude: 0 });
    expect(patch).toEqual({ longitude: 0, latitude: 0, positionSource: "", positionUpdatedAt: null });
  });
});

describe("positionSourceText", () => {
  it("三路来源各有展示名，空值与未知值不展示", () => {
    expect(positionSourceText({ positionSource: "catalog" })).toBe("目录");
    expect(positionSourceText({ positionSource: "mobile" })).toBe("实时");
    expect(positionSourceText({ positionSource: "manual" })).toBe("人工");
    expect(positionSourceText({ positionSource: "" })).toBe("");
    expect(positionSourceText({ positionSource: null })).toBe("");
    expect(positionSourceText({})).toBe("");
    // 未知值（如后端将来加了新来源但前端没同步）宁可什么都不显示，也不要显示原始英文串
    expect(positionSourceText({ positionSource: "satellite" })).toBe("");
  });
});
