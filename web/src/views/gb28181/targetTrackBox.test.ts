import { describe, expect, it } from "vitest";
import { TARGET_TRACK_MIN_BOX_RATIO, buildTargetTrackArea } from "./targetTrackBox";

/**
 * 目标跟踪框选换算（GB/T 28181-2022 A.2.3.1.14 TargetArea）。
 *
 * ⛔ 这组用例钉的是"发出去的六个数是什么"，不是"函数返回了个对象"：
 *    目标跟踪是**无应答命令**，落点算错在设备侧完全不可观测 ——
 *    平台看到的是 200/已下发，画面里什么也没跟踪上。所以每个数字都要有断言。
 */
describe("buildTargetTrackArea", () => {
  it("把框选换算成同一坐标系下的播放窗口像素坐标", () => {
    // 画面在页面上渲染成 960×540（不等于任何"视频原始分辨率"，这正是标准要的口径）。
    const result = buildTargetTrackArea({
      start: { x: 0.25, y: 0.25 },
      end: { x: 0.75, y: 0.75 },
      renderedWidth: 960,
      renderedHeight: 540
    });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    // length/width = **渲染**尺寸原样，不做比例换算（设备负责换算）。
    expect(result.area).toEqual({
      length: 960,
      width: 540,
      midPointX: 480,
      midPointY: 270,
      lengthX: 480,
      lengthY: 270
    });
  });

  it("反方向拖拽（右下 → 左上）与正向完全等价", () => {
    const forward = buildTargetTrackArea({
      start: { x: 0.1, y: 0.2 },
      end: { x: 0.5, y: 0.6 },
      renderedWidth: 1280,
      renderedHeight: 720
    });
    const backward = buildTargetTrackArea({
      start: { x: 0.5, y: 0.6 },
      end: { x: 0.1, y: 0.2 },
      renderedWidth: 1280,
      renderedHeight: 720
    });
    expect(backward).toEqual(forward);
  });

  it("框太小一律拒发，并说清是框的问题", () => {
    const result = buildTargetTrackArea({
      // 刚好小于阈值（0.02）:一次误触点一下就会落在这一段。
      start: { x: 0.5, y: 0.5 },
      end: { x: 0.5 + TARGET_TRACK_MIN_BOX_RATIO / 2, y: 0.9 },
      renderedWidth: 1280,
      renderedHeight: 720
    });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.reason).toContain("太小");
  });

  it("恰好等于阈值时放行（边界值不许比阈值更严）", () => {
    const result = buildTargetTrackArea({
      start: { x: 0.2, y: 0.2 },
      end: { x: 0.2 + TARGET_TRACK_MIN_BOX_RATIO, y: 0.2 + TARGET_TRACK_MIN_BOX_RATIO },
      renderedWidth: 1000,
      renderedHeight: 1000
    });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.area.lengthX).toBe(20);
    expect(result.area.lengthY).toBe(20);
  });

  it("渲染尺寸未知（0）时拒发，绝不退回 0 或某个「默认分辨率」", () => {
    // ⛔ 退回一个"看起来合理"的尺寸比报错更糟：设备会按它换算，框落在完全不相干的位置，
    //    而无应答命令让平台永远发现不了。
    for (const size of [
      { renderedWidth: 0, renderedHeight: 540 },
      { renderedWidth: 960, renderedHeight: 0 },
      { renderedWidth: Number.NaN, renderedHeight: 540 }
    ]) {
      const result = buildTargetTrackArea({ start: { x: 0.25, y: 0.25 }, end: { x: 0.75, y: 0.75 }, ...size });
      expect(result.ok).toBe(false);
      if (result.ok) continue;
      expect(result.reason).toContain("渲染尺寸");
    }
  });

  it("贴着画面边缘的框允许越界，但中心点与尺寸仍落在窗口内", () => {
    // ⛔ 与"拉框放大"刻意不同：那个框是平台自己算的，越界一定是平台错了；
    //    这里框是操作员拖的，拖出边界、或被 UI 裁掉半个框都是正常操作。
    const result = buildTargetTrackArea({
      start: { x: -0.4, y: 0.4 },
      end: { x: 0.1, y: 1.6 },
      renderedWidth: 1280,
      renderedHeight: 720
    });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    // 归一化后按 [0,1] 钳位：横向 [0, 0.1]、纵向 [0.4, 1]。
    expect(result.area.midPointX).toBe(64);
    expect(result.area.lengthX).toBe(128);
    expect(result.area.midPointY).toBe(504);
    expect(result.area.lengthY).toBe(432);
    expect(result.area.midPointX).toBeLessThanOrEqual(result.area.length);
    expect(result.area.midPointY).toBeLessThanOrEqual(result.area.width);
  });

  it("四项尺寸至少 1px —— 取整掉到 0 会被服务端当成非法值拒掉", () => {
    // 极扁的框：横向 0.02 的比例落在 320px 上 = 6.4 → 取整 6，仍 >0；
    // 这里用极小渲染尺寸把比例乘出来压到 0.x 这一段。
    const result = buildTargetTrackArea({
      start: { x: 0.5, y: 0.5 },
      end: { x: 0.5 + TARGET_TRACK_MIN_BOX_RATIO, y: 0.5 + TARGET_TRACK_MIN_BOX_RATIO },
      renderedWidth: 4,
      renderedHeight: 4
    });
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.area.lengthX).toBeGreaterThanOrEqual(1);
    expect(result.area.lengthY).toBeGreaterThanOrEqual(1);
    // 中心点也必须还在窗口里（宽 4px 时半宽 1px，取整后允许贴边到 4）。
    expect(result.area.midPointX).toBeLessThanOrEqual(4);
    expect(result.area.midPointY).toBeLessThanOrEqual(4);
  });
});
