import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { Progress } from "@arco-design/web-vue";

/**
 * 契约：Arco `a-progress` 的 `percent` 是 **0~1 的比值**，不是百分数。
 *
 * 这不是风格偏好，是框架实现 —— `@arco-design/web-vue/es/progress/line.js`：
 *     barStyle.width = `${percent * 100}%`
 *     text           = `${percent * 100}%`
 * 所以把 0~100 的百分数喂进去会渲染成 `width: 1500%`：被容器裁掉后就是
 * **"进度条永远顶满"**（组件自带文字还会显示 "1500%"）。
 *
 * 2026-10-04 的真实翻车：录像缓存弹窗进度 7% 但蓝条顶满 —— 因为
 * `:percent="progressPercent"`（0~100）被喂进了这个 0~1 的口子，而旁边那行
 * 我们自己渲染的 "7%" 又是对的，于是"7% 却满格"自相矛盾。
 * 同源问题当时还有两处，一并修掉：
 *   - `views/gb28181/record-cache/index.vue`（列表进度列）
 *   - `layout/components/Header/components/RecordingDownloadCenter.vue`（下载任务）
 * 口径统一为：**比值喂组件、百分数只用于文字**。
 *
 * ⛔ 本仓曾把这条写反（`record-cache/index.test.ts` 里一句"Arco 的 a-progress
 *    收 0~100"），并据此写了断言。那句是错的，已改正。别照它再翻回去 ——
 *    判单位一律以这里的实测宽度为准。
 */
describe("arco progress percent 单位契约", () => {
  const barWidth = (percent: number) =>
    mount(Progress, { props: { percent, showText: false } })
      .get(".arco-progress-line-bar")
      .attributes("style");

  it("percent 是比值：0.15 ⇒ width: 15%", () => {
    expect(barWidth(0.15)).toContain("width: 15%");
  });

  it("percent 不是百分数：15 ⇒ width: 1500%（被裁掉 = 视觉顶满）", () => {
    expect(barWidth(15)).toContain("width: 1500%");
  });

  it("完成态传 1，不是 100", () => {
    expect(barWidth(1)).toContain("width: 100%");
    expect(barWidth(100)).toContain("width: 10000%");
  });
});
