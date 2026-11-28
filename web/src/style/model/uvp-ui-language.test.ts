import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock, ruleBlocks, squash } from "@/test/source-assert";

const source = readFileSync(resolve(process.cwd(), "src/style/model/uvp-ui-language.scss"), "utf8");

describe("input suffix text buttons", () => {
  const selector = "body .arco-input-wrapper .arco-input-suffix > .arco-btn.arco-btn-text";

  it("keeps embedded actions compact and free of dialog button surfaces", () => {
    expect(
      hasRuleBlock(
        source,
        selector,
        "width: 28px",
        "height: 28px",
        "min-width: 28px",
        "min-height: 0",
        "padding: 0",
        "background: transparent",
        "border: 0",
        "box-shadow: none",
        "transform: none"
      )
    ).toBe(true);
  });

  it("keeps hover feedback inside the input without moving the action", () => {
    expect(
      hasRuleBlock(
        source,
        `${selector}:hover:not(.arco-btn-disabled, [disabled])`,
        "background: var(--color-fill-2)",
        "box-shadow: none",
        "transform: none"
      )
    ).toBe(true);
  });
});

/** 取出「选择器含 `.uvp-system-drawer` 但没往下打到面板」的规则块 —— 即落在 Arco 全屏容器上的那批。 */
function containerScopedDrawerRules(): { selector: string; body: string }[] {
  return [...source.matchAll(/([^{}]+)\{([^{}]*)\}/gs)]
    .map(([, selector, body]) => ({ selector: squash(selector), body: squash(body) }))
    .filter(rule => rule.selector.includes(".uvp-system-drawer") && !rule.selector.includes(".arco-drawer"));
}

describe("table action spacing", () => {
  it("keeps one compact 4px gap between operation icons and labels", () => {
    expect(source).toMatch(/\.uvp-data-table \.uvp-table-action,\s*\.uvp-data-table \.arco-link\s*\{[^}]*gap:\s*4px;/s);
    expect(source).toMatch(/\.uvp-data-table \.arco-link-icon\s*\{[^}]*margin-right:\s*0;/s);
    expect(source).toMatch(
      /\.uvp-data-table \.arco-btn:not\(\.arco-btn-only-icon\) \.arco-btn-icon\s*\{[^}]*margin-right:\s*4px;/s
    );
  });

  it("centers slotted SVG icons against action labels without baseline drift", () => {
    expect(source).toMatch(
      /\.uvp-data-table \.arco-link-icon\s*\{[^}]*display:\s*inline-flex;[^}]*align-items:\s*center;[^}]*justify-content:\s*center;[^}]*line-height:\s*1;/s
    );
    expect(source).toMatch(/\.uvp-data-table \.arco-link-icon\s*>\s*svg\s*\{[^}]*display:\s*block;/s);
  });
});

