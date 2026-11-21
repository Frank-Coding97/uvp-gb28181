import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock, squash } from "@/test/source-assert";

/**
 * 全仓守卫：**所有 VChart 图表悬浮框（tooltip）共用一套皮肤**。
 *
 * 为什么值得一条门禁：
 *  · tooltip 的底色不是我们的 token，而是 `@visactor/vchart-arco-theme` 从 Arco
 *    `popupBackgroundColor` 取的中性灰 —— 平台卡片是 `--uvp-panel-bg` 深蓝/纯白，
 *    于是"灰盒子压在深蓝卡片上"（2026-10-18 首页「媒体速率」曲线被点名）。
 *  · 修法是把面板样式写成 `!important` 覆盖（VChart 把样式写进 inline style，
 *    每次 hover 重设一遍，没有 `!important` 就**静默失效**、不报任何错）。
 *  · 覆盖只靠类名/属性选择器命中，**选择器一旦写错就没人发现**（页面照样能跑，
 *    只是那块灰又回来了）⇒ 必须有一条用例盯着类名与库里的真实类名一致。
 */

const root = resolve(process.cwd(), "src");
const scss = readFileSync(resolve(root, "style/model/uvp-chart-tooltip.scss"), "utf8");
const indexScss = readFileSync(resolve(root, "style/model/index.scss"), "utf8");

const PANEL = "div[vchart-tooltip-id]";

describe("chart tooltip skin", () => {
  it("被 model 入口引入（否则整份规则不参与打包 = 毫无作用）", () => {
    expect(squash(indexScss)).toContain('@use"@/style/model/uvp-chart-tooltip";');
  });

  it("面板本体换掉 VChart 的中性灰：底色 / 边框 / 圆角 / 阴影全套", () => {
    expect(
      hasRuleBlock(
        scss,
        PANEL,
        "background-color: var(--uvp-popconfirm-bg) !important",
        "border: 1px solid var(--uvp-popconfirm-border) !important",
        "border-radius: 8px !important",
        "box-shadow: var(--uvp-popconfirm-shadow) !important"
      )
    ).toBe(true);
  });

  it("标题与正文分级：标题 13px 半粗主色，指标名次要色，数值半粗 + 等宽数字", () => {
    expect(hasRuleBlock(scss, `${PANEL} .vchart-tooltip-title`, "font-weight: 600 !important")).toBe(true);
    expect(hasRuleBlock(scss, `${PANEL} .vchart-tooltip-key`, "color: var(--uvp-text-secondary) !important")).toBe(true);
    expect(hasRuleBlock(scss, `${PANEL} .vchart-tooltip-value`, "font-variant-numeric: tabular-nums", "font-weight: 600")).toBe(
      true
    );
  });

  it("每条覆盖都带 !important（缺了压不住 inline style，等于没写）", () => {
    // 面板块里除了纯布局声明，颜色/边框/圆角/阴影都必须 !important
    const panelBlock = scss.slice(scss.indexOf(`${PANEL} {`), scss.indexOf(`${PANEL} .vchart-tooltip-title`));
    ["padding", "background-color", "border:", "border-radius", "box-shadow"].forEach(declaration => {
      expect(panelBlock).toMatch(new RegExp(`${declaration}[^;]*!important`));
    });
  });

  it("选择器锚的是库里的真实类名（写错了不会报错，只会静默失效）", () => {
    // 类名来自 @visactor/vchart 的 plugin/components/tooltip-handler/constants.js：
    //   TOOLTIP_PREFIX = "vchart-tooltip"、TOOLTIP_TITLE_CLASS_NAME = `${PREFIX}-title`
    // 行/列类名由 dom-tooltip-handler.js 用 `${TOOLTIP_PREFIX}-${colName}` 拼出，
    // colName ∈ {shape, key, value}，与下面三个选择器一一对应。
    const constants = readFileSync(
      resolve(process.cwd(), "node_modules/@visactor/vchart/esm/plugin/components/tooltip-handler/constants.js"),
      "utf8"
    );
    const prefix = /TOOLTIP_PREFIX\s*=\s*"([^"]+)"/.exec(constants)?.[1];
    expect(prefix).toBeTruthy();
    expect(constants).toContain("TOOLTIP_TITLE_CLASS_NAME = `${TOOLTIP_PREFIX}-title`");
    ["title", "key", "value"].forEach(name => {
      expect(scss).toContain(`.${prefix}-${name}`);
    });
    // 根面板只认属性名 `vchart-tooltip-id`（属性值随实例变化，写成值选择器会全灭）
    expect(scss).toContain(`[vchart-tooltip-id]`);
  });
});
