import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock, ruleBlocks, squash } from "@/test/source-assert";

/**
 * 悬停提示（tooltip）统一皮肤 + 「不许退回原生 title」门禁。
 *
 * 两件事守护：
 *  1. **Arco tooltip 的配色**：底色原来是 `--color-tooltip-bg`（暗色 #373739 中性灰），
 *     与平台浮层 `--uvp-popconfirm-*` 不是一套。规则在 `style/model/uvp-tooltip.scss`。
 *  2. **原生 `title` 属性**：浏览器自己画的系统提示，颜色/延迟/字体全不受控，
 *     暗色页面下是刺眼的浅灰块，**任何 CSS 都改不动**（2026-10-18 老板点名设备树）。
 *     只能换成 `<a-tooltip>` —— 已经换过一次的地方不许再退回去。
 */

const root = resolve(process.cwd(), "src");
// ⛔ 先剥注释再解析：本文件的注释里会引用 `.arco-tooltip-popup-arrow` 这类选择器原文，
//    `ruleBlocks` 的 `[^{]*\{` 会从注释里那处开始、一路吃到注释里的 `{`，
//    于是取到错误的块（实测把 `border-top:none` 当成了面板规则）。
const scss = readFileSync(resolve(root, "style/model/uvp-tooltip.scss"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
const indexScss = readFileSync(resolve(root, "style/model/index.scss"), "utf8");

function templateOf(path: string): string {
  const source = readFileSync(resolve(root, path), "utf8");
  const start = source.indexOf("<template>");
  const end = source.lastIndexOf("</template>");
  return start < 0 || end < 0 ? "" : source.slice(start, end).replace(/<!--[\s\S]*?-->/g, "");
}

describe("arco tooltip skin", () => {
  it("被 model 入口引入（否则整份规则不参与打包）", () => {
    expect(squash(indexScss)).toContain('@use"@/style/model/uvp-tooltip";');
  });

  it("面板换成平台浮层 token：底色 / 边框 / 圆角 / 阴影 / 文字色", () => {
    expect(
      hasRuleBlock(
        scss,
        ".arco-tooltip-content",
        "background-color: var(--uvp-popconfirm-bg)",
        "border: 1px solid var(--uvp-popconfirm-border)",
        "border-radius: 8px",
        "box-shadow: var(--uvp-popconfirm-shadow)",
        "color: var(--uvp-text-primary)"
      )
    ).toBe(true);
  });

  it("箭头与面板同底色同边框（只改面板会在接缝处露出原始灰底）", () => {
    expect(
      hasRuleBlock(
        scss,
        ".arco-tooltip-popup-arrow",
        "background-color: var(--uvp-popconfirm-bg)",
        "border: 1px solid var(--uvp-popconfirm-border)"
      )
    ).toBe(true);
  });

  // ⛔ 箭头 border **不能**带 !important：Trigger 靠
  //    `[trigger-placement='right'] .arco-trigger-arrow { border-top:none; border-right:none }`
  //    裁掉朝向面板的两条边（该规则无 !important）。一旦我们也加 !important，
  //    四条边全长出来 ⇒ 箭头变成贴在面板边上的小方框。这条用例专门锁这个坑。
  it("箭头的 border 不带 !important（带了就会变成四边方框）", () => {
    const [block = ""] = ruleBlocks(scss, ".arco-tooltip-popup-arrow");
    expect(block).toContain("border: 1px solid var(--uvp-popconfirm-border)");
    expect(block).not.toMatch(/border:[^;]*!important/);
  });
});

describe("设备树提示走组件而不是原生 title", () => {
  // ⛔ 原生 title 的样式由浏览器决定，暗色下是浅灰块 —— 改 CSS 没用，只能换组件。
  //    这里盯住 DirectoryPanel：它是第一个被换掉的，别哪天又被写回去。
  const panel = "views/gb28181/device-mgmt/components/DirectoryPanel.vue";

  it("模板里不再有原生 :title（含节点计数那处）", () => {
    expect(templateOf(panel)).not.toContain(":title=");
  });

  it("设备行由 a-tooltip 承载 nodeTitle（而不是仅仅删掉提示）", () => {
    const template = squash(templateOf(panel));
    expect(template).toContain('<a-tooltipv-for="{node,level}invisibleRows"');
    expect(template).toContain(':content="nodeTitle(node)"');
  });
});