describe("media workbench theme tokens", () => {
  it("maps ZLM surfaces to the active UVP body theme for legacy and canonical routes", () => {
    expect(source).toMatch(/body,\s*\.gb28181-page,[^{]*\{[^}]*--zlm-card:\s*var\(--uvp-panel-bg\)/s);
    expect(source).toMatch(/body\[arco-theme="dark"\],\s*body\[arco-theme="dark"\]\s+\.gb28181-page,[^{]*\{/s);
  });
});

describe("collapsed sidebar selection", () => {
  it("keeps the menu item square and uses only one selected surface", () => {
    expect(hasRuleBlock(source, ".aside .arco-menu.arco-menu-collapsed > .arco-menu-inner", "padding: 2px 4px 20px")).toBe(true);
    expect(
      hasRuleBlock(
        source,
        ".aside .arco-menu.arco-menu-collapsed > .arco-menu-inner > .arco-menu-item",
        "width: 40px",
        "height: 40px",
        "min-height: 40px",
        "padding: 0",
        "margin: 0 auto 4px"
      )
    ).toBe(true);
    expect(
      hasRuleBlock(
        source,
        ".aside .arco-menu.arco-menu-collapsed > .arco-menu-inner > .arco-menu-item.arco-menu-selected .arco-menu-icon",
        "background: transparent"
      )
    ).toBe(true);
    expect(
      hasRuleBlock(
        source.replace(/\s+/g, " "),
        'body[arco-theme="dark"] .aside .arco-menu.arco-menu-collapsed > .arco-menu-inner > .arco-menu-item.arco-menu-selected .arco-menu-icon',
        "background: transparent"
      )
    ).toBe(true);
  });
});

describe("table fixed-right column alignment", () => {
  it("never lets the native table-body scrollbar take layout width", () => {
    // 表头与表体是两个独立滚动的容器,`fixed="right"` 的列在两侧各自贴**自己**滚动区的右边缘;
    // 表体滚动区一旦被**原生**滚动条吃掉 N 像素宽,表体里的固定列就会比表头整体左移 N 像素
    // (实机复现:滚动区窄 15px ⇒ 列左移 15px),表现就是「表头『操作』与行内按钮错位」。
    // Arco 自带 overlay 滑块,所以原生滚动条必须彻底不占位(曾经写成 width: 8px)。
    expect(hasRuleBlock(source, ".uvp-data-table .arco-table-body::-webkit-scrollbar", "display: none;", "width: 0;")).toBe(true);
    expect(hasRuleBlock(source, ".uvp-data-table .arco-table-body", "scrollbar-width: none;")).toBe(true);

    for (const block of ruleBlocks(source, ".uvp-data-table .arco-table-body::-webkit-scrollbar")) {
      expect(block).not.toMatch(/width:\s*[1-9]/);
    }
  });
});

describe("table loading mask theme", () => {
  it("uses a dark translucent surface instead of the light loading mask in dark mode", () => {
    expect(source).toMatch(
      /body\[arco-theme="dark"\]\s+\.uvp-data-table\s+\.arco-spin-mask\s*\{[^}]*background:\s*rgb\(16 25 35\s*\/\s*78%\)/s
    );
  });
});

describe("system drawer surface scoping", () => {
  // `class="uvp-system-drawer"` 落在 Arco 的 `.arco-drawer-container`（fixed 全屏、z-index 1001）上，
  // 不是抽屉面板；面板是内层 `.arco-drawer`。表面样式一旦写在容器上，整屏会被不透明底盖住，
  // 打开抽屉时背后页面「整片消失」（2026-09-23 实测：采样像素 rgb(119,122,127) = 白底 + 60% 遮罩）。
  it("paints the drawer surface on the inner panel, not on Arco's full-screen container", () => {
    expect(hasRuleBlock(source, ".uvp-system-drawer .arco-drawer", "background: var(--uvp-dialog-bg)")).toBe(true);
    expect(hasRuleBlock(source, ".uvp-system-drawer .arco-drawer", "border-radius: 16px 0 0 16px")).toBe(true);
  });

  it("keeps the container-scoped drawer rules free of any opaque surface", () => {
    const rules = containerScopedDrawerRules();
    // 容器上只允许留「自定义属性」这类规则（如 --uvp-dialog-control-bg）
    for (const rule of rules) {
      for (const banned of ["background", "box-shadow", "border-radius", "border:"]) {
        expect(`${rule.selector} => ${rule.body}`).not.toContain(banned);
      }
    }
    expect(rules.length).toBeGreaterThan(0); // 保住「自定义属性仍挂在容器上」这条事实
  });
});

describe("dialog selection strip theme", () => {
  // 2026-11-04 实机取证：暗色下整条「已选择 N 个 API」是白的（采样 rgb(249,251,255)）。
  // 根因是 `background: #f8fbff` 写死 —— 亮色下看不出问题，暗色下等于在深色抽屉里贴一张白纸；
  // 而且条内次级按钮/tag 的暗色值是**半透明蓝**，压在白色父底上会合成成浅蓝，看着像没切主题。
  it("paints the strip with a theme token instead of a literal light surface", () => {
    expect(hasRuleBlock(source, ".uvp-dialog-selection", "background: var(--uvp-list-toolbar-bg)")).toBe(true);
  });

  it("keeps literal colors out of that rule block", () => {
    const squashed = squash(source);
    const start = squashed.indexOf(".uvp-dialog-selection{");
    expect(start).toBeGreaterThan(-1);
    const block = squashed.slice(start, squashed.indexOf("}", start));
    expect(block).not.toMatch(/background:#/);
  });
});

describe("play console button scope", () => {
  // 2026-11-04 起按钮的几何与配色**不在本文件**：统一真源是 src/styles/uvp-buttons.scss。
  // 原来这里断言的是「系统弹窗那套 34px / 白底 / 渐变」的 4 条规则存在，
  // 目的是让播放控制台逃开它；现在全局只有一套（控制台也吃同一套几何），
  // 所以断言改成：① 统一真源确实给了几何与语义色；② 系统弹窗的**专属表面**仍不得落到控制台。
  it("takes button geometry from the single source instead of dialog-scoped copies", () => {
    const buttons = readFileSync(resolve(process.cwd(), "src/styles/uvp-buttons.scss"), "utf8");
    expect(hasRuleBlock(squash(buttons), ".arco-btn", "height:var(--uvp-btn-h-md)")).toBe(true);
    expect(hasRuleBlock(squash(buttons), ".arco-btn", "border-radius:var(--uvp-btn-radius)")).toBe(true);
    expect(hasRuleBlock(squash(buttons), ".arco-btn-primary", "background:var(--uvp-btn-primary-bg)")).toBe(true);
    expect(hasRuleBlock(squash(buttons), ".arco-btn-secondary", "background:var(--uvp-btn-secondary-bg)")).toBe(true);
  });

  it("keeps dialog-only surfaces off the console modal", () => {
    // 控制台是深色底，系统弹窗的浅色表面对它无效且会糊；这条保护意图不变。
    expect(source).not.toMatch(/\.uvp-system-dialog \.arco-btn(?:[,\s]|-)/);
  });
});
