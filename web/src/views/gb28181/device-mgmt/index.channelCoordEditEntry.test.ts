import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 通道「位置」上的铅笔入口（源码级契约）。
 *
 * 为什么钉源码：这条链是**跨模板与脚本的接线**（位置单元格 → openLocationPickerForChannel
 * → 同一个踩点弹窗 → handleLocationConfirm 落库 → 列表重拉），挂载单测看不到。
 *
 * 要守住的三个不变量（不是"有几个按钮"，按钮增减不该让它红）：
 *   ① 只有一个踩点弹窗实例 —— 列表 / 卡片 / 编辑表单三条来路都喂同一份 locationPickerTarget；
 *   ② 无权限 fail-closed，且**判断在赋值之前**（先拦再写，不能先开弹窗再发现没权限）；
 *   ③ 落库后必须重拉通道列表 —— 坐标来源标签（人工）与「无坐标」文案都不是本地 patch 得到的。
 */
const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");

/** 截取一个函数体：从 `function NAME` 到同缩进的收尾 `\n}`。 */
function functionBody(name: string) {
  const start = source.indexOf(`function ${name}`);
  expect(start, `没找到函数 ${name}`).toBeGreaterThan(-1);
  const end = source.indexOf("\n}\n", start);
  expect(end, `函数 ${name} 的收尾没找到`).toBeGreaterThan(start);
  return source.slice(start, end);
}

describe("通道位置铅笔入口", () => {
  it("列表与卡片都接到同一个函数，且只有一份踩点弹窗", () => {
    expect(source).toContain('@click.stop="openLocationPickerForChannel(record)"');
    expect(source).toContain('@click.stop="openLocationPickerForChannel(item)"');
    // ⛔ 只允许一个 <ChannelLocationPicker：再来一份就会出现"两个弹窗各自持坐标"的经典分叉。
    expect(source.match(/<ChannelLocationPicker\b/g)?.length).toBe(1);
  });

  it("无权限先拦再开弹窗（fail-closed 的顺序也要对）", () => {
    const body = functionBody("openLocationPickerForChannel");
    expect(body).toContain("canEditChannel.value");
    expect(body.indexOf("canEditChannel.value")).toBeLessThan(body.indexOf("locationPickerVisible.value = true"));
  });

  it("踩点落库后重拉列表，而不是只改本地坐标字段", () => {
    const body = functionBody("handleLocationConfirm");
    const saveIndex = body.indexOf("Message.success(cleared");
    expect(saveIndex).toBeGreaterThan(-1);
    // 来源标签与「无坐标」文案都由后端算，本地 patch 必漏；必须在成功提示之后重拉。
    expect(body.indexOf("loadChannelsData(true)", saveIndex)).toBeGreaterThan(saveIndex);
  });
});
