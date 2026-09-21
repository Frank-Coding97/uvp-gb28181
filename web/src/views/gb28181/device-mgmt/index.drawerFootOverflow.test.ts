import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 设备详情抽屉**不许长出横向滚动条**的样式契约（2026-09-20）。
 *
 * ⛔ 机制（别只当成排版偏好）：抽屉里的内容挂在
 *    `.arco-drawer-body`(overflow: auto) > `<a-spin>`(`.arco-spin` 是 `display: inline-block`)，
 *    inline-block 的宽度按**收缩到适应**算，下限取内容的 **min-content**。
 *    一整排 `flex-wrap: nowrap` 的按钮，min-content 就等于"所有按钮宽度之和"：
 *    设备详情最多 6 个按钮（固件升级 / 维护记录 / 重启设备 / 管理订阅 / 刷新目录 / 复制编码，
 *    每个还带 14px 图标）实测需要约 700px，而 `:width="640"` 的抽屉扣掉内边距只剩 608px
 *    （再遇到占宽的竖向滚动条只剩 593px）⇒ 整个 `.arco-spin` 被撑到 700px，
 *    `.arco-drawer-body` 就出现横向滚动条。
 *
 * 修法 = 让页脚**允许换行**（`flex-wrap: wrap`），页脚的 min-content 从"按钮总和"降成
 * "最宽的那个按钮"。无头 Chrome 实测（640 宽抽屉）：加 wrap 前 6 按钮 700px → 横溢 76px；
 * 加 wrap 后回落到一行放不下就换行，横向溢出归零；5 按钮（582px）本来也放得下。
 *
 * ⛔ 因此：**谁把 `flex-wrap: wrap` 从 `.drawer-foot` 拿掉，或加回 `nowrap`，这条测试就会红。**
 *    真需要横向滚动条以外的解法时，请先改抽屉宽度或按钮数量，别退回不换行。
 */
const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");

/** 取出一条 CSS 规则的正体（从 `<选择器> {` 到配对的 `}`）。 */
function ruleBody(selector: string): string {
  const start = source.indexOf(`${selector} {`);
  expect(start, `没找到样式规则 ${selector}`).toBeGreaterThan(-1);
  const end = source.indexOf("}", start);
  return source.slice(start, end);
}

describe("device detail drawer footer must not force a horizontal scrollbar", () => {
  it("抽屉页脚允许换行（横向滚动条的唯一防线）", () => {
    const rule = ruleBody(".drawer-foot");
    expect(rule).toContain("display: flex");
    expect(rule).toContain("flex-wrap: wrap");
    expect(rule).not.toContain("nowrap");
  });

  it("按钮本身不被压缩，靠换行而不是挤压来收场", () => {
    // `flex: 0 0 auto` = 不缩不胀；配合上面的 wrap，超宽时按钮整体掉到下一行，
    // 而不是把文字挤到重叠（按钮里是 nowrap 文本，压缩只会更难读）。
    expect(source).toContain(".drawer-foot .arco-btn { flex: 0 0 auto; }");
    // 主按钮仍然独占剩余空间（保持"主操作"的视觉权重）。
    expect(source).toContain(".drawer-foot .arco-btn-primary { flex: 1; }");
  });

  it("留下机制说明，避免后人当成冗余样式删掉", () => {
    const start = source.indexOf("防横向滚动条");
    expect(start).toBeGreaterThan(-1);
    expect(start).toBeLessThan(source.indexOf(".drawer-foot {"));
  });
});
